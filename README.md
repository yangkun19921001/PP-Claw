# 🐈 go-nanobot

**go-nanobot** 是 [nanobot](https://github.com/HKUDS/nanobot) Python 项目的 Go 语言 1:1 复刻版，一个简洁、透明、高效的个人 AI 助手 Agent。

基于 [Eino ADK](https://github.com/cloudwego/eino) 构建，支持多 LLM Provider、多渠道接入、MCP 工具扩展、定时任务、长期记忆和技能系统。

**go-nanobot** is a Go 1:1 port of [nanobot](https://github.com/HKUDS/nanobot) — a simple, transparent, and efficient personal AI assistant agent. Built on [Eino ADK](https://github.com/cloudwego/eino), it supports 17+ LLM providers, 9 chat channels, MCP tool extensions, cron jobs, long-term memory, and a skills system.

---

## ✨ 特性 / Features

| 特性 | 说明 |
|---|---|
| 🤖 **多 Provider 支持** | OpenAI / Anthropic / DeepSeek / Groq / Gemini / OpenRouter 等 17+ Provider |
| 💬 **多渠道接入** | Telegram / Discord / Slack / 飞书 / 钉钉 / WhatsApp / Email / QQ / MoChat |
| 🔧 **工具系统** | 文件操作 / Shell 执行 / Web 搜索+抓取 / 消息发送 / 子代理 / 定时任务 / 飞书知识库+文档 |
| 🔌 **MCP 协议** | 通过 stdio / Streamable HTTP 连接外部 MCP 服务器，自动注册工具 |
| 🧠 **智能记忆合并** | LLM 驱动的双层记忆系统: MEMORY.md (长期事实) + HISTORY.md (可搜索日志)，自动整合旧消息 |
| 📦 **技能系统** | 内置 8 个技能 + 支持 workspace 自定义技能，always-load 自动加载 |
| ⏰ **定时任务** | Cron 表达式 / 固定间隔 / 一次性定时，默认东八区，JSON 持久化，实时唤醒调度 |
| 💓 **心跳检查** | 定期检查 HEARTBEAT.md，自动执行待办任务 |
| 📡 **Progress 流式推送** | 工具调用时实时推送进度提示，CLI 显示 🔧 tool hint |
| 🚀 **Prompt Caching** | 自动为 Anthropic 注入 prompt caching header，降低延迟和成本 |
| 🛡️ **Shell 安全检查** | 正则模式 deny list + 路径遍历检测 + workspace 限制 |

---

## 🚀 快速开始

### 环境要求

- Go 1.21+
- 至少一个 LLM Provider 的 API Key（如 DeepSeek、OpenAI、Anthropic 等）

### 编译安装

```bash
git clone https://github.com/user/go-nanobot.git
cd go-nanobot
go mod tidy
go build -o nanobot .
```

或直接安装：

```bash
go install github.com/user/go-nanobot@latest
```

### 初始化配置

```bash
./nanobot onboard
```

按照提示输入 API Key 和模型名称。配置文件将创建在 `~/.nanobot/nanobot.yaml`。

### 首次对话

```bash
# 单次对话
./nanobot agent -m "你好，请介绍一下你自己"

# 交互模式
./nanobot agent

# 启动 Gateway（完整服务：Agent + 渠道 + 心跳 + 定时任务）
./nanobot gateway
```

交互模式下支持 `/new`（新会话）、`/help`（帮助）、`exit`（退出）。

工具调用时会实时显示进度提示：

```
> 帮我列出 workspace 下的文件
  🔧 list_directory(".")
  💭 正在处理...

🤖 以下是 workspace 下的文件列表：
...
```

---

## ⚙️ 配置

所有配置统一在 `~/.nanobot/nanobot.yaml`。

### 完整配置模板

```yaml
# ~/.nanobot/nanobot.yaml

agents:
  defaults:
    workspace: "~/.nanobot/workspace"
    model: "deepseek-chat"               # 模型名，原样传给 API
    max_tokens: 8192
    temperature: 0.1
    max_tool_iterations: 40
    memory_window: 100                    # 超过此数量自动触发记忆合并

providers:
  deepseek:
    api_key: "sk-your-key"

gateway:
  host: "0.0.0.0"
  port: 18790
  heartbeat:
    enabled: true
    interval_s: 1800

channels:
  send_progress: true                    # 推送工具调用进度到渠道
  send_tool_hints: true                  # 推送工具名称提示

tools:
  restrict_to_workspace: false           # 限制文件/Shell 操作在 workspace 内
  exec:
    timeout: 60                          # Shell 命令超时（秒）
  web:
    search:
      api_key: ""                        # Brave Search API Key
      max_results: 5
```

---

## 🤖 配置 Provider

`agents.defaults.model` 中填写的模型名会**原样传递**给 API，不做任何前缀剥离。Provider 通过模型名中的关键词自动匹配，也可以显式指定。

每个 Provider 支持 4 个配置字段：

| 字段 | 说明 |
|---|---|
| `api_key` | API 密钥（必填） |
| `base_url` | API 地址（也支持 `api_base`，两者等价，`base_url` 优先） |
| `model` | 可选，覆盖发送给 API 的模型名 |
| `extra_headers` | 可选，自定义 HTTP 请求头（key-value map） |

### DeepSeek

```yaml
agents:
  defaults:
    model: "deepseek-chat"

providers:
  deepseek:
    api_key: "sk-your-deepseek-key"
```

### Anthropic（自动启用 Prompt Caching）

```yaml
agents:
  defaults:
    model: "claude-sonnet-4-20250514"

providers:
  anthropic:
    api_key: "sk-ant-..."
    # anthropic-beta: prompt-caching-2024-07-31 header 自动注入
```

### OpenAI

```yaml
agents:
  defaults:
    model: "gpt-4o"

providers:
  openai:
    api_key: "sk-..."
```

### OpenRouter

```yaml
agents:
  defaults:
    model: "anthropic/claude-sonnet-4"      # OpenRouter 需要 provider/model 格式

providers:
  openrouter:
    api_key: "sk-or-..."
```

### 自定义 API 代理

```yaml
agents:
  defaults:
    model: "gpt-4o"

providers:
  openai:
    api_key: "sk-your-key"
    base_url: "https://your-proxy.com/v1"   # 自定义 base_url
```

### 完全自定义 Provider

```yaml
agents:
  defaults:
    model: "my-local-model"

providers:
  custom:
    api_key: "your-api-key"
    base_url: "https://your-server.com/v1"
    model: "actual-model-name"              # 可选：覆盖模型名
    extra_headers:
      X-Custom-Auth: "token-value"
      X-Project-ID: "my-project"
```

### 其他 Provider 示例

```yaml
# Groq
agents:
  defaults:
    model: "llama-3.3-70b-versatile"
providers:
  groq:
    api_key: "gsk_..."

# Gemini
agents:
  defaults:
    model: "gemini-2.0-flash"
providers:
  gemini:
    api_key: "AIza..."

# Moonshot / Kimi
agents:
  defaults:
    model: "moonshot-v1-128k"
providers:
  moonshot:
    api_key: "sk-..."

# 智谱 AI / Zhipu
agents:
  defaults:
    model: "glm-4-flash"
providers:
  zhipu:
    api_key: "..."

# 阿里 DashScope / 通义千问
agents:
  defaults:
    model: "qwen-plus"
providers:
  dashscope:
    api_key: "sk-..."

# SiliconFlow
agents:
  defaults:
    model: "Qwen/Qwen2.5-72B-Instruct"
providers:
  siliconflow:
    api_key: "sk-..."

# 火山引擎 VolcEngine
agents:
  defaults:
    model: "your-endpoint-id"
providers:
  volcengine:
    api_key: "..."

# 本地 vLLM
agents:
  defaults:
    model: "my-local-model"
providers:
  vllm:
    api_key: "dummy"
    base_url: "http://localhost:8000/v1"
```

---

## 🔌 配置 MCP 服务器

MCP（Model Context Protocol）允许 Agent 调用外部工具。支持两种传输协议：**stdio** 和 **Streamable HTTP**。

工具启动时自动连接并注册，工具名格式为 `mcp_{server}_{tool}`。

### stdio 模式

适用于本地命令行程序。nanobot 启动子进程，通过 stdin/stdout 通信。

```yaml
tools:
  mcp_servers:
    # 文件系统工具
    filesystem:
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      tool_timeout: 30                    # 工具调用超时（秒），默认 30

    # Git 工具
    git:
      command: "uvx"
      args: ["mcp-server-git", "--repository", "/path/to/repo"]
      tool_timeout: 60

    # SQLite 数据库
    sqlite:
      command: "uvx"
      args: ["mcp-server-sqlite", "--db-path", "/path/to/database.db"]

    # 带环境变量的服务
    github:
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_your_token"
      tool_timeout: 30

    # Python MCP Server
    my-python-tool:
      command: "python"
      args: ["-m", "my_mcp_server"]
      env:
        MY_API_KEY: "secret"
```

**stdio 配置字段**：

| 字段 | 必填 | 说明 |
|---|---|---|
| `command` | 是 | 可执行程序路径或命令名 |
| `args` | 否 | 命令行参数列表 |
| `env` | 否 | 额外环境变量（key-value map，会与系统环境合并） |
| `tool_timeout` | 否 | 工具调用超时秒数，默认 30 |

### Streamable HTTP 模式

适用于远程 MCP 服务器。通过 HTTP 请求通信。

```yaml
tools:
  mcp_servers:
    # 远程搜索服务
    web-search:
      url: "https://mcp.example.com/search"
      headers:
        Authorization: "Bearer your-token"
      tool_timeout: 30

    # 远程数据库查询
    remote-db:
      url: "https://mcp-db.internal.company.com/v1"
      headers:
        Authorization: "Bearer internal-token"
        X-Team: "engineering"
      tool_timeout: 60

    # 带 API Key 认证的服务
    analytics:
      url: "https://analytics-mcp.example.com"
      headers:
        X-API-Key: "your-analytics-key"
```

**Streamable HTTP 配置字段**：

| 字段 | 必填 | 说明 |
|---|---|---|
| `url` | 是 | MCP 服务器 URL |
| `headers` | 否 | HTTP 请求头（key-value map，用于认证等） |
| `tool_timeout` | 否 | 工具调用超时秒数，默认 30 |

### 混合配置

stdio 和 HTTP 可以同时使用：

```yaml
tools:
  mcp_servers:
    # 本地 stdio
    filesystem:
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    # 远程 HTTP
    web-search:
      url: "https://mcp.example.com/search"
      headers:
        Authorization: "Bearer token"
```

> **注意**：每个 MCP 服务器独立连接，单个服务器连接失败不影响其他服务器。

---

## 💬 配置渠道

### Telegram

```yaml
channels:
  telegram:
    enabled: true
    token: "123456:ABC-DEF..."               # BotFather 获取的 Bot Token
    allow_from: ["user_id_1", "user_id_2"]   # 允许的用户 ID（空=不限制）
    proxy: "socks5://127.0.0.1:1080"         # 可选代理
    reply_to_message: true                    # 是否引用回复
```

### Discord

```yaml
channels:
  discord:
    enabled: true
    token: "your-discord-bot-token"
    allow_from: ["user_id_1"]
    intents: 33281                            # Gateway Intents 位掩码
```

### Slack

```yaml
channels:
  slack:
    enabled: true
    mode: "socket"                            # Socket Mode
    bot_token: "xoxb-..."
    app_token: "xapp-..."
    reply_in_thread: true
    react_emoji: "eyes"                       # 收到消息时的 reaction
    group_policy: "mention"                   # "open" | "mention" | "allowlist"
    dm:
      enabled: true
      policy: "open"                          # "open" | "allowlist"
```

### 飞书 / Feishu

使用飞书 SDK WebSocket 长连接模式，自动重连，无需配置公网回调地址。

```yaml
channels:
  feishu:
    enabled: true
    app_id: "cli_xxxxx"
    app_secret: "your-app-secret"
    encrypt_key: "your-encrypt-key"           # 可选
    verification_token: "your-token"          # 可选
    wiki_enabled: true                        # 启用飞书知识库工具
    docs_enabled: true                        # 启用飞书文档工具
```

启用 `wiki_enabled` / `docs_enabled` 后，Agent 可通过工具调用读取飞书知识库空间列表、Wiki 节点和文档内容。

### 钉钉 / DingTalk

```yaml
channels:
  dingtalk:
    enabled: true
    client_id: "your-client-id"
    client_secret: "your-client-secret"
```

### WhatsApp

```yaml
channels:
  whatsapp:
    enabled: true
    bridge_url: "ws://localhost:8080/ws"      # WhatsApp Bridge WebSocket URL
    bridge_token: "your-bridge-token"
```

### Email

```yaml
channels:
  email:
    enabled: true
    consent_granted: true                     # 确认同意自动收发邮件
    imap_host: "imap.gmail.com"
    imap_port: 993
    imap_username: "you@gmail.com"
    imap_password: "app-password"
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    smtp_username: "you@gmail.com"
    smtp_password: "app-password"
    from_address: "you@gmail.com"
    poll_interval_seconds: 60
    mark_seen: true
    max_body_chars: 10000
    subject_prefix: "[nanobot] "
```

### QQ

```yaml
channels:
  qq:
    enabled: true
    app_id: "your-app-id"
    secret: "your-secret"
```

### MoChat

```yaml
channels:
  mochat:
    enabled: true
    base_url: "https://mochat.example.com"
```

---

## ⏰ 定时任务

### 时区支持

- **默认时区：Asia/Shanghai（北京时间/东八区）**
- 一次性定时 (`--at`) 和 Cron 表达式在不指定时区时均使用北京时间
- 可通过 `--tz` 参数指定其他 IANA 时区
- 通过对话创建的定时任务也默认使用北京时间

### 调度机制

- **实时唤醒**：添加新任务后立即唤醒调度器，无需等待轮询周期
- **真正的 Cron 解析**：使用 `robfig/cron/v3` 库解析标准 5 字段 Cron 表达式
- **渠道路由**：定时任务触发后，响应会正确路由回创建时的原始渠道（如飞书、Telegram）

### 通过 CLI 管理

```bash
# 添加定时任务 — 每 60 秒执行一次
nanobot cron add --name "check-status" --message "检查服务状态" --every 60

# 添加定时任务 — 使用 Cron 表达式 (每天 9:00 北京时间)
nanobot cron add --name "daily-report" --message "生成每日报告" --cron "0 9 * * *"

# 添加定时任务 — 指定其他时区
nanobot cron add --name "daily-report" --message "生成每日报告" --cron "0 9 * * *" --tz "America/New_York"

# 添加一次性定时任务（北京时间）
nanobot cron add --name "reminder" --message "会议提醒" --at "2025-06-01T09:00:00"

# 添加并投递到渠道
nanobot cron add --name "notify" --message "定期通知" --every 3600 \
  --deliver --channel telegram --to "123456"

# 查看所有定时任务
nanobot cron list

# 启用/禁用任务
nanobot cron enable <job-id>
nanobot cron enable <job-id> --disable

# 删除任务
nanobot cron remove --id <job-id>

# 手动运行
nanobot cron run --id <job-id>
```

`cron add` 参数说明：

| 参数 | 缩写 | 必填 | 说明 |
|---|---|---|---|
| `--name` | `-n` | 是 | 任务名称 |
| `--message` | `-m` | 是 | 发送给 Agent 的消息 |
| `--every` | `-e` | 三选一 | 固定间隔（秒） |
| `--cron` | `-C` | 三选一 | Cron 表达式（标准 5 字段） |
| `--at` | | 三选一 | 一次性定时（ISO 8601 格式，默认北京时间） |
| `--tz` | | 否 | 时区（默认 Asia/Shanghai） |
| `--deliver` | `-d` | 否 | 投递结果到渠道 |
| `--channel` | | 否 | 目标渠道 |
| `--to` | | 否 | 目标用户/Chat ID |

### 通过对话创建

也可以在对话中通过自然语言创建定时任务，Agent 会自动调用 cron 工具。定时任务触发后，Agent 的回复会自动推送回创建时的渠道。

---

## 📦 自定义技能

在 workspace 的 `skills/` 目录下创建技能：

```
~/.nanobot/workspace/skills/
└── my-skill/
    └── SKILL.md
```

`SKILL.md` 格式：

```markdown
---
name: my-skill
description: My custom skill description
always: false
---

# My Skill

Detailed instructions for the agent...
```

设置 `always: true` 会自动加载技能到系统提示词中。

内置 8 个技能：`clawhub`、`cron`、`github`、`memory`、`skill-creator`、`summarize`、`tmux`、`weather`。

---

## 🧠 记忆系统

Agent 自动管理双层记忆：

- `~/.nanobot/workspace/memory/MEMORY.md` — 长期事实记忆
- `~/.nanobot/workspace/memory/HISTORY.md` — 事件日志（可用 grep 搜索）

对话消息数超过 `memory_window` 时自动触发 LLM 驱动的记忆合并：

1. LLM 分析旧消息，提取关键事实和决策
2. 与现有 MEMORY.md 合并，去除过时信息
3. 生成简洁的历史日志条目写入 HISTORY.md

使用 `/new` 命令时会先整合当前会话再清空。

---

## 📝 CLI 命令速览

| 命令 | 说明 |
|---|---|
| `nanobot onboard` | 交互式初始化配置 |
| `nanobot gateway` | 启动完整 Gateway 服务 |
| `nanobot agent` | 交互模式 |
| `nanobot agent -m "..."` | 单次对话 |
| `nanobot status` | 查看运行状态 |
| `nanobot channels status` | 查看渠道状态 |
| `nanobot cron list` | 列出定时任务 |
| `nanobot cron add` | 添加定时任务 |
| `nanobot cron enable <id>` | 启用任务 |
| `nanobot cron enable <id> --disable` | 禁用任务 |
| `nanobot cron remove --id <id>` | 删除任务 |
| `nanobot cron run --id <id>` | 手动运行任务 |
| `nanobot version` | 显示版本号 |

---

## 📁 项目结构

```
go-nanobot/
├── main.go                     # 入口
├── cli/commands.go             # CLI 命令
├── agent/
│   ├── loop.go                 # Agent 核心循环 (Eino ADK Runner)
│   ├── context.go              # 上下文构建 (系统提示词/多模态/技能)
│   ├── memory.go               # 双层记忆系统 (LLM 驱动合并)
│   ├── skills.go               # 技能加载器
│   ├── subagent.go             # 子代理管理器
│   └── tools/
│       ├── registry.go         # 工具注册表 + Eino BaseTool 适配器
│       ├── filesystem.go       # 文件操作 (read/write/edit/list) + 模糊匹配提示
│       ├── shell.go            # Shell 命令执行 (正则安全检查)
│       ├── web.go              # Web 搜索 (Brave) + 网页抓取
│       ├── message.go          # 消息发送工具
│       ├── spawn.go            # 子代理生成工具
│       ├── cron.go             # 定时任务工具 (默认东八区)
│       ├── feishu_wiki.go     # 飞书知识库工具
│       ├── feishu_docs.go     # 飞书文档工具
│       └── mcp.go              # MCP 客户端 (stdio + Streamable HTTP)
├── bus/                        # 消息总线 (广播模式，多订阅者)
├── channels/                   # 9 个渠道实现
├── config/                     # 配置 Schema + YAML 加载
├── providers/                  # 17 Provider 注册表 + ChatModel 创建
├── session/                    # 会话管理 (JSONL 持久化)
├── cron/                       # 定时任务服务
├── heartbeat/                  # 心跳服务
├── skills/                     # 8 个内置技能
└── templates/                  # Workspace 模板文件
```

## 🛠️ 技术栈

- **Go 1.21+**
- **[Eino ADK](https://github.com/cloudwego/eino)** — Agent 核心框架
- **[mcp-go](https://github.com/mark3labs/mcp-go)** — MCP 协议客户端
- **[Cobra](https://github.com/spf13/cobra)** — CLI 框架
- **[Zap](https://go.uber.org/zap)** — 结构化日志

## 📊 与 Python nanobot 对比

| 指标 | Python nanobot | Go nanobot |
|---|---|---|
| 文件数 | 42 .py | 37 .go |
| 代码行数 | ~13,000 | ~6,700 |
| LLM 层 | LiteLLM (多 Provider) | Eino ADK (OpenAI 兼容) |
| 工具接口 | ABC 基类 | Go interface |
| 异步模型 | asyncio | goroutine + channel |
| 消息总线 | asyncio.Queue | Go channel |
| MCP 客户端 | mcp SDK | mcp-go |
| 配置格式 | JSON/YAML/ENV | YAML |

## 📄 License

MIT
