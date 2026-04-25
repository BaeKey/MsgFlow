package wecom

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"msgflow/internal/plugin"
)

const defaultBaseURL = "https://qyapi.weixin.qq.com"

// WeComNotifier 实现企业微信文本消息推送。
type WeComNotifier struct {
	plugin.BaseNotifier
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// sendRequestBody 定义企业微信发送消息接口的请求体。
type sendRequestBody struct {
	ToUser  string          `json:"touser"`
	MsgType string          `json:"msgtype"`
	AgentID string          `json:"agentid"`
	Text    sendRequestText `json:"text"`
}

// sendRequestText 定义 text 消息内容。
type sendRequestText struct {
	Content string `json:"content"`
}

// tokenResponse 对应企业微信获取 access-token 的响应。
type tokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// sendResponse 对应企业微信发送消息的响应。
type sendResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// Name 返回当前通知器的唯一类型标识。
func (n *WeComNotifier) Name() string {
	return "wecom"
}

// Send 调用企业微信 API 发送 text 类型应用消息。
func (n *WeComNotifier) Send(ctx context.Context, msg plugin.Message, config map[string]string) error {
	// 读取企业微信应用配置。
	corpID := config["corp_id"]
	agentID := config["agent_id"]
	corpSecret := config["corp_secret"]
	defaultToUser := config["default_to_user"]
	baseURL := config["base_url"]
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if corpID == "" || agentID == "" || corpSecret == "" {
		return fmt.Errorf("wecom config missing required fields")
	}

	// 接收人优先使用局部覆盖值，否则使用全局默认值。
	toUser := strings.TrimSpace(msg.Extra["wecom_to_user"])
	if toUser == "" {
		toUser = strings.TrimSpace(defaultToUser)
	}
	if toUser == "" {
		return fmt.Errorf("wecom recipient is empty")
	}

	// 获取缓存中的 access_token，不足时自动刷新。
	token, err := n.getAccessToken(ctx, corpID, corpSecret, baseURL)
	if err != nil {
		return err
	}

	// 组合最终发送内容，标题存在时放在正文前。
	content := msg.Body
	if msg.Title != "" {
		content = msg.Title + "\n" + msg.Body
	}

	// 发送企业微信 text 消息。
	respBody := &sendResponse{}
	resp, err := n.Client().R().
		SetContext(ctx).
		SetQueryParam("access_token", token).
		SetBody(sendRequestBody{
			ToUser:  toUser,
			MsgType: "text",
			AgentID: agentID,
			Text: sendRequestText{
				Content: content,
			},
		}).
		SetResult(respBody).
		Post(baseURL + "/cgi-bin/message/send")
	if err != nil {
		return fmt.Errorf("wecom send request failed: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("wecom send request failed with status %d", resp.StatusCode())
	}
	if respBody.ErrCode != 0 {
		return fmt.Errorf("wecom send failed: %s", respBody.ErrMsg)
	}

	return nil
}

// getAccessToken 获取并缓存 access_token，提前 5 分钟刷新。
// 使用 double-check locking：token 有效时走快速路径（不加锁），仅在需要刷新时加锁。
func (n *WeComNotifier) getAccessToken(ctx context.Context, corpID, corpSecret, baseURL string) (string, error) {
	// 快速路径：token 仍在有效期内，直接返回，无需加锁。
	now := time.Now()
	if n.accessToken != "" && now.Before(n.expiresAt) {
		return n.accessToken, nil
	}

	// 慢路径：需要刷新 token，加锁。
	n.mu.Lock()
	defer n.mu.Unlock()

	// 再次检查：可能其他 goroutine 已经在等待锁期间完成了刷新。
	now = time.Now()
	if n.accessToken != "" && now.Before(n.expiresAt) {
		return n.accessToken, nil
	}

	// 请求企业微信接口获取新的 token。
	result := &tokenResponse{}
	resp, err := n.Client().R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"corpid":     corpID,
			"corpsecret": corpSecret,
		}).
		SetResult(result).
		Get(baseURL + "/cgi-bin/gettoken")
	if err != nil {
		return "", fmt.Errorf("wecom get token failed: %w", err)
	}
	if resp.IsError() {
		return "", fmt.Errorf("wecom get token failed with status %d", resp.StatusCode())
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wecom get token failed: %s", result.ErrMsg)
	}

	// 将过期时间提前 5 分钟，避免刚好命中过期边界。
	refreshBefore := 5 * time.Minute
	expireAfter := time.Duration(result.ExpiresIn) * time.Second
	if expireAfter <= refreshBefore {
		refreshBefore = time.Minute
	}

	n.accessToken = result.AccessToken
	n.expiresAt = now.Add(expireAfter - refreshBefore)
	return n.accessToken, nil
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&WeComNotifier{})
}
