package telegram

import (
	"context"
	"fmt"
	"strings"

	"msgflow/internal/plugin"
)

const defaultBaseURL = "https://api.telegram.org"

// TelegramNotifier 实现 Telegram Bot API 推送渠道。
type TelegramNotifier struct {
	plugin.BaseNotifier
}

// Name 返回当前通知器的唯一类型标识。
func (n *TelegramNotifier) Name() string {
	return "telegram"
}

// sendMessageRequest 定义 Telegram sendMessage API 的请求体。
type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// telegramResponse 对应 Telegram API 的响应。
type telegramResponse struct {
	Ok          bool   `json:"ok"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

// Send 通过 Telegram Bot API 发送消息。
func (n *TelegramNotifier) Send(ctx context.Context, msg plugin.Message, config map[string]string) error {
	botToken := config["bot_token"]
	chatID := config["chat_id"]
	baseURL := config["base_url"]
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram config missing bot_token or chat_id")
	}

	// 组合消息内容，标题存在时放在正文前。
	text := msg.Body
	if msg.Title != "" {
		text = msg.Title + "\n" + msg.Body
	}

	// 构建 API 地址：{base_url}/bot{token}/sendMessage
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(baseURL, "/"), botToken)

	parseMode := config["parse_mode"]

	respBody := &telegramResponse{}
	resp, err := n.Client().R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(sendMessageRequest{
			ChatID:    chatID,
			Text:      text,
			ParseMode: parseMode,
		}).
		SetResult(respBody).
		Post(apiURL)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("telegram request failed with status %d", resp.StatusCode())
	}
	if !respBody.Ok {
		return fmt.Errorf("telegram send failed: [%d] %s", respBody.ErrorCode, respBody.Description)
	}

	return nil
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&TelegramNotifier{})
}
