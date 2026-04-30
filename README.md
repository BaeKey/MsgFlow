# MsgFlow

MsgFlow 是一个轻量、无前端界面的 HTTP 消息聚合网关。

它用于接收外部系统发来的 HTTP 请求，完成 Token 鉴权，并将消息并发转发到预配置的通知渠道。渠道采用插件注册机制实现，新增渠道时无需修改核心调度代码。

## 功能特性

- 支持基于 Token 的鉴权
- 支持 GET 和 POST 两种推送方式
- 支持通过 `ch` 查询参数或 `channels` 字段指定目标渠道，缺省时使用配置的默认渠道
- 支持 Bark、Email、企业微信三种内置通知渠道
- 支持任意通知器类型配置多个命名渠道，适合同类目标多实例共存
- 支持渠道分组发送，可将多个渠道收敛为一个组名
- 支持启动前配置校验，避免错误配置带着服务跑起来
- 支持短时间重复请求去重，默认 10 秒内相同消息不重复处理
- 支持按渠道配置并发上限和最小发送间隔，便于控制下游频率
- 发送失败时自动重试，并支持显式配置失败告警渠道
- 使用插件注册表管理通知器，便于后续扩展
- 使用 `zap` 输出结构化日志，日志等级可配置
- 支持优雅关停

## 技术栈

- Go 1.21+
- `github.com/gin-gonic/gin`
- `github.com/spf13/viper`
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
    └── wecom/
        └── wecom.go
```

## 配置说明

配置文件默认为项目根目录下的 `config.yaml`。

```yaml
server:
  port: "8080"
  # unix_socket: "/run/msgflow/msgflow.sock"
  token: "your-secret-token"
  default_channels:
    - default
  alert_channels:
    - alert
  log_level: "error"
  retry: 2
  duplicate_window_seconds: 10

notifiers:
  bark_phone:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "your-phone-device-key"
    max_concurrency: "2"
    min_interval_ms: "500"

  bark_tablet:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "your-tablet-device-key"

  email_ops:
    type: "email"
    smtp_host: "smtp.example.com"
    smtp_port: "465"
    smtp_user: "sender@example.com"
    smtp_pass: "your-password"
    smtp_tls: "true"
    from: "sender@example.com"
    default_to: "ops@example.com"

  email_audit:
    type: "email"
    smtp_host: "smtp.example.com"
    smtp_port: "465"
    smtp_user: "sender@example.com"
    smtp_pass: "your-password"
    smtp_tls: "true"
    from: "sender@example.com"
    default_to: "audit@example.com"

  wecom_notice:
    type: "wecom"
    corp_id: "your-corp-id"
    agent_id: "1000001"
    corp_secret: "your-corp-secret"
    default_to_user: "@all"
    max_concurrency: "1"
    min_interval_ms: "1000"
    # base_url: "https://qyapi.weixin.qq.com"

  wecom_alert:
    type: "wecom"
    corp_id: "your-corp-id"
    agent_id: "1000002"
    corp_secret: "your-other-corp-secret"
    default_to_user: "@all"
    max_concurrency: "1"
    min_interval_ms: "1000"

groups:
  default:
    - bark_phone
  ops:
    - wecom_notice
  alert:
    - wecom_alert
    - bark_tablet
```

### 配置规则

- `server.token` 是调用接口时使用的鉴权 Token
- `server.port` 是 TCP 监听端口，配置了 `unix_socket` 时可忽略
- `server.unix_socket` 是 Unix socket 监听路径，配置后优先使用 socket 而非 TCP 端口
- `server.default_channels` 是不指定渠道时的默认发送列表，YAML 数组格式
- `server.alert_channels` 用于指定失败告警发送到哪些渠道或分组；未配置时保持原有回退策略
- `server.log_level` 控制日志输出等级，支持 `debug`/`info`/`warn`/`error`，默认 `error`
- `server.retry` 控制发送失败时的重试次数，默认 `2`（即首次 + 2 次重试 = 最多 3 次尝试），配置为 `0` 时不重试
- `server.duplicate_window_seconds` 控制重复请求去重窗口，默认 `10`
- `notifiers` 节点下每个键都是“渠道名”，值为对应通知器配置
- `type` 为可选字段；不写时默认将渠道名视为通知器类型，例如 `bark`、`email`、`wecom`
- 当同一通知器需要配置多个渠道时，建议显式写 `type`，例如 `bark_phone.type: bark`
- `max_concurrency` 用于限制该渠道同时并发发送数
- `min_interval_ms` 用于限制该渠道两次发送启动之间的最小间隔
- `groups` 节点用于定义渠道分组，组内成员填写真实渠道名
- `default_channels` 既可以直接写渠道名，也可以写分组名
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
  -d '{"token":"your-secret-token","body":"消息正文","channels":["bark_phone","wecom_notice"]}'

# 按分组发送
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["ops"]}'

# 分组和单独渠道混合发送
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["ops","email"]}'

# 指定单个渠道（兼容 ch 字段）
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","ch":"email"}'

# POST 也支持查询参数指定渠道
curl -X POST "http://127.0.0.1:8080/send?ch=bark_phone&ch=wecom_alert" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文"}'
```

渠道解析优先级：`channels` 数组 > `ch` 字段 > 查询参数 `ch` > `default_channels`

若传入的是分组名，服务端会在发送前展开为组内渠道，并按首次出现顺序去重。
组名和单独渠道名可以混合传递，例如 `["ops","email"]`。

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
curl "http://127.0.0.1:8080/your-secret-token/告警/CPU过高?ch=bark_phone&ch=wecom_alert"

# 通过分组发送
curl "http://127.0.0.1:8080/your-secret-token/告警/CPU过高?ch=alert"

# 分组和单独渠道混合发送
curl "http://127.0.0.1:8080/your-secret-token/告警/CPU过高?ch=ops&ch=email"

# POST 推送同理
curl -X POST "http://127.0.0.1:8080/your-secret-token/系统告警/CPU使用率过高?ch=email"
```

## 响应格式

成功：

```json
{ "code": 200, "message": "success" }
```

重复请求被忽略：

```json
{ "code": 200, "message": "duplicate request ignored" }
```

客户端错误（参数错误、渠道不存在）：

```json
{ "code": 400, "message": "body is required" }
{ "code": 400, "message": "failed: no channels resolved" }
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
- 必填配置：`smtp_host`、`smtp_port`、`smtp_user`、`smtp_pass`、`from`、`default_to`
- 可选配置：`smtp_tls`
- 邮件主题为空时默认使用 `MsgFlow 通知`

### WeCom（企业微信应用消息）

- 类型名：`wecom`
- 必填配置：`corp_id`、`agent_id`、`corp_secret`
- 可选配置：`default_to_user`、`base_url`（自定义 API 地址，默认 `https://qyapi.weixin.qq.com`）
- 支持通过多个命名渠道复用 `wecom` 类型，例如 `wecom_notice`、`wecom_alert`
- 自动缓存 `access_token`，并提前 5 分钟刷新
- 发送消息类型固定为 `text`
- 标题和正文会以 `标题\n\n正文` 形式发送

### 多实例共存

所有内置通知器都支持多个命名渠道共存，不只企业微信。核心规则是：

- `notifiers` 里的键名始终是“渠道名”
- `type` 决定底层使用哪一种通知器
- 只要渠道名和通知器类型不同，就显式写 `type`

例如同一种通知器可以这样配置：

```yaml
notifiers:
  bark_phone:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "phone-key"

  bark_tablet:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "tablet-key"

  email_ops:
    type: "email"
    smtp_host: "smtp.example.com"
    smtp_port: "465"
    smtp_user: "sender@example.com"
    smtp_pass: "your-password"
    from: "sender@example.com"
    default_to: "ops@example.com"

  wecom_alert:
    type: "wecom"
    corp_id: "your-corp-id"
    agent_id: "1000002"
    corp_secret: "your-other-corp-secret"
    default_to_user: "@all"
```

调用时直接通过渠道名区分：

```bash
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["bark_phone","email_ops","wecom_alert"]}'
```

## 并发与错误处理

- 每个渠道独立发送，互不干扰：某个渠道失败不会影响其他渠道
- 所有渠道并发执行，等待全部完成后返回结果
- 发送失败时自动重试，重试次数由 `server.retry` 控制（默认 2 次，配置为 0 可关闭重试）
- 重试间隔递增（第 1 次重试等 1 秒，第 2 次等 2 秒，以此类推）
- 支持按渠道配置 `max_concurrency` 和 `min_interval_ms`，避免短时间内把下游打爆
- 重试受服务级发送上下文控制：客户端断开不会中断已接收的通知投递，服务退出时会取消后续发送
- 默认会优先通过成功渠道发送失败告警；若配置了 `server.alert_channels`，则只走显式配置的告警渠道
- 告警内容包含：失败渠道名称、错误原因、原始消息内容
- 如果部分渠道失败，返回首个错误并附带所有失败渠道的摘要信息
- 不存在的渠道名返回 400 错误（区分于服务端 500 错误）
- 相同标题、正文和目标渠道的请求，在去重窗口内只会处理一次

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

### 配置多个同类型渠道

例如你要给任意通知器配置多个命名渠道：

```yaml
notifiers:
  bark_phone:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "phone-key"

  bark_tablet:
    type: "bark"
    server_url: "https://api.day.app"
    device_key: "tablet-key"

  wecom_alert:
    type: "wecom"
    corp_id: "your-corp-id"
    agent_id: "1000002"
    corp_secret: "your-other-corp-secret"
    default_to_user: "@all"
```

调用时直接通过渠道名区分：

```bash
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["bark_phone","bark_tablet","wecom_alert"]}'
```

### 配置渠道分组

例如你希望把常用接收方做成组：

```yaml
groups:
  ops:
    - wecom_notice
    - email_ops
  alert:
    - wecom_alert
    - bark
```

调用时直接传组名：

```bash
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["ops"]}'
```

也可以和单独渠道混用：

```bash
curl -X POST "http://127.0.0.1:8080/send" \
  -H "Content-Type: application/json" \
  -d '{"token":"your-secret-token","body":"消息正文","channels":["ops","email"]}'
```

### 编译检查

```bash
go build ./...
```

## 安全建议

- 程序默认监听高位端口 `8080`，正常情况下不需要 root 权限
- 若以 root 启动，MsgFlow 会先完成端口或 Unix socket 监听，再主动降权到 `nobody` 用户
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
