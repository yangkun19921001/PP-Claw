package tui

import "time"

// MsgKind 消息类型
type MsgKind int

const (
	MsgUser      MsgKind = iota // 用户消息
	MsgAssistant                // 助手回复
	MsgToolGroup                // 工具调用组
	MsgSystem                   // 系统消息
)

// ChatMessage 聊天消息
type ChatMessage struct {
	Kind        MsgKind
	Content     string
	Timestamp   time.Time
	Tools       []ToolBlock // 仅 MsgToolGroup 使用
	IsStreaming bool        // 流式输出中（未完成的 assistant 消息）
}

// textSelection 鼠标选区状态（坐标为 viewport 内容行/列）
type textSelection struct {
	active bool
	startX int // 起始列（可见字符位置）
	startY int // 起始行（内容行索引，含滚动偏移）
	endX   int
	endY   int
}

// ToolBlock 单个工具执行块
type ToolBlock struct {
	CallID        string
	Name          string
	Args          string
	Status        string // "running", "done", "error"
	ResultPreview string
	DurationMs    int64
}
