package tui

import "github.com/charmbracelet/lipgloss"

// Tokyo Night color palette
const (
	colorBrand    = lipgloss.Color("#7aa2f7") // 蓝色品牌色
	colorUser     = lipgloss.Color("#bb9af7") // 紫色用户
	colorAssist   = lipgloss.Color("#7dcfff") // 青蓝色助手
	colorMuted    = lipgloss.Color("#565f89") // 暗灰
	colorDim      = lipgloss.Color("#414868") // 更暗灰
	colorSuccess  = lipgloss.Color("#9ece6a") // 绿色
	colorError    = lipgloss.Color("#f7768e") // 红色
	colorWarning  = lipgloss.Color("#e0af68") // 黄色
	colorText     = lipgloss.Color("#c0caf5") // 主文本
	colorSubtext  = lipgloss.Color("#a9b1d6") // 副文本
	colorBg       = lipgloss.Color("#1a1b26") // 深色背景
	colorToolBord = lipgloss.Color("#3b4261") // 工具边框
)

var (
	// Header bar
	headerStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorText).
			Padding(0, 1)

	headerBrandStyle = lipgloss.NewStyle().
				Background(colorBg).
				Foreground(colorBrand).
				Bold(true)

	headerModelStyle = lipgloss.NewStyle().
				Background(colorBg).
				Foreground(colorMuted)

	// User message
	userLabelStyle = lipgloss.NewStyle().
			Foreground(colorUser).
			Bold(true).
			PaddingLeft(2)
	userBorderStyle = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(colorUser).
			PaddingLeft(1)

	// Assistant message
	assistantLabelStyle = lipgloss.NewStyle().
				Foreground(colorAssist).
				Bold(true).
				PaddingLeft(2)
	assistantBorderStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(colorAssist).
				PaddingLeft(1)

	// Tool block
	toolBlockStyle = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorToolBord).
			PaddingLeft(1).
			MarginLeft(2)

	// Tool name
	toolNameStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	// Duration badge
	durationStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	// Success/Error icons
	successIcon = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	errorIcon   = lipgloss.NewStyle().Foreground(colorError).Render("✗")

	// Result preview
	previewStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Input area
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	// System message
	systemStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	// Welcome screen styles
	welcomeBrandStyle = lipgloss.NewStyle().
				Foreground(colorBrand).
				Bold(true)

	welcomeHintStyle = lipgloss.NewStyle().
				Foreground(colorMuted)
)
