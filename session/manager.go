package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Session 会话 (对标 nanobot/session/manager.py:Session)
type Session struct {
	Key              string           `json:"key"`
	Messages         []map[string]any `json:"messages"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	LastConsolidated int              `json:"last_consolidated"`
}

// AddMessage 添加消息
func (s *Session) AddMessage(role, content string) {
	s.Messages = append(s.Messages, map[string]any{
		"role":      role,
		"content":   content,
		"timestamp": time.Now().Format(time.RFC3339),
	})
	s.UpdatedAt = time.Now()
}

// GetHistory 获取历史消息 (最近 N 条)
func (s *Session) GetHistory(maxMessages int) []map[string]any {
	if len(s.Messages) <= maxMessages {
		return s.Messages
	}
	return s.Messages[len(s.Messages)-maxMessages:]
}

// Clear 清空会话
func (s *Session) Clear() {
	s.Messages = nil
	s.LastConsolidated = 0
	s.UpdatedAt = time.Now()
}

// Manager 会话管理器 (对标 nanobot/session/manager.py:SessionManager)
type Manager struct {
	workspace string
	sessions  map[string]*Session
	mu        sync.RWMutex
}

// NewManager 创建会话管理器
func NewManager(workspace string) *Manager {
	return &Manager{
		workspace: workspace,
		sessions:  make(map[string]*Session),
	}
}

// GetOrCreate 获取或创建会话
func (m *Manager) GetOrCreate(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		return s
	}

	// 尝试从磁盘加载
	s := m.loadFromDisk(key)
	if s == nil {
		s = &Session{
			Key:       key,
			Messages:  []map[string]any{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}
	m.sessions[key] = s
	return s
}

// Save 保存会话到磁盘 (JSONL 格式)
func (m *Manager) Save(s *Session) {
	m.mu.Lock()
	m.sessions[s.Key] = s
	m.mu.Unlock()

	// 异步写入磁盘
	go m.saveToDisk(s)
}

// Invalidate 使缓存失效
func (m *Manager) Invalidate(key string) {
	m.mu.Lock()
	delete(m.sessions, key)
	m.mu.Unlock()
}

// saveToDisk 保存到磁盘
func (m *Manager) saveToDisk(s *Session) {
	dir := filepath.Join(m.workspace, "sessions")
	os.MkdirAll(dir, 0755)

	// 使用 session key 作为文件名 (替换 : 为 _)
	filename := strings.ReplaceAll(s.Key, ":", "_") + ".jsonl"
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	for _, msg := range s.Messages {
		data, _ := json.Marshal(msg)
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

// loadFromDisk 从磁盘加载
func (m *Manager) loadFromDisk(key string) *Session {
	filename := strings.ReplaceAll(key, ":", "_") + ".jsonl"
	path := filepath.Join(m.workspace, "sessions", filename)

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	s := &Session{
		Key:       key,
		Messages:  []map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	scanner := json.NewDecoder(f)
	for {
		var msg map[string]any
		if err := scanner.Decode(&msg); err != nil {
			break
		}
		s.Messages = append(s.Messages, msg)
	}

	if len(s.Messages) == 0 {
		return nil
	}
	return s
}

// SessionCount 返回活跃会话数
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ListSessions 列出所有会话文件 (对标 session/manager.py:list_sessions)
func (m *Manager) ListSessions() []map[string]string {
	dir := filepath.Join(m.workspace, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var sessions []map[string]string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".jsonl")
		key = strings.ReplaceAll(key, "_", ":")
		info, _ := e.Info()
		updatedAt := ""
		if info != nil {
			updatedAt = info.ModTime().Format(time.RFC3339)
		}
		sessions = append(sessions, map[string]string{
			"key":        key,
			"updated_at": updatedAt,
		})
	}
	return sessions
}
