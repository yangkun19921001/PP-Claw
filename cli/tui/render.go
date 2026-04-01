package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// renderHeader 渲染头部栏
func renderHeader(modelName, channelsSummary string, width int) string {
	brand := headerBrandStyle.Render("🦐 PP-Claw")
	model := modelName
	if model == "" {
		model = "unknown"
	}
	modelTag := headerModelStyle.Render("⟨ " + model + " ⟩")

	brandW := lipgloss.Width(brand)
	modelW := lipgloss.Width(modelTag)
	gap := width - brandW - modelW - 2 // padding
	if gap < 1 {
		gap = 1
	}

	line1 := brand + strings.Repeat(" ", gap) + modelTag

	if channelsSummary == "" {
		return headerStyle.Width(width).Render(line1)
	}

	// 第二行：渠道在线摘要
	chLine := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(channelsSummary)
	return headerStyle.Width(width).Render(line1 + "\n" + chLine)
}

// renderStatusBar 渲染动态状态栏
func renderStatusBar(width int, isSubmitting bool, scrollPct int, atBottom bool, copyFlash string) string {
	// 左侧快捷键提示
	var left string
	if copyFlash != "" {
		left = lipgloss.NewStyle().Foreground(colorSuccess).Render(copyFlash)
	} else if isSubmitting {
		left = "⏎ interrupt  ^C quit  ^Y copy"
	} else {
		left = "⏎ send  ⌥⏎ newline  ^C quit  ^Y copy"
	}

	// 右侧滚动指示
	var right string
	if !atBottom {
		right = fmt.Sprintf("↓ new  %d%%", scrollPct)
	} else if scrollPct > 0 && scrollPct < 100 {
		right = fmt.Sprintf("%d%%", scrollPct)
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	content := left + strings.Repeat(" ", gap) + right
	return statusBarStyle.Width(width).Render(content)
}

// renderWelcome 渲染欢迎屏
func renderWelcome(width int) string {
	logo := welcomeBrandStyle.Render("🦐 PP-Claw")
	hint := welcomeHintStyle.Render("Type a message to get started")
	keys := welcomeHintStyle.Render("⏎ send  ⌥⏎ newline  ^C quit")

	block := lipgloss.JoinVertical(lipgloss.Center, "", logo, "", hint, keys, "")
	return lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Center, block)
}

// renderMessages 渲染所有消息
func renderMessages(messages []ChatMessage, glamourParser *glamour.TermRenderer, width int, spinnerFrame string) string {
	var parts []string
	for i, msg := range messages {
		// 首条 system 消息替换为欢迎屏
		if i == 0 && msg.Kind == MsgSystem {
			parts = append(parts, renderWelcome(width))
			continue
		}
		rendered := renderMessage(msg, glamourParser, width, spinnerFrame)
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n\n")
}

// renderMessage 渲染单条消息
func renderMessage(msg ChatMessage, glamourParser *glamour.TermRenderer, width int, spinnerFrame string) string {
	// 可用内容宽度 = 总宽 - 边框左侧宽度
	contentWidth := width - userBorderStyle.GetHorizontalFrameSize()
	if contentWidth < 20 {
		contentWidth = 20
	}

	switch msg.Kind {
	case MsgUser:
		label := userLabelStyle.Render("You")
		// 应用宽度限制实现自动换行
		wrapped := lipgloss.NewStyle().Width(contentWidth).Render(msg.Content)
		content := userBorderStyle.Render(wrapped)
		return label + "\n" + content

	case MsgAssistant:
		label := assistantLabelStyle.Render("皮皮虾")
		var rendered string
		if msg.IsStreaming {
			// 流式输出：显示原始文本 + 光标动画，不走 glamour
			rendered = msg.Content + "█"
		} else if glamourParser != nil {
			if out, err := glamourParser.Render(msg.Content); err == nil {
				rendered = strings.TrimSpace(out)
			} else {
				rendered = msg.Content
			}
		} else {
			rendered = msg.Content
		}
		content := assistantBorderStyle.Render(rendered)
		return label + "\n" + content

	case MsgToolGroup:
		availWidth := width - toolBlockStyle.GetHorizontalFrameSize()
		if availWidth < 20 {
			availWidth = 20
		}
		var toolLines []string
		for _, tb := range msg.Tools {
			toolLines = append(toolLines, renderToolBlock(tb, spinnerFrame, availWidth))
		}
		return strings.Join(toolLines, "\n")

	case MsgSystem:
		return systemStyle.PaddingLeft(2).Render(msg.Content)
	}
	return ""
}

// renderToolBlock 渲染单个工具块
func renderToolBlock(tb ToolBlock, spinnerFrame string, availWidth int) string {
	var icon string
	switch tb.Status {
	case "running":
		icon = spinnerFrame
	case "done":
		icon = successIcon
	case "error":
		icon = errorIcon
	default:
		icon = spinnerFrame
	}

	// 工具名 + 参数预览
	nameStr := toolNameStyle.Render(tb.Name)

	argPreview := extractFirstArg(tb.Args)
	var sig string
	if argPreview != "" {
		sig = fmt.Sprintf("%s(\"%s\")", nameStr, argPreview)
	} else {
		sig = nameStr + "()"
	}

	// 耗时
	var durationStr string
	if tb.Status != "running" && tb.DurationMs > 0 {
		if tb.DurationMs >= 1000 {
			durationStr = durationStyle.Render(fmt.Sprintf(" [%.1fs]", float64(tb.DurationMs)/1000))
		} else {
			durationStr = durationStyle.Render(fmt.Sprintf(" [%dms]", tb.DurationMs))
		}
	}

	line := fmt.Sprintf("%s %s%s", icon, sig, durationStr)

	// 结果预览（仅在完成时显示）
	var preview string
	if tb.Status != "running" && tb.ResultPreview != "" {
		lines := strings.SplitN(tb.ResultPreview, "\n", 3)
		if len(lines) > 2 {
			lines = lines[:2]
		}
		previewText := strings.Join(lines, "\n")
		if len(previewText) > 120 {
			previewText = previewText[:120] + "..."
		}
		preview = "\n" + previewStyle.Render("  "+previewText)
	}

	return toolBlockStyle.Width(availWidth).Render(line + preview)
}

// extractFirstArg 从 JSON 参数中提取第一个字符串值作为预览
// 优先查找常用 key，fallback 按字母排序
func extractFirstArg(args string) string {
	if args == "" {
		return ""
	}
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
		return ""
	}

	// 优先查找常用 key
	preferredKeys := []string{"command", "path", "file_path", "query", "url", "pattern", "name"}
	for _, k := range preferredKeys {
		if v, ok := argsMap[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				if len(s) > 60 {
					s = s[:60] + "..."
				}
				return s
			}
		}
	}

	// fallback: 按字母排序取第一个字符串值
	keys := make([]string, 0, len(argsMap))
	for k := range argsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := argsMap[k].(string); ok && s != "" {
			if len(s) > 60 {
				s = s[:60] + "..."
			}
			return s
		}
	}
	return ""
}

// ── 鼠标选区相关 ─────────────────────────────────────────

// stripANSI 移除 ANSI 转义序列，返回纯文本
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// normalizeSelection 确保 start 在 end 之前
func normalizeSelection(sel textSelection) (startY, startX, endY, endX int) {
	if sel.startY < sel.endY || (sel.startY == sel.endY && sel.startX <= sel.endX) {
		return sel.startY, sel.startX, sel.endY, sel.endX
	}
	return sel.endY, sel.endX, sel.startY, sel.startX
}

// extractSelectedText 从渲染内容中提取选区纯文本
func extractSelectedText(content string, sel textSelection) string {
	lines := strings.Split(content, "\n")
	startY, startX, endY, endX := normalizeSelection(sel)

	if startY == endY && startX == endX {
		return ""
	}

	var result []string
	for y := startY; y <= endY; y++ {
		if y < 0 || y >= len(lines) {
			continue
		}
		plain := stripANSI(lines[y])
		runes := []rune(plain)

		from, to := 0, len(runes)
		if y == startY && y == endY {
			from, to = startX, endX
		} else if y == startY {
			from = startX
		} else if y == endY {
			to = endX
		}

		if from < 0 {
			from = 0
		}
		if to > len(runes) {
			to = len(runes)
		}
		if from > to {
			from = to
		}
		result = append(result, string(runes[from:to]))
	}
	return strings.Join(result, "\n")
}

// applySelectionHighlight 在渲染内容上叠加反显高亮
func applySelectionHighlight(content string, sel textSelection) string {
	lines := strings.Split(content, "\n")
	startY, startX, endY, endX := normalizeSelection(sel)

	if startY == endY && startX == endX {
		return content
	}

	for y := startY; y <= endY; y++ {
		if y < 0 || y >= len(lines) {
			continue
		}
		var from, to int
		if y == startY && y == endY {
			from, to = startX, endX
		} else if y == startY {
			from, to = startX, 999999
		} else if y == endY {
			from, to = 0, endX
		} else {
			from, to = 0, 999999
		}
		lines[y] = highlightLineRange(lines[y], from, to)
	}
	return strings.Join(lines, "\n")
}

// highlightLineRange 在带 ANSI 的行中，对可见字符位置 [from, to) 应用反显
func highlightLineRange(line string, from, to int) string {
	if from >= to {
		return line
	}

	var result strings.Builder
	result.Grow(len(line) + 20)
	visiblePos := 0
	inEscape := false
	highlighted := false

	for _, r := range line {
		if inEscape {
			result.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
				// reset 序列后重新开启反显，避免中途被取消
				if highlighted {
					result.WriteString("\x1b[7m")
				}
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			result.WriteRune(r)
			continue
		}

		if visiblePos == from && !highlighted {
			result.WriteString("\x1b[7m")
			highlighted = true
		}
		if visiblePos == to && highlighted {
			result.WriteString("\x1b[27m")
			highlighted = false
		}

		result.WriteRune(r)
		visiblePos++
	}

	if highlighted {
		result.WriteString("\x1b[27m")
	}

	return result.String()
}
