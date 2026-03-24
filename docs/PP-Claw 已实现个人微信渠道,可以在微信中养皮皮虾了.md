# PP-Claw 已实现个人微信渠道，可以在微信中养皮皮虾了

如果你已经用过 PP-Claw 的 `agent` 模式，或者已经把它接到了飞书、Telegram 这类渠道上，那现在可以把它真正放进自己最常用的聊天入口了：**PP-Claw 已经支持个人微信渠道**。

这篇文档不讲大而全的背景和原理，只讲怎么尽快把它跑起来。你可以把它理解成一份“从零到能在微信里收发消息给皮皮虾”的操作手册。

---

## 1. 先说结果：现在已经支持什么

当前版本已经内置了原生 `wechat_personal` channel，能力包括：

- 由 `gateway` 统一托管启动
- 支持个人微信扫码登录
- 支持登录态本地保存
- 支持重启后自动恢复账号
- 支持多微信账号同时在线
- 支持文本消息收发
- 支持图片、文件等媒体链路
- 支持终端二维码显示

也就是说，它不是一个临时脚本，也不是外部桥接 demo，而是 PP-Claw 渠道体系里的正式成员。

---

## 2. 接入方式和 README 保持一致

如果你已经看过项目的 README.md 的快速开始，那个人微信接入的顺序基本也是一样的：

1. 编译 `pp-claw`
2. 执行 `./pp-claw onboard`
3. 确认基础模型配置可用
4. 打开 ` .pp-claw.yaml 配置文件中的 wechat_personal` 渠道配置
5. 启动 `gateway`
6. 执行微信扫码登录
7. 用微信发一条消息做验证

你可以把微信理解成“在基础能力已经配置完成后，再额外打开的一个正式 channel”。

---

## 3. 第一步：先按 README 完成基础初始化

### 3.1 编译

先在项目根目录执行：

```bash
go build -o pp-claw .
```

### 3.2 初始化配置

然后执行：

```bash
./pp-claw onboard
```

这一步和 README 一致。它会引导你填写至少一组可用的模型配置，比如：

- API Key
- 模型名
- 工作目录

配置文件默认会写到：

```text
~/.pp-claw/pp-claw.yaml
```

如果你之前已经成功跑过下面这些命令：

```bash
./pp-claw agent
./pp-claw agent -m "你好"
```

那说明 PP-Claw 的基础运行环境已经没问题，可以继续接微信。

---

## 4. 第二步：打开个人微信渠道配置

在 `~/.pp-claw/pp-claw.yaml` 里，找到 `channels` 和 `gateway` 配置。如果你是通过 `onboard` 初始化出来的配置，那通常已经有主结构了，你只需要把下面这段合进去。

```yaml
channels:
  wechat_personal:
    enabled: true
    base_url: "https://ilinkai.weixin.qq.com"
    cdn_base_url: "https://novac2c.cdn.weixin.qq.com/c2c"
    bot_type: "3"
    poll_timeout_ms: 35000
    login_timeout_s: 480
    session_pause_minutes: 60
    request_timeout_ms: 15000
    config_timeout_ms: 10000
    allow_from: []
    accounts:
      wx1:
        enabled: true

gateway:
  host: 127.0.0.1
  port: 28790
```

这里有几个字段最值得说明白。

### 4.1 `enabled: true`

这个一定要开。

如果这里还是 `false`，`gateway` 根本不会注册微信登录路由。最典型的现象就是你执行：

```bash
./pp-claw channels wechat login --account wx1
```

然后直接报 `404`。

### 4.2 `accounts.wx1`

这里的 `wx1` 不是你的微信号，而是 PP-Claw 内部给这个微信实例起的名字。

为什么要这样设计？因为个人微信渠道支持多账号同时在线。今天你接一个号，可以叫 `wx1`；明天再接第二个号，可以加一个 `wx2`。这样 PP-Claw 在内部就能区分“哪个微信账号在收消息、哪个微信账号在发消息”。

### 4.3 `gateway.port`

这里我建议你直接用 `28790`，不要默认用 `18790`。

原因很现实：很多开发机上 `18790` 已经被 Docker、旧服务或者别的本地进程占掉了。你表面上看到的是“微信登录报错”，实际根因往往只是请求打到了错误的服务上。

如果你想确认端口是不是空闲，可以执行：

```bash
lsof -n -P -iTCP:28790 -sTCP:LISTEN
```

### 4.4 `base_url` 和 `cdn_base_url`

这两个地址分别负责：

- `base_url`：微信通道的后端 API
- `cdn_base_url`：媒体上传下载

一般情况下保持默认值即可，不需要改。

---

## 5. 第三步：启动 gateway

配置改好以后，启动完整服务：

```bash
./pp-claw gateway
```

这一步和 README 里的完整服务启动方式完全一致。区别只是这一次，除了 Agent、心跳、定时任务之外，`gateway` 还会顺带把 `wechat_personal` 也拉起来。

然后开另一个终端，执行：

```bash
./pp-claw channels status
```

如果一切正常，你应该能看到类似输出：

```text
wechat_personal enabled
accounts=1
```

这说明配置已经被当前这份二进制识别到了。

### 截图预留

> 截图 1：`./pp-claw channels status` 显示 `wechat_personal enabled`

---

## 6. 第四步：开始微信扫码登录

执行下面这条命令：

```bash
./pp-claw channels wechat login --account wx1
```

如果 `gateway` 正常运行，而且微信渠道已经启用，终端会输出几样东西：

- 当前登录的账号 ID
- 一个二维码链接
- 一个直接显示在终端里的二维码
- 登录成功提示

你要做的事情很简单：

1. 打开手机微信
2. 扫描终端里的二维码
3. 在手机上确认授权
4. 等待终端提示“登录成功”

如果终端二维码没显示出来，也不用慌。CLI 会同时把登录链接打印出来，你可以直接复制到浏览器里打开。

### 截图预留

> 截图 2：`./pp-claw channels wechat login --account wx1` 的终端二维码

> 截图 3：扫码后终端显示“登录成功”

---

## 7. 第五步：为什么登录成功后就能直接用了

很多人会以为“扫码成功”只是拿到一个 token。实际上，登录成功以后，PP-Claw 已经替你把后面的运行时链路都接好了。

内部大致会发生这些事情：

1. 微信账号凭证会保存到本地
2. 当前账号会被标记为可运行状态
3. `gateway` 会为这个账号启动长轮询
4. 新消息会进入 `wechat_personal`
5. 然后转成统一的 `InboundMessage`
6. 消息再进入 `MessageBus` 和 `AgentLoop`
7. Agent 生成回复后，再由 `wechat_personal` 发回微信

所以你不需要再去启动第二个进程，也不需要额外跑一个桥接服务。**登录完成，就是接入完成。**

---

## 8. 第六步：验证消息已经能收发

登录完成后，可以再执行一次：

```bash
./pp-claw channels wechat status
```

确认这个账号已经进入可用状态。

然后用你自己的微信给这个账号发一条最简单的消息，比如：

```text
你好
```

如果一切正常，PP-Claw 会：

1. 收到这条微信消息
2. 交给 Agent 处理
3. 把回复再发回微信

到这一步，才算“真的养起来了”。

### 截图预留

> 截图 4：微信聊天窗口里给皮皮虾发送“你好”

> 截图 5：皮皮虾在微信里返回回复结果

---

## 9. 常见问题

### 9.1 登录时报 404

这通常不是微信接口坏了，而是本地启动条件没满足。优先检查这三项：

- `~/.pp-claw/pp-claw.yaml` 里 `channels.wechat_personal.enabled` 是否已经设置为 `true`
- 当前运行的 `gateway` 是否已经重启到最新版本
- `gateway.port` 是否被别的程序占用了

### 9.2 终端二维码没有正常显示图片

这通常是因为微信返回的是一个登录页面链接，而不是直接的二维码图片。当前 CLI 已经做了兼容，会直接把这个登录链接重新编码成终端二维码，所以一般不影响扫码。

### 9.3 改了配置但还是不生效

这通常说明你改的是一份配置，运行的却是另一份配置，或者 `gateway` 还没重启。

最简单的处理方式就是：

```bash
go build -o pp-claw .
./pp-claw gateway
```

然后再重新执行：

```bash
./pp-claw channels status
```

---

## 10. 最短接入路径

如果你只想记住最短操作顺序，那就是下面这几步：

```bash
go build -o pp-claw .
./pp-claw onboard
./pp-claw gateway
./pp-claw channels status
./pp-claw channels wechat login --account wx1
```

然后用手机微信扫一下终端二维码，确认授权，接下来你就可以直接在微信里养皮皮虾了。
