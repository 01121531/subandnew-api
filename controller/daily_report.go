package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/dailyreport"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type dailyReportRuleRequest struct {
	ID                 int64  `json:"id"`
	InstanceID         int64  `json:"instance_id"`
	SupplierCode       string `json:"supplier_code"`
	SupplierName       string `json:"supplier_name"`
	Filter             any    `json:"filter"`
	SourceTemplateID   int64  `json:"source_template_id"`
	SourceTemplateName string `json:"source_template_name"`
	Enabled            *bool  `json:"enabled"`
	Version            int64  `json:"version"`
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
	listDailyReportsForSource(c, c.Query("source"))
}

func listDailyReportsForSource(c *gin.Context, source string) {
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	date := c.DefaultQuery("date", time.Now().In(dailyreport.BeijingLocation()).Format("2006-01-02"))
	items := make([]dailyreport.Row, 0, len(ids))
	for _, id := range ids {
		var rules []model.DailyReportRule
		query := model.DB.Where("instance_id = ? AND enabled = ?", id, true)
		if supplierCode := strings.TrimSpace(c.Query("supplier_code")); supplierCode != "" {
			query = query.Where("supplier_code = ?", supplierCode)
		}
		if ruleID := strings.TrimSpace(c.Query("rule_id")); ruleID != "" {
			query = query.Where("id = ?", ruleID)
		}
		_ = query.Find(&rules).Error
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

func ListDailyReportAccounts(c *gin.Context) {
	listDailyReportsForSource(c, model.DailyReportSourceManagedAccounts)
}

func GetDailyReportDetail(c *gin.Context) { ListDailyReports(c) }

func ListDailyReportRules(c *gin.Context) {
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	items, err := dailyreport.ListRulesForInstances(ids)
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"items": items})
}

func ListDailyReportSupplierOptions(c *gin.Context) {
	id, err := strconv.ParseInt(c.Query("instance_id"), 10, 64)
	if err != nil || id <= 0 || !adminInstancesAllowed(c, []int64{id}) {
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid instance id"})
		}
		return
	}
	items, err := dailyreport.ListClaudeGatewaySuppliers(c.Request.Context(), id)
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"items": items})
}

func ListDailyReportSuppliers(c *gin.Context) {
	startDate, endDate, ok := dailyReportDateRange(c)
	if !ok {
		return
	}
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	items := make([]dailyreport.SupplierBill, 0)
	for _, id := range ids {
		var instance model.ManagedInstance
		if model.DB.First(&instance, id).Error != nil || instance.Kind != model.ManagedInstanceKindRouter {
			continue
		}
		result, err := dailyreport.CollectSupplierBills(c.Request.Context(), id, startDate, endDate)
		if err != nil {
			continue
		}
		items = append(items, result.Items...)
	}
	result := dailyreport.SupplierBillsOverview{StartDate: startDate, EndDate: endDate, Timezone: "Asia/Shanghai", Items: items, CollectedAt: common.GetTimestamp()}
	for _, item := range items {
		result.BillCount++
		result.TotalRequests += item.Requests
		result.TotalPayable += item.PayableAmount
	}
	adminDataJSON(c, http.StatusOK, result)
}

func ListDailyReportUploads(c *gin.Context) {
	startDate, endDate, ok := dailyReportDateRange(c)
	if !ok {
		return
	}
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	result := dailyreport.UploadsOverview{StartDate: startDate, EndDate: endDate, Timezone: "Asia/Shanghai", CollectedAt: common.GetTimestamp()}
	for _, id := range ids {
		var instance model.ManagedInstance
		if model.DB.First(&instance, id).Error != nil || instance.Kind != model.ManagedInstanceKindNevermore {
			continue
		}
		current, err := dailyreport.CollectUploads(c.Request.Context(), id, startDate, endDate)
		if err != nil {
			continue
		}
		result.Items = append(result.Items, current.Items...)
		result.Days = mergeUploadDays(result.Days, current.Days)
	}
	adminDataJSON(c, http.StatusOK, result)
}

func dailyReportDateRange(c *gin.Context) (string, string, bool) {
	startDate := strings.TrimSpace(c.Query("start_date"))
	endDate := strings.TrimSpace(c.Query("end_date"))
	if startDate == "" {
		startDate = time.Now().In(dailyreport.BeijingLocation()).AddDate(0, 0, -6).Format("2006-01-02")
	}
	if endDate == "" {
		endDate = time.Now().In(dailyreport.BeijingLocation()).Format("2006-01-02")
	}
	if _, _, err := dailyreport.ValidateDateRange(startDate, endDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "date range must be within 31 Beijing days"})
		return "", "", false
	}
	return startDate, endDate, true
}

func mergeUploadDays(left, right []dailyreport.UploadDay) []dailyreport.UploadDay {
	byDate := make(map[string]dailyreport.UploadDay, len(left)+len(right))
	for _, day := range append(left, right...) {
		current := byDate[day.Date]
		if current.Date == "" {
			current = dailyreport.UploadDay{Date: day.Date, ByVendor: map[string]int{}, ByState: map[string]int{}}
		}
		current.UploadCount += day.UploadCount
		current.Submitted += day.Submitted
		current.NeedsFix += day.NeedsFix
		for key, value := range day.ByVendor {
			current.ByVendor[key] += value
		}
		for key, value := range day.ByState {
			current.ByState[key] += value
		}
		byDate[day.Date] = current
	}
	result := make([]dailyreport.UploadDay, 0, len(byDate))
	for _, day := range byDate {
		result = append(result, day)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
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
	rule, err := dailyreport.SaveRule(model.DailyReportRule{ID: request.ID, InstanceID: request.InstanceID, SupplierCode: request.SupplierCode, SupplierName: request.SupplierName, FilterJSON: filterJSON, SourceTemplateID: request.SourceTemplateID, SourceTemplateName: request.SourceTemplateName, Enabled: enabled, Version: request.Version, CreatedBy: c.GetInt("id"), UpdatedBy: c.GetInt("id")})
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
	var rule model.DailyReportRule
	if err := model.DB.First(&rule, id).Error; err != nil {
		adminDataError(c, err)
		return
	}
	if !adminInstancesAllowed(c, []int64{rule.InstanceID}) {
		return
	}
	if err := dailyreport.DeleteRule(id); err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"deleted": true})
}

type dailyReportTemplatePushRequest struct {
	InstanceID      int64  `json:"instance_id"`
	SupplierCode    string `json:"supplier_code"`
	SupplierName    string `json:"supplier_name"`
	TemplateID      int64  `json:"template_id"`
	Enabled         bool   `json:"enabled"`
	ReplaceExisting bool   `json:"replace_existing"`
	Version         int64  `json:"version"`
}

func PushDailyReportFilterTemplate(c *gin.Context) {
	var request dailyReportTemplatePushRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.InstanceID <= 0 || request.TemplateID <= 0 || strings.TrimSpace(request.SupplierCode) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid template push request"})
		return
	}
	if !adminInstancesAllowed(c, []int64{request.InstanceID}) {
		return
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, request.InstanceID).Error; err != nil || instance.Kind != model.ManagedInstanceKindClaudeGateway {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "template push requires Claude Gateway"})
		return
	}
	options, err := dailyreport.ListClaudeGatewaySuppliers(c.Request.Context(), request.InstanceID)
	if err != nil {
		adminDataError(c, err)
		return
	}
	var supplierName string
	for _, option := range options {
		if option.Code == strings.TrimSpace(request.SupplierCode) {
			supplierName = option.Name
			break
		}
	}
	if supplierName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "supplier is not available in the current snapshot"})
		return
	}
	var template model.ManagedAccountFilterTemplate
	if err := model.DB.Where("id = ? AND actor_id = ?", request.TemplateID, c.GetInt("id")).First(&template).Error; err != nil {
		adminDataError(c, err)
		return
	}
	var rules []managedinstance.AccountFilterRule
	if err := json.Unmarshal([]byte(template.Rules), &rules); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid filter template"})
		return
	}
	if err := managedAccountFilterRulesAllowed(c, rules); err != nil {
		adminDataError(c, err)
		return
	}
	filterJSON, _ := json.Marshal(map[string]any{"match_mode": template.MatchMode, "rules": rules})
	var existing model.DailyReportRule
	existingErr := model.DB.Where("instance_id = ? AND supplier_code = ?", request.InstanceID, request.SupplierCode).First(&existing).Error
	if existingErr == nil && !request.ReplaceExisting {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "daily report rule already exists", "data": gin.H{"existing": existing}})
		return
	}
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		adminDataError(c, existingErr)
		return
	}
	id, version := int64(0), int64(0)
	if existingErr == nil {
		id, version = existing.ID, existing.Version
		if request.Version > 0 && request.Version != version {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "daily report rule changed", "data": gin.H{"existing": existing}})
			return
		}
	}
	rule, err := dailyreport.SaveRule(model.DailyReportRule{ID: id, InstanceID: request.InstanceID, SupplierCode: request.SupplierCode, SupplierName: supplierName, FilterJSON: string(filterJSON), SourceTemplateID: template.Id, SourceTemplateName: template.Name, Enabled: request.Enabled, Version: version, CreatedBy: c.GetInt("id"), UpdatedBy: c.GetInt("id")})
	if err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, rule)
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

func ExportDailyReportAccounts(c *gin.Context) {
	c.Request.URL.RawQuery = "source=" + url.QueryEscape(model.DailyReportSourceManagedAccounts) + "&" + c.Request.URL.RawQuery
	ExportDailyReport(c)
}

func ExportDailyReportSuppliers(c *gin.Context) {
	startDate, endDate, ok := dailyReportDateRange(c)
	if !ok {
		return
	}
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	rows := make([]dailyreport.SupplierBill, 0)
	for _, id := range ids {
		var instance model.ManagedInstance
		if err := model.DB.First(&instance, id).Error; err != nil || instance.Kind != model.ManagedInstanceKindRouter {
			continue
		}
		result, err := dailyreport.CollectSupplierBills(c.Request.Context(), id, startDate, endDate)
		if err != nil {
			continue
		}
		rows = append(rows, result.Items...)
	}
	book := excelize.NewFile()
	sheet := book.GetSheetName(0)
	headers := []string{"供应商", "供应商标识", "账单日期", "账单时区", "请求数", "输入 Token", "输出 Token", "缓存 Token", "原价金额", "应付金额", "币种", "Token 明细", "状态", "快照时间", "错误"}
	writeExportHeader(book, sheet, headers)
	for index, item := range rows {
		values := []any{item.SupplierName, item.SupplierCode, item.BillDate, item.Timezone, item.Requests, item.InputTokens, item.OutputTokens, item.CacheTokens, item.OriginalAmount, item.PayableAmount, item.Currency, formatTokenDetails(item.TokenDetails), item.Status, formatReportTimestamp(item.ObservedAt), item.ErrorCode}
		writeExportRow(book, sheet, index+2, values)
	}
	finishExportWorkbook(c, book, sheet, len(headers), len(rows)+1, "supplier-bills.xlsx")
}

func ExportDailyReportUploads(c *gin.Context) {
	startDate, endDate, ok := dailyReportDateRange(c)
	if !ok {
		return
	}
	ids, ok := reportIDs(c)
	if !ok {
		return
	}
	rows := make([]dailyreport.UploadRecord, 0)
	for _, id := range ids {
		var instance model.ManagedInstance
		if err := model.DB.First(&instance, id).Error; err != nil || instance.Kind != model.ManagedInstanceKindNevermore {
			continue
		}
		result, err := dailyreport.CollectUploads(c.Request.Context(), id, startDate, endDate)
		if err != nil {
			continue
		}
		rows = append(rows, result.Items...)
	}
	book := excelize.NewFile()
	sheet := book.GetSheetName(0)
	headers := []string{"实例 ID", "Delivery ID", "供应商 ID", "供应商", "批次", "账号标识", "状态", "问题类型", "已提交", "创建时间", "更新时间", "北京时间日期"}
	writeExportHeader(book, sheet, headers)
	for index, item := range rows {
		values := []any{item.InstanceID, item.ID, item.VendorID, item.Vendor, item.Batch, item.Account, item.State, item.IssueType, item.Submitted, item.CreatedAt, item.UpdatedAt, item.BeijingDate}
		writeExportRow(book, sheet, index+2, values)
	}
	finishExportWorkbook(c, book, sheet, len(headers), len(rows)+1, "nevermore-uploads.xlsx")
}

func writeExportHeader(book *excelize.File, sheet string, headers []string) {
	for index, header := range headers {
		column, _ := excelize.ColumnNumberToName(index + 1)
		_ = book.SetCellValue(sheet, column+"1", header)
	}
	style, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err == nil {
		_ = book.SetRowStyle(sheet, 1, 1, style)
	}
	_ = book.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
}

func writeExportRow(book *excelize.File, sheet string, row int, values []any) {
	for index, value := range values {
		column, _ := excelize.ColumnNumberToName(index + 1)
		if text, ok := value.(string); ok {
			value = exportText(text)
		}
		_ = book.SetCellValue(sheet, column+strconv.Itoa(row), value)
	}
}

func finishExportWorkbook(c *gin.Context, book *excelize.File, sheet string, columnCount, rowCount int, filename string) {
	lastColumn, _ := excelize.ColumnNumberToName(columnCount)
	if rowCount < 1 {
		rowCount = 1
	}
	_ = book.AutoFilter(sheet, "A1:"+lastColumn+strconv.Itoa(rowCount), nil)
	_ = book.SetColWidth(sheet, "A", lastColumn, 16)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	_ = book.Write(c.Writer)
}

func exportText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.ContainsAny(value[:1], "=+-@") {
		return "'" + value
	}
	return value
}

func formatReportTimestamp(timestamp int64) string {
	if timestamp <= 0 {
		return ""
	}
	return time.Unix(timestamp, 0).In(dailyreport.BeijingLocation()).Format("2006-01-02 15:04:05")
}

func formatTokenDetails(details map[string]float64) string {
	if len(details) == 0 {
		return ""
	}
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+": "+strconv.FormatFloat(details[key], 'f', -1, 64))
	}
	return strings.Join(parts, ", ")
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
	query := dailyScheduleScope(c).Where("deleted_at = 0").Order("id desc")
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
		err = dailyScheduleScope(c).Where("id = ? AND version = ? AND deleted_at = 0", row.ID, request.Version).Updates(map[string]any{"name": row.Name, "config_json": row.ConfigJSON, "enabled": row.Enabled, "next_at": row.NextAt, "version": request.Version + 1, "updated_at": now}).Error
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
	if err := dailyScheduleScope(c).Where("id = ?", id).Updates(map[string]any{"deleted_at": common.GetTimestamp(), "enabled": false}).Error; err != nil {
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
	var schedule model.DailyReportSchedule
	if err := dailyScheduleScope(c).Where("id = ? AND deleted_at = 0", id).First(&schedule).Error; err != nil {
		adminDataError(c, err)
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
		if err := dailyScheduleScope(c).Where("id = ? AND deleted_at = 0", id).First(&schedule).Error; err != nil {
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
	if err := dailyScheduleScope(c).Where("id = ? AND deleted_at = 0", id).Updates(updates).Error; err != nil {
		adminDataError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"updated": true})
}

func dailyScheduleScope(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.DailyReportSchedule{})
	if c.GetInt("role") < common.RoleRootUser {
		return query.Where("owner_id = ?", c.GetInt("id"))
	}
	return query
}
