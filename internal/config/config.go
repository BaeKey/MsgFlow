package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// ServerConfig 定义服务监听配置与鉴权参数。
type ServerConfig struct {
	Port            string   `mapstructure:"port"`
	UnixSocket      string   `mapstructure:"unix_socket"`
	Token           string   `mapstructure:"token"`
	DefaultChannels []string `mapstructure:"default_channels"`
	LogLevel        string   `mapstructure:"log_level"`
	Retry           int      `mapstructure:"retry"`
}

// Config 是程序的完整配置结构。
type Config struct {
	Server    ServerConfig                 `mapstructure:"server"`
	Notifiers map[string]map[string]string `mapstructure:"notifiers"`
	Webhooks  map[string]map[string]string `mapstructure:"webhooks"`
}

// Load 从指定路径加载 YAML 配置并反序列化到结构体中。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// 当端口未配置时，使用默认端口 8080。
	if cfg.Server.Port == "" {
		cfg.Server.Port = "8080"
	}

	// 默认重试 2 次。
	if cfg.Server.Retry <= 0 {
		cfg.Server.Retry = 2
	}

	// 确保全局通知器配置始终为非 nil，方便后续读取。
	if cfg.Notifiers == nil {
		cfg.Notifiers = make(map[string]map[string]string)
	}
	if cfg.Webhooks == nil {
		cfg.Webhooks = make(map[string]map[string]string)
	}

	return cfg, nil
}

// ListenAddr 返回实际监听地址。
// 配置了 unix_socket 时返回 socket 文件路径，否则返回 TCP 端口地址。
func (c *Config) ListenAddr() string {
	if c.Server.UnixSocket != "" {
		return c.Server.UnixSocket
	}
	return ":" + c.Server.Port
}

// IsUnixSocket 返回是否使用 Unix socket 监听。
func (c *Config) IsUnixSocket() bool {
	return c.Server.UnixSocket != ""
}

// Authenticate 校验请求携带的 token 是否与配置一致。
func (c *Config) Authenticate(token string) bool {
	return token != "" && token == c.Server.Token
}

// ZapLevel 将配置中的日志等级字符串转为 zap 日志等级。
// 支持 debug/info/warn/error，默认为 error。
func (c *Config) ZapLevel() string {
	switch strings.ToLower(c.Server.LogLevel) {
	case "debug":
		return "debug"
	case "info":
		return "info"
	case "warn":
		return "warn"
	default:
		return "error"
	}
}

// ResolveChannels 将请求中的渠道列表解析为最终发送目标。
//
// ch 为空时返回 default_channels；ch 非空时去空白后直接使用。
func (c *Config) ResolveChannels(ch []string) []string {
	var result []string
	for _, name := range ch {
		name = strings.TrimSpace(name)
		if name != "" {
			result = append(result, name)
		}
	}
	if len(result) == 0 {
		return c.Server.DefaultChannels
	}
	return result
}

// AllChannelNames 返回所有已配置的渠道名称列表（notifiers + webhooks）。
func (c *Config) AllChannelNames() []string {
	names := make([]string, 0, len(c.Notifiers)+len(c.Webhooks))
	for name := range c.Notifiers {
		names = append(names, name)
	}
	for name := range c.Webhooks {
		names = append(names, name)
	}
	return names
}

// NotifierConfig 返回指定通知器或 webhook 的全局配置副本。
// 先查 notifiers，未找到再查 webhooks，避免并发场景下修改原始数据。
func (c *Config) NotifierConfig(name string) map[string]string {
	if raw, ok := c.Notifiers[name]; ok {
		cloned := make(map[string]string, len(raw))
		for k, v := range raw {
			cloned[k] = v
		}
		return cloned
	}
	if raw, ok := c.Webhooks[name]; ok {
		cloned := make(map[string]string, len(raw))
		for k, v := range raw {
			cloned[k] = v
		}
		return cloned
	}
	return map[string]string{}
}


