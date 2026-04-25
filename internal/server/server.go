package server

import (
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"msgflow/internal/config"
	"msgflow/internal/handler"
	"msgflow/internal/middleware"
)

// New 创建并配置 Gin HTTP 服务器。
func New(cfg *config.Config, logger *zap.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// 不信任任何反向代理头，避免伪造客户端 IP。
	if err := router.SetTrustedProxies(nil); err != nil {
		logger.Fatal("set trusted proxies failed", zap.Error(err))
	}

	// 注册恢复中间件、请求体大小限制和请求日志中间件。
	router.Use(gin.Recovery())
	router.Use(middleware.BodyLimit())
	router.Use(middleware.Logger(logger))

	h := handler.New(cfg, logger)

	// 注册标准发送接口。
	router.POST("/send", h.SendHandler)

	// 注册 GET 推送接口（用 *path 避免与 /:token/:body 冲突）。
	// URL 格式: /:token/:body 或 /:token/:title/:body，handler 内部解析。
	router.GET("/:token/*path", h.PushHandler)
	router.POST("/:token/*path", h.PushHandler)

	return router
}

// NewHTTPServer 创建带超时配置的 HTTP 服务器，降低慢连接攻击风险。
//
// 当 cfg 配置了 unix_socket 时，Addr 字段为空（实际监听由 main 中的
// net.Listen + Serve 控制）；否则使用 TCP 端口监听。
func NewHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	srv := &http.Server{
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if cfg.IsUnixSocket() {
		srv.Addr = ""
	} else {
		srv.Addr = ":" + cfg.Server.Port
	}

	return srv
}

// ListenAndServe 根据配置选择 TCP 或 Unix socket 监听。
//
// Unix socket 模式：
//   - 启动前删除残留的 socket 文件
//   - 设置 socket 文件权限为 0666，允许同机其他用户访问
//   - 返回 listener 和服务错误通道供优雅关停使用
func ListenAndServe(srv *http.Server, cfg *config.Config) (net.Listener, <-chan error, error) {
	if cfg.IsUnixSocket() {
		return listenUnix(srv, cfg.Server.UnixSocket)
	}
	return listenTCP(srv, cfg)
}

// listenUnix 在 Unix socket 上启动 HTTP 服务。
func listenUnix(srv *http.Server, socketPath string) (net.Listener, <-chan error, error) {
	// 清理残留的 socket 文件（上次异常退出时可能残留）。
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, err
	}

	// 关闭时自动删除 socket 文件。
	ln.(*net.UnixListener).SetUnlinkOnClose(true)

	// 设置 socket 文件权限为 0666，允许同机其他用户（如 nginx）连接。
	if err := os.Chmod(socketPath, 0666); err != nil {
		ln.Close()
		return nil, nil, err
	}

	// 异步启动服务，将异常退出错误回传给调用方。
	errCh := make(chan error, 1)
	go serveAsync(srv, ln, errCh)

	return ln, errCh, nil
}

// listenTCP 在 TCP 端口上启动 HTTP 服务。
func listenTCP(srv *http.Server, cfg *config.Config) (net.Listener, <-chan error, error) {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, nil, err
	}

	errCh := make(chan error, 1)
	go serveAsync(srv, ln, errCh)

	return ln, errCh, nil
}

func serveAsync(srv *http.Server, ln net.Listener, errCh chan<- error) {
	defer close(errCh)

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}
