package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"go.uber.org/zap"
)

// Tool 工具接口 (对标 pp-claw/agent/tools/base.py:Tool)
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// ContextSetter 需要上下文信息的工具实现此接口
type ContextSetter interface {
	SetContext(channel, chatID string)
}

// Registry 工具注册表 (对标 pp-claw/agent/tools/registry.py:ToolRegistry)
type Registry struct {
	tools  map[string]Tool
	logger *zap.Logger
}

// NewRegistry 创建工具注册表
func NewRegistry(logger ...*zap.Logger) *Registry {
	l := zap.NewNop()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &Registry{tools: make(map[string]Tool), logger: l}
}

// Register 注册工具
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get 根据名称获取工具
func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// Has 检查工具是否已注册
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Names 获取所有工具名称
func (r *Registry) Names() []string {
	var names []string
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Execute 执行工具 (对标 pp-claw/agent/tools/registry.py:execute)
func (r *Registry) Execute(ctx context.Context, name string, params map[string]any) string {
	hint := "\n\n[Analyze the error above and try a different approach.]"
	t := r.tools[name]
	if t == nil {
		return fmt.Sprintf("Error: Tool '%s' not found. Available: %s", name, strings.Join(r.Names(), ", "))
	}

	result, err := t.Execute(ctx, params)
	if err != nil {
		return fmt.Sprintf("Error executing %s: %s", name, err.Error()) + hint
	}
	if strings.HasPrefix(result, "Error") {
		return result + hint
	}
	return result
}

// GetDefinitions 获取所有工具的 OpenAI 格式定义
func (r *Registry) GetDefinitions() []map[string]any {
	var defs []map[string]any
	for _, t := range r.tools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return defs
}

// ToEinoTools 将注册表中的工具转换为 Eino BaseTool 列表
func (r *Registry) ToEinoTools() []tool.BaseTool {
	var result []tool.BaseTool
	for _, t := range r.tools {
		result = append(result, &einoToolAdapter{tool: t, logger: r.logger})
	}
	return result
}

// einoToolAdapter 将 Tool 接口适配为 Eino BaseTool (桥接层)
type einoToolAdapter struct {
	tool   Tool
	logger *zap.Logger
}

func (a *einoToolAdapter) Info(_ context.Context) (*schema.ToolInfo, error) {
	// 优先使用原始 JSON Schema（MCP 工具），避免手动转换丢失 schema 信息
	if mcpTool, ok := a.tool.(*MCPToolWrapper); ok && mcpTool.rawSchema != nil {
		var js jsonschema.Schema
		if err := json.Unmarshal(mcpTool.rawSchema, &js); err == nil {
			return &schema.ToolInfo{
				Name:        a.tool.Name(),
				Desc:        a.tool.Description(),
				ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&js),
			}, nil
		}
	}

	// 回退: 手动转换 parameters map (非 MCP 工具)
	params := a.tool.Parameters()
	properties := map[string]*schema.ParameterInfo{}
	if props, ok := params["properties"].(map[string]any); ok {
		required := map[string]bool{}
		if req, ok := params["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required[s] = true
				}
			}
		}
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				pi := &schema.ParameterInfo{
					Required: required[name],
				}
				if desc, ok := propMap["description"].(string); ok {
					pi.Desc = desc
				}
				if typ, ok := propMap["type"].(string); ok {
					switch typ {
					case "string":
						pi.Type = schema.String
					case "integer":
						pi.Type = schema.Integer
					case "number":
						pi.Type = schema.Number
					case "boolean":
						pi.Type = schema.Boolean
					case "array":
						pi.Type = schema.Array
					default:
						pi.Type = schema.String
					}
				}
				if enumVals, ok := propMap["enum"].([]any); ok {
					for _, v := range enumVals {
						if s, ok := v.(string); ok {
							pi.Enum = append(pi.Enum, s)
						}
					}
				}
				properties[name] = pi
			}
		}
	}

	return &schema.ToolInfo{
		Name:        a.tool.Name(),
		Desc:        a.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(properties),
	}, nil
}

func (a *einoToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	name := a.tool.Name()

	// 日志：工具调用参数
	a.logger.Info("🔧 Tool call",
		zap.String("tool", name),
		zap.String("params", truncateLog(argumentsInJSON, 500)),
	)

	var params map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		a.logger.Warn("🔧 Tool params parse error", zap.String("tool", name), zap.Error(err))
		return fmt.Sprintf("Error: invalid JSON arguments: %s", err.Error()), nil
	}

	start := time.Now()
	result, err := a.tool.Execute(ctx, params)
	elapsed := time.Since(start)

	if err != nil {
		a.logger.Warn("🔧 Tool error",
			zap.String("tool", name),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
		return fmt.Sprintf("Error: %s", err.Error()), nil
	}

	// 日志：工具返回结果预览
	a.logger.Info("🔧 Tool result",
		zap.String("tool", name),
		zap.Duration("elapsed", elapsed),
		zap.Int("result_len", len(result)),
		zap.String("preview", truncateLog(result, 300)),
	)

	return result, nil
}

// truncateLog 截断日志字符串
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
