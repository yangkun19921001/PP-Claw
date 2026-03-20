//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"go.uber.org/zap"
)

const (
	textMaxLen = 200
	postMaxLen = 2000
)

var (
	complexMDRe = regexp.MustCompile("(?m)(^```|^\\|.*\\||^#{1,6}\\s)")
	simpleMDRe  = regexp.MustCompile(`(\*\*.*?\*\*|\*.*?\*|~~.*?~~)`)
	mdLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	listRe      = regexp.MustCompile(`(?m)^[-*+]\s+`)
	olistRe     = regexp.MustCompile(`(?m)^\d+\.\s+`)
)

// detectMsgFormat determines the best Feishu message format for the content.
// Returns "text", "post", or "interactive".
// 与 Python _detect_msg_format 保持一致。
func detectMsgFormat(content string) string {
	stripped := strings.TrimSpace(content)
	if stripped == "" {
		return "text"
	}
	contentLen := len([]rune(stripped))

	// Complex markdown (code blocks, tables, headings) → always card
	if complexMDRe.MatchString(stripped) {
		return "interactive"
	}

	// Long content → card
	if contentLen > postMaxLen {
		return "interactive"
	}

	// Has bold/italic/strikethrough → card (post format can't render these)
	if simpleMDRe.MatchString(stripped) {
		return "interactive"
	}

	// Has list items → card (post format can't render list bullets well)
	if listRe.MatchString(stripped) || olistRe.MatchString(stripped) {
		return "interactive"
	}

	// Has links → post format (supports <a> tags)
	if mdLinkRe.MatchString(stripped) {
		return "post"
	}

	// Short plain text → text format
	if contentLen <= textMaxLen {
		return "text"
	}

	// Medium plain text without any formatting → post format
	return "post"
}

// markdownToPost converts simple markdown content to Feishu post JSON.
// Handles links [text](url) and basic text lines.
func markdownToPost(content string) string {
	lines := strings.Split(content, "\n")
	var postContent [][]map[string]any

	for _, line := range lines {
		var lineElements []map[string]any

		// Parse markdown links
		remaining := line
		for {
			loc := mdLinkRe.FindStringIndex(remaining)
			if loc == nil {
				if remaining != "" {
					lineElements = append(lineElements, map[string]any{
						"tag":  "text",
						"text": remaining,
					})
				}
				break
			}

			// Text before the link
			if loc[0] > 0 {
				lineElements = append(lineElements, map[string]any{
					"tag":  "text",
					"text": remaining[:loc[0]],
				})
			}

			// The link itself
			matches := mdLinkRe.FindStringSubmatch(remaining[loc[0]:loc[1]])
			if len(matches) >= 3 {
				lineElements = append(lineElements, map[string]any{
					"tag":  "a",
					"text": matches[1],
					"href": matches[2],
				})
			}

			remaining = remaining[loc[1]:]
		}

		if len(lineElements) == 0 {
			lineElements = append(lineElements, map[string]any{
				"tag":  "text",
				"text": "",
			})
		}

		postContent = append(postContent, lineElements)
	}

	postJSON := map[string]any{
		"zh_cn": map[string]any{
			"content": postContent,
		},
	}

	data, _ := json.Marshal(postJSON)
	return string(data)
}

// stripMdFormatting removes markdown formatting markers.
func stripMdFormatting(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "~~", "")
	// Remove single * (italic) but not **
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '*' && (i == 0 || s[i-1] != '*') && (i+1 >= len(s) || s[i+1] != '*') {
			continue
		}
		result = append(result, s[i])
	}
	return string(result)
}

// formatToolHintLines splits a tool hint string by commas at depth 0
// for readable multi-line display.
func formatToolHintLines(hint string) string {
	if hint == "" {
		return ""
	}
	depth := 0
	var lines []string
	var current strings.Builder
	for _, ch := range hint {
		switch ch {
		case '(', '[', '{':
			depth++
			current.WriteRune(ch)
		case ')', ']', '}':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				lines = append(lines, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, strings.TrimSpace(current.String()))
	}
	return strings.Join(lines, "\n")
}

// sendMsgOrReply 发送消息或引用回复。replyTo 非空时使用 Reply API，否则用 Create API。
func (f *FeishuChannel) sendMsgOrReply(ctx context.Context, receiveIDType, receiveID, msgType, content, replyTo string) error {
	f.Logger.Info("飞书 sendMsgOrReply",
		zap.String("receive_id_type", receiveIDType),
		zap.String("receive_id", receiveID),
		zap.String("msg_type", msgType),
		zap.String("reply_to", replyTo),
		zap.Int("content_len", len(content)),
	)
	if replyTo != "" {
		// 使用 Reply API 引用回复
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(replyTo).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(msgType).
				Content(content).
				Build()).
			Build()

		resp, err := f.client.Im.Message.Reply(ctx, req)
		if err != nil {
			return fmt.Errorf("飞书回复消息失败: %w", err)
		}
		if resp != nil && !resp.Success() {
			return fmt.Errorf("飞书回复消息失败: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return nil
	}

	// 使用 Create API 直接发送
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	resp, err := f.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("飞书发送消息失败: %w", err)
	}
	if resp != nil && !resp.Success() {
		return fmt.Errorf("飞书发送消息失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// addReaction adds an emoji reaction to a message.
func (f *FeishuChannel) addReaction(ctx context.Context, messageID, emojiType string) {
	if f.client == nil || messageID == "" || emojiType == "" {
		return
	}
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	_, err := f.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		f.Logger.Debug("飞书添加反应失败", zap.String("message_id", messageID), zap.Error(err))
	}
}

// sendToolHintCard sends a lightweight card for tool execution hints.
// 与 Python _send_tool_hint_card 保持一致：使用 markdown code block 格式。
func (f *FeishuChannel) sendToolHintCard(ctx context.Context, receiveIDType, chatID, content string) {
	formatted := formatToolHintLines(content)
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": []map[string]any{
			{
				"tag":     "markdown",
				"content": fmt.Sprintf("**Tool Calls**\n\n```text\n%s\n```", formatted),
			},
		},
	}
	cardJSON, _ := json.Marshal(card)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("interactive").
			Content(string(cardJSON)).
			Build()).
		Build()

	_, err := f.client.Im.Message.Create(ctx, req)
	if err != nil {
		f.Logger.Debug("飞书发送工具提示失败", zap.Error(err))
	}
}
