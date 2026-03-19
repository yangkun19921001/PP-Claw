package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/yangkun19921001/PP-Claw/bus"
)

type chatModel struct {
	ctx           context.Context
	cancel        func()
	msgBus        *bus.MessageBus
	viewport      viewport.Model
	textarea      textarea.Model
	spinner       spinner.Model
	messages      []ChatMessage
	err           error
	width         int
	height        int
	isSubmitting  bool
	glamourParser *glamour.TermRenderer
	modelName     string

	// 工具追踪
	activeTools map[string]*ToolBlock

	// 智能滚动
	userScrolledUp bool

	// 流式显示
	streamingMsgIdx int // 当前流式消息索引，-1 表示无

	// 鼠标选区
	sel         textSelection
	lastContent string // 缓存渲染内容，用于文本提取

	// 复制反馈
	copyFlash     string    // 短暂的复制状态提示
	copyFlashTime time.Time // 闪烁开始时间

	// 接收通道
	subChan   <-chan *bus.OutboundMessage
	unsubFunc func()
}

// 消息包装：用于在 Update 循环中传递从 msgBus 接收的数据
type busMsgMsg struct {
	msg *bus.OutboundMessage
}

// spinnerTickMsg 用于触发 spinner 动画更新
type spinnerTickMsg struct{}

// clearCopyFlashMsg 清除复制提示
type clearCopyFlashMsg struct{}

// copyToClipboard 将文本复制到系统剪贴板
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// 优先 xclip，fallback xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// lastAssistantContent 返回最后一条 assistant 消息的原始内容
func (m *chatModel) lastAssistantContent() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Kind == MsgAssistant && m.messages[i].Content != "" {
			return m.messages[i].Content
		}
	}
	return ""
}

func initialModel(ctx context.Context, msgBus *bus.MessageBus, cancel func(), modelName string) *chatModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.Prompt = "  "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(78)
	ta.SetHeight(3)
	ta.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	// 去掉输入框的背景色
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorWarning)

	vp := viewport.New(80, 10)

	tr, _ := glamour.NewTermRenderer(
		glamour.WithStyles(styles.DarkStyleConfig),
		glamour.WithWordWrap(78),
	)

	sub, unsub := msgBus.SubscribeOutbound()

	m := &chatModel{
		ctx:           ctx,
		cancel:        cancel,
		msgBus:        msgBus,
		textarea:      ta,
		spinner:       sp,
		viewport:      vp,
		modelName:     modelName,
		messages: []ChatMessage{
			{Kind: MsgSystem, Content: "pp-claw ready! Type your message.", Timestamp: time.Now()},
		},
		glamourParser:   tr,
		activeTools:     make(map[string]*ToolBlock),
		streamingMsgIdx: -1,
		subChan:         sub,
		unsubFunc:       unsub,
	}

	// 初始化 lastContent，确保鼠标选区在 WindowSizeMsg 之前也有内容可用
	m.lastContent = renderMessages(m.messages, tr, 80, "")

	return m
}

func (m *chatModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.waitForMessage(),
		m.spinner.Tick,
	)
}

// 持续监听来自核心逻辑的输出消息（循环代替递归，避免栈溢出）
func (m *chatModel) waitForMessage() tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case <-m.ctx.Done():
				return nil
			case msg, ok := <-m.subChan:
				if !ok {
					return nil
				}
				if msg.Channel != "cli" {
					continue // 循环代替递归
				}
				return busMsgMsg{msg: msg}
			}
		}
	}
}

func (m *chatModel) refreshViewport() {
	content := renderMessages(m.messages, m.glamourParser, m.width, m.spinner.View())
	m.lastContent = content
	if m.sel.active {
		content = applySelectionHighlight(content, m.sel)
	}
	m.viewport.SetContent(content)
	if !m.userScrolledUp {
		m.viewport.GotoBottom()
	}
}

// refreshSelection 仅更新选区高亮，不重新渲染消息（拖拽时性能优化）
func (m *chatModel) refreshSelection() {
	content := m.lastContent
	if m.sel.active {
		content = applySelectionHighlight(content, m.sel)
	}
	m.viewport.SetContent(content)
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	// 记录滚动前的位置
	wasAtBottom := m.viewport.AtBottom()

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.spinner, spCmd = m.spinner.Update(msg)

	// 检测用户是否手动滚动离开底部
	if !m.viewport.AtBottom() && wasAtBottom {
		// viewport 更新后不再在底部，说明用户向上滚动了
	}
	if !m.viewport.AtBottom() {
		m.userScrolledUp = true
	} else {
		m.userScrolledUp = false
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Layout: header(1) + \n + viewport + \n + inputBox(5) + \n + status(1)
		// 3 个 \n 连接符
		headerH := 1
		inputH := 5
		statusH := 1
		joinNewlines := 3
		vpHeight := msg.Height - headerH - inputH - statusH - joinNewlines
		if vpHeight < 3 {
			vpHeight = 3
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = vpHeight

		// 输入框宽度：总宽 - 边框水平占用
		inputInnerWidth := msg.Width - inputBoxStyle.GetHorizontalFrameSize()
		if inputInnerWidth < 20 {
			inputInnerWidth = 20
		}
		m.textarea.SetWidth(inputInnerWidth - lipgloss.Width(m.textarea.Prompt))

		// glamour 换行宽度：总宽 - assistant 边框占用 - 安全余量
		glamourWrap := msg.Width - assistantBorderStyle.GetHorizontalFrameSize() - 2
		if glamourWrap < 40 {
			glamourWrap = 40
		}
		m.glamourParser, _ = glamour.NewTermRenderer(
			glamour.WithStyles(styles.DarkStyleConfig),
			glamour.WithWordWrap(glamourWrap),
		)
		m.refreshViewport()

	case clearCopyFlashMsg:
		m.copyFlash = ""

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancel()
			return m, tea.Quit

		case tea.KeyCtrlY:
			// 复制最后一条 assistant 消息到剪贴板
			if content := m.lastAssistantContent(); content != "" {
				if err := copyToClipboard(content); err == nil {
					m.copyFlash = "Copied!"
				} else {
					m.copyFlash = "Copy failed"
				}
			} else {
				m.copyFlash = "Nothing to copy"
			}
			m.copyFlashTime = time.Now()
			cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
				return clearCopyFlashMsg{}
			}))

		case tea.KeyEnter:
			v := strings.TrimSpace(m.textarea.Value())
			if v == "" {
				m.textarea.Reset()
				break
			}
			if v == "exit" || v == "quit" {
				m.cancel()
				return m, tea.Quit
			}

			m.isSubmitting = true
			m.userScrolledUp = false // 发送消息时重置滚动状态
			m.messages = append(m.messages, ChatMessage{
				Kind:      MsgUser,
				Content:   v,
				Timestamp: time.Now(),
			})
			m.refreshViewport()

			inbound := bus.NewInboundMessage("cli", "user", "direct", v)
			m.msgBus.PublishInbound(inbound)
			m.textarea.Reset()
		}

	case tea.MouseMsg:
		// 动态计算 viewport 在屏幕上的起始行
		vpStart := lipgloss.Height(renderHeader(m.modelName, m.width))
		vpEnd := vpStart + m.viewport.Height

		switch {
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			// 仅在 viewport 区域内开始选区
			if msg.Y >= vpStart && msg.Y < vpEnd {
				contentY := (msg.Y - vpStart) + m.viewport.YOffset
				m.sel = textSelection{
					active: true,
					startX: msg.X, startY: contentY,
					endX: msg.X, endY: contentY,
				}
			}

		case msg.Action == tea.MouseActionMotion && m.sel.active:
			contentY := (msg.Y - vpStart) + m.viewport.YOffset
			if contentY < 0 {
				contentY = 0
			}
			m.sel.endX = msg.X
			m.sel.endY = contentY
			m.refreshSelection()

		case msg.Action == tea.MouseActionRelease && m.sel.active:
			contentY := (msg.Y - vpStart) + m.viewport.YOffset
			if contentY < 0 {
				contentY = 0
			}
			m.sel.endX = msg.X
			m.sel.endY = contentY

			text := extractSelectedText(m.lastContent, m.sel)
			m.sel.active = false

			if strings.TrimSpace(text) != "" {
				if err := copyToClipboard(text); err == nil {
					m.copyFlash = "Copied!"
				} else {
					m.copyFlash = "Copy failed"
				}
				m.copyFlashTime = time.Now()
				cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return clearCopyFlashMsg{}
				}))
			}
			m.refreshViewport()
		}

	case busMsgMsg:
		if msg.msg == nil {
			// bus 断开，安全重置 isSubmitting
			m.isSubmitting = false
		} else {
			m.handleBusMessage(msg.msg)
		}
		cmds = append(cmds, m.waitForMessage())

	case spinner.TickMsg:
		// 有活跃工具或流式消息时刷新 viewport 以更新动画
		if len(m.activeTools) > 0 || m.streamingMsgIdx >= 0 {
			m.refreshViewport()
		}
	}

	cmds = append(cmds, tiCmd, vpCmd, spCmd)
	return m, tea.Batch(cmds...)
}

// handleBusMessage 处理来自 agent 的消息
func (m *chatModel) handleBusMessage(out *bus.OutboundMessage) {
	isProgress, _ := out.Metadata["_progress"].(bool)

	if !isProgress {
		// 最终回复
		m.isSubmitting = false

		if out.Content != "" {
			// 如果有流式消息，替换其内容并标记完成
			if m.streamingMsgIdx >= 0 && m.streamingMsgIdx < len(m.messages) {
				m.messages[m.streamingMsgIdx].Content = out.Content
				m.messages[m.streamingMsgIdx].IsStreaming = false
				m.streamingMsgIdx = -1
			} else {
				m.messages = append(m.messages, ChatMessage{
					Kind:      MsgAssistant,
					Content:   out.Content,
					Timestamp: time.Now(),
				})
			}
		} else if m.streamingMsgIdx >= 0 && m.streamingMsgIdx < len(m.messages) {
			// 最终回复为空但有流式消息，标记完成
			m.messages[m.streamingMsgIdx].IsStreaming = false
			m.streamingMsgIdx = -1
		}
		m.refreshViewport()
		return
	}

	// 进度消息
	toolStatus, _ := out.Metadata["_tool_status"].(string)
	toolCallID, _ := out.Metadata["_tool_call_id"].(string)
	toolName, _ := out.Metadata["_tool_name"].(string)
	toolArgs, _ := out.Metadata["_tool_args"].(string)

	lookupKey := toolCallID
	if lookupKey == "" {
		lookupKey = toolName
	}

	if toolStatus != "" {
		switch toolStatus {
		case "running":
			// 工具开始时，如果有活跃的流式消息，先终结它
			// 否则后续回复会覆盖工具组之前的位置，导致消息顺序错乱
			if m.streamingMsgIdx >= 0 && m.streamingMsgIdx < len(m.messages) {
				m.messages[m.streamingMsgIdx].IsStreaming = false
				m.streamingMsgIdx = -1
			}

			tb := &ToolBlock{
				CallID: lookupKey,
				Name:   toolName,
				Args:   toolArgs,
				Status: "running",
			}
			m.activeTools[lookupKey] = tb

			if len(m.messages) > 0 && m.messages[len(m.messages)-1].Kind == MsgToolGroup {
				m.messages[len(m.messages)-1].Tools = append(m.messages[len(m.messages)-1].Tools, *tb)
			} else {
				m.messages = append(m.messages, ChatMessage{
					Kind:      MsgToolGroup,
					Timestamp: time.Now(),
					Tools:     []ToolBlock{*tb},
				})
			}

		case "done", "error":
			durationMs, _ := out.Metadata["_tool_duration_ms"].(int64)
			if durationMs == 0 {
				if f, ok := out.Metadata["_tool_duration_ms"].(float64); ok {
					durationMs = int64(f)
				}
			}
			resultPreview, _ := out.Metadata["_tool_result_preview"].(string)

			delete(m.activeTools, lookupKey)

			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Kind != MsgToolGroup {
					continue
				}
				for j := range m.messages[i].Tools {
					if m.messages[i].Tools[j].CallID == lookupKey {
						m.messages[i].Tools[j].Status = toolStatus
						m.messages[i].Tools[j].DurationMs = durationMs
						m.messages[i].Tools[j].ResultPreview = resultPreview
						break
					}
				}
				break
			}
		}
		m.refreshViewport()
		return
	}

	// 旧格式兼容
	isToolHint, _ := out.Metadata["_tool_hint"].(bool)
	if isToolHint {
		tb := ToolBlock{
			Name:   out.Content,
			Status: "running",
		}
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Kind == MsgToolGroup {
			m.messages[len(m.messages)-1].Tools = append(m.messages[len(m.messages)-1].Tools, tb)
		} else {
			m.messages = append(m.messages, ChatMessage{
				Kind:      MsgToolGroup,
				Timestamp: time.Now(),
				Tools:     []ToolBlock{tb},
			})
		}
	} else if out.Content != "" {
		// "thought" 进度文本 → 流式 assistant 消息
		if m.streamingMsgIdx >= 0 && m.streamingMsgIdx < len(m.messages) {
			// 追加到现有流式消息
			m.messages[m.streamingMsgIdx].Content = out.Content
		} else {
			// 创建新的流式 assistant 消息
			m.streamingMsgIdx = len(m.messages)
			m.messages = append(m.messages, ChatMessage{
				Kind:        MsgAssistant,
				Content:     out.Content,
				IsStreaming: true,
				Timestamp:   time.Now(),
			})
		}
	}
	m.refreshViewport()
}

func (m *chatModel) View() string {
	// Header
	header := renderHeader(m.modelName, m.width)

	// Viewport (chat messages)
	vpView := m.viewport.View()

	// Input area — 宽度使用精确计算
	inputWidth := m.width - inputBoxStyle.GetHorizontalFrameSize()
	if inputWidth < 20 {
		inputWidth = 20
	}
	var inputView string
	if m.isSubmitting {
		thinkingText := fmt.Sprintf(" %s Thinking...", m.spinner.View())
		inputView = inputBoxStyle.Width(inputWidth).Render(thinkingText)
	} else {
		inputView = inputBoxStyle.Width(inputWidth).Render(m.textarea.View())
	}

	// Status bar — 动态内容
	scrollPct := int(m.viewport.ScrollPercent() * 100)
	statusBar := renderStatusBar(m.width, m.isSubmitting, scrollPct, m.viewport.AtBottom(), m.copyFlash)

	return fmt.Sprintf("%s\n%s\n%s\n%s", header, vpView, inputView, statusBar)
}

// RunChat 启动 CLI 聊天 (阻塞直到退出)
func RunChat(ctx context.Context, msgBus *bus.MessageBus, cancel func(), modelName string) error {
	lipgloss.SetHasDarkBackground(true)

	m := initialModel(ctx, msgBus, cancel, modelName)
	defer m.unsubFunc()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
