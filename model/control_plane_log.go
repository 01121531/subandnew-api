package model

import (
	"context"
	"fmt"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/logger"
)

const (
	LogTypeTopup  = 1
	LogTypeSystem = 4
	LogTypeLogin  = 7
)

// RecordLog writes control-plane security events to the application log. The
// control plane intentionally does not create or write the legacy usage table.
func RecordLog(userID int, logType int, content string) {
	logger.LogInfo(context.Background(), fmt.Sprintf("control-plane audit user=%d type=%d content=%s", userID, logType, content))
}

func RecordLoginLog(userID int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	metadata, _ := common.Marshal(map[string]interface{}{
		"username": username,
		"ip":       ip,
		"action":   action,
		"params":   params,
		"extra":    extra,
	})
	logger.LogInfo(context.Background(), fmt.Sprintf("control-plane audit user=%d type=%d content=%s metadata=%s", userID, LogTypeLogin, content, metadata))
}

// RecordTopupLog keeps dormant legacy billing code independent from the
// removed usage-log database until that billing module is deleted.
func RecordTopupLog(userID int, content, callerIP, paymentMethod, callbackPaymentMethod string) {
	metadata, _ := common.Marshal(map[string]string{
		"caller_ip":               callerIP,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
	})
	logger.LogInfo(context.Background(), fmt.Sprintf("control-plane audit user=%d type=%d content=%s metadata=%s", userID, LogTypeTopup, content, metadata))
}

func RecordOperationAuditLog(userID int, content, ip, action string, params, adminInfo, auditInfo map[string]interface{}) {
	metadata, _ := common.Marshal(map[string]interface{}{
		"ip":         ip,
		"action":     action,
		"params":     params,
		"admin_info": adminInfo,
		"audit_info": auditInfo,
	})
	logger.LogInfo(context.Background(), fmt.Sprintf("control-plane audit user=%d type=manage content=%s metadata=%s", userID, content, metadata))
}
