package email

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	gomail "gopkg.in/gomail.v2"

	"msgflow/internal/plugin"
)

// EmailNotifier 实现 SMTP 邮件通知。
type EmailNotifier struct{}

// Name 返回当前通知器的唯一类型标识。
func (n *EmailNotifier) Name() string {
	return "email"
}

// Send 使用 gomail 发送纯文本邮件。
func (n *EmailNotifier) Send(ctx context.Context, msg plugin.Message, config map[string]string) error {
	// 从全局配置中提取 SMTP 连接参数。
	smtpHost := config["smtp_host"]
	smtpPort := config["smtp_port"]
	smtpUser := config["smtp_user"]
	smtpPass := config["smtp_pass"]
	from := config["from"]
	defaultTo := config["default_to"]

	if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" || from == "" {
		return fmt.Errorf("email config missing required fields")
	}

	// 收件人使用全局默认值。
	to := strings.TrimSpace(defaultTo)
	if to == "" {
		return fmt.Errorf("email recipient is empty")
	}

	port, err := strconv.Atoi(smtpPort)
	if err != nil {
		return fmt.Errorf("invalid smtp_port: %w", err)
	}

	useTLS, err := strconv.ParseBool(config["smtp_tls"])
	if err != nil && config["smtp_tls"] != "" {
		return fmt.Errorf("invalid smtp_tls: %w", err)
	}

	// 标题为空时使用项目默认标题。
	subject := msg.Title
	if subject == "" {
		subject = "MsgFlow 通知"
	}

	// 构造纯文本邮件内容。
	message := gomail.NewMessage()
	message.SetHeader("From", from)
	message.SetHeader("To", to)
	message.SetHeader("Subject", subject)
	message.SetBody("text/plain; charset=UTF-8", msg.Body)

	dialer := gomail.NewDialer(smtpHost, port, smtpUser, smtpPass)
	dialer.SSL = useTLS

	// gomail 不支持 context 取消，DialAndSend 会一直阻塞直到 SMTP 操作完成。
	// 为防止 goroutine 无限挂起，用带超时的 context 控制整体时间上限。
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 将阻塞发送放到 goroutine 中，以便响应 ctx 取消信号。
	errCh := make(chan error, 1)
	go func() {
		errCh <- dialer.DialAndSend(message)
	}()

	select {
	case <-sendCtx.Done():
		// 注意：DialAndSend 不支持 context 取消，goroutine 会在 SMTP 操作
		// 完成后自动退出（gomail 内部 dial 超时 10s），不会无限挂起。
		return sendCtx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("send email failed: %w", err)
		}
		return nil
	}
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&EmailNotifier{})
}
