package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yangkun19921001/PP-Claw/config"
)

// modelSelectedMsg 用户选择了一个模型
type modelSelectedMsg struct {
	model string
}

// modelPickerDismissedMsg 用户关闭了 picker
type modelPickerDismissedMsg struct{}

// modelPickerModel 模型选择器子模型
type modelPickerModel struct {
	models       []config.ModelOption
	filtered     []int // 过滤后的索引
	cursor       int
	filter       string
	currentModel string
	width        int
	height       int
}

func newModelPicker(models []config.ModelOption, currentModel string, width, height int) modelPickerModel {
	m := modelPickerModel{
		models:       models,
		currentModel: currentModel,
		width:        width,
		height:       height,
	}
	m.applyFilter()
	return m
}

func (m *modelPickerModel) applyFilter() {
	m.filtered = nil
	for i, opt := range m.models {
		label := m.formatLabel(opt)
		if m.filter == "" || strings.Contains(strings.ToLower(label), strings.ToLower(m.filter)) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *modelPickerModel) formatLabel(opt config.ModelOption) string {
	if opt.Provider != "" {
		return fmt.Sprintf("%s / %s", opt.Provider, opt.Model)
	}
	return opt.Model
}

// Update 处理 picker 内部按键
func (m *modelPickerModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			return func() tea.Msg { return modelPickerDismissedMsg{} }

		case tea.KeyEnter:
			if len(m.filtered) > 0 {
				selected := m.models[m.filtered[m.cursor]]
				return func() tea.Msg { return modelSelectedMsg{model: selected.Model} }
			}
			return func() tea.Msg { return modelPickerDismissedMsg{} }

		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}

		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}

		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}

		case tea.KeyRunes:
			m.filter += string(msg.Runes)
			m.applyFilter()
		}
	}
	return nil
}

// View 渲染 picker 浮层
func (m *modelPickerModel) View() string {
	// 内容区宽度
	innerWidth := 50
	if m.width > 0 && m.width < innerWidth+6 {
		innerWidth = m.width - 6
	}
	if innerWidth < 30 {
		innerWidth = 30
	}

	// 标题
	title := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true).
		Render("Switch Model")

	// 搜索框
	filterDisplay := m.filter
	if filterDisplay == "" {
		filterDisplay = lipgloss.NewStyle().Foreground(colorDim).Render("type to filter...")
	}
	searchLine := fmt.Sprintf("🔍 %s", filterDisplay)

	// 分隔线
	separator := lipgloss.NewStyle().
		Foreground(colorDim).
		Render(strings.Repeat("─", innerWidth))

	// 列表项
	maxVisible := 10
	if m.height > 0 {
		maxVisible = m.height/2 - 4
		if maxVisible < 5 {
			maxVisible = 5
		}
	}

	// 计算滚动偏移
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	var items []string
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		opt := m.models[idx]
		label := m.formatLabel(opt)

		// 截断过长的标签
		if len(label) > innerWidth-4 {
			label = label[:innerWidth-7] + "..."
		}

		isCurrent := opt.Model == m.currentModel
		isSelected := i == m.cursor

		var line string
		switch {
		case isSelected && isCurrent:
			line = lipgloss.NewStyle().
				Foreground(colorBrand).Bold(true).
				Render(fmt.Sprintf("▸ %s ✓", label))
		case isSelected:
			line = lipgloss.NewStyle().
				Foreground(colorText).Bold(true).
				Render(fmt.Sprintf("▸ %s", label))
		case isCurrent:
			line = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Render(fmt.Sprintf("  %s ✓", label))
		default:
			line = lipgloss.NewStyle().
				Foreground(colorSubtext).
				Render(fmt.Sprintf("  %s", label))
		}
		items = append(items, line)
	}

	if len(m.filtered) == 0 {
		items = append(items, lipgloss.NewStyle().
			Foreground(colorDim).Italic(true).
			Render("  No matching models"))
	}

	listContent := strings.Join(items, "\n")

	// 底部提示
	hint := lipgloss.NewStyle().
		Foreground(colorDim).
		Render("↑↓ navigate  ⏎ select  ESC close")

	// 计算滚动指示
	scrollHint := ""
	if len(m.filtered) > maxVisible {
		scrollHint = lipgloss.NewStyle().
			Foreground(colorDim).
			Render(fmt.Sprintf(" (%d/%d)", m.cursor+1, len(m.filtered)))
	}

	// 组装内容
	content := lipgloss.JoinVertical(lipgloss.Left,
		title+scrollHint,
		"",
		searchLine,
		separator,
		listContent,
		separator,
		hint,
	)

	// 外框样式
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBrand).
		Padding(1, 2).
		Width(innerWidth + 4)

	box := boxStyle.Render(content)

	// 居中放置
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}
