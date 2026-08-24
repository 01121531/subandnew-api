package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/metricalert"
	"github.com/gin-gonic/gin"
)

func ListAlertTasks(c *gin.Context) {
	billingRules, billingErr := billingalert.ListRules()
	if billingErr != nil {
		billingError(c, billingErr)
		return
	}
	metricRules, metricErr := metricalert.ListRules()
	if metricErr != nil {
		metricAlertError(c, metricErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"billing": billingRules, "metric": metricRules}})
}

func ListMetricAlertRules(c *gin.Context) {
	data, err := metricalert.ListRules()
	metricAlertJSON(c, data, err)
}

func GetMetricAlertRule(c *gin.Context) {
	id, ok := metricAlertID(c)
	if !ok {
		return
	}
	data, err := metricalert.GetRule(id)
	metricAlertJSON(c, data, err)
}

func CreateMetricAlertRule(c *gin.Context) {
	var input metricalert.RuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_metric_alert_input"})
		return
	}
	data, err := metricalert.CreateRule(input, c.GetInt("id"))
	metricalert.Audit(c.GetInt("id"), "create", dataID(data), metricOutcome(err), input)
	if err != nil {
		metricAlertError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": data})
}

func UpdateMetricAlertRule(c *gin.Context) {
	id, ok := metricAlertID(c)
	if !ok {
		return
	}
	var input metricalert.RuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_metric_alert_input"})
		return
	}
	data, err := metricalert.UpdateRule(id, input, c.GetInt("id"))
	metricalert.Audit(c.GetInt("id"), "update", id, metricOutcome(err), input)
	metricAlertJSON(c, data, err)
}

func DeleteMetricAlertRule(c *gin.Context) {
	id, ok := metricAlertID(c)
	if !ok {
		return
	}
	err := metricalert.DeleteRule(id)
	metricalert.Audit(c.GetInt("id"), "delete", id, metricOutcome(err), nil)
	if err != nil {
		metricAlertError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func EvaluateMetricAlertRule(c *gin.Context) {
	id, ok := metricAlertID(c)
	if !ok {
		return
	}
	task, created, err := service.EnqueueMetricAlertEvaluation(id)
	metricalert.Audit(c.GetInt("id"), "evaluate", id, metricOutcome(err), nil)
	if err != nil {
		metricAlertError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "", "data": gin.H{"task": task.ToResponse(), "created": created}})
}

func ListMetricAlertCapabilities(c *gin.Context) {
	ids := make([]int64, 0)
	for _, value := range strings.Split(c.Query("instance_ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	data, err := metricalert.Capabilities(ids, c.DefaultQuery("scope_mode", metricalert.ScopePerInstance))
	metricAlertJSON(c, data, err)
}

func metricAlertID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_metric_alert_input"})
		return 0, false
	}
	return id, true
}

func metricAlertJSON(c *gin.Context, data any, err error) {
	if err != nil {
		metricAlertError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

func metricAlertError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, metricalert.ErrInvalidInput) {
		status = http.StatusBadRequest
	} else if errors.Is(err, metricalert.ErrNotFound) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"success": false, "message": metricalert.ErrorMessage(err)})
}

func metricOutcome(err error) string {
	if err == nil {
		return "success"
	}
	return "failed"
}
