package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := logrus.Fields{
			"status":     status,
			"method":     c.Request.Method,
			"path":       path,
			"latency":    latency.String(),
			"latency_ms": latency.Milliseconds(),
			"client_ip":  c.ClientIP(),
			"body_size":  c.Writer.Size(),
		}

		if query != "" {
			fields["query"] = query
		}

		if reqID, exists := c.Get("request_id"); exists {
			fields["request_id"] = reqID
		}

		if len(c.Errors) > 0 {
			fields["errors"] = c.Errors.String()
		}

		entry := logrus.WithFields(fields)

		switch {
		case status >= 500:
			entry.Error("server error")
		case status >= 400:
			entry.Warn("client error")
		default:
			entry.Info("request completed")
		}
	}
}
