//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/utils"
	"go.uber.org/zap"
)

type feishuReferencedMessage struct {
	MessageID   string
	RootID      string
	ParentID    string
	ThreadID    string
	MessageType string
	Content     string
	Media       []string
	SenderID    string
}

// FeishuChannel 飞书渠道 — SDK WebSocket 实现
type FeishuChannel struct {
	BaseChannel
	AppID             string
	AppSecret         string
	EncryptKey        string
	VerificationToken string

	client   *lark.Client
	wsClient *ws.Client
	mu       sync.Mutex
	cancel   context.CancelFunc

	dedup          *utils.LRUCache           // 消息去重
	TranscribeFunc func(path string) string // 音频转写回调

	// Config-driven behavior
	groupPolicy    string // "open" or "mention" (default)
	reactEmoji     string // emoji type for reaction (e.g. "THUMBSUP")
	replyToMessage bool   // reply to the original message
}

func init() {
	RegisterFactory("feishu", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &FeishuChannel{
			BaseChannel: BaseChannel{
				ChannelName: "feishu",
				Bus:         msgBus,
				Logger:      logger,
			},
			dedup: utils.NewLRUCache(1000),
		}, nil
	})
}

func (f *FeishuChannel) Name() string { return "feishu" }

// Configure 配置飞书渠道
func (f *FeishuChannel) Configure(appID, appSecret, encryptKey, verificationToken string, allowFrom []string) {
	f.AppID = appID
	f.AppSecret = appSecret
	f.EncryptKey = encryptKey
	f.VerificationToken = verificationToken
	f.AllowFrom = allowFrom
	f.client = lark.NewClient(appID, appSecret)
}

// ConfigureExtended applies extended configuration options.
func (f *FeishuChannel) ConfigureExtended(groupPolicy, reactEmoji string, replyToMessage bool) {
	f.groupPolicy = groupPolicy
	if f.groupPolicy == "" {
		f.groupPolicy = "mention"
	}
	f.reactEmoji = reactEmoji
	if f.reactEmoji == "" {
		f.reactEmoji = "THINKING"
	}
	f.replyToMessage = replyToMessage
}

// Start 启动飞书渠道 (WebSocket 长连接)
func (f *FeishuChannel) Start(ctx context.Context) error {
	if f.AppID == "" || f.AppSecret == "" {
		return fmt.Errorf("feishu app_id and app_secret not configured")
	}

	if f.client == nil {
		f.client = lark.NewClient(f.AppID, f.AppSecret)
	}

	f.Running = true
	f.Logger.Info("飞书渠道启动 (WebSocket 模式)")

	// 创建事件分发器
	eventDispatcher := dispatcher.NewEventDispatcher(f.VerificationToken, f.EncryptKey).
		OnP2MessageReceiveV1(f.handleMessageReceive).
		OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
			// 忽略已读事件
			return nil
		}).
		OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
			// 忽略 reaction 创建事件（消除 SDK 报错噪音）
			return nil
		}).
		OnP2ChatAccessEventBotP2pChatEnteredV1(func(ctx context.Context, event *larkim.P2ChatAccessEventBotP2pChatEnteredV1) error {
			// 忽略 bot 进入私聊事件
			return nil
		}).
		OnP1P2PChatCreatedV1(func(ctx context.Context, event *larkim.P1P2PChatCreatedV1) error {
			// 忽略 P2P 聊天创建事件
			return nil
		})

	// 创建 WebSocket 客户端
	f.wsClient = ws.NewClient(f.AppID, f.AppSecret,
		ws.WithEventHandler(eventDispatcher),
	)

	ctx, f.cancel = context.WithCancel(ctx)

	// Start 是阻塞调用, 返回后由 manager wg.Wait 管理
	return f.wsClient.Start(ctx)
}

// Stop 停止飞书渠道
func (f *FeishuChannel) Stop() error {
	f.Running = false
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}

// handleMessageReceive 处理收到的消息事件
func (f *FeishuChannel) handleMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	f.Logger.Info("飞书收到消息事件")

	if event == nil || event.Event == nil || event.Event.Message == nil {
		f.Logger.Warn("飞书消息事件为空")
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	// 消息去重：同一 messageID 只处理一次
	messageID := ptrValue(msg.MessageId)
	if messageID != "" && f.dedup != nil {
		if f.dedup.Contains(messageID) {
			f.Logger.Debug("飞书消息去重，跳过", zap.String("message_id", messageID))
			return nil
		}
		f.dedup.Add(messageID)
	}

	// Bot 消息过滤
	if sender != nil && sender.SenderType != nil && *sender.SenderType == "bot" {
		f.Logger.Debug("飞书 Bot 消息，跳过")
		return nil
	}

	// 提取 chatID
	chatID := ptrValue(msg.ChatId)
	if chatID == "" {
		f.Logger.Warn("飞书消息 chatID 为空")
		return nil
	}

	chatType := ptrValue(msg.ChatType)

	// 群聊 mention 检查：group_policy == "open" 跳过
	rawContent := ptrValue(msg.Content)
	if chatType == "group" && f.groupPolicy != "open" && !hasBotMention(rawContent, msg.Mentions) {
		return nil
	}

	// 提取 senderID: 优先 userId > openId
	senderID := extractSenderID(sender)

	// Add reaction emoji asynchronously
	if f.reactEmoji != "" && messageID != "" {
		go f.addReaction(ctx, messageID, f.reactEmoji)
	}

	// 提取消息内容和媒体
	messageType := ptrValue(msg.MessageType)
	content, media := extractMessageContent(ptrValue(msg.Content), messageType)

	// 清理 @mention 占位符 (如 "@_user_1 你好" → "你好")
	if messageType == "text" && content != "" {
		content = mentionRE.ReplaceAllString(content, "")
		content = strings.TrimSpace(content)
	}

	f.Logger.Info("飞书消息解析完成",
		zap.String("sender", senderID),
		zap.String("chat_id", chatID),
		zap.String("type", messageType),
		zap.String("chat_type", chatType),
		zap.String("content", content),
	)

	// 构建 metadata
	metadata := map[string]any{
		"message_id":   ptrValue(msg.MessageId),
		"message_type": messageType,
		"chat_type":    chatType,
		"root_id":      ptrValue(msg.RootId),
		"parent_id":    ptrValue(msg.ParentId),
		"thread_id":    ptrValue(msg.ThreadId),
	}

	f.attachReferencedMessages(ctx, msg, metadata)
	content = buildFeishuInboundContent(content, metadata)

	// 将被引用消息的媒体附件合并到当前消息，使 Agent 能直接访问引用的图片/文件
	media = mergeReferencedMedia(media, metadata)

	// 下载图片到本地，使 Agent 能直接读取图片内容
	media = f.downloadMediaItems(ctx, media, ptrValue(msg.MessageId))

	if content == "" && len(media) == 0 && !hasReferencedMessage(metadata) {
		return nil
	}

	// DM: use senderID as reply target instead of chatID
	replyTarget := chatID
	if chatType != "group" && senderID != "" {
		replyTarget = senderID
	}

	f.HandleMessage(senderID, replyTarget, content, media, metadata)
	return nil
}

// Send 发送消息到飞书
func (f *FeishuChannel) Send(msg *bus.OutboundMessage) error {
	if f.client == nil {
		return fmt.Errorf("feishu client not initialized")
	}

	ctx := context.Background()

	receiveIDType := detectReceiveIDType(msg.ChatID)
	f.Logger.Info("飞书 Send 开始",
		zap.String("chat_id", msg.ChatID),
		zap.String("receive_id_type", receiveIDType),
		zap.String("reply_to", msg.ReplyTo),
		zap.Int("content_len", len(msg.Content)),
		zap.Int("media_count", len(msg.Media)),
		zap.Bool("reply_to_message", f.replyToMessage),
	)

	// Tool hint messages: send lightweight card (bypass reply routing)
	if isToolHint, ok := msg.Metadata["_tool_hint"].(bool); ok && isToolHint {
		f.sendToolHintCard(ctx, receiveIDType, msg.ChatID, msg.Content)
		return nil
	}

	// Determine reply-to message ID — only first send uses Reply API (与 Python first_send 逻辑一致)
	isProgress := false
	if p, ok := msg.Metadata["_progress"].(bool); ok && p {
		isProgress = true
	}

	replyTo := ""
	if f.replyToMessage && msg.ReplyTo != "" && !isProgress {
		replyTo = msg.ReplyTo
	}
	firstSend := true // 仅首条消息（媒体或文本）使用 Reply API

	// doSend 发送消息：首条用 Reply API（引用回复），后续用 Create API
	doSend := func(msgType, content string) error {
		useReply := ""
		if replyTo != "" && firstSend {
			useReply = replyTo
			firstSend = false
		}
		return f.sendMsgOrReply(ctx, receiveIDType, msg.ChatID, msgType, content, useReply)
	}

	// ① 先发送媒体文件（与 Python 顺序一致）
	for _, mediaPath := range msg.Media {
		ext := strings.ToLower(filepath.Ext(mediaPath))
		if isImageExt(ext) {
			imageKey, err := f.uploadImage(ctx, mediaPath)
			if err != nil {
				f.Logger.Error("上传图片失败", zap.Error(err))
				continue
			}
			imgJSON, _ := json.Marshal(map[string]string{"image_key": imageKey})
			if err := doSend("image", string(imgJSON)); err != nil {
				f.Logger.Error("发送图片失败", zap.Error(err))
			}
		} else if isAudioExt(ext) {
			fileKey, err := f.uploadFile(ctx, mediaPath)
			if err != nil {
				f.Logger.Error("上传音频失败", zap.Error(err))
				continue
			}
			audioJSON, _ := json.Marshal(map[string]string{"file_key": fileKey})
			if err := doSend("audio", string(audioJSON)); err != nil {
				f.Logger.Error("发送音频失败", zap.Error(err))
			}
		} else {
			fileKey, err := f.uploadFile(ctx, mediaPath)
			if err != nil {
				f.Logger.Error("上传文件失败", zap.Error(err))
				continue
			}
			fileJSON, _ := json.Marshal(map[string]string{"file_key": fileKey})
			if err := doSend("file", string(fileJSON)); err != nil {
				f.Logger.Error("发送文件失败", zap.Error(err))
			}
		}
	}

	// ② 再发送文本消息
	if msg.Content != "" {
		format := detectMsgFormat(msg.Content)
		f.Logger.Info("飞书 Send 文本消息",
			zap.String("format", format),
			zap.Int("content_runes", len([]rune(msg.Content))),
		)

		switch format {
		case "text":
			contentJSON, _ := json.Marshal(map[string]string{"text": msg.Content})
			if err := doSend("text", string(contentJSON)); err != nil {
				return err
			}
		case "post":
			postJSON := markdownToPost(msg.Content)
			if err := doSend("post", postJSON); err != nil {
				return err
			}
		default: // "interactive"
			parts := splitContentByTables(msg.Content)
			for i, part := range parts {
				cardJSON := f.buildCardJSON(part)
				if err := doSend("interactive", cardJSON); err != nil {
					return err
				}
				if i < len(parts)-1 {
					time.Sleep(200 * time.Millisecond)
				}
			}
		}
	}

	return nil
}

// uploadImage 上传图片到飞书
func (f *FeishuChannel) uploadImage(ctx context.Context, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开图片文件失败: %w", err)
	}
	defer file.Close()

	req := larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(file).
			Build()).
		Build()

	resp, err := f.client.Im.Image.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("上传图片失败: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("上传图片失败: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return *resp.Data.ImageKey, nil
}

// uploadFile 上传文件到飞书
func (f *FeishuChannel) uploadFile(ctx context.Context, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	req := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType(inferFileType(filepath.Ext(filePath))).
			FileName(filepath.Base(filePath)).
			File(file).
			Build()).
		Build()

	resp, err := f.client.Im.File.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("上传文件失败: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("上传文件失败: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return *resp.Data.FileKey, nil
}

// downloadImage 使用 Lark SDK 下载图片到本地临时文件
func (f *FeishuChannel) downloadImage(ctx context.Context, messageID, imageKey string) (string, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(imageKey).
		Type("image").
		Build()

	resp, err := f.client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("download image %s: %w", imageKey, err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("download image %s: code=%d msg=%s", imageKey, resp.Code, resp.Msg)
	}

	tmpFile, err := os.CreateTemp("", "feishu-img-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if err := resp.WriteFile(tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write image to %s: %w", tmpPath, err)
	}

	f.Logger.Info("飞书图片下载完成", zap.String("image_key", imageKey), zap.String("path", tmpPath))
	return tmpPath, nil
}

// downloadMediaItems 遍历 media 列表，将 "image:xxx" 项下载为本地文件路径
func (f *FeishuChannel) downloadMediaItems(ctx context.Context, media []string, messageID string) []string {
	if len(media) == 0 || messageID == "" {
		return media
	}

	result := make([]string, 0, len(media))
	for _, item := range media {
		if strings.HasPrefix(item, "image:") {
			imageKey := strings.TrimPrefix(item, "image:")
			localPath, err := f.downloadImage(ctx, messageID, imageKey)
			if err != nil {
				f.Logger.Warn("飞书图片下载失败，跳过", zap.String("image_key", imageKey), zap.Error(err))
				continue
			}
			result = append(result, localPath)
		} else if strings.HasPrefix(item, "audio:") {
			fileKey := strings.TrimPrefix(item, "audio:")
			localPath, err := f.downloadResource(ctx, messageID, fileKey, "audio", ".opus")
			if err != nil {
				f.Logger.Warn("飞书音频下载失败，跳过", zap.String("file_key", fileKey), zap.Error(err))
				continue
			}
			// Try transcription if callback is set
			if f.TranscribeFunc != nil {
				if text := f.TranscribeFunc(localPath); text != "" {
					result = append(result, localPath)
					result = append(result, "transcription:"+text)
					continue
				}
			}
			result = append(result, localPath)
		} else if strings.HasPrefix(item, "media:") {
			fileKey := strings.TrimPrefix(item, "media:")
			localPath, err := f.downloadResource(ctx, messageID, fileKey, "file", "")
			if err != nil {
				f.Logger.Warn("飞书媒体下载失败，跳过", zap.String("file_key", fileKey), zap.Error(err))
				continue
			}
			result = append(result, localPath)
		} else {
			result = append(result, item)
		}
	}
	return result
}

// downloadResource 下载飞书消息资源（音频/文件等）到本地临时文件
func (f *FeishuChannel) downloadResource(ctx context.Context, messageID, fileKey, resourceType, ext string) (string, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(fileKey).
		Type(resourceType).
		Build()

	resp, err := f.client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("download %s %s: %w", resourceType, fileKey, err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("download %s %s: code=%d msg=%s", resourceType, fileKey, resp.Code, resp.Msg)
	}

	if ext == "" {
		ext = ".bin"
	}
	tmpFile, err := os.CreateTemp("", "feishu-"+resourceType+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if err := resp.WriteFile(tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write %s to %s: %w", resourceType, tmpPath, err)
	}

	f.Logger.Info("飞书资源下载完成",
		zap.String("type", resourceType),
		zap.String("file_key", fileKey),
		zap.String("path", tmpPath),
	)
	return tmpPath, nil
}

// ============ 辅助函数 ============

// ptrValue 安全解引用字符串指针
func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// detectReceiveIDType 根据 ID 格式推断飞书 receive_id_type
// oc_ → chat_id, 其他 → open_id（与 Python 保持一致）
func detectReceiveIDType(id string) string {
	if strings.HasPrefix(id, "oc_") {
		return "chat_id"
	}
	return "open_id"
}

// extractSenderID 从事件中提取发送者 ID（优先 openId，与 Python 保持一致）
func extractSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	if oid := ptrValue(sender.SenderId.OpenId); oid != "" {
		return oid
	}
	if uid := ptrValue(sender.SenderId.UserId); uid != "" {
		return uid
	}
	return ""
}

// extractMessageContent 从消息内容中提取文本和媒体附件
func extractMessageContent(rawContent, messageType string) (string, []string) {
	if rawContent == "" {
		return "", nil
	}

	var contentMap map[string]any
	if err := json.Unmarshal([]byte(rawContent), &contentMap); err != nil {
		return rawContent, nil
	}

	switch messageType {
	case "text":
		if text, ok := contentMap["text"].(string); ok {
			return text, nil
		}
	case "image":
		if imageKey, ok := contentMap["image_key"].(string); ok {
			return "", []string{"image:" + imageKey}
		}
	case "file":
		if fileKey, ok := contentMap["file_key"].(string); ok {
			return "", []string{"file:" + fileKey}
		}
	case "post":
		return extractPostContent(contentMap)
	case "audio":
		if fileKey, ok := contentMap["file_key"].(string); ok {
			return "", []string{"audio:" + fileKey}
		}
	case "media":
		if fileKey, ok := contentMap["file_key"].(string); ok {
			return "", []string{"media:" + fileKey}
		}
		if imageKey, ok := contentMap["image_key"].(string); ok {
			return "", []string{"image:" + imageKey}
		}
	case "interactive":
		return extractInteractiveContent(contentMap), nil
	case "share_chat", "share_user", "share_calendar_event", "system", "merge_forward", "sticker":
		return extractShareCardContent(contentMap, messageType), nil
	}

	return rawContent, nil
}

// extractPostContent 从 post 富文本消息中提取文本和图片
// 支持三种 payload 格式:
//  1. direct: {"title":"...", "content":[[...]]}
//  2. localized: {"zh_cn":{"title":"...","content":[...]}, "en_us":...}
//  3. wrapped: {"post":{"zh_cn":{"title":"...","content":[...]}}}
func extractPostContent(contentMap map[string]any) (string, []string) {
	// Try wrapped format: {"post":{...}}
	if postMap, ok := contentMap["post"].(map[string]any); ok {
		return extractPostFromLocalized(postMap)
	}

	// Try direct format: has "content" array at top level
	if _, ok := contentMap["content"].([]any); ok {
		return extractPostDirect(contentMap)
	}

	// Try localized format: {"zh_cn":{...}, "en_us":{...}}
	return extractPostFromLocalized(contentMap)
}

// extractPostFromLocalized extracts post from localized map (zh_cn > en_us > ja_jp > first found)
func extractPostFromLocalized(localizedMap map[string]any) (string, []string) {
	for _, lang := range []string{"zh_cn", "en_us", "ja_jp"} {
		if postData, ok := localizedMap[lang].(map[string]any); ok {
			return extractPostDirect(postData)
		}
	}
	// Fallback: try first available language
	for _, v := range localizedMap {
		if postData, ok := v.(map[string]any); ok {
			return extractPostDirect(postData)
		}
	}
	return "", nil
}

// extractPostDirect extracts text and media from a direct post structure
func extractPostDirect(postData map[string]any) (string, []string) {
	var texts []string
	var media []string

	if title, _ := postData["title"].(string); title != "" {
		texts = append(texts, title)
	}

	contentArr, ok := postData["content"].([]any)
	if !ok {
		return strings.Join(texts, "\n"), media
	}

	for _, line := range contentArr {
		lineArr, ok := line.([]any)
		if !ok {
			continue
		}
		var lineTexts []string
		for _, elem := range lineArr {
			elemMap, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			tag, _ := elemMap["tag"].(string)
			switch tag {
			case "text":
				if text, _ := elemMap["text"].(string); text != "" {
					lineTexts = append(lineTexts, text)
				}
			case "img":
				if imageKey, _ := elemMap["image_key"].(string); imageKey != "" {
					media = append(media, "image:"+imageKey)
				}
			case "a":
				text, _ := elemMap["text"].(string)
				href, _ := elemMap["href"].(string)
				if text != "" && href != "" {
					lineTexts = append(lineTexts, fmt.Sprintf("[%s](%s)", text, href))
				} else if text != "" {
					lineTexts = append(lineTexts, text)
				}
			case "at":
				// @mention in post
				userName, _ := elemMap["user_name"].(string)
				if userName != "" {
					lineTexts = append(lineTexts, "@"+userName)
				}
			case "code_block":
				lang, _ := elemMap["language"].(string)
				text, _ := elemMap["text"].(string)
				if text != "" {
					if lang != "" {
						lineTexts = append(lineTexts, fmt.Sprintf("```%s\n%s\n```", lang, text))
					} else {
						lineTexts = append(lineTexts, fmt.Sprintf("```\n%s\n```", text))
					}
				}
			}
		}
		if len(lineTexts) > 0 {
			texts = append(texts, strings.Join(lineTexts, ""))
		}
	}

	return strings.Join(texts, "\n"), media
}

func (f *FeishuChannel) attachReferencedMessages(ctx context.Context, msg *larkim.EventMessage, metadata map[string]any) {
	if msg == nil || metadata == nil {
		return
	}

	parentID := ptrValue(msg.ParentId)
	rootID := ptrValue(msg.RootId)

	if parentID != "" {
		if quoted, err := f.fetchReferencedMessage(ctx, parentID); err != nil {
			f.Logger.Warn("查询飞书引用父消息失败", zap.String("parent_id", parentID), zap.Error(err))
		} else if quoted != nil {
			metadata["quoted_message"] = quoted.toMap()
		}
	}

	if rootID != "" && rootID != parentID {
		if root, err := f.fetchReferencedMessage(ctx, rootID); err != nil {
			f.Logger.Warn("查询飞书根消息失败", zap.String("root_id", rootID), zap.Error(err))
		} else if root != nil {
			metadata["root_message"] = root.toMap()
		}
	}
}

func (f *FeishuChannel) fetchReferencedMessage(ctx context.Context, messageID string) (*feishuReferencedMessage, error) {
	if f.client == nil || messageID == "" {
		return nil, nil
	}

	resp, err := f.client.Im.Message.Get(ctx,
		larkim.NewGetMessageReqBuilder().
			MessageId(messageID).
			Build())
	if err != nil {
		return nil, fmt.Errorf("get message %s: %w", messageID, err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("get message %s failed: code=%d msg=%s", messageID, resp.Code, resp.Msg)
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
		return nil, nil
	}

	item := resp.Data.Items[0]
	content, media := extractMessageContent(ptrValueFromBody(item.Body), ptrValue(item.MsgType))
	if ptrValue(item.MsgType) == "text" && content != "" {
		content = mentionRE.ReplaceAllString(content, "")
		content = strings.TrimSpace(content)
	}

	// 用被引用消息自己的 message_id 下载图片（API 要求 message_id 与资源匹配）
	media = f.downloadMediaItems(ctx, media, ptrValue(item.MessageId))

	return &feishuReferencedMessage{
		MessageID:   ptrValue(item.MessageId),
		RootID:      ptrValue(item.RootId),
		ParentID:    ptrValue(item.ParentId),
		ThreadID:    ptrValue(item.ThreadId),
		MessageType: ptrValue(item.MsgType),
		Content:     content,
		Media:       media,
		SenderID:    ptrValueFromSender(item.Sender),
	}, nil
}

func (m *feishuReferencedMessage) toMap() map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{
		"message_id":   m.MessageID,
		"root_id":      m.RootID,
		"parent_id":    m.ParentID,
		"thread_id":    m.ThreadID,
		"message_type": m.MessageType,
		"content":      m.Content,
		"media":        m.Media,
		"sender_id":    m.SenderID,
	}
}

func ptrValueFromBody(body *larkim.MessageBody) string {
	if body == nil {
		return ""
	}
	return ptrValue(body.Content)
}

func ptrValueFromSender(sender *larkim.Sender) string {
	if sender == nil {
		return ""
	}
	return ptrValue(sender.Id)
}

func hasReferencedMessage(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	_, hasQuoted := metadata["quoted_message"]
	_, hasRoot := metadata["root_message"]
	return hasQuoted || hasRoot
}

func buildFeishuInboundContent(content string, metadata map[string]any) string {
	var parts []string

	if quoted := formatFeishuReferencedBlock("引用消息", metadataMap(metadata, "quoted_message")); quoted != "" {
		parts = append(parts, quoted)
	}

	if root := metadataMap(metadata, "root_message"); root != nil {
		quotedID := metadataNestedString(metadata, "quoted_message", "message_id")
		rootID := stringValue(root["message_id"])
		if rootID != "" && rootID != quotedID {
			if block := formatFeishuReferencedBlock("根消息", root); block != "" {
				parts = append(parts, block)
			}
		}
	}

	content = strings.TrimSpace(content)
	if content != "" {
		parts = append(parts, content)
	}

	return strings.Join(parts, "\n\n")
}

func formatFeishuReferencedBlock(title string, data map[string]any) string {
	if data == nil {
		return ""
	}

	var lines []string
	if messageType := stringValue(data["message_type"]); messageType != "" {
		lines = append(lines, fmt.Sprintf("[%s]", messageType))
	}
	if senderID := stringValue(data["sender_id"]); senderID != "" {
		lines = append(lines, fmt.Sprintf("发送者: %s", senderID))
	}
	if content := truncateFeishuQuotedText(stringValue(data["content"]), 1200); content != "" {
		lines = append(lines, content)
	}
	if media := stringSliceValue(data["media"]); len(media) > 0 {
		lines = append(lines, fmt.Sprintf("媒体: %s", strings.Join(media, ", ")))
	}
	if len(lines) == 0 {
		return ""
	}

	return fmt.Sprintf("%s:\n%s", title, strings.Join(lines, "\n"))
}

func truncateFeishuQuotedText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" || limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n[已截断]"
}

func metadataMap(metadata map[string]any, key string) map[string]any {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func metadataNestedString(metadata map[string]any, key, field string) string {
	return stringValue(metadataMap(metadata, key)[field])
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

// mergeReferencedMedia 将被引用消息中的媒体附件合并到当前 media 列表中，去重
func mergeReferencedMedia(media []string, metadata map[string]any) []string {
	seen := make(map[string]bool, len(media))
	for _, m := range media {
		seen[m] = true
	}
	for _, key := range []string{"quoted_message", "root_message"} {
		for _, m := range stringSliceValue(metadataMap(metadata, key)["media"]) {
			if !seen[m] {
				media = append(media, m)
				seen[m] = true
			}
		}
	}
	return media
}

func stringSliceValue(v any) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// hasBotMention 检查消息是否 @了机器人。
// 与 Python _is_bot_mentioned 保持一致：
// 1. rawContent 包含 "@_all" → true
// 2. mention 的 id.user_id 为空但 id.open_id 以 "ou_" 开头 → true（Bot mention 特征）
func hasBotMention(rawContent string, mentions []*larkim.MentionEvent) bool {
	if strings.Contains(rawContent, "@_all") {
		return true
	}
	for _, m := range mentions {
		if m == nil || m.Id == nil {
			continue
		}
		uid := ptrValue(m.Id.UserId)
		oid := ptrValue(m.Id.OpenId)
		if uid == "" && strings.HasPrefix(oid, "ou_") {
			return true
		}
	}
	return false
}

// inferFileType 根据扩展名推断飞书文件类型
func inferFileType(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".opus", ".ogg":
		return "opus"
	case ".mp4", ".mov", ".avi":
		return "stream" // 视频以 file 类型发送，上传需用 stream 匹配
	case ".pdf":
		return "pdf"
	case ".doc", ".docx":
		return "doc"
	case ".xls", ".xlsx":
		return "xls"
	case ".ppt", ".pptx":
		return "ppt"
	default:
		return "stream"
	}
}

// ============ Card 构建 (从原 feishu.go 保留) ============

// mentionRE 匹配飞书 @mention 占位符 (如 @_user_1)
var mentionRE = regexp.MustCompile(`@_user_\d+\s*`)

// Markdown table regex
var tableRE = regexp.MustCompile(`(?m)((?:^[ \t]*\|.+\|[ \t]*\n)(?:^[ \t]*\|[-:\s|]+\|[ \t]*\n)(?:^[ \t]*\|.+\|[ \t]*\n?)+)`)
var headingRE = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// buildCardElements 构建飞书 Card 消息 (Card JSON 2.0)
func (f *FeishuChannel) buildCardElements(content string) map[string]any {
	elements := f.splitHeadings(content)
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true},
		"body": map[string]any{
			"elements": elements,
		},
	}
}

// codeBlockRE matches fenced code blocks (``` ... ```)
var codeBlockRE = regexp.MustCompile("(?s)```[^`]*```")

// splitHeadings 将内容按标题分割，同时将 Markdown 表格转为飞书 table 组件
// Code blocks are protected from heading/table splitting using placeholder substitution.
func (f *FeishuChannel) splitHeadings(content string) []map[string]any {
	// Protect code blocks: replace with placeholders
	var codeBlocks []string
	protected := codeBlockRE.ReplaceAllStringFunc(content, func(match string) string {
		idx := len(codeBlocks)
		codeBlocks = append(codeBlocks, match)
		return fmt.Sprintf("\x00CODEBLOCK_%d\x00", idx)
	})

	var elements []map[string]any

	matches := headingRE.FindAllStringIndex(protected, -1)
	if len(matches) == 0 {
		elements = processContent(protected)
	} else {
		lastEnd := 0
		for _, loc := range matches {
			before := strings.TrimSpace(protected[lastEnd:loc[0]])
			if before != "" {
				elements = append(elements, processContent(before)...)
			}
			heading := headingRE.FindStringSubmatch(protected[loc[0]:loc[1]])
			if len(heading) >= 3 {
				elements = append(elements, map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**%s**", heading[2]),
					},
				})
			}
			lastEnd = loc[1]
		}

		remaining := strings.TrimSpace(protected[lastEnd:])
		if remaining != "" {
			elements = append(elements, processContent(remaining)...)
		}
	}

	// Restore code blocks from placeholders
	if len(codeBlocks) > 0 {
		for i, elem := range elements {
			if content, ok := elem["content"].(string); ok {
				for j, block := range codeBlocks {
					placeholder := fmt.Sprintf("\x00CODEBLOCK_%d\x00", j)
					content = strings.ReplaceAll(content, placeholder, block)
				}
				elements[i]["content"] = content
			}
		}
	}

	return elements
}

// processContent 处理一段文本内容，将其中的 Markdown 表格转为飞书 table 组件，
// 非表格部分保持为 markdown 元素
func processContent(content string) []map[string]any {
	locs := tableRE.FindAllStringIndex(content, -1)
	if len(locs) == 0 {
		return []map[string]any{{"tag": "markdown", "content": content}}
	}

	var elements []map[string]any
	lastEnd := 0
	for _, loc := range locs {
		// 表格前的文字
		before := strings.TrimSpace(content[lastEnd:loc[0]])
		if before != "" {
			elements = append(elements, map[string]any{"tag": "markdown", "content": before})
		}
		// 将 Markdown 表格转为飞书 table 组件
		tableElement := mdTableToCardTable(content[loc[0]:loc[1]])
		if tableElement != nil {
			elements = append(elements, tableElement)
		} else {
			// 解析失败时回退到 markdown 原文
			elements = append(elements, map[string]any{"tag": "markdown", "content": content[loc[0]:loc[1]]})
		}
		lastEnd = loc[1]
	}
	// 表格后的文字
	remaining := strings.TrimSpace(content[lastEnd:])
	if remaining != "" {
		elements = append(elements, map[string]any{"tag": "markdown", "content": remaining})
	}
	return elements
}

// mdTableToCardTable 将 Markdown 表格文本解析为飞书 Card table 组件
// 输入示例:
//
//	| 标的 | 今日收盘 | 明日涨跌 |
//	|:---:|:---:|:---:|
//	| 黄金 | ¥36.49 | 65% |
//
// 输出: {"tag": "table", "columns": [...], "rows": [...]}
func mdTableToCardTable(mdTable string) map[string]any {
	lines := strings.Split(strings.TrimSpace(mdTable), "\n")
	if len(lines) < 3 {
		return nil // 至少需要: header + separator + 1 data row
	}

	// 解析表头
	headerCells := parseTableRow(lines[0])
	if len(headerCells) == 0 {
		return nil
	}

	// lines[1] 是分隔符行 (|---|---|---| )，跳过

	// 构建 columns
	columns := make([]map[string]any, len(headerCells))
	for i, cell := range headerCells {
		columns[i] = map[string]any{
			"name":         fmt.Sprintf("col_%d", i),
			"display_name": cell,
			"data_type":    "lark_md",
			"width":        "auto",
		}
	}

	// 构建 rows
	var rows []map[string]any
	for _, line := range lines[2:] {
		cells := parseTableRow(line)
		if len(cells) == 0 {
			continue
		}
		row := make(map[string]any)
		for i := range columns {
			if i < len(cells) {
				row[fmt.Sprintf("col_%d", i)] = cells[i]
			} else {
				row[fmt.Sprintf("col_%d", i)] = ""
			}
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil
	}

	// 动态设置 page_size：行数不超过 10 行时全部显示
	pageSize := len(rows)
	if pageSize > 10 {
		pageSize = 10
	}

	return map[string]any{
		"tag":        "table",
		"page_size":  pageSize,
		"row_height": "auto",
		"header_style": map[string]any{
			"text_align":       "center",
			"text_size":        "normal",
			"background_style": "grey",
			"bold":             true,
		},
		"columns": columns,
		"rows":    rows,
	}
}

// parseTableRow 解析 Markdown 表格的一行，返回各单元格内容 (去除首尾空白)
// 输入: "| 黄金 | ¥36.49 | 65% |"
// 输出: ["黄金", "¥36.49", "65%"]
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	// 去掉首尾的 |
	line = line[1 : len(line)-1]
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}


// splitContentByTables 按表格边界拆分内容，每段最多包含 5 张表格
// 避免触发飞书 card table number over limit (ErrCode 11310)
func splitContentByTables(content string) []string {
	locs := tableRE.FindAllStringIndex(content, -1)
	if len(locs) <= 5 {
		return []string{content}
	}

	var parts []string
	segStart := 0
	tableCount := 0

	for i, loc := range locs {
		tableCount++
		// 当数到第 5 个表格时，进行截断
		if tableCount == 5 {
			part := strings.TrimSpace(content[segStart:loc[1]])
			if part != "" {
				parts = append(parts, part)
			}
			segStart = loc[1]
			tableCount = 0
		} else if i == len(locs)-1 {
			// 最后一个表格，如果有剩余内容则拼接
			remaining := strings.TrimSpace(content[segStart:])
			if remaining != "" {
				parts = append(parts, remaining)
			}
		}
	}

	// 处理如果最后一段没有表格但有普通文本的情况
	if segStart < len(content) && tableCount == 0 {
		remaining := strings.TrimSpace(content[segStart:])
		if remaining != "" {
			parts = append(parts, remaining)
		}
	}

	return parts
}

// isImageExt 判断是否是图片扩展名
func isImageExt(ext string) bool {
	imageExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".bmp": true, ".webp": true, ".ico": true, ".tiff": true, ".tif": true,
	}
	return imageExts[ext]
}

// isAudioExt 判断是否是飞书支持的语音扩展名 (OPUS)
func isAudioExt(ext string) bool {
	audioExts := map[string]bool{
		".opus": true, ".ogg": true,
	}
	return audioExts[ext]
}

// buildCardJSON 将内容构建为飞书卡片 JSON 字符串
func (f *FeishuChannel) buildCardJSON(content string) string {
	card := f.buildCardElements(content)
	data, _ := json.Marshal(card)
	return string(data)
}
