package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				reqID, _ := c.Get("request_id")
				logrus.WithFields(logrus.Fields{
					"request_id": reqID,
					"panic":      r,
					"path":       c.Request.URL.Path,
				}).Error("panic recovered")

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"code":       "INTERNAL_ERROR",
						"message":    "internal server error",
						"request_id": reqID,
					},
				})
			}
		}()
		c.Next()
	}
}
