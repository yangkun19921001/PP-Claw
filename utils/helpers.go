package utils

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureDir 确保目录存在 (对标 utils/helpers.py:ensure_dir)
func EnsureDir(path string) string {
	os.MkdirAll(path, 0755)
	return path
}

// GetDataPath 获取 nanobot 数据目录 (~/.nanobot)
func GetDataPath() string {
	home, _ := os.UserHomeDir()
	return EnsureDir(filepath.Join(home, ".nanobot"))
}

// GetWorkspacePath 获取 workspace 路径
func GetWorkspacePath(workspace string) string {
	if workspace != "" {
		path := ExpandHome(workspace)
		return EnsureDir(path)
	}
	home, _ := os.UserHomeDir()
	return EnsureDir(filepath.Join(home, ".nanobot", "workspace"))
}

// GetSessionsPath 获取 sessions 存储目录
func GetSessionsPath() string {
	return EnsureDir(filepath.Join(GetDataPath(), "sessions"))
}

// GetSkillsPath 获取 skills 目录
func GetSkillsPath(workspace string) string {
	if workspace == "" {
		workspace = GetWorkspacePath("")
	}
	return EnsureDir(filepath.Join(workspace, "skills"))
}

// Timestamp 获取当前时间戳 (ISO格式)
func Timestamp() string {
	return time.Now().Format(time.RFC3339)
}

// TruncateString 截断字符串
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	suffix := "..."
	return s[:maxLen-len(suffix)] + suffix
}

// SafeFilename 将字符串转为安全文件名
func SafeFilename(name string) string {
	unsafe := `<>:"/\|?*`
	for _, c := range unsafe {
		name = strings.ReplaceAll(name, string(c), "_")
	}
	return strings.TrimSpace(name)
}

// ParseSessionKey 解析 session key 为 channel + chat_id
func ParseSessionKey(key string) (string, string, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return "", "", &InvalidSessionKeyError{Key: key}
	}
	return parts[0], parts[1], nil
}

// InvalidSessionKeyError 无效 session key 错误
type InvalidSessionKeyError struct {
	Key string
}

func (e *InvalidSessionKeyError) Error() string {
	return "invalid session key: " + e.Key
}

// ExpandHome 展开 ~ 为用户主目录
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}
	return path
}
