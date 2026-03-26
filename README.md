<div align="center">

<h1>🦐 皮皮虾 · PP-Claw</h1>

<h3>Go 语言编写的全能个人 AI 助手 Agent</h3>

<p>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License"></a>
  <a href="https://github.com/yangkun19921001/PP-Claw"><img src="https://img.shields.io/badge/Based_on-nanobot-orange?style=for-the-badge" alt="Based on nanobot"></a>
</p>

<p><strong>18 个 LLM Provider · 12 个消息渠道 · MCP 工具扩展 · 长期记忆 · 子代理 · 定时任务</strong></p>

</div>

---

## 🦐 简介

**PP-Claw（皮皮虾）** 是一个简洁、高效的个人 AI 助手 Agent，基于 [Eino ADK](https://github.com/cloudwego/eino) 构建。本项目参考 Python 版 [nanobot](https://github.com/pinkpiglet/nanobot) 的架构与功能设计，使用 Go 语言完整重新实现，在保持功能对齐的同时充分利用 Go 的并发性能和静态编译优势。

支持接入飞书、Telegram、Discord、Slack、企业微信、个人微信、Matrix 等 12 个渠道，对接 OpenAI、Anthropic、DeepSeek、Gemini 等 18 个 LLM Provider，并可通过 MCP 协议无限扩展工具能力。

---

## ✨ 特性一览

| 模块 | 能力 |
|---|---|
| 🤖 **LLM Provider** | OpenAI / Anthropic / DeepSeek / Gemini / Groq / OpenRouter / Azure OpenAI / 智谱 / 通义千问 / Moonshot / MiniMax / SiliconFlow / 火山引擎 / vLLM 等 **18+** |
| 💬 **消息渠道** | Telegram / Discord / Slack / 飞书 / 钉钉 / WhatsApp / Email / QQ / MoChat / 企业微信 / 个人微信 / Matrix **共 12 个** |
| 🔧 **内置工具** | 文件读写编辑 / Shell 执行 / Web 搜索+抓取 / 消息发送(含媒体) / 子代理 / 定时任务 / 飞书知识库+文档+Aily |
| 🔌 **MCP 协议** | Stdio / SSE / Streamable HTTP 三种传输，自动发现注册工具 |
| 🧠 **智能记忆** | LLM 驱动双层记忆（MEMORY.md 长期事实 + HISTORY.md 事件日志），Token 级自动整合 |
| 🤝 **子代理** | 后台独立 LLM 循环，拥有文件/Shell/Web 工具，`/stop` 一键取消 |
| 📦 **技能系统** | 8 个内置技能 + workspace 自定义技能，always-load 自动加载 |
| ⏰ **定时任务** | Cron 表达式 / 固定间隔 / 一次性，结果自动路由回原渠道 |
| 💓 **心跳检查** | 定期执行 HEARTBEAT.md 待办任务，智能评估是否通知 |
| 🛡️ **安全防护** | SSRF 拦截 / Shell 危险命令阻断 / 路径遍历检测 / workspace 沙箱 |
| 📡 **实时进度** | 工具调用时向渠道推送进度提示 |
| 🚀 **Prompt Caching** | Anthropic 自动注入 caching header |

---

## 🚀 快速开始

### 环境要求

- Go 1.21+
- 至少一个 LLM Provider 的 API Key(建议 claude 目前只试了这个)

### 编译安装

```bash
git clone https://github.com/yangkun19921001/PP-Claw.git
cd PP-Claw
go build -o pp-claw .
```

### 初始化配置

```bash
./pp-claw onboard
```

按照引导输入 API Key 和模型名称，配置文件保存在 `~/.pp-claw/pp-claw.yaml`。

### 运行

```bash
# 交互式对话
./pp-claw agent

# 单次对话
./pp-claw agent -m "你好"

# 启动完整服务（Agent + 渠道 + 心跳 + 定时任务）
./pp-claw gateway
```

---

## 🐳 Docker 部署

```bash
# 启动 Gateway 服务
docker compose up -d gateway

# 交互式 CLI
docker compose run --rm cli

# 查看日志
docker compose logs -f gateway
```

容器通过挂载 `~/.pp-claw` 目录读取配置：

```
~/.pp-claw/
├── pp-claw.yaml          # 主配置（必须提前创建）
├── workspace/
│   ├── memory/           # 记忆文件
│   └── skills/           # 自定义技能
└── sessions/             # 会话持久化
```

---

## 🤖 支持的 Provider

> 模型名原样传递给 API，Provider 通过关键词/前缀自动匹配。

<details>
<summary>展开全部 18 个 Provider 配置示例</summary>

### DeepSeek

```yaml
agents:
  defaults:
    model: "deepseek-chat"
providers:
  deepseek:
    api_key: "sk-your-key"
```

### Anthropic（自动启用 Prompt Caching）

```yaml
agents:
  defaults:
    model: "claude-sonnet-4-20250514"
providers:
  anthropic:
    api_key: "sk-ant-..."
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

### Azure OpenAI

```yaml
agents:
  defaults:
    model: "gpt-4o"
providers:
  azure_openai:
    api_key: "your-azure-key"
    api_base: "https://your-resource.openai.azure.com"
    api_version: "2024-10-21"
```

### Gemini

```yaml
agents:
  defaults:
    model: "gemini-2.0-flash"
providers:
  gemini:
    api_key: "AIza..."
```

### OpenRouter

```yaml
agents:
  defaults:
    model: "anthropic/claude-sonnet-4"
providers:
  openrouter:
    api_key: "sk-or-..."
```

### 其他

支持 Groq / 智谱 / 通义千问 / Moonshot / MiniMax / SiliconFlow / 火山引擎 / vLLM / AiHubMix / OpenAI Codex / GitHub Copilot，配置格式相同：

```yaml
providers:
  <provider_name>:
    api_key: "your-key"
    base_url: "https://..."   # 可选
    model: "override-model"   # 可选
    extra_headers:             # 可选
      X-Custom: "value"
```

</details>

---

## 💬 支持的渠道

| 渠道 | 连接方式 | 特色功能 |
|---|---|---|
| **飞书** | SDK WebSocket 长连接 | 引用回复 / 表情反应 / 智能格式(text/post/card) / 知识库+文档工具 / 图片音频文件收发 |
| **Telegram** | Long Polling | 代理支持 / 引用回复 / 多媒体收发 |
| **Discord** | Gateway WebSocket | Intents 配置 |
| **Slack** | Socket Mode | Thread 回复 / 表情反应 / 群聊策略(open/mention/allowlist) / DM 策略 |
| **企业微信** | WebSocket | 欢迎消息 / 流式回复 / 去重 |
| **个人微信** | ClawBot HTTP JSON API | Gateway 托管登录 / 多账号同时在线 / context token 回复 / CDN 媒体收发 |
| **Matrix** | Sync 长轮询 | E2EE 端到端加密 / Thread 支持 / Typing 指示器 / Markdown→HTML |
| **钉钉** | SDK | Client ID/Secret 认证 |
| **WhatsApp** | Bridge WebSocket | Matrix Bridge 模式 |
| **Email** | IMAP/SMTP | 自动收发 / 轮询间隔 / 主题前缀 |
| **QQ** | SDK | App ID/Secret 认证 |
| **MoChat** | HTTP | Base URL 配置 |

<details>
<summary>飞书配置示例</summary>

```yaml
channels:
  feishu:
    enabled: true
    app_id: "cli_xxxxx"
    app_secret: "your-app-secret"
    encrypt_key: ""                  # 可选
    verification_token: ""           # 可选
    group_policy: "mention"          # "open" 或 "mention"
    react_emoji: "THINKING"          # 收到消息时的表情反应
    reply_to_message: true           # 引用回复
    wiki_enabled: true               # 启用飞书知识库工具
    docs_enabled: true               # 启用飞书文档工具
```

</details>

<details>
<summary>Telegram 配置示例</summary>

```yaml
channels:
  telegram:
    enabled: true
    token: "123456:ABC-DEF..."
    allow_from: []                   # 空=不限制
    proxy: "socks5://127.0.0.1:1080" # 可选
```

</details>

<details>
<summary>企业微信配置示例</summary>

```yaml
channels:
  wecom:
    enabled: true
    bot_id: "your-bot-id"
    secret: "your-secret"
    welcome_message: "你好！我是皮皮虾 🦐"
```

</details>

<details>
<summary>个人微信配置示例</summary>

```yaml
channels:
  wechat_personal:
    enabled: true
    base_url: "https://ilinkai.weixin.qq.com"
    cdn_base_url: "https://novac2c.cdn.weixin.qq.com/c2c"
    bot_type: "3"
    login_timeout_s: 480
    accounts:
      wx1:
        enabled: true
```

首次登录：

```bash
# 1. 先启动 gateway
pp-claw gateway

# 2. 在另一个终端执行登录命令
pp-claw channels wechat login --account wx1
```

命令会优先在终端直接渲染二维码，同时也会打印二维码链接。用手机微信扫码并确认后，账号会自动保存到本地，后续重启 gateway 会自动接入。

</details>

<details>
<summary>Matrix 配置示例</summary>

```yaml
channels:
  matrix:
    enabled: true
    homeserver: "https://matrix.org"
    access_token: "syt_..."
    user_id: "@bot:matrix.org"
    device_id: "DEVICEID"
    e2ee_enabled: true
    group_policy: "mention"          # "open"/"mention"/"allowlist"
```

</details>

---

## 🔧 工具系统

### 内置工具

| 工具 | 说明 |
|---|---|
| `read_file` / `write_file` / `edit_file` / `list_directory` | 文件操作，edit 支持模糊匹配提示 |
| `execute` | Shell 命令执行，危险命令阻断，超时控制 |
| `web_search` | Brave Search API 搜索 |
| `web_fetch` | 网页抓取，HTML→文本，SSRF 防护 |
| `message` | 消息发送，支持文本+媒体附件（图片/音频/视频/文档） |
| `spawn` | 后台子代理，独立 LLM 循环 |
| `cron` | 定时任务管理（add/list/remove） |
| `feishu_wiki` | 飞书知识库：空间列表/节点浏览/文档搜索 |
| `feishu_docs` | 飞书文档：读取/信息/块列表 |
| `feishu_knowledge` | 飞书 Aily 数据知识问答 |

### MCP 扩展

通过 MCP 协议连接外部工具服务器，支持 **Stdio**、**SSE**、**Streamable HTTP** 三种传输：

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

---

## ⏰ 定时任务

默认时区 **Asia/Shanghai**，支持 Cron 表达式 / 固定间隔 / 一次性定时。

```bash
# 每天 9:00 执行
pp-claw cron add -n "daily" -m "生成报告" --cron "0 9 * * *"

# 每 60 秒执行
pp-claw cron add -n "check" -m "检查状态" --every 60

# 一次性定时
pp-claw cron add -n "remind" -m "会议提醒" --at "2026-06-01T09:00:00"

# 管理
pp-claw cron list
pp-claw cron remove --id <job-id>
pp-claw cron run --id <job-id>
```

也可以在对话中用自然语言创建，结果自动路由回原渠道。

---

## 📦 技能系统

在 `~/.pp-claw/workspace/skills/` 下创建自定义技能：

```markdown
<!-- skills/my-skill/SKILL.md -->
---
name: my-skill
description: My custom skill
always: false
---

Instructions for the agent...
```

`always: true` 的技能自动加载到系统提示词。

内置技能：`clawhub` / `cron` / `github` / `memory` / `skill-creator` / `summarize` / `tmux` / `weather`

---

## 🧠 记忆系统

双层记忆，LLM 驱动自动整合：

- **MEMORY.md** — 长期事实和决策
- **HISTORY.md** — 可搜索的事件日志

消息数超过 `memory_window` 或 Token 超过 `context_window_tokens/2` 时自动触发整合，最多 5 轮迭代。连续 3 次 LLM 整合失败自动降级为原始归档。

---

## ⚙️ 完整配置模板

```yaml
# ~/.pp-claw/pp-claw.yaml

agents:
  defaults:
    workspace: "~/.pp-claw/workspace"
    model: "deepseek-chat"
    max_tokens: 8192
    temperature: 0.1
    max_tool_iterations: 40
    memory_window: 100
    context_window_tokens: 65536

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
  send_progress: true
  send_tool_hints: true

tools:
  restrict_to_workspace: false
  exec:
    timeout: 60
  web:
    search:
      api_key: ""
      max_results: 5
  mcp_servers: {}
```

---

## 📝 CLI 命令

| 命令 | 说明 |
|---|---|
| `pp-claw onboard` | 交互式初始化配置 |
| `pp-claw gateway` | 启动完整服务 |
| `pp-claw agent` | 交互模式 |
| `pp-claw agent -m "..."` | 单次对话 |
| `pp-claw status` | 运行状态 |
| `pp-claw channels status` | 渠道状态 |
| `pp-claw channels wechat login --account wx1` | 通过 gateway 发起个人微信扫码登录 |
| `pp-claw channels wechat status` | 查看个人微信运行状态 |
| `pp-claw cron list/add/remove/run/enable` | 定时任务管理 |
| `pp-claw version` | 版本信息 |

交互模式命令：`/new`（新会话）、`/stop`（取消任务）、`/help`（帮助）、`exit`（退出）

---

## 📁 项目结构

```
PP-Claw/
├── main.go                      # 入口
├── cli/
│   ├── commands.go              # CLI 命令 (Cobra)
│   ├── wizard.go                # 交互式引导向导
│   └── tui/                     # Bubble Tea 交互界面
├── agent/
│   ├── loop.go                  # Agent 核心循环 (Eino ADK)
│   ├── context.go               # 上下文构建
│   ├── memory.go                # 双层记忆系统
│   ├── memory_consolidator.go   # Token 级记忆整合
│   ├── skills.go                # 技能加载器
│   ├── subagent.go              # 子代理管理器
│   └── tools/                   # 13+ 内置工具
├── bus/                         # 消息总线
├── channels/                    # 12 个渠道实现
├── config/                      # 配置 Schema + YAML 加载
├── providers/                   # 18 Provider + Azure OpenAI
├── session/                     # 会话管理 (JSONL 持久化)
├── security/                    # SSRF 防护
├── cron/                        # 定时任务服务
├── heartbeat/                   # 心跳服务
├── skills/                      # 8 个内置技能
├── utils/                       # LRU 缓存 / 通知评估器
└── templates/                   # Workspace 模板
```

---

## 🛠️ 技术栈

| 组件 | 技术 |
|---|---|
| Agent 框架 | [Eino ADK](https://github.com/cloudwego/eino) |
| MCP 客户端 | [mcp-go](https://github.com/mark3labs/mcp-go) |
| CLI | [Cobra](https://github.com/spf13/cobra) |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) |
| 日志 | [Zap](https://go.uber.org/zap) |
| 飞书 SDK | [oapi-sdk-go](https://github.com/larksuite/oapi-sdk-go) |
| Matrix SDK | [mautrix-go](https://github.com/mautrix/go) |

---

## 🏗️ 跨平台构建

```bash
make build          # 当前平台
make build-all      # 全部平台
```

| 平台 | 架构 |
|---|---|
| linux | amd64 / arm64 / mips64le |
| darwin | amd64 / arm64 (Apple Silicon) |
| windows | amd64 |
| android | arm64 |

全部静态编译（CGO_ENABLED=0），无外部依赖。

---

## 📄 License

MIT
