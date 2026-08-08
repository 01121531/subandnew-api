package middleware

import (
	"github.com/01121531/subandnew-api/common"
	"github.com/gin-gonic/gin"
)

func Version() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Control-Plane-Version", common.Version)
		c.Next()
	}
}
