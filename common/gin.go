package common

import (
	"errors"
	"net/http"

	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/gin-gonic/gin"
)

var ErrRequestBodyTooLarge = errors.New("request body too large")

func IsRequestBodyTooLargeError(err error) bool {
	if errors.Is(err, ErrRequestBodyTooLarge) {
		return true
	}
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func SetContextKey(c *gin.Context, key constant.ContextKey, value any) {
	c.Set(string(key), value)
}

func GetContextKeyBool(c *gin.Context, key constant.ContextKey) bool {
	value, exists := c.Get(string(key))
	if !exists {
		return false
	}
	result, _ := value.(bool)
	return result
}

func GetContextKeyType[T any](c *gin.Context, key constant.ContextKey) (T, bool) {
	value, exists := c.Get(string(key))
	if !exists {
		var zero T
		return zero, false
	}
	result, ok := value.(T)
	return result, ok
}

func ApiError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}

func ApiErrorMsg(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": message})
}

func ApiSuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

func ApiErrorI18n(c *gin.Context, key string, args ...map[string]any) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": TranslateMessage(c, key, args...)})
}

func ApiSuccessI18n(c *gin.Context, key string, data any, args ...map[string]any) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": TranslateMessage(c, key, args...),
		"data":    data,
	})
}

var TranslateMessage = func(_ *gin.Context, key string, _ ...map[string]any) string {
	return key
}
