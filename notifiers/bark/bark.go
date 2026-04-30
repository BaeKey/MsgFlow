package bark

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"msgflow/internal/plugin"
)

// BarkNotifier 实现 Bark 推送渠道。
type BarkNotifier struct {
	plugin.BaseNotifier
}

// Name 返回当前通知器的唯一类型标识。
func (n *BarkNotifier) Name() string {
	return "bark"
}

// ValidateConfig 校验 Bark 配置。
func (n *BarkNotifier) ValidateConfig(config map[string]string) error {
	serverURL := strings.TrimRight(config["server_url"], "/")
	deviceKey := strings.Trim(config["device_key"], "/")
	if serverURL == "" || deviceKey == "" {
		return fmt.Errorf("bark config missing server_url or device_key")
	}
	return nil
}

// Send 按 Bark API 规范发送消息。
func (n *BarkNotifier) Send(ctx context.Context, msg plugin.Message, config map[string]string) error {
	serverURL := strings.TrimRight(config["server_url"], "/")
	deviceKey := strings.Trim(config["device_key"], "/")
	if serverURL == "" || deviceKey == "" {
		return fmt.Errorf("bark config missing server_url or device_key")
	}

	// 根据是否存在标题选择不同的 Bark URL 路径格式。
	var requestURL string
	if msg.Title == "" {
		requestURL = fmt.Sprintf("%s/%s/%s", serverURL, url.PathEscape(deviceKey), url.PathEscape(msg.Body))
	} else {
		requestURL = fmt.Sprintf("%s/%s/%s/%s", serverURL, url.PathEscape(deviceKey), url.PathEscape(msg.Title), url.PathEscape(msg.Body))
	}

	// 通过复用的 HTTP 客户端发送 GET 请求。
	resp, err := n.Client().R().SetContext(ctx).Get(requestURL)
	if err != nil {
		return fmt.Errorf("bark request failed: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("bark request failed with status %d", resp.StatusCode())
	}

	return nil
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&BarkNotifier{})
}
