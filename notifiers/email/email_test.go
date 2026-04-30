package email

import (
	"strings"
	"testing"
)

func TestValidateConfigRejectsHeaderInjectionInAddresses(t *testing.T) {
	notifier := &EmailNotifier{}

	cfg := validEmailConfig()
	cfg["from"] = "sender@example.com\r\nBcc: victim@example.com"
	if err := notifier.ValidateConfig(cfg); err == nil {
		t.Fatal("expected injected from address to be rejected")
	}

	cfg = validEmailConfig()
	cfg["default_to"] = "ops@example.com\r\nBcc: victim@example.com"
	if err := notifier.ValidateConfig(cfg); err == nil {
		t.Fatal("expected injected recipient list to be rejected")
	}
}

func TestValidateConfigRejectsInvalidRecipients(t *testing.T) {
	notifier := &EmailNotifier{}

	cfg := validEmailConfig()
	cfg["default_to"] = "ops@example.com, not an address"
	if err := notifier.ValidateConfig(cfg); err == nil {
		t.Fatal("expected invalid recipient list to be rejected")
	}
}

func TestBuildMessageUsesParsedAddressHeaders(t *testing.T) {
	from, err := parseEmailAddress("MsgFlow <sender@example.com>")
	if err != nil {
		t.Fatalf("parse from: %v", err)
	}
	to, err := parseEmailAddressList("Ops <ops@example.com>, audit@example.com")
	if err != nil {
		t.Fatalf("parse recipients: %v", err)
	}

	raw, err := buildMessage(from, to, "hello", "body")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	message := string(raw)
	if strings.Contains(message, "\r\nBcc:") {
		t.Fatalf("message unexpectedly contains injected header: %q", message)
	}
	if !strings.Contains(message, "From:") || !strings.Contains(message, "To:") {
		t.Fatalf("message is missing address headers: %q", message)
	}
}

func validEmailConfig() map[string]string {
	return map[string]string{
		"smtp_host":  "smtp.example.com",
		"smtp_port":  "465",
		"smtp_user":  "sender@example.com",
		"smtp_pass":  "password",
		"smtp_tls":   "true",
		"from":       "MsgFlow <sender@example.com>",
		"default_to": "Ops <ops@example.com>, audit@example.com",
	}
}
