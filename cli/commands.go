package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yangkun19921001/PP-Claw/agent"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/channels"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
	"github.com/yangkun19921001/PP-Claw/providers"
	"github.com/yangkun19921001/PP-Claw/session"
	"go.uber.org/zap"
)

var (
	cfgFile   string
	workspace string
)

// NewRootCmd 创建根命令 (对标 pp-claw/cli/commands.py)
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pp-claw",
		Short: "🦞 pp-claw - A helpful AI assistant",
		Long:  "pp-claw is a simple, transparent, and efficient AI assistant agent.",
	}

	root.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file path (default: ~/.pp-claw/pp-claw.yaml)")
	root.PersistentFlags().StringVarP(&workspace, "workspace", "w", "", "workspace directory")

	root.AddCommand(newGatewayCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newOnboardCmd())
	root.AddCommand(newAgentCmd())
	root.AddCommand(newChannelsCmd())
	root.AddCommand(newCronCmd())
	root.AddCommand(newStatusCmd())

	return root
}

// newGatewayCmd gateway 命令 — 启动 Agent
func newGatewayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start the pp-claw gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGateway()
		},
	}
}

// newVersionCmd version 命令
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show pp-claw version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("🦞 pp-claw v0.1.0")
		},
	}
}

// newOnboardCmd onboard 命令 — 交互式初始化
func newOnboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "onboard",
		Short: "Initialize pp-claw configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard()
		},
	}
}

// runGateway 启动 Gateway (对标 pp-claw/cli/commands.py gateway 命令)
func runGateway() error {
	// 初始化 Logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	logger.Info("🦞 pp-claw starting...")

	// 加载配置
	cfgPath := cfgFile
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.pp-claw/pp-claw.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 解析 workspace
	ws := workspace
	if ws == "" {
		ws = config.ExpandHome(cfg.Agents.Defaults.Workspace)
	}

	// 确保 workspace 存在
	os.MkdirAll(ws, 0755)
	os.MkdirAll(ws+"/memory", 0755)
	os.MkdirAll(ws+"/skills", 0755)
	os.MkdirAll(ws+"/sessions", 0755)

	logger.Info("Workspace", zap.String("path", ws))

	// 创建消息总线
	msgBus := bus.NewMessageBus()

	// 创建 Provider
	chatModel, err := providers.NewChatModel(logger, cfg)
	if err != nil {
		return fmt.Errorf("创建 Provider 失败: %w", err)
	}

	// 创建 Session Manager
	sessions := session.NewManager(ws)

	// 创建并启动 CronService
	cronSvc := cron.NewService(ws+"/data/cron/jobs.json", logger)
	cronSvc.SetOnJob(func(job *cron.CronJob) (string, error) {
		// 使用原始 channel 和 chatID，确保响应能路由回正确的渠道
		msgBus.PublishInbound(bus.NewInboundMessage(job.Payload.Channel, "cron", job.Payload.To, job.Payload.Message))
		return "Job dispatched to agent", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cronSvc.Start(ctx)

	// 创建 Agent Loop
	agentLoop, err := agent.NewAgentLoop(&agent.AgentLoopConfig{
		Bus:         msgBus,
		Config:      cfg,
		Workspace:   ws,
		Logger:      logger,
		Sessions:    sessions,
		ChatModel:   chatModel,
		CronService: cronSvc,
	})
	if err != nil {
		return fmt.Errorf("创建 Agent Loop 失败: %w", err)
	}

	// 启动 Agent Loop (协程)
	agentReady := make(chan struct{}, 1)
	go func() {
		agentReady <- struct{}{}
		if err := agentLoop.Run(ctx); err != nil {
			logger.Error("Agent loop 退出", zap.Error(err))
		}
	}()
	<-agentReady

	// 启动渠道管理器
	channelMgr := channels.NewManager(cfg, msgBus, logger)
	go func() {
		if err := channelMgr.StartAll(ctx); err != nil {
			logger.Error("渠道启动失败", zap.Error(err))
		}
	}()

	// 启动 CLI 渠道 (outbound 消费者)
	go cliOutboundHandler(ctx, msgBus, logger)

	// 等待启动日志刷出后再显示提示符
	time.Sleep(50 * time.Millisecond)

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// CLI 输入循环 (对标 pp-claw 的 CLI 渠道)
	fmt.Println("\n🦞 pp-claw ready! Type your message:")
	fmt.Print("\n> ")

	scanner := bufio.NewScanner(os.Stdin)
	inputChan := make(chan string)

	go func() {
		for scanner.Scan() {
			inputChan <- scanner.Text()
		}
		close(inputChan)
	}()

	for {
		select {
		case input, ok := <-inputChan:
			if !ok {
				// stdin EOF (e.g. Docker non-interactive mode)
				// 不退出，改为等待信号
				logger.Info("stdin closed, waiting for signal to shutdown...")
				<-sigChan
				fmt.Println("\n👋 Shutting down...")
				cronSvc.Stop()
				channelMgr.StopAll()
				agentLoop.Stop()
				cancel()
				return nil
			}
			input = strings.TrimSpace(input)
			if input == "" {
				fmt.Print("> ")
				continue
			}
			if input == "exit" || input == "quit" {
				fmt.Println("👋 Goodbye!")
				cancel()
				return nil
			}

			// 发送到消息总线
			msg := bus.NewInboundMessage("cli", "user", "direct", input)
			msgBus.PublishInbound(msg)

		case <-sigChan:
			fmt.Println("\n👋 Shutting down...")
			cronSvc.Stop()
			channelMgr.StopAll()
			agentLoop.Stop()
			cancel()
			return nil
		}
	}
}

// cliOutboundHandler CLI 出站消息处理
func cliOutboundHandler(ctx context.Context, msgBus *bus.MessageBus, logger *zap.Logger) {
	sub, unsub := msgBus.SubscribeOutbound()
	defer unsub()

	for {
		select {
		case msg := <-sub:
			// 只处理 CLI 渠道的消息
			if msg.Channel != "cli" {
				continue
			}

			// 跳过进度消息
			if isProgress, ok := msg.Metadata["_progress"].(bool); ok && isProgress {
				if isToolHint, ok := msg.Metadata["_tool_hint"].(bool); ok && isToolHint {
					fmt.Printf("  🔧 %s\n", msg.Content)
				} else if msg.Content != "" {
					fmt.Printf("  💭 %s\n", msg.Content)
				}
				continue
			}

			if msg.Content != "" {
				fmt.Printf("\n🤖 %s\n", msg.Content)
			}
			fmt.Print("\n> ")
		case <-ctx.Done():
			return
		}
	}
}

// onboardProviders 可选 Provider 列表（显示顺序）
var onboardProviders = []struct {
	number string
	name   string
	label  string
	hint   string // model 示例
}{
	{"1", "openai", "OpenAI", "gpt-4o"},
	{"2", "anthropic", "Anthropic", "claude-sonnet-4-20250514"},
	{"3", "deepseek", "DeepSeek", "deepseek-chat"},
	{"4", "openrouter", "OpenRouter", "anthropic/claude-sonnet-4"},
	{"5", "groq", "Groq", "llama-3.3-70b-versatile"},
	{"6", "gemini", "Gemini", "gemini-2.0-flash"},
	{"7", "zhipu", "Zhipu AI (智谱)", "glm-4-flash"},
	{"8", "dashscope", "DashScope (通义千问)", "qwen-plus"},
	{"9", "moonshot", "Moonshot (Kimi)", "moonshot-v1-128k"},
	{"10", "siliconflow", "SiliconFlow", "Qwen/Qwen2.5-72B-Instruct"},
	{"11", "volcengine", "VolcEngine (火山引擎)", "your-endpoint-id"},
	{"12", "minimax", "MiniMax", "MiniMax-Text-01"},
	{"13", "aihubmix", "AiHubMix", "gpt-4o"},
	{"14", "vllm", "vLLM / Local", "my-local-model"},
	{"15", "custom", "Custom (自定义 API)", "your-model-name"},
}

// runOnboard 交互式初始化
func runOnboard() error {
	home, _ := os.UserHomeDir()
	cfgDir := home + "/.pp-claw"
	cfgPath := cfgDir + "/pp-claw.yaml"

	// 检查是否已存在
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Println("✅ Configuration already exists at:", cfgPath)
		fmt.Println("   " + cfgPath)
		fmt.Println("   Delete it first if you want to re-initialize.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🦞 Welcome to pp-claw!")
	fmt.Println("Let's set up your configuration.")
	fmt.Println()

	// ── Step 1: 选择 Provider ──
	fmt.Println("Select your LLM provider:")
	fmt.Println()
	for _, p := range onboardProviders {
		fmt.Printf("  %2s) %-28s  e.g. %s\n", p.number, p.label, p.hint)
	}
	fmt.Println()
	fmt.Print("Enter number [3]: ")
	choiceStr, _ := reader.ReadString('\n')
	choiceStr = strings.TrimSpace(choiceStr)
	if choiceStr == "" {
		choiceStr = "3" // 默认 DeepSeek
	}

	// 查找选择的 Provider
	var chosen struct {
		name  string
		label string
		hint  string
	}
	found := false
	for _, p := range onboardProviders {
		if p.number == choiceStr || strings.EqualFold(p.name, choiceStr) {
			chosen.name = p.name
			chosen.label = p.label
			chosen.hint = p.hint
			found = true
			break
		}
	}
	if !found {
		// 尝试当成 provider name
		chosen.name = "custom"
		chosen.label = "Custom"
		chosen.hint = "your-model-name"
		fmt.Printf("  (unrecognized choice %q, using Custom provider)\n", choiceStr)
	}
	fmt.Printf("\n  ✓ Provider: %s\n\n", chosen.label)

	// ── Step 2: API Key ──
	fmt.Print("Enter API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		fmt.Println("  (skipped — you can set it later in ~/.pp-claw/pp-claw.yaml)")
	}

	// ── Step 3: Base URL ──
	defaultBase := ""
	// 查找 Provider 默认 base URL
	spec := providers.FindByName(chosen.name)
	if spec != nil && spec.DefaultAPIBase != "" {
		defaultBase = spec.DefaultAPIBase
	}

	promptBase := "Enter API Base URL"
	if defaultBase != "" {
		promptBase += " [" + defaultBase + "]"
	} else {
		promptBase += " (press Enter to use provider default)"
	}
	fmt.Print(promptBase + ": ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)

	// ── Step 4: Model ──
	fmt.Printf("Enter model name [%s]: ", chosen.hint)
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = chosen.hint
	}

	// ── 构建配置 ──
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = model

	// 设置 Provider
	providerCfg := cfg.GetProviderByName(chosen.name)
	if providerCfg == nil {
		// fallback to custom
		providerCfg = &cfg.Providers.Custom
		chosen.name = "custom"
	}
	providerCfg.APIKey = apiKey
	if baseURL != "" {
		providerCfg.BaseURL = baseURL
	}

	if err := config.Save(cfg, cfgPath); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	ws := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	os.MkdirAll(ws+"/memory", 0755)
	os.MkdirAll(ws+"/skills", 0755)
	os.MkdirAll(ws+"/sessions", 0755)

	// ── 打印结果 ──
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────┐")
	fmt.Println("│         ✅ Configuration saved!          │")
	fmt.Println("└─────────────────────────────────────────┘")
	fmt.Println()
	fmt.Printf("  Provider:   %s\n", chosen.label)
	fmt.Printf("  Model:      %s\n", model)
	if baseURL != "" {
		fmt.Printf("  Base URL:   %s\n", baseURL)
	}
	fmt.Printf("  Config:     %s\n", cfgPath)
	fmt.Printf("  Workspace:  %s\n", ws)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  pp-claw agent -m \"Hello!\"    # Quick test")
	fmt.Println("  pp-claw agent               # Interactive chat")
	fmt.Println("  pp-claw gateway             # Full service")
	fmt.Println()
	fmt.Println("Edit ~/.pp-claw/pp-claw.yaml to add channels, MCP servers, etc.")

	return nil
}

// ============================================================================
// Agent Command (对标 commands.py: agent)
// ============================================================================

func newAgentCmd() *cobra.Command {
	var message string
	var sessionID string

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Interact with the agent directly",
		Long:  "Send a message to the agent or start an interactive chat session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(message, sessionID)
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Message to send (non-interactive mode)")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "cli:direct", "Session ID")

	return cmd
}

func runAgent(message, sessionID string) error {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfgPath := cfgFile
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.pp-claw/pp-claw.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	ws := workspace
	if ws == "" {
		ws = config.ExpandHome(cfg.Agents.Defaults.Workspace)
	}
	os.MkdirAll(ws, 0755)

	msgBus := bus.NewMessageBus()
	chatModel, err := providers.NewChatModel(logger, cfg)
	if err != nil {
		return fmt.Errorf("创建 Provider 失败: %w", err)
	}

	sessions := session.NewManager(ws)
	agentLoop, err := agent.NewAgentLoop(&agent.AgentLoopConfig{
		Bus:       msgBus,
		Config:    cfg,
		Workspace: ws,
		Logger:    logger,
		Sessions:  sessions,
		ChatModel: chatModel,
	})
	if err != nil {
		return fmt.Errorf("创建 Agent Loop 失败: %w", err)
	}

	ctx := context.Background()

	if message != "" {
		// 单消息模式 (对标 commands.py: agent -m)
		response, err := agentLoop.ProcessDirect(ctx, message)
		if err != nil {
			return err
		}
		fmt.Println(response)
		return nil
	}

	// 交互模式 (对标 commands.py: agent 无 -m)
	agentReady := make(chan struct{}, 1)
	go func() {
		agentReady <- struct{}{}
		if err := agentLoop.Run(ctx); err != nil {
			logger.Error("Agent loop 退出", zap.Error(err))
		}
	}()
	<-agentReady
	// 等待启动日志刷出
	time.Sleep(50 * time.Millisecond)

	go cliOutboundHandler(ctx, msgBus, logger)

	fmt.Println("\n\xf0\x9f\xa6\x9e pp-claw interactive mode. Type 'exit' to quit.")
	fmt.Print("\nYou: ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Print("You: ")
			continue
		}
		if input == "exit" || input == "quit" || input == "/exit" || input == "/quit" {
			fmt.Println("ð\x9f\x91\x8b Goodbye!")
			return nil
		}
		msgBus.PublishInbound(bus.NewInboundMessage("cli", "user", "direct", input))
	}

	return nil
}

// ============================================================================
// Channels Command (对标 commands.py: channels)
// ============================================================================

func newChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage chat channels",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show channel status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showChannelStatus()
		},
	})

	return cmd
}

func showChannelStatus() error {
	cfgPath := cfgFile
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.pp-claw/pp-claw.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	fmt.Println("ð\x9f\x93\xa1 Channel Status:")
	channels := []struct {
		name    string
		enabled bool
	}{
		{"telegram", cfg.Channels.Telegram.Enabled},
		{"discord", cfg.Channels.Discord.Enabled},
		{"slack", cfg.Channels.Slack.Enabled},
		{"whatsapp", cfg.Channels.WhatsApp.Enabled},
		{"feishu", cfg.Channels.Feishu.Enabled},
		{"dingtalk", cfg.Channels.DingTalk.Enabled},
		{"email", cfg.Channels.Email.Enabled},
		{"qq", cfg.Channels.QQ.Enabled},
		{"mochat", cfg.Channels.Mochat.Enabled},
	}

	for _, ch := range channels {
		status := "❌ disabled"
		if ch.enabled {
			status = "✅ enabled"
		}
		fmt.Printf("  %-12s %s\n", ch.name, status)
	}
	return nil
}

// ============================================================================
// Cron Command (对标 commands.py: cron)
// ============================================================================

func newCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage scheduled jobs",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all scheduled jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronList()
		},
	})

	var jobID string
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a scheduled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronRemove(jobID)
		},
	}
	removeCmd.Flags().StringVar(&jobID, "id", "", "Job ID to remove")
	removeCmd.MarkFlagRequired("id")
	cmd.AddCommand(removeCmd)

	var runJobID string
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Manually run a scheduled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronRun(runJobID)
		},
	}
	runCmd.Flags().StringVar(&runJobID, "id", "", "Job ID to run")
	runCmd.MarkFlagRequired("id")
	cmd.AddCommand(runCmd)

	// cron add
	var addName, addMessage, addCronExpr, addTz, addAt, addTo, addChannel string
	var addEvery int
	var addDeliver bool
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new scheduled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronAdd(addName, addMessage, addEvery, addCronExpr, addTz, addAt, addDeliver, addTo, addChannel)
		},
	}
	addCmd.Flags().StringVarP(&addName, "name", "n", "", "Job name (required)")
	addCmd.Flags().StringVarP(&addMessage, "message", "m", "", "Message to send (required)")
	addCmd.Flags().IntVarP(&addEvery, "every", "e", 0, "Interval in seconds")
	addCmd.Flags().StringVarP(&addCronExpr, "cron", "C", "", "Cron expression")
	addCmd.Flags().StringVar(&addTz, "tz", "", "Timezone (only with --cron)")
	addCmd.Flags().StringVar(&addAt, "at", "", "One-time ISO datetime (e.g. 2025-01-01T12:00:00Z)")
	addCmd.Flags().BoolVarP(&addDeliver, "deliver", "d", false, "Deliver response to channel")
	addCmd.Flags().StringVar(&addTo, "to", "", "Target user/chat for delivery")
	addCmd.Flags().StringVar(&addChannel, "channel", "", "Target channel for delivery")
	addCmd.MarkFlagRequired("name")
	addCmd.MarkFlagRequired("message")
	cmd.AddCommand(addCmd)

	// cron enable
	var disableFlag bool
	enableCmd := &cobra.Command{
		Use:   "enable [job-id]",
		Short: "Enable or disable a scheduled job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronEnable(args[0], !disableFlag)
		},
	}
	enableCmd.Flags().BoolVar(&disableFlag, "disable", false, "Disable the job instead of enabling")
	cmd.AddCommand(enableCmd)

	return cmd
}

func getCronService() *cron.Service {
	home, _ := os.UserHomeDir()
	storePath := home + "/.pp-claw/data/cron/jobs.json"
	return cron.NewService(storePath, nil)
}

func cronList() error {
	svc := getCronService()
	jobs := svc.ListJobs(true)

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return nil
	}

	fmt.Printf("ð\x9f\x95\x92 Scheduled Jobs (%d):\n\n", len(jobs))
	for _, j := range jobs {
		status := "✅"
		if !j.Enabled {
			status = "⏸️"
		}
		fmt.Printf("  %s %s (id: %s)\n", status, j.Name, j.ID)
		fmt.Printf("    Schedule: %s\n", formatSchedule(j.Schedule))
		if j.Payload.Message != "" {
			msg := j.Payload.Message
			if len(msg) > 60 {
				msg = msg[:60] + "..."
			}
			fmt.Printf("    Message: %s\n", msg)
		}
		fmt.Println()
	}
	return nil
}

func formatSchedule(s cron.CronSchedule) string {
	switch s.Kind {
	case "every":
		secs := s.EveryMs / 1000
		if secs >= 3600 {
			return fmt.Sprintf("every %dh", secs/3600)
		}
		if secs >= 60 {
			return fmt.Sprintf("every %dm", secs/60)
		}
		return fmt.Sprintf("every %ds", secs)
	case "cron":
		if s.Tz != "" {
			return fmt.Sprintf("cron '%s' (%s)", s.Expr, s.Tz)
		}
		return fmt.Sprintf("cron '%s'", s.Expr)
	case "at":
		return fmt.Sprintf("at %d (one-time)", s.AtMs)
	default:
		return s.Kind
	}
}

func cronRemove(jobID string) error {
	svc := getCronService()
	if svc.RemoveJob(jobID) {
		fmt.Printf("✅ Removed job %s\n", jobID)
	} else {
		fmt.Printf("❌ Job %s not found\n", jobID)
	}
	return nil
}

func cronAdd(name, message string, every int, cronExpr, tz, at string, deliver bool, to, channel string) error {
	// 校验: 必须指定 --every / --cron / --at 之一
	schedCount := 0
	if every > 0 {
		schedCount++
	}
	if cronExpr != "" {
		schedCount++
	}
	if at != "" {
		schedCount++
	}
	if schedCount == 0 {
		return fmt.Errorf("must specify one of --every, --cron, or --at")
	}
	if schedCount > 1 {
		return fmt.Errorf("specify only one of --every, --cron, or --at")
	}

	// 校验: --tz 只能与 --cron 一起使用
	if tz != "" && cronExpr == "" {
		return fmt.Errorf("--tz can only be used with --cron")
	}

	var schedule cron.CronSchedule
	deleteAfterRun := false

	if every > 0 {
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMs: int64(every) * 1000,
		}
	} else if cronExpr != "" {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
			Tz:   tz,
		}
	} else if at != "" {
		t, err := parseISOTime(at)
		if err != nil {
			return fmt.Errorf("invalid --at time format: %w (expected ISO 8601, e.g. 2025-01-01T12:00:00Z)", err)
		}
		schedule = cron.CronSchedule{
			Kind: "at",
			AtMs: t.UnixMilli(),
		}
		deleteAfterRun = true
	}

	svc := getCronService()
	job := svc.AddJob(name, schedule, message, deliver, channel, to, deleteAfterRun)
	fmt.Printf("✅ Added job '%s' (id: %s)\n", job.Name, job.ID)
	fmt.Printf("   Schedule: %s\n", formatSchedule(job.Schedule))
	return nil
}

func cronEnable(jobID string, enabled bool) error {
	svc := getCronService()
	job := svc.EnableJob(jobID, enabled)
	if job == nil {
		return fmt.Errorf("job %s not found", jobID)
	}
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("✅ Job '%s' (id: %s) %s\n", job.Name, job.ID, action)
	return nil
}

// parseISOTime 解析 ISO 8601 时间格式
func parseISOTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized format: %s", s)
}

func cronRun(jobID string) error {
	svc := getCronService()
	jobs := svc.ListJobs(true)
	for _, j := range jobs {
		if j.ID == jobID {
			fmt.Printf("▶️ Running job '%s'...\n", j.Name)
			fmt.Printf("   Message: %s\n", j.Payload.Message)
			// Note: Full execution would go through agent.ProcessDirect
			return nil
		}
	}
	fmt.Printf("❌ Job %s not found\n", jobID)
	return nil
}

// ============================================================================
// Status Command (对标 commands.py: status)
// ============================================================================

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show pp-claw status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showStatus()
		},
	}
}

func showStatus() error {
	cfgPath := cfgFile
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.pp-claw/pp-claw.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	ws := config.ExpandHome(cfg.Agents.Defaults.Workspace)

	fmt.Println("\xf0\x9f\xa6\x9e pp-claw status")
	fmt.Println()
	fmt.Printf("  Version:    v0.1.0\n")
	fmt.Printf("  Model:      %s\n", cfg.Agents.Defaults.Model)
	fmt.Printf("  Workspace:  %s\n", ws)
	fmt.Printf("  Config:     %s\n", cfgPath)

	// Provider
	providerName := cfg.GetProviderName(cfg.Agents.Defaults.Model)
	if providerName != "" {
		fmt.Printf("  Provider:   %s\n", providerName)
	}

	// Channels
	var enabled []string
	if cfg.Channels.Telegram.Enabled {
		enabled = append(enabled, "telegram")
	}
	if cfg.Channels.Discord.Enabled {
		enabled = append(enabled, "discord")
	}
	if cfg.Channels.Slack.Enabled {
		enabled = append(enabled, "slack")
	}
	if cfg.Channels.Feishu.Enabled {
		enabled = append(enabled, "feishu")
	}
	if cfg.Channels.DingTalk.Enabled {
		enabled = append(enabled, "dingtalk")
	}
	if cfg.Channels.WhatsApp.Enabled {
		enabled = append(enabled, "whatsapp")
	}
	if cfg.Channels.Email.Enabled {
		enabled = append(enabled, "email")
	}
	if cfg.Channels.QQ.Enabled {
		enabled = append(enabled, "qq")
	}
	if cfg.Channels.Mochat.Enabled {
		enabled = append(enabled, "mochat")
	}
	if len(enabled) > 0 {
		fmt.Printf("  Channels:   %s\n", strings.Join(enabled, ", "))
	} else {
		fmt.Printf("  Channels:   none\n")
	}

	// Cron
	svc := getCronService()
	jobs := svc.ListJobs(false)
	fmt.Printf("  Cron jobs:  %d\n", len(jobs))

	// Sessions
	sessMgr := session.NewManager(ws)
	sessList := sessMgr.ListSessions()
	fmt.Printf("  Sessions:   %d\n", len(sessList))

	return nil
}

// 确保 json import 被使用
var _ = json.Marshal
