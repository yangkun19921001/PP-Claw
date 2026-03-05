package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFileTool 读取文件 (对标 pp-claw/agent/tools/filesystem.py:ReadFileTool)
type ReadFileTool struct {
	Workspace  string
	AllowedDir string // 为空则不限制
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file." }
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read (relative to workspace or absolute)",
			},
		},
		"required": []any{"path"},
	}
}
func (t *ReadFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	fullPath := t.resolvePath(path)
	if t.AllowedDir != "" && !strings.HasPrefix(fullPath, t.AllowedDir) {
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}
func (t *ReadFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.Workspace, path)
}

// WriteFileTool 写入文件 (对标 pp-claw/agent/tools/filesystem.py:WriteFileTool)
type WriteFileTool struct {
	Workspace  string
	AllowedDir string
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file. Creates parent directories if needed."
}
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Path to write to"},
			"content": map[string]any{"type": "string", "description": "Content to write"},
		},
		"required": []any{"path", "content"},
	}
}
func (t *WriteFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	fullPath := t.resolvePath(path)
	if t.AllowedDir != "" && !strings.HasPrefix(fullPath, t.AllowedDir) {
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directories: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
}
func (t *WriteFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.Workspace, path)
}

// EditFileTool 编辑文件 (对标 pp-claw/agent/tools/filesystem.py:EditFileTool)
type EditFileTool struct {
	Workspace  string
	AllowedDir string
}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edit a file by replacing a specific string with new content."
}
func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "Path to the file"},
			"old_string": map[string]any{"type": "string", "description": "The exact string to find and replace"},
			"new_string": map[string]any{"type": "string", "description": "The replacement string"},
		},
		"required": []any{"path", "old_string", "new_string"},
	}
}
func (t *EditFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	oldStr, _ := params["old_string"].(string)
	newStr, _ := params["new_string"].(string)
	if path == "" || oldStr == "" {
		return "", fmt.Errorf("path and old_string are required")
	}
	fullPath := t.resolvePath(path)
	if t.AllowedDir != "" && !strings.HasPrefix(fullPath, t.AllowedDir) {
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		hint := notFoundMessage(oldStr, content, path)
		return "", fmt.Errorf("%s", hint)
	}
	count := strings.Count(content, oldStr)
	if count > 1 {
		return "", fmt.Errorf("old_string found %d times, must be unique (found in %d places)", count, count)
	}
	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully edited %s", path), nil
}
func (t *EditFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.Workspace, path)
}

// ListDirTool 列出目录 (对标 pp-claw/agent/tools/filesystem.py:ListDirTool)
type ListDirTool struct {
	Workspace  string
	AllowedDir string
}

func (t *ListDirTool) Name() string        { return "list_directory" }
func (t *ListDirTool) Description() string { return "List files and directories at the given path." }
func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to list (default: workspace root)"},
		},
	}
}
func (t *ListDirTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		path = t.Workspace
	}
	fullPath := t.resolvePath(path)
	if t.AllowedDir != "" && !strings.HasPrefix(fullPath, t.AllowedDir) {
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var sb strings.Builder
	for _, e := range entries {
		info, _ := e.Info()
		prefix := "📄"
		size := ""
		if e.IsDir() {
			prefix = "📁"
		} else if info != nil {
			size = fmt.Sprintf(" (%d bytes)", info.Size())
		}
		sb.WriteString(fmt.Sprintf("%s %s%s\n", prefix, e.Name(), size))
	}
	if sb.Len() == 0 {
		return "(empty directory)", nil
	}
	return sb.String(), nil
}
func (t *ListDirTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.Workspace, path)
}

// notFoundMessage 当 old_string 找不到时，尝试模糊匹配并生成提示
func notFoundMessage(oldText, content, path string) string {
	oldLines := strings.Split(oldText, "\n")
	contentLines := strings.Split(content, "\n")
	windowSize := len(oldLines)

	if windowSize == 0 || len(contentLines) == 0 {
		return "old_string not found in file. No similar text found. Verify the file content."
	}

	bestScore := 0.0
	bestStart := 0

	// 滑动窗口遍历文件内容，寻找最相似的片段
	for i := 0; i <= len(contentLines)-windowSize; i++ {
		window := contentLines[i : i+windowSize]
		score := lineSimilarity(oldLines, window)
		if score > bestScore {
			bestScore = score
			bestStart = i
		}
	}

	// 也检查窗口大小 ±1 的偏差
	for delta := -1; delta <= 1; delta++ {
		ws := windowSize + delta
		if ws <= 0 || ws > len(contentLines) {
			continue
		}
		for i := 0; i <= len(contentLines)-ws; i++ {
			window := contentLines[i : i+ws]
			score := lineSimilarityVar(oldLines, window)
			if score > bestScore {
				bestScore = score
				bestStart = i
			}
		}
	}

	if bestScore > 0.5 {
		// 生成 diff 提示
		endIdx := bestStart + windowSize
		if endIdx > len(contentLines) {
			endIdx = len(contentLines)
		}
		actualLines := contentLines[bestStart:endIdx]

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("old_string not found in file %s.\n", path))
		sb.WriteString(fmt.Sprintf("Best match (%.0f%% similar) found at lines %d-%d:\n\n",
			bestScore*100, bestStart+1, bestStart+len(actualLines)))
		sb.WriteString("--- old_string (provided)\n")
		sb.WriteString("+++ actual (in file)\n")

		maxLines := max(len(oldLines), len(actualLines))
		for i := 0; i < maxLines; i++ {
			oldLine := ""
			if i < len(oldLines) {
				oldLine = oldLines[i]
			}
			actLine := ""
			if i < len(actualLines) {
				actLine = actualLines[i]
			}
			if oldLine == actLine {
				sb.WriteString(fmt.Sprintf(" %s\n", oldLine))
			} else {
				if i < len(oldLines) {
					sb.WriteString(fmt.Sprintf("-%s\n", oldLine))
				}
				if i < len(actualLines) {
					sb.WriteString(fmt.Sprintf("+%s\n", actLine))
				}
			}
		}
		return sb.String()
	}

	return "old_string not found in file. No similar text found. Verify the file content."
}

// lineSimilarity 计算两组行之间的匹配行数作为相似度 (行数相同)
func lineSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	total := max(len(a), len(b))
	if total == 0 {
		return 0
	}
	matching := 0
	minLen := min(len(a), len(b))
	for i := 0; i < minLen; i++ {
		if strings.TrimSpace(a[i]) == strings.TrimSpace(b[i]) {
			matching++
		}
	}
	return float64(matching) / float64(total)
}

// lineSimilarityVar 计算两组行之间的相似度 (行数可能不同)
func lineSimilarityVar(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	total := max(len(a), len(b))
	if total == 0 {
		return 0
	}

	// 用简单的 LCS 计算公共行数
	matching := 0
	bUsed := make([]bool, len(b))
	for _, lineA := range a {
		ta := strings.TrimSpace(lineA)
		for j, lineB := range b {
			if !bUsed[j] && strings.TrimSpace(lineB) == ta {
				matching++
				bUsed[j] = true
				break
			}
		}
	}
	return float64(matching) / float64(total)
}

