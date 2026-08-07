package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				common.SysLog(fmt.Sprintf("panic detected: %v", err))
				common.SysLog(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please report it here: https://github.com/01121531/subandnew-api/issues", err),
						"type":    "huichuan_panic",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
