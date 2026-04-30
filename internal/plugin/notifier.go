package plugin

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

// Message 是统一消息结构体
type Message struct {
	// Title 表示消息标题，可为空。
	Title string
	// Body 表示消息正文，为必填字段。
	Body string
	// Extra 存放渠道特定的覆盖参数，来自 HTTP 请求
	Extra map[string]string
}

// FormatTextMessage 将标题和正文拼成纯文本消息。
// 标题存在时，与正文之间保留一个空行。
func FormatTextMessage(msg Message) string {
	if strings.TrimSpace(msg.Title) == "" {
		return msg.Body
	}
	return msg.Title + "\n\n" + msg.Body
}

// Notifier 是所有渠道插件必须实现的接口
type Notifier interface {
	// Name 返回渠道唯一标识，与配置文件中的 type 字段对应
	Name() string
	// Send 执行发送，config 是该 Notifier 从配置文件读取的原始 map
	Send(ctx context.Context, msg Message, config map[string]string) error
}

// ConfigValidator 用于在服务启动前校验通知器配置。
type ConfigValidator interface {
	ValidateConfig(config map[string]string) error
}

// BaseNotifier 提供 HTTP 客户端复用和超时控制的公共能力，
// Resty 类型的 Notifier 可嵌入此结构体以避免重复代码。
type BaseNotifier struct {
	once   sync.Once
	client *resty.Client
}

// Client 惰性初始化并返回复用的 Resty 客户端（10s 超时）。
func (b *BaseNotifier) Client() *resty.Client {
	b.once.Do(func() {
		b.client = resty.New().SetTimeout(10 * time.Second)
	})
	return b.client
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Notifier{}
	aliases    = map[string]string{}
)

// Register 将通知器注册到全局表中，键为通知器名称。
func Register(n Notifier) {
	registryMu.Lock()
	defer registryMu.Unlock()

	name := n.Name()
	registry[name] = n
	aliases[name] = name
}

// RegisterAlias 将渠道名绑定到指定通知器类型。
func RegisterAlias(channelName, notifierName string) {
	registryMu.Lock()
	defer registryMu.Unlock()

	aliases[channelName] = notifierName
}

// Get 按名称从全局注册表中获取通知器实例。
func Get(name string) (Notifier, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	target, ok := aliases[name]
	if !ok {
		target = name
	}
	n, ok := registry[target]
	return n, ok
}

// RegisteredNames 返回所有已注册的通知器类型名称。
func RegisteredNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
