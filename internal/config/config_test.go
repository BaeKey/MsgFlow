package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadKeepsExplicitRetryZero(t *testing.T) {
	cfg := loadTestConfig(t, `
server:
  token: "secret"
  retry: 0
notifiers:
  bark: {}
`)

	if cfg.Server.Retry != 0 {
		t.Fatalf("expected explicit retry=0 to be preserved, got %d", cfg.Server.Retry)
	}
}

func TestLoadDefaultsRetryWhenUnset(t *testing.T) {
	cfg := loadTestConfig(t, `
server:
  token: "secret"
notifiers:
  bark: {}
`)

	if cfg.Server.Retry != 2 {
		t.Fatalf("expected default retry=2, got %d", cfg.Server.Retry)
	}
}

func TestValidateRejectsNegativeRetry(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Token:           "secret",
			Retry:           -1,
			DefaultChannels: []string{"bark"},
		},
		Notifiers: map[string]map[string]string{
			"bark": {},
		},
	}

	err := cfg.Validate([]string{"bark"})
	if err == nil || !strings.Contains(err.Error(), "server.retry") {
		t.Fatalf("expected server.retry validation error, got %v", err)
	}
}

func loadTestConfig(t *testing.T, content string) *Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}
