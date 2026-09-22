package controller

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/dailyreport"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type dailyReportRuleRequest struct {
	ID           int64  `json:"id"`
	InstanceID   int64  `json:"instance_id"`
	SupplierCode string `json:"supplier_code"`
	SupplierName string `json:"supplier_name"`
	Filter       any    `json:"filter"`
	Enabled      *bool  `json:"enabled"`
	Version      int64  `json:"version"`
}

func reportIDs(c *gin.Context) ([]int64, bool) {
	value := strings.TrimSpace(c.Query("instance_ids"))
	if value == "" {
		ids, err := authorizedRealtimeIDs(c)
		if err != nil {
			adminDataError(c, err)
			return nil, false
		}
		return ids, true
	}
	parts := strings.Split(value, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid instance ids"})
			return nil, false
		}
		ids = append(ids, id)
	}
	if !adminInstancesAllowed(c, ids) {
		return nil, false
	}
	return ids, true
}

func ListDailyReports(c *gin.Context) {
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	date := c.DefaultQuery("date", time.Now().In(dailyreport.BeijingLocation()).Format("2006-01-02"))
	source := c.Query("source")
	items := make([]dailyreport.Row, 0, len(ids))
	for _, id := range ids {
		var rules []model.DailyReportRule
		_ = model.DB.Where("instance_id = ? AND enabled = ?", id, true).Find(&rules).Error
		if len(rules) == 0 {
			row, err := dailyreport.Collect(c.Request.Context(), id, date, source)
			if err == nil {
				items = append(items, row)
			}
			continue
		}
		for index := range rules {
			row, err := dailyreport.CollectWithRule(c.Request.Context(), id, date, source, &rules[index])
			if err != nil {
				continue
			}
			items = append(items, row)
		}
	}
	adminDataJSON(c, http.StatusOK, dailyreport.Overview{Date: date, Timezone: "Asia/Shanghai", Items: items, CollectedAt: common.GetTimestamp()})
}

func GetDailyReportDetail(c *gin.Context) { ListDailyReports(c) }

func ListDailyReportRules(c *gin.Context) {
	items, err := dailyreport.ListRules(0)
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"items": items})
}

func SaveDailyReportRule(c *gin.Context) {
	var request dailyReportRuleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	if !adminInstancesAllowed(c, []int64{request.InstanceID}) {
		return
	}
	filterJSON := `{}`
	if request.Filter != nil {
		data, err := json.Marshal(request.Filter)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid filter"})
			return
		}
		filterJSON = string(data)
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	rule, err := dailyreport.SaveRule(model.DailyReportRule{ID: request.ID, InstanceID: request.InstanceID, SupplierCode: request.SupplierCode, SupplierName: request.SupplierName, FilterJSON: filterJSON, Enabled: enabled, Version: request.Version, CreatedBy: c.GetInt("id"), UpdatedBy: c.GetInt("id")})
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, rule)
}

func DeleteDailyReportRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}
	if err := dailyreport.DeleteRule(id); err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"deleted": true})
}

func ExportDailyReport(c *gin.Context) {
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	date := c.DefaultQuery("date", time.Now().In(dailyreport.BeijingLocation()).Format("2006-01-02"))
	source := c.Query("source")
	book := excelize.NewFile()
	sheet := book.GetSheetName(0)
	headers := []string{"实例 ID", "日期", "来源", "请求数", "总 Token", "费用", "币种", "账号数", "渠道数", "上传数", "状态", "旧快照", "采集时间", "错误"}
	for i, header := range headers {
		_ = book.SetCellValue(sheet, string(rune('A'+i))+"1", header)
	}
	rowIndex := 2
	for _, id := range ids {
		row, err := dailyreport.Collect(c.Request.Context(), id, date, source)
		if err != nil {
			continue
		}
		values := []any{id, date, row.Snapshot.Source, row.Full.Requests, row.Full.TotalTokens, row.Full.Cost, row.Full.Currency, row.Snapshot.AccountCount, row.Snapshot.ChannelCount, row.Snapshot.UploadCount, row.Snapshot.Status, row.Snapshot.Stale, time.Unix(row.Snapshot.ObservedAt, 0).In(dailyreport.BeijingLocation()).Format("2006-01-02 15:04:05"), row.Snapshot.ErrorCode}
		for i, value := range values {
			_ = book.SetCellValue(sheet, string(rune('A'+i))+strconv.Itoa(rowIndex), value)
		}
		rowIndex++
	}
	_ = book.AutoFilter(sheet, "A1:N"+strconv.Itoa(rowIndex-1), nil)
	_ = book.SetColWidth(sheet, "A", "N", 16)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="daily-report.xlsx"`)
	if err := book.Write(c.Writer); err != nil {
		return
	}
}

type dailyReportScheduleRequest struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Config  any    `json:"config"`
	Enabled bool   `json:"enabled"`
	Version int64  `json:"version"`
	NextAt  int64  `json:"next_at"`
}

func ListDailyReportSchedules(c *gin.Context) {
	var rows []model.DailyReportSchedule
	query := model.DB.Where("owner_id = ? AND deleted_at = 0", c.GetInt("id")).Order("id desc")
	if c.GetInt("role") >= common.RoleRootUser {
		query = model.DB.Where("deleted_at = 0").Order("id desc")
	}
	if err := query.Find(&rows).Error; err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"items": rows})
}

func SaveDailyReportSchedule(c *gin.Context) {
	var request dailyReportScheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	config, _ := json.Marshal(request.Config)
	now := common.GetTimestamp()
	row := model.DailyReportSchedule{ID: request.ID, OwnerID: c.GetInt("id"), Name: strings.TrimSpace(request.Name), ConfigJSON: string(config), Enabled: request.Enabled, Version: request.Version, NextAt: request.NextAt, CreatedAt: now, UpdatedAt: now}
	var err error
	if row.ID > 0 {
		err = model.DB.Model(&model.DailyReportSchedule{}).Where("id = ? AND version = ? AND deleted_at = 0", row.ID, request.Version).Updates(map[string]any{"name": row.Name, "config_json": row.ConfigJSON, "enabled": row.Enabled, "next_at": row.NextAt, "version": request.Version + 1, "updated_at": now}).Error
	} else {
		err = model.DB.Create(&row).Error
	}
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, row)
}

func DeleteDailyReportSchedule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.DB.Model(&model.DailyReportSchedule{}).Where("id = ?", id).Updates(map[string]any{"deleted_at": common.GetTimestamp(), "enabled": false}).Error; err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"deleted": true})
}

func ListDailyReportScheduleRuns(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}
	var runs []model.DailyReportRun
	if err := model.DB.Where("schedule_id = ?", id).Order("id desc").Limit(100).Find(&runs).Error; err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"items": runs})
}

func DailyReportScheduleAction(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	action := c.Param("action")
	updates := map[string]any{}
	switch action {
	case "pause":
		updates["enabled"] = false
	case "resume":
		updates["enabled"] = true
	case "execute":
		var schedule model.DailyReportSchedule
		if err := model.DB.Where("id = ? AND deleted_at = 0", id).First(&schedule).Error; err != nil {
			adminDataError(c, err)
			return
		}
		var config struct {
			InstanceIDs []int64 `json:"instance_ids"`
			Source      string  `json:"source"`
			Date        string  `json:"date"`
		}
		if err := json.Unmarshal([]byte(schedule.ConfigJSON), &config); err != nil || len(config.InstanceIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid schedule config"})
			return
		}
		if config.Date == "" {
			config.Date = time.Now().In(dailyreport.BeijingLocation()).AddDate(0, 0, -1).Format("2006-01-02")
		}
		run := model.DailyReportRun{ScheduleID: id, Occurrence: "manual-" + strconv.FormatInt(common.GetTimestamp(), 10), Status: "running", StartedAt: common.GetTimestamp()}
		if err := model.DB.Create(&run).Error; err != nil {
			adminDataError(c, err)
			return
		}
		status, code := "succeeded", ""
		for _, instanceID := range config.InstanceIDs {
			if _, err := dailyreport.Collect(c.Request.Context(), instanceID, config.Date, config.Source); err != nil {
				status, code = "failed", "collection_failed"
			}
		}
		_ = model.DB.Model(&run).Updates(map[string]any{"status": status, "error_code": code, "finished_at": common.GetTimestamp()})
		adminDataJSON(c, http.StatusOK, gin.H{"run_id": run.ID, "status": status, "error_code": code})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "unknown action"})
		return
	}
	if err := model.DB.Model(&model.DailyReportSchedule{}).Where("id = ? AND deleted_at = 0", id).Updates(updates).Error; err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"updated": true})
}
