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
	"go.uber.org/zap"
)

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
}

func init() {
	RegisterFactory("feishu", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &FeishuChannel{
			BaseChannel: BaseChannel{
				ChannelName: "feishu",
				Bus:         msgBus,
				Logger:      logger,
			},
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

	// 提取 chatID
	chatID := ptrValue(msg.ChatId)
	if chatID == "" {
		f.Logger.Warn("飞书消息 chatID 为空")
		return nil
	}

	// 提取 senderID: 优先 userId > openId
	senderID := extractSenderID(sender)

	// 提取消息内容和媒体
	messageType := ptrValue(msg.MessageType)
	content, media := extractMessageContent(msg, messageType)

	// 清理 @mention 占位符 (如 "@_user_1 你好" → "你好")
	if messageType == "text" && content != "" {
		content = mentionRE.ReplaceAllString(content, "")
		content = strings.TrimSpace(content)
	}

	f.Logger.Info("飞书消息解析完成",
		zap.String("sender", senderID),
		zap.String("chat_id", chatID),
		zap.String("type", messageType),
		zap.String("content", content),
	)

	if content == "" && len(media) == 0 {
		return nil
	}

	// 构建 metadata
	metadata := map[string]any{
		"message_id":   ptrValue(msg.MessageId),
		"message_type": messageType,
		"chat_type":    ptrValue(msg.ChatType),
	}

	f.HandleMessage(senderID, chatID, content, media, metadata)
	return nil
}

// Send 发送消息到飞书
func (f *FeishuChannel) Send(msg *bus.OutboundMessage) error {
	if f.client == nil {
		return fmt.Errorf("feishu client not initialized")
	}

	ctx := context.Background()

	receiveIDType := "chat_id"
	if !strings.HasPrefix(msg.ChatID, "oc_") {
		receiveIDType = "open_id"
	}

	// 发送文本消息：按表格数量拆分，避免超出飞书 card table number over limit
	if msg.Content != "" {
		parts := splitContentByTables(msg.Content)
		lastReplyTo := msg.ReplyTo
		for i, part := range parts {
			msgID, err := f.sendCard(ctx, receiveIDType, msg.ChatID, lastReplyTo, part)
			if err != nil {
				return err
			}
			lastReplyTo = msgID
			if i < len(parts)-1 {
				time.Sleep(200 * time.Millisecond)
			}
		}
	}

	// 发送媒体文件
	for _, mediaPath := range msg.Media {
		ext := strings.ToLower(filepath.Ext(mediaPath))
		if isImageExt(ext) {
			if err := f.sendImage(ctx, receiveIDType, msg.ChatID, mediaPath); err != nil {
				f.Logger.Error("发送图片失败", zap.Error(err))
			}
		} else if isAudioExt(ext) {
			if err := f.sendAudio(ctx, receiveIDType, msg.ChatID, mediaPath); err != nil {
				f.Logger.Error("发送语音流失败", zap.Error(err))
			}
		} else {
			if err := f.sendFile(ctx, receiveIDType, msg.ChatID, mediaPath); err != nil {
				f.Logger.Error("发送文件失败", zap.Error(err))
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

// sendImage 上传并发送图片
func (f *FeishuChannel) sendImage(ctx context.Context, receiveIDType, receiveID, filePath string) error {
	imageKey, err := f.uploadImage(ctx, filePath)
	if err != nil {
		return err
	}

	content, _ := json.Marshal(map[string]string{"image_key": imageKey})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("image").
			Content(string(content)).
			Build()).
		Build()

	_, err = f.client.Im.Message.Create(ctx, req)
	return err
}

// sendFile 上传并发送文件
func (f *FeishuChannel) sendFile(ctx context.Context, receiveIDType, receiveID, filePath string) error {
	fileKey, err := f.uploadFile(ctx, filePath)
	if err != nil {
		return err
	}

	content, _ := json.Marshal(map[string]string{"file_key": fileKey})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("file").
			Content(string(content)).
			Build()).
		Build()

	_, err = f.client.Im.Message.Create(ctx, req)
	return err
}

// sendAudio 上传并发送语音
func (f *FeishuChannel) sendAudio(ctx context.Context, receiveIDType, receiveID, filePath string) error {
	fileKey, err := f.uploadFile(ctx, filePath)
	if err != nil {
		return err
	}

	content, _ := json.Marshal(map[string]string{"file_key": fileKey})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("audio").
			Content(string(content)).
			Build()).
		Build()

	_, err = f.client.Im.Message.Create(ctx, req)
	return err
}

// ============ 辅助函数 ============

// ptrValue 安全解引用字符串指针
func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// extractSenderID 从事件中提取发送者 ID (优先 userId > openId)
func extractSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	if uid := ptrValue(sender.SenderId.UserId); uid != "" {
		return uid
	}
	if oid := ptrValue(sender.SenderId.OpenId); oid != "" {
		return oid
	}
	return ""
}

// extractMessageContent 从消息事件中提取文本内容和媒体附件
func extractMessageContent(msg *larkim.EventMessage, messageType string) (string, []string) {
	rawContent := ptrValue(msg.Content)
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
	}

	return rawContent, nil
}

// inferFileType 根据扩展名推断飞书文件类型
func inferFileType(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".opus", ".ogg":
		return "opus"
	case ".mp4", ".mov", ".avi":
		return "mp4"
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

// splitHeadings 将内容按标题分割，同时将 Markdown 表格转为飞书 table 组件
func (f *FeishuChannel) splitHeadings(content string) []map[string]any {
	var elements []map[string]any

	matches := headingRE.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return processContent(content)
	}

	lastEnd := 0
	for _, loc := range matches {
		before := strings.TrimSpace(content[lastEnd:loc[0]])
		if before != "" {
			elements = append(elements, processContent(before)...)
		}
		heading := headingRE.FindStringSubmatch(content[loc[0]:loc[1]])
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

	remaining := strings.TrimSpace(content[lastEnd:])
	if remaining != "" {
		elements = append(elements, processContent(remaining)...)
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

// sendCard 发送或回复一张卡片消息，返回新消息的 message_id
func (f *FeishuChannel) sendCard(ctx context.Context, receiveIDType, chatID, replyTo, content string) (string, error) {
	card := f.buildCardElements(content)
	cardJSON, _ := json.Marshal(card)

	if replyTo != "" {
		resp, err := f.client.Im.Message.Reply(ctx,
			larkim.NewReplyMessageReqBuilder().
				MessageId(replyTo).
				Body(larkim.NewReplyMessageReqBodyBuilder().
					MsgType("interactive").
					Content(string(cardJSON)).
					Build()).
				Build())
		if err != nil {
			return "", fmt.Errorf("回复飞书消息失败: %w", err)
		}
		if !resp.Success() {
			return "", fmt.Errorf("回复飞书消息失败: code=%d msg=%s", resp.Code, resp.Msg)
		}
		if resp.Data != nil && resp.Data.MessageId != nil {
			return *resp.Data.MessageId, nil
		}
		return replyTo, nil
	}

	resp, err := f.client.Im.Message.Create(ctx,
		larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(receiveIDType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType("interactive").
				Content(string(cardJSON)).
				Build()).
			Build())
	if err != nil {
		return "", fmt.Errorf("发送飞书消息失败: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("发送飞书消息失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
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
