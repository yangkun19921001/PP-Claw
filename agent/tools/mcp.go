package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// MCPToolWrapper 将单个 MCP 工具包装为 pp-claw Tool (对标 mcp.py:MCPToolWrapper)
type MCPToolWrapper struct {
	client       *mcpclient.Client
	serverName   string
	originalName string
	toolName     string
	description  string
	parameters   map[string]any
	rawSchema    json.RawMessage // 保留原始 JSON Schema，用于 Eino 直接透传
	toolTimeout  int
	logger       *zap.Logger
}

func (t *MCPToolWrapper) Name() string        { return t.toolName }
func (t *MCPToolWrapper) Description() string { return t.description }
func (t *MCPToolWrapper) Parameters() map[string]any {
	if t.parameters != nil {
		return t.parameters
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// Execute 调用 MCP 工具 (对标 mcp.py:MCPToolWrapper.execute)
func (t *MCPToolWrapper) Execute(ctx context.Context, params map[string]any) (string, error) {
	// 记录 MCP 工具调用参数
	if t.logger != nil {
		paramsJSON, _ := json.Marshal(params)
		t.logger.Info("MCP tool call",
			zap.String("tool", t.toolName),
			zap.String("original_name", t.originalName),
			zap.String("params", string(paramsJSON)),
		)
	}

	timeout := time.Duration(t.toolTimeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := t.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      t.originalName,
			Arguments: params,
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			errMsg := fmt.Sprintf("(MCP tool call timed out after %ds)", t.toolTimeout)
			if t.logger != nil {
				t.logger.Warn("MCP tool call timeout", zap.String("tool", t.toolName), zap.Int("timeout", t.toolTimeout))
			}
			return errMsg, nil
		}
		errMsg := fmt.Sprintf("Error calling MCP tool '%s': %s", t.toolName, err.Error())
		if t.logger != nil {
			t.logger.Error("MCP tool call error", zap.String("tool", t.toolName), zap.Error(err))
		}
		return errMsg, nil
	}

	var parts []string
	for _, block := range result.Content {
		if textContent, ok := block.(mcp.TextContent); ok {
			parts = append(parts, textContent.Text)
		} else {
			parts = append(parts, fmt.Sprintf("%v", block))
		}
	}
	output := strings.Join(parts, "\n")
	if len(parts) == 0 {
		output = "(no output)"
	}

	if t.logger != nil {
		preview := output
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		t.logger.Info("MCP tool call result",
			zap.String("tool", t.toolName),
			zap.String("result_preview", preview),
		)
	}
	return output, nil
}

var _ Tool = (*MCPToolWrapper)(nil)

// MCPManager 管理所有 MCP 服务器连接 (对标 loop.py 中的 _mcp_* 字段和方法)
type MCPManager struct {
	clients    []*mcpclient.Client
	connected  bool
	connecting bool
	mu         sync.Mutex
	logger     *zap.Logger
}

// NewMCPManager 创建 MCP 管理器
func NewMCPManager(logger *zap.Logger) *MCPManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MCPManager{
		logger: logger,
	}
}

// MCPServerConfig MCP 服务器配置 (对齐 config/schema.go:MCPServerConfig)
type MCPServerConfig struct {
	Command     string
	Args        []string
	Env         map[string]string
	URL         string
	Headers     map[string]string
	ToolTimeout int
}

// Connect 连接所有 MCP 服务器并注册工具 (对标 mcp.py:connect_mcp_servers + loop.py:_connect_mcp)
func (m *MCPManager) Connect(ctx context.Context, servers map[string]MCPServerConfig, registry *Registry) error {
	m.mu.Lock()
	if m.connected || m.connecting {
		m.mu.Unlock()
		return nil
	}
	m.connecting = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.connecting = false
		m.mu.Unlock()
	}()

	for name, cfg := range servers {
		if err := m.connectServer(ctx, name, cfg, registry); err != nil {
			m.logger.Error("MCP server connection failed",
				zap.String("server", name),
				zap.Error(err),
			)
			// 与 Python 一致: 单个服务器失败不影响其他服务器
			continue
		}
	}

	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()

	return nil
}

// connectServer 连接单个 MCP 服务器 (对标 mcp.py:connect_mcp_servers 内部逻辑)
func (m *MCPManager) connectServer(ctx context.Context, name string, cfg MCPServerConfig, registry *Registry) error {
	var client *mcpclient.Client

	toolTimeout := cfg.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = 30
	}

	if cfg.Command != "" {
		// Stdio 模式 (对标 mcp.py: StdioServerParameters)
		m.logger.Info("MCP connecting via stdio",
			zap.String("server", name),
			zap.String("command", cfg.Command),
			zap.Strings("args", cfg.Args),
		)

		// 构建环境变量
		env := os.Environ()
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}

		var err error
		client, err = mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
		if err != nil {
			return fmt.Errorf("failed to create stdio client: %w", err)
		}
	} else if cfg.URL != "" {
		// 根据 URL 后缀判断协议: /sse → SSE, 其他 → Streamable HTTP
		if strings.HasSuffix(cfg.URL, "/sse") {
			// SSE 模式
			m.logger.Info("MCP connecting via SSE",
				zap.String("server", name),
				zap.String("url", cfg.URL),
			)

			var sseOpts []transport.ClientOption
			if len(cfg.Headers) > 0 {
				sseOpts = append(sseOpts, transport.WithHeaders(cfg.Headers))
			}
			sseTransport, err := transport.NewSSE(cfg.URL, sseOpts...)
			if err != nil {
				return fmt.Errorf("failed to create SSE transport: %w", err)
			}

			client = mcpclient.NewClient(sseTransport)
			if err := client.Start(ctx); err != nil {
				return fmt.Errorf("failed to start SSE client: %w", err)
			}
		} else {
			// Streamable HTTP 模式 (对标 mcp.py: streamable_http_client)
			m.logger.Info("MCP connecting via Streamable HTTP",
				zap.String("server", name),
				zap.String("url", cfg.URL),
			)

			var opts []transport.StreamableHTTPCOption
			if len(cfg.Headers) > 0 {
				opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
			}
			httpTransport, err := transport.NewStreamableHTTP(cfg.URL, opts...)
			if err != nil {
				return fmt.Errorf("failed to create HTTP transport: %w", err)
			}

			client = mcpclient.NewClient(httpTransport)
			if err := client.Start(ctx); err != nil {
				return fmt.Errorf("failed to start HTTP client: %w", err)
			}
		}
	} else {
		m.logger.Warn("MCP server: no command or url configured, skipping",
			zap.String("server", name),
		)
		return nil
	}

	// 初始化客户端 (对标 mcp.py: session.initialize())
	initReq := mcp.InitializeRequest{}
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "pp-claw",
		Version: "0.1.0",
	}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION

	_, err := client.Initialize(ctx, initReq)
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// 列出工具 (对标 mcp.py: session.list_tools())
	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// 注册工具 (对标 mcp.py: MCPToolWrapper + registry.register)
	for _, toolDef := range toolsResult.Tools {
		wrapper := &MCPToolWrapper{
			client:       client,
			serverName:   name,
			originalName: toolDef.Name,
			toolName:     fmt.Sprintf("mcp_%s_%s", name, toolDef.Name),
			description:  toolDef.Description,
			toolTimeout:  toolTimeout,
			logger:       m.logger,
		}

		// 从 InputSchema 构建 parameters 和 rawSchema
		// 注意: mcp-go 的 Tool.RawInputSchema 字段有 json:"-" 标签，反序列化时不会填充
		// 实际 schema 在 Tool.InputSchema (ToolInputSchema) 中
		inputSchemaJSON, err := json.Marshal(toolDef.InputSchema)
		if err == nil && len(inputSchemaJSON) > 2 { // > 2 排除 "{}"
			wrapper.rawSchema = inputSchemaJSON
			var inputSchema map[string]any
			if err := json.Unmarshal(inputSchemaJSON, &inputSchema); err == nil {
				wrapper.parameters = inputSchema
			}
			m.logger.Debug("MCP tool schema",
				zap.String("tool", wrapper.toolName),
				zap.String("schema", string(inputSchemaJSON)),
			)
		}

		registry.Register(wrapper)
		m.logger.Debug("MCP: registered tool",
			zap.String("tool", wrapper.toolName),
			zap.String("server", name),
		)
	}

	m.clients = append(m.clients, client)
	m.logger.Info("MCP server connected",
		zap.String("server", name),
		zap.Int("tools", len(toolsResult.Tools)),
	)

	return nil
}

// Close 关闭所有 MCP 客户端 (对标 loop.py:close_mcp)
func (m *MCPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.clients {
		if err := c.Close(); err != nil {
			m.logger.Error("MCP client close error", zap.Error(err))
		}
	}
	m.clients = nil
	m.connected = false
}

// IsConnected 检查是否已连接
func (m *MCPManager) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}
