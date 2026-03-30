package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/spf13/cobra"
	"github.com/yangkun19921001/PP-Claw/agent"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/channels"
	"github.com/yangkun19921001/PP-Claw/cli/tui"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
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
			fmt.Printf("🦞 pp-claw %s (%s) built %s\n", agent.GetVersion(), agent.GetCommit(), agent.GetBuildTime())
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
// initLogger 创建 logger。toStdout=true 时同时输出到终端和文件，否则只输出到文件（TUI 模式）。
func initLogger(toStdout bool) *zap.Logger {
	home, _ := os.UserHomeDir()
	logPath := home + "/.pp-claw/pp-claw.log"
	os.MkdirAll(home+"/.pp-claw", 0755)

	cfg := zap.NewDevelopmentConfig()
	if toStdout {
		cfg.OutputPaths = []string{"stdout", logPath}
		cfg.ErrorOutputPaths = []string{"stderr", logPath}
	} else {
		cfg.OutputPaths = []string{logPath}
		cfg.ErrorOutputPaths = []string{logPath}
	}
	logger, _ := cfg.Build()
	return logger
}

func runGateway() error {
	// Gateway 模式：日志输出到 stdout + 文件
	logger := initLogger(true)
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
	os.MkdirAll(ws+"/channels", 0755)

	logger.Info("Workspace", zap.String("path", ws))

	// 创建消息总线
	msgBus := bus.NewMessageBus()

	// 创建并启动 CronService
	cronSvc := cron.NewService(ws+"/data/cron/jobs.json", logger)
	cronSvc.SetOnJob(func(job *cron.CronJob) (string, error) {
		// 使用原始 channel 和 chatID，确保响应能路由回正确的渠道
		inbound := bus.NewInboundMessage(job.Payload.Channel, "cron", job.Payload.To, job.Payload.Message)
		// 补全 accountID：cron payload 可能为空（历史任务或创建时未设置）
		accountID := job.Payload.Account
		if accountID == "" && job.Payload.Channel != "" {
			accountID = cfg.Channels.DefaultAccountIDForChannel(job.Payload.Channel)
		}
		inbound.AccountID = accountID
		msgBus.PublishInbound(inbound)
		return "Job dispatched to agent", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cronSvc.Start(ctx)

	// 创建路由器和 Agent 环境池
	router := agent.NewAgentRouter(&cfg.Agents, logger)
	pool := agent.NewAgentEnvPool(cfg, msgBus, logger, cronSvc)

	// 创建 Agent Loop（多 Agent 并发 dispatcher）
	agentLoop := agent.NewAgentLoop(&agent.AgentLoopConfig{
		Bus:         msgBus,
		Config:      cfg,
		Logger:      logger,
		Router:      router,
		Pool:        pool,
		CronService: cronSvc,
	})

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

	// 启动 Gateway HTTP 服务器（复用 gateway 端口，挂载 OAuth 回调等路由）
	if cfg.Gateway.Port > 0 {
		mux := http.NewServeMux()
		// 健康检查
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		})
		mux.HandleFunc("/channels/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(channelMgr.StatusSnapshot())
		})
		// 飞书 OAuth 回调
		if handler := agentLoop.GetFeishuOAuthHandler(); handler != nil {
			mux.Handle("/feishu/oauth/callback", handler)
			logger.Info("Feishu OAuth callback registered",
				zap.String("path", "/feishu/oauth/callback"),
				zap.Int("port", cfg.Gateway.Port))
		}
		channelMgr.RegisterRoutes(mux)
		gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
		srv := &http.Server{Addr: gatewayAddr, Handler: mux}
		go func() {
			logger.Info("Gateway HTTP server starting", zap.String("addr", gatewayAddr))
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("Gateway HTTP server error", zap.Error(err))
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			srv.Shutdown(shutdownCtx)
		}()
	}

	// 检查是否为交互式终端 (非交互模式，比如 Docker 守护进程，则不启动 TUI)
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		logger.Info("Non-interactive mode detected (no TTY), running strictly as a daemon...")
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		logger.Info("Shutdown signal received")
		cronSvc.Stop()
		channelMgr.StopAll()
		agentLoop.Stop()
		cancel()
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	// Gateway 模式：传统 CLI（stdin/stdout），不启动 TUI
	fmt.Println("\n🦞 pp-claw gateway ready!")
	fmt.Printf("   Model: %s\n", cfg.Agents.Defaults.Model)
	fmt.Println("   Type your message and press Enter. Type 'exit' to quit.")
	fmt.Print("\n> ")

	// 启动输出监听
	go cliOutboundHandler(ctx, msgBus, logger)

	// stdin 输入循环
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}
		inbound := bus.NewInboundMessage("cli", "user", "direct", line)
		msgBus.PublishInbound(inbound)
	}

	fmt.Println("\n👋 Shutting down gateway...")
	cronSvc.Stop()
	channelMgr.StopAll()
	agentLoop.Stop()
	cancel()
	time.Sleep(200 * time.Millisecond)

	return nil
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
				fmt.Printf("\n🤖 %s\n", renderInlineImages(msg.Content))
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

	fmt.Println("🦞 Welcome to pp-claw!")
	fmt.Println("Let's set up your configuration.")
	fmt.Println()

	// 映射到 TUI 配置格式
	var tuiProviders []tui.ProviderOption
	for _, p := range onboardProviders {
		tuiProviders = append(tuiProviders, tui.ProviderOption{
			Name:  p.name,
			Label: p.label,
			Hint:  p.hint,
		})
	}

	// 启动交互表单
	chosenName, apiKey, baseURL, model, err := tui.RunOnboardForm(tuiProviders)
	if err != nil {
		return fmt.Errorf("表单被取消或发生错误: %w", err)
	}

	// 查找选择的 Provider 信息以得到其配置
	var chosenHint, chosenLabel string
	for _, p := range onboardProviders {
		if p.name == chosenName {
			chosenHint = p.hint
			chosenLabel = p.label
			break
		}
	}

	if model == "" {
		model = chosenHint
	}

	// ── 构建配置 ──
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = model

	// 设置 Provider
	providerCfg := cfg.GetProviderByName(chosenName)
	if providerCfg == nil {
		providerCfg = &cfg.Providers.Custom
		chosenName = "custom"
	}
	providerCfg.APIKey = apiKey
	providerCfg.Model = model
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
	fmt.Printf("  Provider:   %s\n", chosenLabel)
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
	// 禁用底层 termenv 库的主动终端颜色探测，彻底断绝乱码泄露
	os.Setenv("COLORFGBG", "15;0")

	// Agent TUI 模式：日志只输出到文件，防止破坏 TUI 渲染
	logger := initLogger(false)
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

	// 强制为 CLI TUI 开启进度和工具提示，保证能看到思考和工具日志
	cfg.Channels.SendProgress = true
	cfg.Channels.SendToolHints = true

	ws := workspace
	if ws == "" {
		ws = config.ExpandHome(cfg.Agents.Defaults.Workspace)
	}
	os.MkdirAll(ws, 0755)

	msgBus := bus.NewMessageBus()

	router := agent.NewAgentRouter(&cfg.Agents, logger)
	pool := agent.NewAgentEnvPool(cfg, msgBus, logger, nil)

	agentLoop := agent.NewAgentLoop(&agent.AgentLoopConfig{
		Bus:    msgBus,
		Config: cfg,
		Logger: logger,
		Router: router,
		Pool:   pool,
	})

	ctx := context.Background()

	if message != "" {
		// 单消息模式 (对标 commands.py: agent -m)
		response, err := agentLoop.ProcessDirect(ctx, message)
		if err != nil {
			return err
		}
		fmt.Println(renderInlineImages(response))
		return nil
	}

	// 交互模式 (对标 commands.py: agent 无 -m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 检查交互环境
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("interactive agent mode requires a TTY terminal")
	}

	go func() {
		if err := agentLoop.Run(ctx); err != nil {
			if ctx.Err() == nil {
				logger.Error("Agent loop exit with error", zap.Error(err))
			}
		}
	}()

	// 启动 Bubble Tea TUI (阻塞直到退出)
	if err := tui.RunChat(ctx, msgBus, cancel, cfg.Agents.Defaults.Model); err != nil {
		return fmt.Errorf("TUI runtime error: %w", err)
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

	wechatCmd := &cobra.Command{
		Use:   "wechat",
		Short: "Manage wechat_personal channel",
	}

	var wechatAccountID string
	var wechatTimeoutS int
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Start WeChat QR login via the running gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWechatLogin(wechatAccountID, wechatTimeoutS)
		},
	}
	loginCmd.Flags().StringVar(&wechatAccountID, "account", "", "Target account ID, default auto-generated")
	loginCmd.Flags().IntVar(&wechatTimeoutS, "timeout", 480, "Wait timeout in seconds")
	wechatCmd.AddCommand(loginCmd)

	wechatCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show wechat_personal runtime status from gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showWechatGatewayStatus()
		},
	})

	cmd.AddCommand(wechatCmd)

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

	fmt.Println("Channel Status:")
	enabledMap := cfg.Channels.GetEnabledMap()

	for name, enabled := range enabledMap {
		status := "disabled"
		if enabled {
			status = "enabled"
		}
		fmt.Printf("  %-12s %s\n", name, status)
		if name == "wechat_personal" && enabled {
			fmt.Printf("  %-12s accounts=%d\n", "", len(cfg.Channels.WechatPersonal.Accounts))
		}
	}
	return nil
}

func gatewayBaseURLFromConfig(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.Gateway.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, cfg.Gateway.Port)
}

func gatewayJSONRequest(method, endpoint string, body any, out any) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.pp-claw/pp-claw.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, gatewayBaseURLFromConfig(cfg)+endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 gateway，请先运行 `pp-claw gateway`: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusNotFound && strings.HasPrefix(endpoint, "/channels/wechat_personal/") {
			return fmt.Errorf("gateway 未注册 wechat_personal 路由。通常是因为: 1) gateway 不是最新编译版本 2) 运行中的配置里 `channels.wechat_personal.enabled` 不是 true 3) gateway 还没重启。原始响应: %s", string(raw))
		}
		return fmt.Errorf("gateway %s %s 返回 %d: %s", method, endpoint, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func runWechatLogin(accountID string, timeoutS int) error {
	fmt.Println("微信登录向导")
	fmt.Println("1. 确保 `pp-claw gateway` 已经在另一个终端运行")
	fmt.Println("2. 执行本命令后，会生成一个二维码链接")
	fmt.Println("3. 用手机微信扫码并确认授权")
	fmt.Println()

	var startResp struct {
		SessionKey string `json:"session_key"`
		AccountID  string `json:"account_id"`
		QRCodeURL  string `json:"qrcode_url"`
		Message    string `json:"message"`
	}
	if err := gatewayJSONRequest(http.MethodPost, "/channels/wechat_personal/login/start", map[string]any{
		"account_id": accountID,
	}, &startResp); err != nil {
		return err
	}

	fmt.Printf("账号 ID: %s\n", startResp.AccountID)
	fmt.Printf("二维码链接: %s\n", startResp.QRCodeURL)
	fmt.Printf("提示: %s\n", startResp.Message)
	if err := renderWechatQRCodeToTerminal(startResp.QRCodeURL); err == nil {
		fmt.Println("上面已经尝试在终端渲染二维码，可直接扫码。")
	} else {
		fmt.Printf("终端二维码渲染失败: %v\n", err)
	}
	fmt.Println("如果终端无法直接扫码，请把上面的二维码链接复制到浏览器打开。")
	fmt.Println()

	var waitResp struct {
		Connected bool   `json:"connected"`
		AccountID string `json:"account_id"`
		UserID    string `json:"user_id"`
		BaseURL   string `json:"base_url"`
		Message   string `json:"message"`
	}
	if err := gatewayJSONRequest(http.MethodPost, "/channels/wechat_personal/login/wait", map[string]any{
		"session_key": startResp.SessionKey,
		"timeout_ms":  timeoutS * 1000,
	}, &waitResp); err != nil {
		return err
	}

	if !waitResp.Connected {
		fmt.Println(waitResp.Message)
		return nil
	}

	fmt.Println("登录成功")
	fmt.Printf("账号 ID: %s\n", waitResp.AccountID)
	if waitResp.UserID != "" {
		fmt.Printf("微信用户 ID: %s\n", waitResp.UserID)
	}
	if waitResp.BaseURL != "" {
		fmt.Printf("后端地址: %s\n", waitResp.BaseURL)
	}
	fmt.Println("这个账号已经保存到本地，之后重新启动 gateway 会自动接入。")
	return nil
}

func showWechatGatewayStatus() error {
	var resp struct {
		Accounts []struct {
			AccountID     string `json:"account_id"`
			Active        bool   `json:"active"`
			LoggedIn      bool   `json:"logged_in"`
			ILinkUserID   string `json:"ilink_user_id"`
			LastError     string `json:"last_error"`
			BaseURL       string `json:"base_url"`
			LastMessageAt string `json:"last_message_at"`
		} `json:"accounts"`
	}
	if err := gatewayJSONRequest(http.MethodGet, "/channels/wechat_personal/status", nil, &resp); err != nil {
		return err
	}

	if len(resp.Accounts) == 0 {
		fmt.Println("当前没有已登录的微信账号。")
		return nil
	}

	fmt.Println("wechat_personal 状态:")
	for _, account := range resp.Accounts {
		running := "stopped"
		if account.Active {
			running = "running"
		}
		loggedIn := "not-logged-in"
		if account.LoggedIn {
			loggedIn = "logged-in"
		}
		fmt.Printf("  - %s  %s  %s\n", account.AccountID, running, loggedIn)
		if account.ILinkUserID != "" {
			fmt.Printf("    user_id: %s\n", account.ILinkUserID)
		}
		if account.BaseURL != "" {
			fmt.Printf("    base_url: %s\n", account.BaseURL)
		}
		if account.LastError != "" {
			fmt.Printf("    last_error: %s\n", account.LastError)
		}
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
	job := svc.AddJob(name, schedule, message, deliver, channel, "", to, deleteAfterRun)
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
	for name, on := range cfg.Channels.GetEnabledMap() {
		if on {
			enabled = append(enabled, name)
		}
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

// imageTagRE 匹配 MCP 图片标记 <<IMAGE:/path/to/file>>
var imageTagRE = regexp.MustCompile(`<<IMAGE:([^>]+)>>`)

// renderInlineImages 将文本中的 <<IMAGE:path>> 标记替换为 iTerm2 内联图片
// 支持 iTerm2、WezTerm、Kitty 等兼容终端；不支持的终端会显示 "[图片: path]"
func renderInlineImages(text string) string {
	return imageTagRE.ReplaceAllStringFunc(text, func(match string) string {
		subs := imageTagRE.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		imgPath := subs[1]

		data, err := os.ReadFile(imgPath)
		if err != nil {
			return fmt.Sprintf("[图片: %s]", imgPath)
		}

		// 检测终端是否支持 iTerm2 inline image 协议
		if !supportsInlineImages() {
			return fmt.Sprintf("[图片: %s]", imgPath)
		}

		b64 := base64.StdEncoding.EncodeToString(data)
		// iTerm2 Inline Images Protocol:
		// ESC ] 1337 ; File=inline=1;width=auto : BASE64 ST
		return fmt.Sprintf("\033]1337;File=inline=1;width=80;preserveAspectRatio=1:%s\a", b64)
	})
}

// supportsInlineImages 检测当前终端是否支持 iTerm2 inline image 协议
func supportsInlineImages() bool {
	term := os.Getenv("TERM_PROGRAM")
	// iTerm2, WezTerm, Mintty 原生支持
	switch strings.ToLower(term) {
	case "iterm.app", "wezterm", "mintty":
		return true
	}
	// Kitty 也支持（通过 TERM 环境变量检测）
	if strings.Contains(os.Getenv("TERM"), "kitty") {
		return true
	}
	return false
}
