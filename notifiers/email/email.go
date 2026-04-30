package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"msgflow/internal/plugin"
)

const defaultSMTPTimeout = 30 * time.Second

// EmailNotifier 实现 SMTP 邮件通知。
type EmailNotifier struct{}

// Name 返回当前通知器的唯一类型标识。
func (n *EmailNotifier) Name() string {
	return "email"
}

// ValidateConfig 校验 Email 配置。
func (n *EmailNotifier) ValidateConfig(config map[string]string) error {
	smtpHost := config["smtp_host"]
	smtpPort := config["smtp_port"]
	smtpUser := config["smtp_user"]
	smtpPass := config["smtp_pass"]
	from := config["from"]
	defaultTo := strings.TrimSpace(config["default_to"])

	if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" || from == "" {
		return fmt.Errorf("email config missing required fields")
	}
	if defaultTo == "" {
		return fmt.Errorf("email recipient is empty")
	}
	if _, err := strconv.Atoi(smtpPort); err != nil {
		return fmt.Errorf("invalid smtp_port: %w", err)
	}
	if _, err := strconv.ParseBool(config["smtp_tls"]); err != nil && config["smtp_tls"] != "" {
		return fmt.Errorf("invalid smtp_tls: %w", err)
	}
	if _, err := parseEmailAddress(from); err != nil {
		return fmt.Errorf("invalid email from: %w", err)
	}
	if _, err := parseEmailAddressList(defaultTo); err != nil {
		return fmt.Errorf("invalid email recipients: %w", err)
	}
	return nil
}

// Send 使用标准库 SMTP 客户端发送纯文本邮件。
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

	sendCtx, cancel := context.WithTimeout(ctx, defaultSMTPTimeout)
	defer cancel()

	// 标题为空时使用项目默认标题。
	subject := msg.Title
	if subject == "" {
		subject = "MsgFlow 通知"
	}

	fromAddr, err := parseEmailAddress(from)
	if err != nil {
		return fmt.Errorf("invalid email from: %w", err)
	}
	recipients, err := parseEmailAddressList(to)
	if err != nil {
		return fmt.Errorf("invalid email recipients: %w", err)
	}

	rawMessage, err := buildMessage(fromAddr, recipients, subject, msg.Body)
	if err != nil {
		return fmt.Errorf("build email message failed: %w", err)
	}

	if err := sendSMTP(sendCtx, smtpHost, port, smtpUser, smtpPass, fromAddr.Address, emailAddresses(recipients), rawMessage, useTLS); err != nil {
		return fmt.Errorf("send email failed: %w", err)
	}
	return nil
}

func parseEmailAddress(raw string) (*mail.Address, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty address")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("address contains line break")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(addr.Address) == "" {
		return nil, fmt.Errorf("empty address")
	}
	return addr, nil
}

func parseEmailAddressList(raw string) ([]*mail.Address, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty recipient list")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("recipient list contains line break")
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("empty recipient list")
	}
	for _, addr := range addrs {
		if strings.TrimSpace(addr.Address) == "" {
			return nil, fmt.Errorf("empty recipient address")
		}
	}
	return addrs, nil
}

func emailAddresses(addrs []*mail.Address) []string {
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		result = append(result, addr.Address)
	}
	return result
}

func formatEmailAddressList(addrs []*mail.Address) string {
	formatted := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		formatted = append(formatted, addr.String())
	}
	return strings.Join(formatted, ", ")
}

func buildMessage(from *mail.Address, to []*mail.Address, subject, body string) ([]byte, error) {
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	headers := []string{
		fmt.Sprintf("From: %s", from.String()),
		fmt.Sprintf("To: %s", formatEmailAddressList(to)),
		fmt.Sprintf("Subject: %s", encodedSubject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}

	var builder strings.Builder
	for _, header := range headers {
		builder.WriteString(header)
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")
	builder.WriteString(body)

	return []byte(builder.String()), nil
}

func sendSMTP(ctx context.Context, host string, port int, username, password, from string, to []string, message []byte, useTLS bool) error {
	conn, err := dialSMTP(ctx, host, port, useTLS)
	if err != nil {
		return err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set smtp connection deadline failed: %w", err)
		}
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("create smtp client failed: %w", err)
	}
	defer client.Close()

	if !useTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return fmt.Errorf("smtp starttls failed: %w", err)
			}
		}
	}

	if username != "" {
		auth := smtp.PlainAuth("", username, password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from failed: %w", err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp rcpt to failed for %q: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data failed: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return fmt.Errorf("write smtp message failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close smtp message failed: %w", err)
	}

	if err := client.Quit(); err != nil && !isClosedConnErr(err) {
		return fmt.Errorf("smtp quit failed: %w", err)
	}
	return nil
}

func dialSMTP(ctx context.Context, host string, port int, useTLS bool) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{}

	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	} else {
		dialer.Timeout = defaultSMTPTimeout
	}

	if useTLS {
		return tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func isClosedConnErr(err error) bool {
	return err == io.EOF || strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}

// init 在包加载时自动注册插件。
func init() {
	plugin.Register(&EmailNotifier{})
}
