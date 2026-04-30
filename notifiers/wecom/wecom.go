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
	mu         sync.Mutex
	tokens     map[string]tokenCache
	refreshing map[string]chan struct{}
}

type tokenCache struct {
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

// ValidateConfig 校验企业微信配置。
func (n *WeComNotifier) ValidateConfig(config map[string]string) error {
	corpID := strings.TrimSpace(config["corp_id"])
	agentID := strings.TrimSpace(config["agent_id"])
	corpSecret := strings.TrimSpace(config["corp_secret"])
	defaultToUser := strings.TrimSpace(config["default_to_user"])
	if corpID == "" || agentID == "" || corpSecret == "" {
		return fmt.Errorf("wecom config missing required fields")
	}
	if defaultToUser == "" {
		return fmt.Errorf("wecom recipient is empty")
	}
	return nil
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

	// 组合最终发送内容，标题和正文之间保留一个空行。
	content := plugin.FormatTextMessage(msg)

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
// 读取和刷新都受同一把锁保护，避免并发发送时发生数据竞态。
func (n *WeComNotifier) getAccessToken(ctx context.Context, corpID, corpSecret, baseURL string) (string, error) {
	cacheKey := strings.Join([]string{corpID, corpSecret, strings.TrimRight(baseURL, "/")}, "|")
	for {
		n.mu.Lock()
		if n.tokens == nil {
			n.tokens = make(map[string]tokenCache)
		}
		if n.refreshing == nil {
			n.refreshing = make(map[string]chan struct{})
		}

		// token 仍在有效期内时直接复用。
		now := time.Now()
		if cached, ok := n.tokens[cacheKey]; ok && cached.accessToken != "" && now.Before(cached.expiresAt) {
			n.mu.Unlock()
			return cached.accessToken, nil
		}

		if waitCh, ok := n.refreshing[cacheKey]; ok {
			n.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-waitCh:
				continue
			}
		}

		waitCh := make(chan struct{})
		n.refreshing[cacheKey] = waitCh
		n.mu.Unlock()

		token, expiresAt, err := n.fetchAccessToken(ctx, corpID, corpSecret, baseURL)

		n.mu.Lock()
		if err == nil {
			n.tokens[cacheKey] = tokenCache{
				accessToken: token,
				expiresAt:   expiresAt,
			}
		}
		close(waitCh)
		delete(n.refreshing, cacheKey)
		n.mu.Unlock()

		if err != nil {
			return "", err
		}
		return token, nil
	}
}

func (n *WeComNotifier) fetchAccessToken(ctx context.Context, corpID, corpSecret, baseURL string) (string, time.Time, error) {
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
		return "", time.Time{}, fmt.Errorf("wecom get token failed: %w", err)
	}
	if resp.IsError() {
		return "", time.Time{}, fmt.Errorf("wecom get token failed with status %d", resp.StatusCode())
	}
	if result.ErrCode != 0 {
		return "", time.Time{}, fmt.Errorf("wecom get token failed: %s", result.ErrMsg)
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("wecom get token failed: empty access_token")
	}
	if result.ExpiresIn <= 0 {
		return "", time.Time{}, fmt.Errorf("wecom get token failed: invalid expires_in %d", result.ExpiresIn)
	}

	// 将过期时间提前 5 分钟，避免刚好命中过期边界。
	refreshBefore := 5 * time.Minute
	expireAfter := time.Duration(result.ExpiresIn) * time.Second
	if expireAfter <= refreshBefore {
		refreshBefore = time.Minute
	}

	return result.AccessToken, time.Now().Add(expireAfter - refreshBefore), nil
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&WeComNotifier{})
}
