package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"msgflow/internal/config"
	"msgflow/internal/plugin"
	"msgflow/internal/security"
	"msgflow/internal/server"

	_ "msgflow/notifiers/bark"
	_ "msgflow/notifiers/email"
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
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Printf("sync logger failed: %v", err)
		}
	}()

	registeredNames := plugin.RegisteredNames()
	if err := cfg.Validate(registeredNames); err != nil {
		logger.Fatal("config validation failed", zap.Error(err))
	}

	// 将配置中的渠道名绑定到具体通知器类型。
	// 未显式配置 type 时，默认按渠道名匹配内置通知器。
	for name, nc := range cfg.Notifiers {
		notifierType := strings.TrimSpace(nc["type"])
		if notifierType == "" {
			notifierType = name
		}
		notifier, ok := plugin.Get(notifierType)
		if !ok {
			logger.Fatal("notifier type not found after validation",
				zap.String("channel", name),
				zap.String("type", notifierType))
		}
		if validator, ok := notifier.(plugin.ConfigValidator); ok {
			if err := validator.ValidateConfig(nc); err != nil {
				logger.Fatal("channel config validation failed",
					zap.String("channel", name),
					zap.String("type", notifierType),
					zap.Error(err))
			}
		}
		plugin.RegisterAlias(name, notifierType)
	}

	deliveryCtx, cancelDelivery := context.WithCancel(context.Background())
	defer cancelDelivery()

	// 构建 Gin 路由并启动网关。
	router := server.NewWithContext(deliveryCtx, cfg, logger)
	httpServer := server.NewHTTPServer(cfg, router)

	// 根据配置选择 TCP 或 Unix socket 监听。
	ln, serveErrCh, err := server.ListenAndServe(httpServer, cfg)
	if err != nil {
		logger.Fatal("start server failed", zap.Error(err))
	}

	// 监听已建立后再降权，允许 root 启动时先绑定特权端口或创建受限目录下的 Unix socket。
	if err := security.DropRootPrivileges(); err != nil {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			logger.Warn("close listener after privilege drop failure failed", zap.Error(closeErr))
		}
		logger.Fatal("drop root privileges failed", zap.Error(err))
	}

	if cfg.IsUnixSocket() {
		logger.Info("server starting", zap.String("socket", cfg.Server.UnixSocket))
	} else {
		logger.Info("server starting", zap.String("port", cfg.Server.Port))
	}

	// 等待中断信号，执行优雅关停。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err, ok := <-serveErrCh:
		if ok && err != nil {
			logger.Fatal("server exited unexpectedly", zap.Error(err))
		}
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
	}

	// 给进行中的请求 10 秒时间完成。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server shutdown failed", zap.Error(err))
		}
	}

	// 关闭 listener（Unix socket 模式下会自动删除 socket 文件）。
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		logger.Warn("close listener failed", zap.Error(err))
	}

	logger.Info("server stopped")
}
