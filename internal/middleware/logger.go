package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 记录每个 HTTP 请求的基础访问日志。
func Logger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间，用于统计耗时。
		start := time.Now()
		c.Next()

		// 优先记录路由模板，避免将 token 等敏感路径参数写入日志。
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// 在请求处理结束后输出访问日志。
		logger.Info("http request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.String("client_ip", c.ClientIP()),
			zap.Duration("latency", time.Since(start)),
		)
	}
}
