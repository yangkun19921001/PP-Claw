//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"fmt"
	"strings"
)

// extractInteractiveContent recursively extracts text from a Feishu interactive card message.
func extractInteractiveContent(content map[string]any) string {
	var parts []string

	// Card title from header
	if header, ok := content["header"].(map[string]any); ok {
		if title, ok := header["title"].(map[string]any); ok {
			if text, ok := title["content"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}

	// Card body elements
	if body, ok := content["body"].(map[string]any); ok {
		if elements, ok := body["elements"].([]any); ok {
			for _, elem := range elements {
				if elemMap, ok := elem.(map[string]any); ok {
					parts = append(parts, extractElementContent(elemMap)...)
				}
			}
		}
	}

	// Legacy card format: direct "elements" at top level
	if elements, ok := content["elements"].([]any); ok {
		for _, elem := range elements {
			if elemMap, ok := elem.(map[string]any); ok {
				parts = append(parts, extractElementContent(elemMap)...)
			}
		}
	}

	return strings.Join(parts, "\n")
}

// extractElementContent extracts text from a single card element.
func extractElementContent(element map[string]any) []string {
	var results []string

	tag, _ := element["tag"].(string)
	switch tag {
	case "markdown", "lark_md":
		if content, ok := element["content"].(string); ok && content != "" {
			results = append(results, content)
		}
	case "plain_text":
		if content, ok := element["content"].(string); ok && content != "" {
			results = append(results, content)
		}
	case "div":
		if text, ok := element["text"].(map[string]any); ok {
			results = append(results, extractElementContent(text)...)
		}
		if fields, ok := element["fields"].([]any); ok {
			for _, field := range fields {
				if fm, ok := field.(map[string]any); ok {
					results = append(results, extractElementContent(fm)...)
				}
			}
		}
	case "a":
		text, _ := element["content"].(string)
		href, _ := element["href"].(string)
		if text != "" && href != "" {
			results = append(results, fmt.Sprintf("[%s](%s)", text, href))
		} else if text != "" {
			results = append(results, text)
		}
	case "button":
		if text, ok := element["text"].(map[string]any); ok {
			results = append(results, extractElementContent(text)...)
		}
	case "img":
		alt, _ := element["alt"].(map[string]any)
		if alt != nil {
			if content, ok := alt["content"].(string); ok && content != "" {
				results = append(results, fmt.Sprintf("[image: %s]", content))
			}
		} else {
			results = append(results, "[image]")
		}
	case "note":
		if elements, ok := element["elements"].([]any); ok {
			for _, e := range elements {
				if em, ok := e.(map[string]any); ok {
					results = append(results, extractElementContent(em)...)
				}
			}
		}
	case "column_set":
		if columns, ok := element["columns"].([]any); ok {
			for _, col := range columns {
				if cm, ok := col.(map[string]any); ok {
					if elements, ok := cm["elements"].([]any); ok {
						for _, e := range elements {
							if em, ok := e.(map[string]any); ok {
								results = append(results, extractElementContent(em)...)
							}
						}
					}
				}
			}
		}
	case "table":
		results = append(results, "[table]")
	case "hr":
		results = append(results, "---")
	default:
		// For unknown tags, try to extract any nested text/content
		if content, ok := element["content"].(string); ok && content != "" {
			results = append(results, content)
		}
		if text, ok := element["text"].(map[string]any); ok {
			results = append(results, extractElementContent(text)...)
		}
	}

	return results
}

// extractShareCardContent extracts text from share/system messages.
func extractShareCardContent(contentMap map[string]any, msgType string) string {
	switch msgType {
	case "share_chat":
		name, _ := contentMap["chat_name"].(string)
		if name == "" {
			name, _ = contentMap["name"].(string)
		}
		if name != "" {
			return fmt.Sprintf("[Shared group chat: %s]", name)
		}
		return "[Shared group chat]"

	case "share_user":
		name, _ := contentMap["name"].(string)
		userID, _ := contentMap["user_id"].(string)
		if name != "" {
			return fmt.Sprintf("[Shared contact: %s]", name)
		}
		if userID != "" {
			return fmt.Sprintf("[Shared contact: %s]", userID)
		}
		return "[Shared contact]"

	case "share_calendar_event":
		summary, _ := contentMap["summary"].(string)
		if summary != "" {
			return fmt.Sprintf("[Shared calendar event: %s]", summary)
		}
		return "[Shared calendar event]"

	case "system":
		// System events like user joined, etc.
		if text, ok := contentMap["text"].(string); ok && text != "" {
			return fmt.Sprintf("[System: %s]", text)
		}
		if typ, ok := contentMap["type"].(string); ok && typ != "" {
			return fmt.Sprintf("[System event: %s]", typ)
		}
		return "[System message]"

	case "merge_forward":
		return "[Merged forwarded messages]"

	case "sticker":
		if key, ok := contentMap["file_key"].(string); ok && key != "" {
			return fmt.Sprintf("[Sticker: %s]", key)
		}
		return "[Sticker]"

	default:
		return fmt.Sprintf("[%s message]", msgType)
	}
}
