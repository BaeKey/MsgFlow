package handler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"msgflow/internal/config"
	"msgflow/internal/plugin"
)

// Handler 聚合配置与日志对象，提供 HTTP 处理方法。
type Handler struct {
	cfg            *config.Config
	logger         *zap.Logger
	deliveryCtx    context.Context
	deduper        *requestDeduper
	channelControl *channelControlManager
}

// sendRequest 对应 POST /send 的 JSON 请求体。
type sendRequest struct {
	Token    string   `json:"token"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Channels []string `json:"channels,omitempty"`
	Channel  string   `json:"ch,omitempty"`
}

// apiResponse 定义统一的 JSON 响应结构。
type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const alertSendTimeout = 5 * time.Second

// dispatchError 是 dispatch 返回的错误类型，携带是否为客户端错误的信息。
type dispatchError struct {
	message    string
	badRequest bool
}

func (e *dispatchError) Error() string { return e.message }

// isBadRequest 返回是否为客户端参数错误（如渠道名不存在）。
func (e *dispatchError) isBadRequest() bool { return e.badRequest }

// New 创建 Handler 实例。
func New(cfg *config.Config, logger *zap.Logger) *Handler {
	return NewWithContext(context.Background(), cfg, logger)
}

// NewWithContext 创建 Handler 实例，并使用 ctx 控制服务级发送生命周期。
func NewWithContext(ctx context.Context, cfg *config.Config, logger *zap.Logger) *Handler {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Handler{
		cfg:            cfg,
		logger:         logger,
		deliveryCtx:    ctx,
		deduper:        newRequestDeduper(cfg.Server.DuplicateWindowSeconds),
		channelControl: newChannelControlManager(cfg),
	}
}

// SendHandler 处理标准 POST /send 请求。
func (h *Handler) SendHandler(c *gin.Context) {
	var req sendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("bind send request failed", zap.Error(err))
		jsonResponse(c, 400, "invalid request body")
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	req.Body = strings.TrimSpace(req.Body)
	if req.Token == "" {
		jsonResponse(c, 401, "unauthorized")
		return
	}
	if req.Body == "" {
		jsonResponse(c, 400, "body is required")
		return
	}
	if !h.cfg.Authenticate(req.Token) {
		jsonResponse(c, 401, "unauthorized")
		return
	}

	// 解析目标渠道：JSON channels 数组 > JSON ch 字段 > 查询参数 ch > default_channels。
	var channels []string
	if len(req.Channels) > 0 {
		channels = h.cfg.ResolveChannels(req.Channels)
	} else if req.Channel != "" {
		channels = h.cfg.ResolveChannels([]string{req.Channel})
	} else {
		channels = h.cfg.ResolveChannels(c.QueryArray("ch"))
	}

	msg := plugin.Message{
		Title: req.Title,
		Body:  req.Body,
	}
	dedupKey := ""
	if len(channels) > 0 {
		var ok bool
		dedupKey, ok = startRequestDedup(h.deduper, msg, channels)
		if !ok {
			jsonResponse(c, 200, "duplicate request ignored")
			return
		}
	}

	if err := h.dispatch(channels, msg); err != nil {
		finishRequestDedup(h.deduper, dedupKey, false)
		h.logger.Error("dispatch send request failed", zap.Error(err))
		statusCode := 500
		var de *dispatchError
		if errors.As(err, &de) && de.isBadRequest() {
			statusCode = 400
		}
		jsonResponse(c, statusCode, "failed: "+err.Error())
		return
	}

	finishRequestDedup(h.deduper, dedupKey, true)
	jsonResponse(c, 200, "success")
}

// channelResult 记录单个渠道的发送结果。
type channelResult struct {
	channel    string
	err        error
	badRequest bool // true 表示客户端参数错误（如渠道名不存在）
}

// dispatch 负责并发调度指定渠道的通知器发送消息。
//
// 流程：
//  1. 所有渠道并发发送，每个渠道独立重试
//  2. 某渠道失败不影响其他渠道
//  3. 重试仍失败的渠道，通过其他可用渠道发送失败通知
func (h *Handler) dispatch(channels []string, baseMsg plugin.Message) error {
	if len(channels) == 0 {
		return &dispatchError{
			message:    "no channels resolved",
			badRequest: true,
		}
	}

	var wg sync.WaitGroup
	results := make([]channelResult, len(channels))

	for i, name := range channels {
		notifier, ok := plugin.Get(name)
		if !ok {
			results[i] = channelResult{channel: name, err: fmt.Errorf("unknown channel: %s", name), badRequest: true}
			continue
		}

		wg.Add(1)
		go func(idx int, n plugin.Notifier, chName string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[idx] = channelResult{
						channel: chName,
						err:     fmt.Errorf("panic recovered: %v", r),
					}
					h.logger.Error("panic recovered in channel send goroutine",
						zap.String("channel", chName),
						zap.Any("panic", r),
						zap.ByteString("stack", debug.Stack()))
				}
			}()

			// 带重试的发送：首次 + 配置的重试次数。
			var lastErr error
			attempts := 1 + h.cfg.Server.Retry
			ctx := h.deliveryCtx
			for attempt := 0; attempt < attempts; attempt++ {
				if attempt > 0 {
					// 重试前等待，支持 ctx 取消时提前退出，并及时释放 timer 资源。
					if err := waitRetryDelay(ctx, time.Duration(attempt)*time.Second); err != nil {
						results[idx] = channelResult{channel: chName, err: err}
						return
					}
				}
				msg := plugin.Message{
					Title: baseMsg.Title,
					Body:  baseMsg.Body,
				}
				release, err := h.channelControl.Acquire(ctx, chName)
				if err != nil {
					lastErr = err
					break
				}
				lastErr = func() error {
					defer release()
					return n.Send(ctx, msg, h.cfg.NotifierConfig(chName))
				}()
				if lastErr == nil {
					break
				}
				h.logger.Warn("channel send failed, may retry",
					zap.String("channel", chName),
					zap.Int("attempt", attempt+1),
					zap.Int("max_attempts", attempts),
					zap.Error(lastErr))
			}
			results[idx] = channelResult{channel: chName, err: lastErr}
		}(i, notifier, name)
	}

	wg.Wait()

	// 分离成功和失败的渠道。
	var failed []string
	var failedDetails []string
	var successChannels []string
	hasBadRequest := false
	for _, r := range results {
		if r.err != nil {
			failed = append(failed, r.channel)
			failedDetails = append(failedDetails, fmt.Sprintf("%s: %v", r.channel, r.err))
			if r.badRequest {
				hasBadRequest = true
			}
		} else {
			successChannels = append(successChannels, r.channel)
		}
	}

	// 如果有失败的渠道，通过其他可用渠道发送失败回告。
	if len(failed) > 0 {
		alertTargets := h.resolveAlertTargets(channels, successChannels, failed)
		if len(alertTargets) > 0 {
			alertMsg := plugin.Message{
				Title: "MsgFlow 发送失败告警",
				Body: fmt.Sprintf("以下渠道发送失败：\n%s\n\n原始消息：\n%s",
					strings.Join(failedDetails, "\n"),
					formatOriginalMessage(baseMsg)),
			}
			h.sendAlert(alertTargets, alertMsg)
		}
	}

	if len(failed) > 0 {
		return &dispatchError{
			message:    fmt.Sprintf("partial failure: %s", strings.Join(failedDetails, "; ")),
			badRequest: hasBadRequest,
		}
	}
	return nil
}

// sendAlert 通过指定渠道发送告警消息，用于通知某个渠道发送失败。
// 告警发送本身的失败只记日志，不影响主流程返回。
func (h *Handler) sendAlert(channels []string, msg plugin.Message) {
	alertCtx, cancel := context.WithTimeout(h.deliveryCtx, alertSendTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, name := range channels {
		notifier, ok := plugin.Get(name)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(n plugin.Notifier, chName string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					h.logger.Error("panic recovered in alert send goroutine",
						zap.String("alert_channel", chName),
						zap.Any("panic", r),
						zap.ByteString("stack", debug.Stack()))
				}
			}()

			release, err := h.channelControl.Acquire(alertCtx, chName)
			if err != nil {
				h.logger.Error("acquire alert channel control failed",
					zap.String("alert_channel", chName), zap.Error(err))
				return
			}
			defer release()
			if err := n.Send(alertCtx, msg, h.cfg.NotifierConfig(chName)); err != nil {
				h.logger.Error("send failure alert failed",
					zap.String("alert_channel", chName), zap.Error(err))
			}
		}(notifier, name)
	}
	wg.Wait()
}

func waitRetryDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fallbackChannels 返回所有已配置渠道中排除本次指定渠道后的列表，
// 用于所有指定渠道都失败时的回退告警。
func (h *Handler) fallbackChannels(exclude []string) []string {
	excluded := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		excluded[name] = struct{}{}
	}

	var result []string
	for _, name := range h.cfg.AllChannelNames() {
		if _, ok := excluded[name]; ok {
			continue
		}
		if _, ok := plugin.Get(name); ok {
			result = append(result, name)
		}
	}
	return result
}

func (h *Handler) resolveAlertTargets(requested, successChannels, failed []string) []string {
	explicit := h.cfg.ExpandChannels(h.cfg.Server.AlertChannels)
	if len(explicit) > 0 {
		return filterChannels(explicit, failed)
	}

	if len(successChannels) > 0 {
		return filterChannels(successChannels, failed)
	}

	return filterChannels(h.fallbackChannels(requested), failed)
}

func filterChannels(channels, exclude []string) []string {
	excluded := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		excluded[name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(channels))
	result := make([]string, 0, len(channels))
	for _, name := range channels {
		if _, ok := excluded[name]; ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

// formatOriginalMessage 格式化原始消息用于告警内容。
func formatOriginalMessage(msg plugin.Message) string {
	return plugin.FormatTextMessage(msg)
}

// jsonResponse 按项目约定返回统一格式的 JSON 响应。
func jsonResponse(c *gin.Context, code int, message string) {
	c.JSON(code, apiResponse{
		Code:    code,
		Message: message,
	})
}

// PushHandler 处理 GET 推送路由（/:token/*path）。
//
// URL 格式:
//
//	/:token/:body              → title 为空
//	/:token/:title/:body       → title 取第一段
//
// 查询参数:
//
//	ch=bark            → 只发 bark
//	ch=bark&ch=wecom   → 发 bark + wecom
//	缺省               → 使用配置的 default_channels
func (h *Handler) PushHandler(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	rawPath := strings.TrimSpace(c.Param("path"))

	if token == "" || !h.cfg.Authenticate(token) {
		jsonResponse(c, 401, "unauthorized")
		return
	}
	if rawPath == "" || rawPath == "/" {
		jsonResponse(c, 400, "body is required")
		return
	}

	// 去掉前导斜杠，按 / 分割。
	segments := strings.Split(strings.TrimPrefix(rawPath, "/"), "/")

	var title, body string
	switch len(segments) {
	case 1:
		body = segments[0]
	default:
		title = segments[0]
		body = segments[1]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		jsonResponse(c, 400, "body is required")
		return
	}

	// 解析目标渠道：查询参数 ch > default_channels。
	channels := h.cfg.ResolveChannels(c.QueryArray("ch"))

	msg := plugin.Message{
		Title: title,
		Body:  body,
	}
	dedupKey := ""
	if len(channels) > 0 {
		var ok bool
		dedupKey, ok = startRequestDedup(h.deduper, msg, channels)
		if !ok {
			jsonResponse(c, 200, "duplicate request ignored")
			return
		}
	}

	if err := h.dispatch(channels, msg); err != nil {
		finishRequestDedup(h.deduper, dedupKey, false)
		h.logger.Error("dispatch push request failed", zap.Error(err))
		statusCode := 500
		var de *dispatchError
		if errors.As(err, &de) && de.isBadRequest() {
			statusCode = 400
		}
		jsonResponse(c, statusCode, "failed: "+err.Error())
		return
	}

	finishRequestDedup(h.deduper, dedupKey, true)
	jsonResponse(c, 200, "success")
}
