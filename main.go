package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"msgflow/internal/config"
	"msgflow/internal/plugin"
	"msgflow/internal/security"
	"msgflow/internal/server"
	"msgflow/notifiers/webhook"

	_ "msgflow/notifiers/bark"
	_ "msgflow/notifiers/email"
	_ "msgflow/notifiers/telegram"
	_ "msgflow/notifiers/wecom"
)

// main 是程序入口，负责初始化日志、加载配置并启动 HTTP 服务。
func main() {
	// 从项目根目录读取 YAML 配置文件（先加载配置，以便读取日志等级）。
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// 根据配置的日志等级创建 zap 日志实例。
	level, err := zapcore.ParseLevel(cfg.ZapLevel())
	if err != nil {
		level = zap.ErrorLevel
	}
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		os.Stdout,
		level,
	))
	defer logger.Sync()

	// 若以 root 启动，则在正式监听前主动降权到 nobody。
	if err := security.DropRootPrivileges(); err != nil {
		logger.Fatal("drop root privileges failed", zap.Error(err))
	}

	// 根据配置动态注册 webhook 通知器。
	// 每个 webhook 以自定义名称注册到插件表，如 wework、qq 等。
	for name, wc := range cfg.Webhooks {
		url := wc["url"]
		if url == "" {
			logger.Warn("webhook missing url, skipping", zap.String("name", name))
			continue
		}
		method := wc["method"]
		plugin.Register(webhook.New(name, url, method))
		logger.Info("webhook registered", zap.String("name", name))
	}

	// 构建 Gin 路由并启动网关。
	router := server.New(cfg, logger)
	httpServer := server.NewHTTPServer(cfg, router)

	// 根据配置选择 TCP 或 Unix socket 监听。
	ln, err := server.ListenAndServe(httpServer, cfg)
	if err != nil {
		logger.Fatal("start server failed", zap.Error(err))
	}

	if cfg.IsUnixSocket() {
		logger.Info("server starting", zap.String("socket", cfg.Server.UnixSocket))
	} else {
		logger.Info("server starting", zap.String("port", cfg.Server.Port))
	}

	// 等待中断信号，执行优雅关停。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", zap.String("signal", sig.String()))

	// 给进行中的请求 10 秒时间完成。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", zap.Error(err))
	}

	// 关闭 listener（Unix socket 模式下会自动删除 socket 文件）。
	ln.Close()

	logger.Info("server stopped")
}
