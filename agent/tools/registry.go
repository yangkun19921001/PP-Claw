package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
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
	tools map[string]Tool
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
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
		result = append(result, &einoToolAdapter{tool: t})
	}
	return result
}

// einoToolAdapter 将 Tool 接口适配为 Eino BaseTool (桥接层)
type einoToolAdapter struct {
	tool Tool
}

func (a *einoToolAdapter) Info(_ context.Context) (*schema.ToolInfo, error) {
	params := a.tool.Parameters()

	// 解析 parameters 中的 properties 和 required
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
	var params map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return fmt.Sprintf("Error: invalid JSON arguments: %s", err.Error()), nil
	}
	result, err := a.tool.Execute(ctx, params)
	if err != nil {
		return fmt.Sprintf("Error: %s", err.Error()), nil
	}
	return result, nil
}
