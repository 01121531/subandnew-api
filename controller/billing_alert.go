package controller

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func ListInstanceAlertRules(c *gin.Context) {
	data, err := managedinstance.ListAlertRules(authz.DataAccessFrom(c.Request.Context()))
	instanceAlertRuleJSON(c, data, err)
}

func GetInstanceAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	data, err := managedinstance.GetAlertRule(id, authz.DataAccessFrom(c.Request.Context()))
	instanceAlertRuleJSON(c, data, err)
}

func CreateInstanceAlertRule(c *gin.Context) {
	var input managedinstance.AlertRuleInput
	if !billingBind(c, &input) {
		return
	}
	data, err := managedinstance.CreateAlertRule(input, c.GetInt("id"))
	billingAuditResult(c, "create", "instance_alert_rule", dataID(data), err, input)
	instanceAlertRuleJSON(c, data, err)
}

func UpdateInstanceAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	var input managedinstance.AlertRuleInput
	if !billingBind(c, &input) {
		return
	}
	if !alertRuleAllowed(c, id, "instance") {
		return
	}
	data, err := managedinstance.UpdateAlertRule(id, input, c.GetInt("id"))
	billingAuditResult(c, "update", "instance_alert_rule", id, err, input)
	instanceAlertRuleJSON(c, data, err)
}

func DeleteInstanceAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	if !alertRuleAllowed(c, id, "instance") {
		return
	}
	err := managedinstance.DeleteAlertRule(id)
	billingAuditResult(c, "delete", "instance_alert_rule", id, err, nil)
	instanceAlertRuleJSON(c, nil, err)
}

func instanceAlertRuleJSON(c *gin.Context, data any, err error) {
	if err == nil {
		alertDataJSON(c, http.StatusOK, data)
		return
	}
	if errors.Is(err, authz.ErrDataForbidden) || errors.Is(err, authz.ErrAuthorizationChanged) {
		adminDataError(c, err)
		return
	}
	status, message := http.StatusInternalServerError, "instance_alert_rule_operation_failed"
	response := gin.H{"success": false}
	var conflict *managedinstance.AlertRuleConflictError
	switch {
	case errors.As(err, &conflict):
		status, message = http.StatusConflict, "instance_alert_rule_conflict"
		response["data"] = gin.H{"instance_ids": conflict.InstanceIDs}
	case errors.Is(err, managedinstance.ErrAlertRuleNotFound):
		status, message = http.StatusNotFound, "instance_alert_rule_not_found"
	case errors.Is(err, managedinstance.ErrInvalidInstance):
		status, message = http.StatusUnprocessableEntity, "invalid_instance_alert_rule"
	}
	response["message"] = message
	c.JSON(status, response)
}

func ListBillingFilterTemplates(c *gin.Context) {
	data, err := billingalert.ListTemplates()
	billingJSON(c, data, err)
}

func GetBillingFilterTemplate(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	data, err := billingalert.GetTemplate(id)
	billingJSON(c, data, err)
}

func CreateBillingFilterTemplate(c *gin.Context) {
	var input billingalert.TemplateInput
	if !billingBind(c, &input) {
		return
	}
	data, err := billingalert.CreateTemplate(input, c.GetInt("id"))
	billingAuditResult(c, "create", "filter_template", dataID(data), err, input)
	if err != nil {
		billingError(c, err)
		return
	}
	alertDataJSON(c, http.StatusCreated, data)
}

func PreviewBillingFilterTemplate(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	var input billingalert.TemplateInput
	if !billingBind(c, &input) {
		return
	}
	if err := billingalert.CheckTemplateAccess(authz.DataAccessFrom(c.Request.Context()), id); err != nil {
		adminDataError(c, err)
		return
	}
	data, err := billingalert.PreviewTemplateUpdate(id, input)
	billingJSON(c, data, err)
}

func UpdateBillingFilterTemplate(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	var input billingalert.TemplateInput
	if !billingBind(c, &input) {
		return
	}
	if err := billingalert.CheckTemplateAccess(authz.DataAccessFrom(c.Request.Context()), id); err != nil {
		adminDataError(c, err)
		return
	}
	data, err := billingalert.UpdateTemplate(id, input, c.GetInt("id"))
	billingAuditResult(c, "update", "filter_template", id, err, input)
	billingJSON(c, data, err)
}

func DeleteBillingFilterTemplate(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	if err := billingalert.CheckTemplateAccess(authz.DataAccessFrom(c.Request.Context()), id); err != nil {
		adminDataError(c, err)
		return
	}
	err := billingalert.DeleteTemplate(id)
	billingAuditResult(c, "delete", "filter_template", id, err, nil)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func ListBillingAlertRules(c *gin.Context) {
	data, err := billingalert.ListRules(authz.DataAccessFrom(c.Request.Context()))
	billingJSON(c, data, err)
}

func GetBillingAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	data, err := billingalert.GetRule(id, authz.DataAccessFrom(c.Request.Context()))
	billingJSON(c, data, err)
}

func CreateBillingAlertRule(c *gin.Context) {
	var input billingalert.RuleInput
	if !billingBind(c, &input) {
		return
	}
	data, err := billingalert.CreateRule(input, c.GetInt("id"))
	billingAuditResult(c, "create", "alert_rule", dataID(data), err, input)
	if err != nil {
		billingError(c, err)
		return
	}
	alertDataJSON(c, http.StatusCreated, data)
}

func PreviewBillingAlertRule(c *gin.Context) {
	id := int64(0)
	if c.Param("id") != "" {
		var ok bool
		id, ok = billingResourceID(c)
		if !ok {
			return
		}
	}
	var input billingalert.RuleInput
	if !billingBind(c, &input) {
		return
	}
	if id > 0 && !alertRuleAllowed(c, id, "billing") {
		return
	}
	data, err := billingalert.PreviewRule(id, input, c.GetInt("id"))
	billingJSON(c, data, err)
}

func UpdateBillingAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	var input billingalert.RuleInput
	if !billingBind(c, &input) {
		return
	}
	if !alertRuleAllowed(c, id, "billing") {
		return
	}
	data, err := billingalert.UpdateRule(id, input, c.GetInt("id"))
	billingAuditResult(c, "update", "alert_rule", id, err, input)
	billingJSON(c, data, err)
}

func DeleteBillingAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	if !alertRuleAllowed(c, id, "billing") {
		return
	}
	err := billingalert.DeleteRule(id)
	billingAuditResult(c, "delete", "alert_rule", id, err, nil)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func EvaluateBillingAlertRule(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	var input struct {
		InstanceID int64 `json:"instance_id"`
	}
	if !billingBind(c, &input) {
		return
	}
	if input.InstanceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid instance id"})
		return
	}
	if !alertRuleAllowed(c, id, "billing") {
		return
	}
	if !adminInstancesAllowed(c, []int64{input.InstanceID}) || !alertFieldsAllowed(c, "amount") {
		return
	}
	task, created, err := service.EnqueueBillingRuleEvaluation(id, input.InstanceID)
	billingAuditResult(c, "evaluate", "alert_rule", id, err, input)
	if err != nil {
		billingError(c, err)
		return
	}
	alertDataJSON(c, http.StatusAccepted, gin.H{
		"task": task.ToResponse(), "created": created,
	})
}

func ListBillingAlertRecords(c *gin.Context) {
	data, err := billingalert.ListAlertRecords(billingalert.AlertRecordFilter{
		Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20),
		InstanceID: queryInt64(c, "instance_id"), RuleID: queryInt64(c, "rule_id"),
		EventType: c.Query("event_type"), SourceType: c.Query("source_type"), MetricKey: c.Query("metric_key"), Currency: c.Query("currency"), Recipient: c.Query("recipient"),
		StartTime: queryInt64(c, "start_time"), EndTime: queryInt64(c, "end_time"),
	}, authz.DataAccessFrom(c.Request.Context()))
	billingJSON(c, data, err)
}

func ListBillingInstanceAlerts(c *gin.Context) {
	data, err := billingalert.ListInstanceAlerts(billingalert.InstanceAlertFilter{
		Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20),
		InstanceID: queryInt64(c, "instance_id"), Status: c.Query("status"),
		AlertType: c.Query("alert_type"), DeliveryStatus: c.Query("delivery_status"),
		Search: c.Query("search"), StartTime: queryInt64(c, "start_time"), EndTime: queryInt64(c, "end_time"),
	}, authz.DataAccessFrom(c.Request.Context()))
	billingJSON(c, data, err)
}

func GetBillingAlertRecord(c *gin.Context) {
	id, ok := billingResourceID(c)
	if !ok {
		return
	}
	data, err := billingalert.GetAlertRecord(id, authz.DataAccessFrom(c.Request.Context()))
	billingJSON(c, data, err)
}

func CreateBillingAlertRecordExport(c *gin.Context) {
	filter := billingAlertRecordFilter(c)
	record, err := service.EnqueueBillingAlertExport(filter, c.GetInt("id"))
	billingAuditResult(c, "export", "alert_records", dataID(record), err, filter)
	if err != nil {
		billingError(c, err)
		return
	}
	alertDataJSON(c, http.StatusAccepted, record)
}

func ListBillingAlertRecordExports(c *gin.Context) {
	data, err := billingalert.ListAlertExports(c.GetInt("id"), c.GetInt("role") >= common.RoleRootUser)
	billingJSON(c, data, err)
}

func DownloadBillingAlertRecordExport(c *gin.Context) {
	record, err := billingalert.GetAlertExport(c.Param("task_id"), c.GetInt("id"), c.GetInt("role") >= common.RoleRootUser)
	if err != nil {
		billingError(c, err)
		return
	}
	if record.Status == "expired" {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export_file_expired"})
		return
	}
	if record.Status != "succeeded" || record.FilePath == "" {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "export_not_ready"})
		return
	}
	if _, err := os.Stat(record.FilePath); err != nil {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export_file_missing"})
		return
	}
	if err := billingalert.CheckAlertExportAccess(record, authz.DataAccessFrom(c.Request.Context())); err != nil {
		adminDataError(c, err)
		return
	}
	c.FileAttachment(record.FilePath, record.FileName)
}

func billingAlertRecordFilter(c *gin.Context) billingalert.AlertRecordFilter {
	return billingalert.AlertRecordFilter{
		InstanceID: queryInt64(c, "instance_id"), RuleID: queryInt64(c, "rule_id"),
		EventType: c.Query("event_type"), SourceType: c.Query("source_type"), MetricKey: c.Query("metric_key"), Currency: c.Query("currency"), Recipient: c.Query("recipient"),
		StartTime: queryInt64(c, "start_time"), EndTime: queryInt64(c, "end_time"),
	}
}

func GetBillingExchangeSettings(c *gin.Context) {
	data, err := billingalert.EnsureExchangeRateSetting()
	billingJSON(c, data, err)
}

func UpdateBillingExchangeSettings(c *gin.Context) {
	var input billingalert.ExchangeSettingInput
	if !billingBind(c, &input) {
		return
	}
	data, err := billingalert.UpdateExchangeRateSetting(input, c.GetInt("id"))
	billingAuditResult(c, "update", "exchange_settings", 1, err, input)
	billingJSON(c, data, err)
}

func ListBillingExchangeRates(c *gin.Context) {
	data, err := billingalert.ListExchangeRates(queryInt(c, "limit", 100))
	billingJSON(c, data, err)
}

func RefreshBillingExchangeRate(c *gin.Context) {
	data, err := billingalert.RefreshExchangeRate(c.Request.Context(), c.GetInt("id"))
	billingAuditResult(c, "refresh", "exchange_rate", dataID(data), err, nil)
	billingJSON(c, data, err)
}

func GetBillingSMTPSettings(c *gin.Context) {
	data, err := billingalert.GetSMTPSetting()
	billingJSON(c, data, err)
}

func UpdateBillingSMTPSettings(c *gin.Context) {
	var input billingalert.SMTPSettingInput
	if !billingBind(c, &input) {
		return
	}
	data, err := billingalert.UpdateSMTPSetting(input, c.GetInt("id"))
	if err == nil && input.Enabled {
		if retryErr := service.RetryManagedInstanceAlertEmailsNow(); retryErr != nil {
			common.SysError("failed to wake managed instance alert email tasks: " + retryErr.Error())
		}
	}
	billingAuditResult(c, "update", "smtp_settings", 1, err, gin.H{
		"host": input.Host, "port": input.Port, "security": input.Security,
		"username": input.Username, "from_address": input.FromAddress, "enabled": input.Enabled,
	})
	billingJSON(c, data, err)
}

func TestBillingSMTPSettings(c *gin.Context) {
	var input billingalert.SMTPTestInput
	if !billingBind(c, &input) {
		return
	}
	err := billingalert.SendSMTPTest(c.Request.Context(), input.Recipient)
	billingAuditResult(c, "test", "smtp_settings", 1, err, gin.H{"recipient": input.Recipient})
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func billingResourceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return 0, false
	}
	return id, true
}

func billingBind(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return false
	}
	switch input := target.(type) {
	case *billingalert.RuleInput:
		return adminInstancesAllowed(c, input.InstanceIDs) && alertFieldsAllowed(c, "amount", "status", "email")
	case *managedinstance.AlertRuleInput:
		return adminInstancesAllowed(c, input.InstanceIDs) && alertFieldsAllowed(c, "status", "email")
	}
	return true
}

func billingJSON(c *gin.Context, data any, err error) {
	if err != nil {
		billingError(c, err)
		return
	}
	alertDataJSON(c, http.StatusOK, data)
}

func alertDataJSON(c *gin.Context, status int, data any) {
	a := authz.DataAccessFrom(c.Request.Context())
	if a != nil {
		if err := a.Current(model.DB); err != nil {
			adminDataError(c, err)
			return
		}
	}
	projected, err := billingalert.ProjectAlertData(data, a)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(status, gin.H{"success": true, "message": "", "data": projected})
}

func alertRuleAllowed(c *gin.Context, id int64, source string) bool {
	table, bindings := "billing_alert_rules", "billing_alert_rule_instances"
	switch source {
	case model.AlertSourceMetric:
		table, bindings = "metric_alert_rules", "metric_alert_rule_instances"
	case model.AlertSourceInstance:
		table, bindings = "managed_instance_alert_rules", "managed_instance_alert_rule_instances"
	}
	a := authz.DataAccessFrom(c.Request.Context())
	if a != nil {
		if err := a.Current(model.DB); err != nil {
			adminDataError(c, err)
			return false
		}
	}
	if err := billingalert.CheckRuleAccess(a, id, table, bindings); err != nil {
		adminDataError(c, err)
		return false
	}
	return true
}

func alertFieldsAllowed(c *gin.Context, fields ...string) bool {
	a := authz.DataAccessFrom(c.Request.Context())
	if a == nil {
		return true
	}
	if err := a.Current(model.DB); err != nil {
		adminDataError(c, err)
		return false
	}
	for _, field := range fields {
		if !a.HasField(field) {
			adminDataError(c, authz.ErrDataForbidden)
			return false
		}
	}
	return true
}

func billingError(c *gin.Context, err error) {
	if errors.Is(err, authz.ErrDataForbidden) || errors.Is(err, authz.ErrAuthorizationChanged) {
		adminDataError(c, err)
		return
	}
	status := http.StatusInternalServerError
	message := "billing_alert_operation_failed"
	switch {
	case errors.Is(err, billingalert.ErrInvalidBillingInput):
		status, message = http.StatusUnprocessableEntity, "invalid_billing_alert_input"
	case errors.Is(err, billingalert.ErrBillingNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		status, message = http.StatusNotFound, "billing_alert_not_found"
	case errors.Is(err, billingalert.ErrExchangeRateUnavailable):
		status, message = http.StatusBadGateway, "exchange_rate_unavailable"
	case errors.Is(err, billingalert.ErrSMTPNotConfigured):
		status, message = http.StatusConflict, "smtp_not_configured"
	case errors.Is(err, billingalert.ErrSMTPKeyUnavailable):
		status, message = http.StatusServiceUnavailable, "smtp_encryption_key_unavailable"
	}
	c.JSON(status, gin.H{"success": false, "message": message})
}

func billingAuditResult(c *gin.Context, action string, resource string, resourceID int64, err error, details any) {
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	billingalert.WriteAudit(c.GetInt("id"), action, resource, resourceID, outcome, details)
}

func dataID(value any) int64 {
	switch item := value.(type) {
	case *billingalert.TemplateView:
		return item.ID
	case *billingalert.RuleView:
		return item.ID
	case *model.BillingAlertExport:
		return item.ID
	case *managedinstance.AlertRuleView:
		return item.ID
	case interface{ GetID() int64 }:
		return item.GetID()
	}
	return 0
}

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}

func queryInt64(c *gin.Context, key string) int64 {
	value, _ := strconv.ParseInt(c.Query(key), 10, 64)
	return value
}
