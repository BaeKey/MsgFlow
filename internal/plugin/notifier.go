package plugin

import (
	"context"
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

// Notifier 是所有渠道插件必须实现的接口
type Notifier interface {
	// Name 返回渠道唯一标识，与配置文件中的 type 字段对应
	Name() string
	// Send 执行发送，config 是该 Notifier 从配置文件读取的原始 map
	Send(ctx context.Context, msg Message, config map[string]string) error
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

// Registry 是全局插件注册表
var registry = map[string]Notifier{}

// Register 将通知器注册到全局表中，键为通知器名称。
func Register(n Notifier) {
	registry[n.Name()] = n
}

// Get 按名称从全局注册表中获取通知器实例。
func Get(name string) (Notifier, bool) {
	n, ok := registry[name]
	return n, ok
}
