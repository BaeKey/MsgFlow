package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// ServerConfig 定义服务监听配置与鉴权参数。
type ServerConfig struct {
	Port                   string   `mapstructure:"port"`
	UnixSocket             string   `mapstructure:"unix_socket"`
	Token                  string   `mapstructure:"token"`
	DefaultChannels        []string `mapstructure:"default_channels"`
	AlertChannels          []string `mapstructure:"alert_channels"`
	LogLevel               string   `mapstructure:"log_level"`
	Retry                  int      `mapstructure:"retry"`
	DuplicateWindowSeconds int      `mapstructure:"duplicate_window_seconds"`
}

// Config 是程序的完整配置结构。
type Config struct {
	Server    ServerConfig                 `mapstructure:"server"`
	Notifiers map[string]map[string]string `mapstructure:"notifiers"`
	Groups    map[string][]string          `mapstructure:"groups"`
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

	// 未配置时默认重试 2 次；显式配置 0 表示不重试。
	if !v.IsSet("server.retry") {
		cfg.Server.Retry = 2
	}
	if !v.IsSet("server.duplicate_window_seconds") {
		cfg.Server.DuplicateWindowSeconds = 10
	}

	// 确保全局通知器配置始终为非 nil，方便后续读取。
	if cfg.Notifiers == nil {
		cfg.Notifiers = make(map[string]map[string]string)
	}
	if cfg.Groups == nil {
		cfg.Groups = make(map[string][]string)
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
// 若名称命中 groups，则展开为对应渠道列表，并按首次出现顺序去重。
func (c *Config) ResolveChannels(ch []string) []string {
	raw := normalizeChannelList(ch)
	if len(raw) == 0 {
		raw = c.Server.DefaultChannels
	}

	return c.expandChannels(raw)
}

// ExpandChannels 仅展开分组和去重，不使用 default_channels 回退。
func (c *Config) ExpandChannels(ch []string) []string {
	return c.expandChannels(normalizeChannelList(ch))
}

func normalizeChannelList(ch []string) []string {
	var raw []string
	for _, name := range ch {
		name = strings.TrimSpace(name)
		if name != "" {
			raw = append(raw, name)
		}
	}
	return raw
}

func (c *Config) expandChannels(raw []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(raw))
	for _, name := range raw {
		members, ok := c.Groups[name]
		if !ok {
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
			continue
		}

		for _, member := range members {
			member = strings.TrimSpace(member)
			if member == "" {
				continue
			}
			if _, exists := seen[member]; exists {
				continue
			}
			seen[member] = struct{}{}
			result = append(result, member)
		}
	}

	return result
}

// Validate 在服务启动前校验配置，避免运行中才暴露配置问题。
func (c *Config) Validate(knownNotifierTypes []string) error {
	if strings.TrimSpace(c.Server.Token) == "" {
		return fmt.Errorf("server.token is required")
	}
	if c.Server.Retry < 0 {
		return fmt.Errorf("server.retry must be >= 0")
	}
	if c.Server.DuplicateWindowSeconds < 0 {
		return fmt.Errorf("server.duplicate_window_seconds must be >= 0")
	}

	known := make(map[string]struct{}, len(knownNotifierTypes))
	for _, name := range knownNotifierTypes {
		known[name] = struct{}{}
	}

	if len(c.Notifiers) == 0 {
		return fmt.Errorf("notifiers must not be empty")
	}

	channelNames := make(map[string]struct{}, len(c.Notifiers))
	for name := range c.Notifiers {
		channelNames[name] = struct{}{}
	}

	for groupName, members := range c.Groups {
		if strings.TrimSpace(groupName) == "" {
			return fmt.Errorf("groups contains empty group name")
		}
		if _, exists := channelNames[groupName]; exists {
			return fmt.Errorf("group %q conflicts with an existing channel name", groupName)
		}
		if len(members) == 0 {
			return fmt.Errorf("group %q must contain at least one channel", groupName)
		}
		for _, member := range members {
			member = strings.TrimSpace(member)
			if member == "" {
				return fmt.Errorf("group %q contains empty channel name", groupName)
			}
			if _, ok := channelNames[member]; !ok {
				return fmt.Errorf("group %q references unknown channel %q", groupName, member)
			}
		}
	}

	if err := c.validateChannelList("server.default_channels", c.Server.DefaultChannels, channelNames); err != nil {
		return err
	}
	if len(c.ExpandChannels(c.Server.DefaultChannels)) == 0 {
		return fmt.Errorf("server.default_channels resolved to empty channel list")
	}
	if err := c.validateChannelList("server.alert_channels", c.Server.AlertChannels, channelNames); err != nil {
		return err
	}

	for channelName, raw := range c.Notifiers {
		notifierType := strings.TrimSpace(raw["type"])
		if notifierType == "" {
			notifierType = channelName
		}
		if _, ok := known[notifierType]; !ok {
			validTypes := append([]string(nil), knownNotifierTypes...)
			sort.Strings(validTypes)
			return fmt.Errorf("channel %q references unknown notifier type %q, valid types: %s",
				channelName, notifierType, strings.Join(validTypes, ", "))
		}
		if err := validateNonNegativeIntField(raw, channelName, "max_concurrency"); err != nil {
			return err
		}
		if err := validateNonNegativeIntField(raw, channelName, "min_interval_ms"); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) validateChannelList(field string, channels []string, channelNames map[string]struct{}) error {
	for _, name := range channels {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s contains empty entry", field)
		}
		if _, isGroup := c.Groups[name]; isGroup {
			continue
		}
		if _, ok := channelNames[name]; !ok {
			return fmt.Errorf("%s references unknown channel or group %q", field, name)
		}
	}
	return nil
}

func validateNonNegativeIntField(raw map[string]string, channelName, field string) error {
	value := strings.TrimSpace(raw[field])
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("channel %q has invalid %s: %w", channelName, field, err)
	}
	if parsed < 0 {
		return fmt.Errorf("channel %q has negative %s", channelName, field)
	}
	return nil
}

// AllChannelNames 返回所有已配置的渠道名称列表。
func (c *Config) AllChannelNames() []string {
	names := make([]string, 0, len(c.Notifiers))
	for name := range c.Notifiers {
		names = append(names, name)
	}
	return names
}

// NotifierConfig 返回指定通知器配置的副本，避免并发场景下修改原始数据。
func (c *Config) NotifierConfig(name string) map[string]string {
	if raw, ok := c.Notifiers[name]; ok {
		cloned := make(map[string]string, len(raw))
		for k, v := range raw {
			cloned[k] = v
		}
		return cloned
	}
	return map[string]string{}
}
