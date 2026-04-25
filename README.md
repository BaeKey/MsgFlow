# MsgFlow

MsgFlow 是一个轻量、无前端界面的 HTTP 消息聚合网关。

它用于接收外部系统发来的 HTTP 请求，完成 Token 鉴权，并将消息并发转发到预配置的通知渠道。渠道采用插件注册机制实现，新增渠道时无需修改核心调度代码。

## 功能特性

- 支持基于 Token 的鉴权
- 支持 GET 和 POST 两种推送方式
- 支持通过 `ch` 查询参数或 `channels` 字段指定目标渠道，缺省时使用配置的默认渠道
- 支持 Bark、Email、企业微信、Telegram 四种内置通知渠道
- 支持通用 Webhook 渠道，可自定义命名和请求体模板（适配飞书、钉钉等群机器人）
- 发送失败时自动重试，所有指定渠道失败时回退到其他已配置渠道发送告警
- 使用插件注册表管理通知器，便于后续扩展
- 使用 `zap` 输出结构化日志，日志等级可配置
- 支持优雅关停

## 技术栈

- Go 1.21+
- `github.com/gin-gonic/gin`
- `github.com/spf13/viper`
- `gopkg.in/gomail.v2`
- `github.com/go-resty/resty/v2`
- `go.uber.org/zap`

## 项目结构

```text
msgflow/
├── main.go
├── config.yaml
├── README.md
├── go.mod
├── deploy/
│   └── msgflow.service
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── server/
│   │   └── server.go
│   ├── handler/
│   │   └── send.go
│   ├── middleware/
│   │   ├── body_limit.go
│   │   └── logger.go
│   ├── plugin/
│   │   └── notifier.go
│   └── security/
│       ├── privilege_unix.go
│       └── privilege_other.go
└── notifiers/
    ├── bark/
    │   └── bark.go
    ├── email/
    │   └── email.go
    ├── wecom/
    │   └── wecom.go
    ├── telegram/
    │   └── telegram.go
    └── webhook/
        └── webhook.go
```

## 配置说明

配置文件默认为项目根目录下的 `config.yaml`。

```yaml
server:
  port: "8080"
  # unix_socket: "/run/msgflow/msgflow.sock"
  token: "your-secret-token"
  default_channels:
    - bark
  log_level: "error"
  retry: 2

notifiers:
  bark:
    server_url: "https://api.day.app"
    device_key: "your-device-key"

  email:
    smtp_host: "smtp.example.com"
    smtp_port: "465"
    smtp_user: "sender@example.com"
    smtp_pass: "your-password"
    smtp_tls: "true"
    from: "sender@example.com"
    default_to: "receiver@example.com"

  wecom:
    corp_id: "your-corp-id"
    agent_id: "1000001"
    corp_secret: "your-corp-secret"
    default_to_user: "@all"
    # base_url: "https://qyapi.weixin.qq.com"

  telegram:
    bot_token: "your-bot-token"
    chat_id: "your-chat-id"
    # 自定义 API 地址（国内可用反代或自建 Bot API 服务器）
    # base_url: "https://api.telegram.org"
    # parse_mode: "HTML"

webhooks:
  # 企业微信群机器人
  wework:
    url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=your-key"
    body_template: '{"msgtype":"text","text":{"content":"{{.Title}}\n{{.Body}}"}}'
  # 飞书机器人
  feishu:
    url: "https://open.feishu.cn/open-apis/bot/v2/hook/your-token"
    body_template: '{"msg_type":"text","content":{"text":"{{.Title}}\n{{.Body}}"}}'
  # 钉钉机器人（markdown 格式）
  dingtalk:
    url: "https://oapi.dingtalk.com/robot/send?access_token=your-token"
    body_template: '{"msgtype":"markdown","markdown":{"title":"{{.Title}}","text":"### {{.Title}}\n{{.Body}}"}}'
```

### 配置规则

- `server.token` 是调用接口时使用的鉴权 Token
- `server.port` 是 TCP 监听端口，配置了 `unix_socket` 时可忽略
- `server.unix_socket` 是 Unix socket 监听路径，配置后优先使用 socket 而非 TCP 端口
- `server.default_channels` 是不指定渠道时的默认发送列表，YAML 数组格式
- `server.log_level` 控制日志输出等级，支持 `debug`/`info`/`warn`/`error`，默认 `error`
- `server.retry` 控制发送失败时的重试次数，默认 `2`（即首次 + 2 次重试 = 最多 3 次尝试）
- `notifiers` 节点下保存各内置渠道的全局配置，按 `map[string]string` 传递给插件
- `webhooks` 节点下可自定义任意数量的 Webhook 渠道，键名即渠道名
- `body_template` 使用 Go text/template 语法，可用变量：`{{.Title}}`、`{{.Body}}`
- 配置中的端口和数字字段统一使用字符串

## 安装依赖

```bash
go mod tidy
```

## 启动方式

直接运行：

```bash
go run .
```

先编译再运行：

```bash
go build -o msgflow .
./msgflow
```

默认监听地址：

```text
http://127.0.0.1:8080
```

### Unix Socket 模式

配置 `unix_socket` 后，MsgFlow 将监听 Unix socket 而非 TCP 端口。适用于与 nginx 等反向代理同机部署、减少网络开销的场景。

```yaml
server:
  # unix_socket: "/run/msgflow/msgflow.sock"
```

启动后日志会显示 socket 路径而非端口号。

**nginx 反代示例：**

```nginx
upstream msgflow {
    server unix:/run/msgflow/msgflow.sock;
}

server {
    listen 80;
    location / {
        proxy_pass http://msgflow;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

**curl 测试 Unix socket：**

```bash
curl --unix-socket /run/msgflow/msgflow.sock "http://localhost/your-token/测试消息"
```

> 注意：socket 文件权限默认为 0666，允许同机所有用户访问。如需限制，可通过 systemd `RuntimeDirectory` 或手动 `chmod` 控制。

## HTTP API

### 1. POST /send（标准发送）

请求：

```http
POST /send
Content-Type: application/json
```

请求体：

```json
{
  "token": "your-secret-token",
  "title": "可选标题",
  "body": "消息正文",
  "channels": ["bark", "wecom"]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `token` | string | 是 | 鉴权 Token |
| `body` | string | 是 | 消息正文 |
| `title` | string | 否 | 消息标题 |
| `channels` | string[] | 否 | 目标渠道列表，缺省使用 `default_channels` |
| `ch` | string | 否 | 单个渠道名（兼容字段，优先级低于 `channels`） |

调用示例：

```bash
# 发送到默认渠道
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","title":"测试标题","body":"消息正文"}'

# 指定多个渠道
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["bark","wecom"]}'

# 指定单个渠道（兼容 ch 字段）
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","ch":"email"}'

# POST 也支持查询参数指定渠道
curl -X POST "http://127.0.0.1:8080/send?ch=bark&ch=wecom" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文"}'
```

渠道解析优先级：`channels` 数组 > `ch` 字段 > 查询参数 `ch` > `default_channels`

### 2. GET /:token/*path（推送接口）

支持以下路由：

```text
GET  /:token/:body              → title 为空
GET  /:token/:title/:body       → 带标题
POST /:token/:body
POST /:token/:title/:body
```

通过 `ch` 查询参数指定渠道，支持多次传递指定多渠道，缺省时使用 `default_channels` 配置。

调用示例：

```bash
# 发送到默认渠道
curl "http://127.0.0.1:8080/your-secret-token/这是一条消息"

# 带标题，发送到默认渠道
curl "http://127.0.0.1:8080/your-secret-token/告警通知/服务异常"

# 指定单个渠道
curl "http://127.0.0.1:8080/your-secret-token/消息正文?ch=email"

# 指定多个渠道（重复 ch 参数）
curl "http://127.0.0.1:8080/your-secret-token/告警/CPU过高?ch=bark&ch=wecom"

# 发送到 Webhook 渠道
curl "http://127.0.0.1:8080/your-secret-token/告警/服务异常?ch=wework"

# 同时发送到内置渠道和 Webhook
curl "http://127.0.0.1:8080/your-secret-token/告警/CPU过高?ch=bark&ch=wework&ch=feishu"

# POST 推送同理
curl -X POST "http://127.0.0.1:8080/your-secret-token/系统告警/CPU使用率过高?ch=email"
```

## 响应格式

成功：

```json
{ "code": 200, "message": "success" }
```

客户端错误（参数错误、渠道不存在）：

```json
{ "code": 400, "message": "body is required" }
{ "code": 400, "message": "failed: partial failure: xxx: unknown channel: notexist" }
```

鉴权失败：

```json
{ "code": 401, "message": "unauthorized" }
```

部分失败（某些渠道发送失败）：

```json
{ "code": 500, "message": "failed: partial failure: bark: bark request failed: connection refused; wecom: wecom send failed: invalid token" }
```

## 通知器说明

### Bark

- 类型名：`bark`
- 必填配置：`server_url`、`device_key`
- 调用方式：发送 GET 请求到 Bark 服务

### Email

- 类型名：`email`
- 必填配置：`smtp_host`、`smtp_port`、`smtp_user`、`smtp_pass`、`from`
- 可选配置：`smtp_tls`、`default_to`
- 邮件主题为空时默认使用 `MsgFlow 通知`

### WeCom（企业微信应用消息）

- 类型名：`wecom`
- 必填配置：`corp_id`、`agent_id`、`corp_secret`
- 可选配置：`default_to_user`、`base_url`（自定义 API 地址，默认 `https://qyapi.weixin.qq.com`）
- 自动缓存 `access_token`，并提前 5 分钟刷新
- 发送消息类型固定为 `text`

### Telegram

- 类型名：`telegram`
- 必填配置：`bot_token`、`chat_id`
- 可选配置：
  - `base_url`：自定义 Bot API 地址（国内可配置反代或自建 API 服务器，默认 `https://api.telegram.org`）
  - `parse_mode`：消息解析模式，支持 `HTML`、`Markdown`、`MarkdownV2`，不配置则为纯文本
- 消息格式：标题存在时以 `标题\n正文` 形式发送

### Webhook

Webhook 是通用 HTTP 推送渠道，在 `webhooks` 节点下以自定义名称配置。

- 必填配置：`url`（Webhook 接收地址）
- 可选配置：
  - `method`：请求方法，默认 `POST`
  - `body_template`：自定义请求体模板，使用 Go text/template 语法
- 不配 `body_template` 时，请求体格式为：`{"title": "...", "body": "..."}`
- 每个自定义名称就是一个渠道，可直接在 `ch` 参数中使用

**body_template 示例：**

```yaml
webhooks:
  # 企业微信群机器人
  wework:
    url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"
    body_template: '{"msgtype":"text","text":{"content":"{{.Title}}\n{{.Body}}"}}'
  # 飞书机器人
  feishu:
    url: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx"
    body_template: '{"msg_type":"text","content":{"text":"{{.Title}}\n{{.Body}}"}}'
  # 钉钉机器人（markdown 格式，标题加粗）
  dingtalk:
    url: "https://oapi.dingtalk.com/robot/send?access_token=xxx"
    body_template: '{"msgtype":"markdown","markdown":{"title":"{{.Title}}","text":"### {{.Title}}\n{{.Body}}"}}'
```

使用示例：

```bash
# 发送到企业微信群机器人
curl "http://127.0.0.1:8080/your-secret-token/告警/服务异常?ch=wework"

# 同时发送到多个 webhook
curl "http://127.0.0.1:8080/your-secret-token/通知/内容?ch=wework&ch=feishu"
```

## 并发与错误处理

- 每个渠道独立发送，互不干扰：某个渠道失败不会影响其他渠道
- 所有渠道并发执行，等待全部完成后返回结果
- 发送失败时自动重试，重试次数由 `server.retry` 控制（默认 2 次）
- 重试间隔递增（第 1 次重试等 1 秒，第 2 次等 2 秒，以此类推）
- 重试支持 context 取消：请求中断时立即停止等待重试
- 重试仍失败的渠道，会通过其他可用渠道发送失败告警通知
- 告警内容包含：失败渠道名称、错误原因、原始消息内容
- 如果部分渠道失败，返回首个错误并附带所有失败渠道的摘要信息
- 不存在的渠道名返回 400 错误（区分于服务端 500 错误）

## 开发说明

### 新增内置通知器

新增通知器时，只需实现 `internal/plugin.Notifier` 接口，并在插件包的 `init()` 中注册：

```go
func init() {
    plugin.Register(&YourNotifier{})
}
```

然后在 `main.go` 中通过空白导入加载插件：

```go
import _ "msgflow/notifiers/your_notifier"
```

### 新增 Webhook 渠道

无需修改代码，在 `config.yaml` 的 `webhooks` 节点下添加即可：

```yaml
webhooks:
  your_name:
    url: "https://your-webhook-url"
    body_template: '{"text":"{{.Title}}\n{{.Body}}"}'
```

### 编译检查

```bash
go build ./...
```

## 安全建议

- 程序默认监听高位端口 `8080`，正常情况下不需要 root 权限
- 若以 root 启动，MsgFlow 会在服务启动前主动降权到 `nobody` 用户
- 建议将 `config.yaml` 权限收紧为仅运行用户可读，例如 `chmod 600 config.yaml`
- 建议仅在内网或受控网关后暴露服务，并通过防火墙限制来源 IP
- 推送路由中 token 位于 URL 路径，MsgFlow 已改为记录路由模板而不是原始路径，避免 token 写入访问日志
- 服务已配置请求体大小限制和 HTTP 超时，以降低滥用和慢连接风险

## systemd 部署

项目内已提供示例服务文件：[msgflow.service](deploy/msgflow.service)

### 1. 编译程序

```bash
go build -o msgflow .
```

### 2. 部署到服务器

```bash
# 创建安装目录
sudo mkdir -p /opt/msgflow

# 复制程序和配置
sudo cp msgflow /opt/msgflow/
sudo cp config.yaml /opt/msgflow/
```

### 3. 安装服务文件

```bash
sudo cp deploy/msgflow.service /etc/systemd/system/msgflow.service
```

### 4. 重新加载并启动

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now msgflow
```

### 5. 查看状态与日志

```bash
systemctl status msgflow
journalctl -u msgflow -f
```

### 6. 最小权限建议

- 推荐不要直接让 `systemd` 以 root 长期运行 MsgFlow
- 当前示例使用 `User=nobody`、`Group=nogroup` 运行
- 如果你的系统没有 `nogroup`，请替换成目标系统上的低权限组
- 如果你希望 `config.yaml` 保持 `600` 权限，建议创建专用账户，例如 `msgflow:msgflow`
- 若改用专用账户，请同步修改服务文件中的 `User`、`Group` 和程序目录权限

### 7. 创建专用服务账户示例

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin msgflow
sudo chown -R msgflow:msgflow /opt/msgflow
sudo chmod 750 /opt/msgflow
sudo chmod 640 /opt/msgflow/config.yaml
```

然后将服务文件中的：

```ini
User=nobody
Group=nogroup
```

改为：

```ini
User=msgflow
Group=msgflow
```

## 日志

程序使用 `zap` 输出结构化日志，包含以下字段：

- `method`
- `path`
- `status`
- `client_ip`
- `latency`

日志等级通过 `server.log_level` 配置，默认 `error`（只记录错误）。

## 许可证

当前仓库未单独声明许可证，如需开源发布，请补充 `LICENSE` 文件。
