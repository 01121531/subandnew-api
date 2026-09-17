package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/accountexport"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func exportScheduleError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, authz.ErrDataForbidden), errors.Is(err, authz.ErrAuthorizationChanged):
		status = http.StatusForbidden
	case errors.Is(err, model.ErrExportScheduleConflict):
		status = http.StatusConflict
	case errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"success": false, "message": accountexport.SafeCode(err)})
}
func exportScheduleID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		exportScheduleError(c, accountexport.ErrInvalidSchedule)
		return 0, false
	}
	return id, true
}
func exportScheduleAudit(c *gin.Context, action string, id int64, extra map[string]interface{}) {
	if extra == nil {
		extra = map[string]interface{}{}
	}
	extra["schedule_id"] = id
	model.RecordOperationAuditLog(c.GetInt("id"), "account export schedule", c.ClientIP(), "account_export_schedule_"+action, extra, nil, nil)
}

func ListAccountExportSchedules(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	items, total, err := accountexport.ListPlans(c.GetInt("id"), page, size)
	if err != nil {
		exportScheduleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "smtp_ready": accountexport.SMTPReady() == nil}})
}
func GetAccountExportSchedule(c *gin.Context) {
	id, ok := exportScheduleID(c)
	if !ok {
		return
	}
	plan, err := accountexport.LoadPlan(id, c.GetInt("id"))
	if err != nil {
		exportScheduleError(c, err)
		return
	}
	view, err := accountexport.View(*plan)
	if err != nil {
		exportScheduleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}
func SaveAccountExportSchedule(c *gin.Context) {
	id := int64(0)
	if c.Param("id") != "" {
		var ok bool
		id, ok = exportScheduleID(c)
		if !ok {
			return
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	var input accountexport.PlanInput
	if err := c.ShouldBindJSON(&input); err != nil {
		exportScheduleError(c, accountexport.ErrInvalidSchedule)
		return
	}
	view, err := accountexport.SavePlan(c.GetInt("id"), id, input)
	if err != nil {
		exportScheduleError(c, err)
		return
	}
	exportScheduleAudit(c, "save", view.ID, map[string]interface{}{"name": view.Name, "enabled": view.Enabled, "recipients": view.Settings.Recipients, "schedule": view.Settings.Schedule, "period": view.Settings.Period, "scope": view.Settings.Scope, "instance_ids": view.Settings.Query.InstanceIDs})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}
func AccountExportScheduleAction(c *gin.Context) {
	id, ok := exportScheduleID(c)
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		exportScheduleError(c, accountexport.ErrInvalidSchedule)
		return
	}
	action := c.Param("action")
	if c.Request.Method == http.MethodDelete {
		action = "delete"
	}
	if action == "execute" {
		run, err := accountexport.ExecuteNow(c.Request.Context(), c.GetInt("id"), id, input.Version)
		if err != nil {
			exportScheduleError(c, err)
			return
		}
		exportScheduleAudit(c, action, id, map[string]interface{}{"run_id": run.ID})
		c.JSON(http.StatusOK, gin.H{"success": true, "data": run})
		return
	}
	if err := accountexport.ChangePlanState(c.GetInt("id"), id, input.Version, action); err != nil {
		exportScheduleError(c, err)
		return
	}
	exportScheduleAudit(c, action, id, nil)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func ListAccountExportScheduleRuns(c *gin.Context) {
	id, ok := exportScheduleID(c)
	if !ok {
		return
	}
	if _, err := accountexport.LoadPlan(id, c.GetInt("id")); err != nil {
		exportScheduleError(c, err)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	var rows []model.ManagedAccountExportRun
	var total int64
	q := model.DB.Model(&model.ManagedAccountExportRun{}).Where("schedule_id = ?", id)
	if err := q.Count(&total).Error; err != nil {
		exportScheduleError(c, err)
		return
	}
	if err := q.Order("id DESC").Offset((page - 1) * 20).Limit(20).Find(&rows).Error; err != nil {
		exportScheduleError(c, err)
		return
	}
	type runView struct {
		model.ManagedAccountExportRun
		Export     map[string]any                       `json:"export"`
		Deliveries []model.ManagedAccountExportDelivery `json:"deliveries"`
	}
	views := make([]runView, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	taskIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		taskIDs = append(taskIDs, row.TaskID)
	}
	var deliveries []model.ManagedAccountExportDelivery
	var exports []model.ManagedUsageExport
	if len(ids) > 0 {
		if err := model.DB.Where("run_id IN ?", ids).Order("id ASC").Find(&deliveries).Error; err != nil {
			exportScheduleError(c, err)
			return
		}
		if err := model.DB.Where("task_id IN ?", taskIDs).Find(&exports).Error; err != nil {
			exportScheduleError(c, err)
			return
		}
	}
	for _, row := range rows {
		view := runView{ManagedAccountExportRun: row, Deliveries: []model.ManagedAccountExportDelivery{}}
		for i := range exports {
			if exports[i].TaskID == row.TaskID {
				view.Export = map[string]any{"status": exports[i].Status, "file_name": exports[i].FileName, "file_size": exports[i].FileSize, "error_code": exports[i].ErrorCode}
			}
		}
		for _, d := range deliveries {
			if d.RunID == row.ID {
				view.Deliveries = append(view.Deliveries, d)
			}
		}
		views = append(views, view)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": views, "total": total}})
}
func RetryAccountExportDelivery(c *gin.Context) {
	id, ok := exportScheduleID(c)
	if !ok {
		return
	}
	deliveryID, err := strconv.ParseInt(c.Param("delivery_id"), 10, 64)
	if err != nil || deliveryID <= 0 {
		exportScheduleError(c, accountexport.ErrInvalidSchedule)
		return
	}
	var input struct {
		Version   int64 `json:"version"`
		Confirmed bool  `json:"confirmed"`
	}
	if err = c.ShouldBindJSON(&input); err != nil {
		exportScheduleError(c, accountexport.ErrInvalidSchedule)
		return
	}
	if err = accountexport.RetryDelivery(c.GetInt("id"), id, deliveryID, input.Version, input.Confirmed); err != nil {
		exportScheduleError(c, err)
		return
	}
	exportScheduleAudit(c, "retry_delivery", id, map[string]interface{}{"delivery_id": deliveryID, "confirmed": input.Confirmed})
	c.JSON(http.StatusOK, gin.H{"success": true})
}
