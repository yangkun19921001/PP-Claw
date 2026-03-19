package tui

import (
	"strings"

	"github.com/charmbracelet/huh"
)

// ProviderOption 定义选填的 Provider 列表供表单使用
type ProviderOption struct {
	Name  string
	Label string
	Hint  string
}

// RunOnboardForm 启动一个漂亮的交互式配置引导表单
func RunOnboardForm(providers []ProviderOption) (string, string, string, string, error) {
	var (
		providerName string
		apiKey       string
		baseURL      string
		model        string
	)

	var options []huh.Option[string]
	for _, p := range providers {
		label := p.Label
		if p.Hint != "" {
			label += " (" + p.Hint + ")"
		}
		options = append(options, huh.NewOption(label, p.Name))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select your LLM provider").
				Options(options...).
				Value(&providerName),
		),
		// Group 2 depends on provider selection (logical split, huh supports consecutive groups seamlessly)
		huh.NewGroup(
			huh.NewInput().
				Title("Enter API Key").
				Description("(Optional) You can set it later in ~/.pp-claw/pp-claw.yaml").
				Value(&apiKey),
			huh.NewInput().
				Title("Enter API Base URL").
				Description("(Optional) Leave empty to use provider default").
				Value(&baseURL),
			huh.NewInput().
				Title("Enter Model Name").
				Description("Leave empty to use the provider's default recommended model").
				Value(&model),
		),
	).WithTheme(huh.ThemeBase16())

	err := form.Run()
	if err != nil {
		return "", "", "", "", err
	}

	return strings.TrimSpace(providerName), strings.TrimSpace(apiKey), strings.TrimSpace(baseURL), strings.TrimSpace(model), nil
}
