package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/xuri/excelize/v2"
)

const accountExportLimit = managedInstanceInventoryMaxItems

type AccountExportSelection struct {
	InstanceID   int64         `json:"instance_id"`
	InstanceName string        `json:"instance_name"`
	InstanceKind string        `json:"instance_kind"`
	SourceName   string        `json:"source_name,omitempty"`
	Account      InventoryItem `json:"account"`
}

type AccountExportInput struct {
	Source   string                   `json:"source"`
	Window   TimeWindow               `json:"window"`
	Locale   string                   `json:"locale"`
	ActorID  int                      `json:"actor_id,omitempty"`
	Selected []AccountExportSelection `json:"selected"`
}

type AccountExportRow struct {
	Selection        AccountExportSelection
	Requests         *float64
	InputTokens      *float64
	OutputTokens     *float64
	CacheWriteTokens *float64
	CacheReadTokens  *float64
	Amount           *float64
	TotalTokens      *float64
	Status           string
	ErrorCode        string
}

type accountExportMetrics struct {
	requests, input, output, cacheWrite, cacheRead, amount float64
	totalTokens                                            float64
	hasRequests, hasTotalTokens, hasAmount                 bool
}

func ExportAccountsXLSXToTaskFile(ctx context.Context, taskID string, input AccountExportInput, onProgress UsageRecordExportProgressCallback) (*UsageRecordExportArtifact, error) {
	if len(input.Selected) == 0 || len(input.Selected) > accountExportLimit || input.Window.Start <= 0 || input.Window.End <= input.Window.Start {
		return nil, ErrInvalidInstance
	}
	if input.Window.Timezone == "" {
		input.Window.Timezone = "Asia/Shanghai"
	}
	rows, warnings, err := collectAccountExportRows(ctx, input, onProgress)
	if err != nil {
		return nil, err
	}
	return writeAccountExportWorkbook(taskID, input, rows, warnings)
}

func collectAccountExportRows(ctx context.Context, input AccountExportInput, onProgress UsageRecordExportProgressCallback) ([]AccountExportRow, int, error) {
	rows := make([]AccountExportRow, len(input.Selected))
	byInstance := make(map[int64][]int)
	for index, selected := range input.Selected {
		rows[index] = AccountExportRow{Selection: selected, Status: model.ManagedInstanceCollectionFailed}
		byInstance[selected.InstanceID] = append(byInstance[selected.InstanceID], index)
	}
	processed := 0
	warnings := 0
	for instanceID, indexes := range byInstance {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		kind := input.Selected[indexes[0]].InstanceKind
		var results map[int64]accountExportMetrics
		failures := map[int64]string{}
		var collectionErr error
		switch kind {
		case model.ManagedInstanceKindConductor:
			results, collectionErr = collectConductorAccountExportMetrics(ctx, instanceID, input.ActorID, input.Window)
		case model.ManagedInstanceKindSub2API, model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan, model.ManagedInstanceKindMercerRouter:
			results, failures, collectionErr = collectPagedAccountExportMetrics(ctx, instanceID, input.ActorID, indexes, input.Selected, input.Window)
		case model.ManagedInstanceKindClaudeGateway:
			results = collectClaudeGatewayAccountExportMetrics(indexes, input.Selected, input.Source, input.Window)
		default:
			collectionErr = ErrUnsupportedCapability
		}
		if accountExportFatalError(collectionErr) {
			return nil, warnings, collectionErr
		}
		for _, index := range indexes {
			row := &rows[index]
			metrics, exists := results[row.Selection.Account.ID]
			if !exists && collectionErr == nil && kind == model.ManagedInstanceKindConductor {
				metrics = accountExportMetrics{hasAmount: true}
				exists = true
			}
			if collectionErr != nil || !exists {
				row.ErrorCode = failures[row.Selection.Account.ID]
				if row.ErrorCode == "" {
					row.ErrorCode = managedInstanceObservationErrorCode(collectionErr)
				}
				if row.ErrorCode == "" {
					row.ErrorCode = "account_usage_unavailable"
				}
				warnings++
			} else {
				row.Status = model.ManagedInstanceCollectionSucceeded
				if kind == model.ManagedInstanceKindClaudeGateway {
					if metrics.hasRequests {
						row.Requests = float64Pointer(metrics.requests)
					}
					if metrics.hasTotalTokens {
						row.TotalTokens = float64Pointer(metrics.totalTokens)
					}
				} else {
					row.Requests = float64Pointer(metrics.requests)
					row.InputTokens = float64Pointer(metrics.input)
					row.OutputTokens = float64Pointer(metrics.output)
					row.CacheWriteTokens = float64Pointer(metrics.cacheWrite)
					row.CacheReadTokens = float64Pointer(metrics.cacheRead)
					total := metrics.input + metrics.output + metrics.cacheWrite + metrics.cacheRead
					row.TotalTokens = float64Pointer(total)
				}
				if metrics.hasAmount {
					row.Amount = float64Pointer(metrics.amount)
				}
			}
			processed++
			if err := reportUsageRecordExportProgress(onProgress, processed, int64(len(rows)), "exporting"); err != nil {
				return nil, warnings, err
			}
		}
	}
	return rows, warnings, nil
}

func collectClaudeGatewayAccountExportMetrics(indexes []int, selected []AccountExportSelection, source string, window TimeWindow) map[int64]accountExportMetrics {
	result := make(map[int64]accountExportMetrics, len(indexes))
	for _, index := range indexes {
		account := selected[index].Account
		// Account-output rows only contain accounts created inside the selected
		// window, so their lifetime counters are also valid period totals.
		if source != "account_output" && !claudeGatewayUsageWindowMatches(account, window) {
			continue
		}
		metrics := accountExportMetrics{}
		if account.Requests != nil {
			metrics.requests = *account.Requests
			metrics.hasRequests = true
		}
		if account.Tokens != nil {
			metrics.totalTokens = *account.Tokens
			metrics.hasTotalTokens = true
		}
		if account.Cost != nil && strings.EqualFold(account.CostUnit, "usd") {
			metrics.amount = *account.Cost
			metrics.hasAmount = true
		}
		if metrics.hasRequests || metrics.hasTotalTokens || metrics.hasAmount {
			result[account.ID] = metrics
		}
	}
	return result
}

func claudeGatewayUsageWindowMatches(account InventoryItem, window TimeWindow) bool {
	if account.UsageWindowDays <= 0 || window.End <= window.Start {
		return false
	}
	days := int((window.End - window.Start + 1 + 86399) / 86400)
	return days == account.UsageWindowDays
}

func collectConductorAccountExportMetrics(ctx context.Context, instanceID int64, actorID int, window TimeWindow) (map[int64]accountExportMetrics, error) {
	_, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	location, _ := time.LoadLocation(window.Timezone)
	if location == nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	var report *conductorReportPayload
	err = retryAccountExport(ctx, instanceID, actorID, func() error {
		var fetchErr error
		report, fetchErr = conductorUsageReport(ctx, connector, credential, "account", time.Unix(window.Start, 0).In(location).Format("2006-01-02"), time.Unix(window.End, 0).In(location).Format("2006-01-02"), location.String(), "")
		return fetchErr
	})
	if err != nil {
		return nil, err
	}
	result := make(map[int64]accountExportMetrics, len(report.Rows))
	for _, item := range report.Rows {
		accountID, parseErr := strconv.ParseInt(strings.TrimSpace(item.AccountID), 10, 64)
		if parseErr != nil || accountID <= 0 {
			continue
		}
		current := result[accountID]
		current.requests += item.Requests
		current.input += item.InputTokens
		current.output += item.OutputTokens
		current.cacheRead += item.CacheReadTokens
		current.cacheWrite += item.CacheCreationTokens
		current.amount += item.Cost
		current.hasAmount = true
		result[accountID] = current
	}
	return result, nil
}

func collectPagedAccountExportMetrics(ctx context.Context, instanceID int64, actorID int, indexes []int, selected []AccountExportSelection, window TimeWindow) (map[int64]accountExportMetrics, map[int64]string, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, nil, err
	}
	result := make(map[int64]accountExportMetrics, len(indexes))
	failures := make(map[int64]string)
	var fatalErr error
	var resultMu sync.Mutex
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(6, len(indexes))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				accountID := selected[index].Account.ID
				var metrics accountExportMetrics
				fetchErr := retryAccountExport(ctx, instanceID, actorID, func() error {
					var collectErr error
					metrics, collectErr = collectAccountUsagePages(ctx, client, accountID, window)
					return collectErr
				})
				if fetchErr == nil {
					resultMu.Lock()
					result[accountID] = metrics
					resultMu.Unlock()
				} else {
					resultMu.Lock()
					if accountExportFatalError(fetchErr) {
						fatalErr = fetchErr
					} else {
						failures[accountID] = managedInstanceObservationErrorCode(fetchErr)
					}
					resultMu.Unlock()
				}
			}
		}()
	}
	for _, index := range indexes {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return result, failures, fatalErr
}

func accountExportFatalError(err error) bool {
	var probeError *ProbeError
	if !errors.As(err, &probeError) {
		return false
	}
	return probeError.Code == ProbeErrorAuthentication || probeError.Code == ProbeErrorPermission
}

func collectAccountUsagePages(ctx context.Context, client *usageRecordClient, accountID int64, window TimeWindow) (accountExportMetrics, error) {
	query := url.Values{}
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		location, timezone := summaryLocation(window.Timezone)
		query.Set("account_id", strconv.FormatInt(accountID, 10))
		query.Set("start_date", time.Unix(window.Start, 0).In(location).Format("2006-01-02"))
		query.Set("end_date", time.Unix(window.End, 0).In(location).Format("2006-01-02"))
		query.Set("timezone", timezone)
		query.Set("exact_total", "false")
	} else {
		query.Set("channel", strconv.FormatInt(accountID, 10))
		query.Set("start_timestamp", strconv.FormatInt(window.Start, 10))
		query.Set("end_timestamp", strconv.FormatInt(window.End, 10))
	}
	query.Set("page_size", strconv.Itoa(usageRecordMaxPageSize))
	query, err := normalizeUsageRecordQuery(client.instance.Kind, query)
	if err != nil {
		return accountExportMetrics{}, err
	}
	result := accountExportMetrics{}
	for pageNumber := 1; pageNumber <= usageRecordExportLimit/usageRecordMaxPageSize; pageNumber++ {
		setUsageRecordPage(query, client.instance.Kind, pageNumber, usageRecordMaxPageSize)
		page, err := client.list(ctx, query)
		if err != nil {
			return accountExportMetrics{}, err
		}
		for _, raw := range page.Items {
			var item map[string]any
			if json.Unmarshal(raw, &item) != nil {
				return accountExportMetrics{}, &ProbeError{Code: ProbeErrorInvalidResponse}
			}
			value := func(key string) float64 {
				number, _ := usageNumber(item[key])
				return number
			}
			result.requests++
			if client.instance.Kind == model.ManagedInstanceKindSub2API {
				result.input += value("input_tokens")
				result.output += value("output_tokens")
				result.cacheRead += value("cache_read_tokens")
				result.cacheWrite += value("cache_creation_tokens")
				result.amount += value("actual_cost")
				result.hasAmount = true
			} else if client.instance.Kind == model.ManagedInstanceKindMercerRouter {
				result.input += value("prompt_tokens")
				result.output += value("completion_tokens")
				if amount, ok := firstJSONFloat64Raw(item, "amount_usd"); ok {
					result.amount += amount
					result.hasAmount = true
				}
			} else {
				result.input += value("prompt_tokens")
				result.output += value("completion_tokens")
				result.amount += value("quota")
			}
		}
		if !page.HasMore || len(page.Items) == 0 {
			break
		}
	}
	if client.instance.Kind != model.ManagedInstanceKindSub2API && client.instance.Kind != model.ManagedInstanceKindMercerRouter {
		amount, currency := client.newAPIQuotaAmount(ctx, result.amount)
		result.amount = amount
		result.hasAmount = strings.EqualFold(currency, "usd")
	}
	return result, nil
}

func firstJSONFloat64Raw(item map[string]any, key string) (float64, bool) {
	value, exists := item[key]
	if !exists || value == nil {
		return 0, false
	}
	return usageNumber(value)
}

func retryAccountExport(ctx context.Context, instanceID int64, actorID int, operation func() error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = operation(); err == nil {
			return nil
		}
		if attempt == 2 {
			break
		}
		_ = RecoverDataConnection(ctx, instanceID, actorID)
		timer := time.NewTimer(time.Duration(1<<attempt) * 500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func writeAccountExportWorkbook(taskID string, input AccountExportInput, rows []AccountExportRow, warnings int) (*UsageRecordExportArtifact, error) {
	path, temporaryPath, err := accountExportTaskPaths(taskID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	_ = os.Remove(temporaryPath)
	workbook := excelize.NewFile()
	defer workbook.Close()
	const sheet = "账号导出"
	workbook.SetSheetName("Sheet1", sheet)
	headers := []string{"账号归属", "供应商", "供应商邮箱", "账号 Email", "账号类型", "录入时间", "存活时间（截至结算时刻）", "录入备注", "请求数", "输入 Tokens", "输出 Tokens", "缓存写入 Tokens", "缓存读取 Tokens", "消费金额 ($)", "实例", "平台", "账号 ID", "可用状态", "总 Tokens", "统计状态", "统计错误"}
	for column, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(column+1, 1)
		_ = workbook.SetCellValue(sheet, cell, header)
	}
	location, _ := time.LoadLocation(input.Window.Timezone)
	if location == nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	for index, row := range rows {
		values := accountExportCellValues(row, input.Window, location)
		for column, value := range values {
			cell, _ := excelize.CoordinatesToCellName(column+1, index+2)
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}
	headerStyle, _ := workbook.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "#111827"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F3F4F6"}, Pattern: 1}, Border: []excelize.Border{{Type: "bottom", Color: "#D1D5DB", Style: 1}}, Alignment: &excelize.Alignment{Vertical: "center"}})
	numberStyle, _ := workbook.NewStyle(&excelize.Style{NumFmt: 3})
	moneyStyle, _ := workbook.NewStyle(&excelize.Style{CustomNumFmt: stringPointer("$#,##0.00000000")})
	_ = workbook.SetCellStyle(sheet, "A1", "U1", headerStyle)
	if len(rows) > 0 {
		end := len(rows) + 1
		_ = workbook.SetCellStyle(sheet, "I2", fmt.Sprintf("M%d", end), numberStyle)
		_ = workbook.SetCellStyle(sheet, "S2", fmt.Sprintf("S%d", end), numberStyle)
		_ = workbook.SetCellStyle(sheet, "N2", fmt.Sprintf("N%d", end), moneyStyle)
	}
	widths := map[string]float64{"A": 20, "B": 20, "C": 30, "D": 32, "E": 16, "F": 20, "G": 25, "H": 26, "I": 14, "J": 16, "K": 16, "L": 18, "M": 18, "N": 18, "O": 20, "P": 16, "Q": 22, "R": 14, "S": 18, "T": 16, "U": 30}
	for column, width := range widths {
		_ = workbook.SetColWidth(sheet, column, column, width)
	}
	_ = workbook.SetRowHeight(sheet, 1, 24)
	_ = workbook.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	_ = workbook.AutoFilter(sheet, "A1:U1", nil)
	temporaryFile, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := workbook.Write(temporaryFile); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &UsageRecordExportArtifact{FileName: "accounts-" + time.Now().Format("20060102-150405") + ".xlsx", RecordCount: len(rows), WarningCount: warnings, Size: info.Size(), ExpiresAt: time.Now().Add(usageRecordExportRetention).Unix()}, nil
}

func accountExportCellValues(row AccountExportRow, window TimeWindow, location *time.Location) []any {
	item := row.Selection.Account
	ownership := firstNonEmpty(row.Selection.SourceName, item.Ownership, item.Group, row.Selection.InstanceName)
	note := firstNonEmpty(item.Note, item.Name)
	accountType := firstNonEmpty(item.Type, item.Group)
	createdAt := ""
	if item.CreatedAt > 0 {
		createdAt = time.Unix(item.CreatedAt, 0).In(location).Format("2006/01/02 15:04")
	}
	available := "未知"
	if item.Enabled != nil {
		if *item.Enabled {
			available = "可用"
		} else {
			available = "不可用"
		}
	}
	status := "成功"
	if row.Status != model.ManagedInstanceCollectionSucceeded {
		status = "不可用"
		if row.ErrorCode == managedInstanceObservationErrorCode(ErrUnsupportedCapability) {
			status = "不支持该时间范围"
		}
	}
	return []any{
		ownership, item.VendorName, item.VendorEmail, item.Email, accountType, createdAt, accountExportSurvival(item, window.End), note,
		optionalFloat(row.Requests), optionalFloat(row.InputTokens), optionalFloat(row.OutputTokens), optionalFloat(row.CacheWriteTokens), optionalFloat(row.CacheReadTokens), optionalFloat(row.Amount),
		row.Selection.InstanceName, row.Selection.InstanceKind, firstNonEmpty(item.IDText, strconv.FormatInt(item.ID, 10)), available, optionalFloat(row.TotalTokens), status, row.ErrorCode,
	}
}

func accountExportSurvival(item InventoryItem, settlement int64) string {
	if item.CreatedAt <= 0 || settlement <= item.CreatedAt {
		return ""
	}
	end := settlement
	if item.Enabled != nil && !*item.Enabled {
		for _, candidate := range []int64{item.LastActivityAt, item.DisabledAt} {
			if candidate > item.CreatedAt && candidate < end {
				end = candidate
			}
		}
	}
	duration := time.Duration(end-item.CreatedAt) * time.Second
	days := int(duration / (24 * time.Hour))
	duration -= time.Duration(days) * 24 * time.Hour
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	return fmt.Sprintf("%d天%d小时%d分钟", days, hours, minutes)
}

func optionalFloat(value *float64) any {
	if value == nil {
		return ""
	}
	return *value
}

func float64Pointer(value float64) *float64 { return &value }
func stringPointer(value string) *string    { return &value }

func accountExportTaskPaths(taskID string) (string, string, error) {
	if _, err := usageRecordExportTaskPath(taskID, false); err != nil {
		return "", "", err
	}
	directory := strings.TrimSpace(os.Getenv("MANAGED_USAGE_EXPORT_DIR"))
	if directory == "" {
		directory = filepath.Join(".", "exports", "usage-records")
	}
	path := filepath.Join(directory, "managed-account-"+taskID+".xlsx")
	return path, path + ".part", nil
}

func OpenManagedExportArtifact(taskID string, fileFormat string) (*os.File, error) {
	if fileFormat == model.ManagedExportFormatXLSX {
		path, _, err := accountExportTaskPaths(taskID)
		if err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	return OpenUsageRecordExportArtifact(taskID)
}

func RemoveManagedExportArtifact(taskID string, fileFormat string) {
	if fileFormat == model.ManagedExportFormatXLSX {
		path, temporary, err := accountExportTaskPaths(taskID)
		if err == nil {
			_ = os.Remove(path)
			_ = os.Remove(temporary)
		}
		return
	}
	RemoveUsageRecordExportArtifact(taskID)
}
