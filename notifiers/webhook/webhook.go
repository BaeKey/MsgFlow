package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/go-resty/resty/v2"

	"msgflow/internal/plugin"
)

// WebhookNotifier 实现通用 Webhook 推送渠道。
//
// 每个 WebhookNotifier 实例对应一个命名 webhook 配置，
// 通过 plugin.Register 以自定义名称注册（如 wework、feishu、dingtalk）。
//
// 支持通过 body_template 配置自定义请求体格式，适配不同平台：
//
//	企业微信群机器人：'{"msgtype":"text","text":{"content":"{{.Body}}"}}'
//	飞书机器人：     '{"msg_type":"text","content":{"text":"{{.Body}}"}}'
//	钉钉机器人：     '{"msgtype":"text","text":{"content":"{{.Body}}"}}'
//
// 不配 body_template 时，使用默认格式：{"title":"...","body":"..."}
type WebhookNotifier struct {
	plugin.BaseNotifier
	name   string
	url    string
	method string // POST（默认）或 GET
}

// New 创建指定名称和配置的 WebhookNotifier 实例。
func New(name, url, method string) *WebhookNotifier {
	if method == "" {
		method = "POST"
	}
	return &WebhookNotifier{
		name:   name,
		url:    url,
		method: method,
	}
}

// Name 返回当前通知器的唯一类型标识（即配置中的自定义名称）。
func (n *WebhookNotifier) Name() string {
	return n.name
}

// webhookPayload 是默认的 JSON 请求体结构（无 body_template 时使用）。
type webhookPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// templateData 是 body_template 渲染时可用的数据结构。
type templateData struct {
	Title string
	Body  string
}

// Send 向 webhook URL 发送 JSON 请求。
//
// 请求体生成规则：
//  1. 有 body_template → 用 Go text/template 渲染，可用 {{.Title}} 和 {{.Body}}
//  2. 无 body_template → 使用默认格式 {"title":"...","body":"..."}
func (n *WebhookNotifier) Send(ctx context.Context, msg plugin.Message, config map[string]string) error {
	var body []byte
	var err error

	if tpl := config["body_template"]; tpl != "" {
		// 使用自定义模板渲染请求体。
		body, err = renderTemplate(tpl, msg)
		if err != nil {
			return fmt.Errorf("webhook render template failed: %w", err)
		}
	} else {
		// 使用默认 JSON 格式。
		body, err = json.Marshal(webhookPayload{
			Title: msg.Title,
			Body:  msg.Body,
		})
		if err != nil {
			return fmt.Errorf("webhook marshal failed: %w", err)
		}
	}

	req := n.Client().R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)

	var resp *resty.Response
	switch n.method {
	case "GET":
		resp, err = req.Get(n.url)
	default:
		resp, err = req.Post(n.url)
	}

	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("webhook request failed with status %d", resp.StatusCode())
	}

	return nil
}

// renderTemplate 用 Go text/template 渲染自定义请求体。
func renderTemplate(tpl string, msg plugin.Message) ([]byte, error) {
	t, err := template.New("webhook").Parse(tpl)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, templateData{
		Title: msg.Title,
		Body:  msg.Body,
	}); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	return buf.Bytes(), nil
}
