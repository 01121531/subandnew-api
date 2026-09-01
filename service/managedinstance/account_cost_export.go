package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/xuri/excelize/v2"
)

const (
	accountCostExportMaxAttempts         = 4
	accountCostExportGlobalConcurrency   = 8
	accountCostExportInstanceConcurrency = 4
	accountCostExportRequestTimeout      = 60 * time.Second
	accountCostExportMaxBodyBytes        = 2 << 20
)

var accountCostExportRetryDelay = func(attempt int, retryAfter time.Duration) time.Duration {
	wait := time.Duration(1<<(attempt-1)) * time.Second
	if retryAfter > wait {
		wait = retryAfter
	}
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	return wait
}

type AccountCostExportResult struct {
	LifetimeCost       *float64 `json:"lifetime_cost,omitempty"`
	TodayCost          *float64 `json:"today_cost,omitempty"`
	CostExcludingToday *float64 `json:"cost_excluding_today,omitempty"`
	TodayRequests      *float64 `json:"today_requests,omitempty"`
	TodayTokens        *float64 `json:"today_tokens,omitempty"`
	ObservedAt         int64    `json:"observed_at"`
	LifetimeSource     string   `json:"lifetime_source,omitempty"`
}

type accountCostExportWork struct {
	item      *model.ManagedExportItem
	selection AccountExportSelection
}

type accountCostExportClient struct {
	once       sync.Once
	connector  *Connector
	credential *CredentialMaterial
	err        error
	semaphore  chan struct{}

	mu            sync.RWMutex
	authErrorCode string
}

type accountCostFetchError struct {
	code       string
	detail     string
	statusCode int
	retryAfter time.Duration
	retryable  bool
	auth       bool
}

func (err *accountCostFetchError) Error() string {
	if err.detail != "" {
		return err.detail
	}
	return err.code
}

func ExportClaudeGatewayAccountCostsXLSXToTaskFile(ctx context.Context, taskID string, actorID int, locale string, onProgress UsageRecordExportProgressCallback) (*UsageRecordExportArtifact, error) {
	_ = actorID
	processed, total, err := model.PrepareManagedExportItemsForResume(taskID)
	if err != nil {
		return nil, err
	}
	if total <= 0 || total > accountExportLimit {
		return nil, ErrInvalidInstance
	}
	items, err := model.ListManagedExportItems(taskID)
	if err != nil {
		return nil, err
	}
	if err := reportUsageRecordExportProgress(onProgress, int(processed), total, "collecting_account_costs"); err != nil {
		return nil, err
	}

	clients := make(map[int64]*accountCostExportClient)
	works := make([]accountCostExportWork, 0, len(items))
	for _, item := range items {
		if item.Status == model.ManagedExportItemStatusSucceeded || item.Status == model.ManagedExportItemStatusFailed {
			continue
		}
		var selection AccountExportSelection
		if json.Unmarshal([]byte(item.Metadata), &selection) != nil || selection.InstanceID <= 0 || selection.InstanceKind != model.ManagedInstanceKindClaudeGateway {
			if err := model.FinishManagedExportItem(item.ID, model.ManagedExportItemStatusFailed, item.Attempts, "", "account_cost_invalid_selection", "invalid frozen Claude Gateway account selection"); err != nil {
				return nil, err
			}
			processed++
			continue
		}
		works = append(works, accountCostExportWork{item: item, selection: selection})
		if clients[selection.InstanceID] == nil {
			clients[selection.InstanceID] = &accountCostExportClient{semaphore: make(chan struct{}, accountCostExportInstanceConcurrency)}
		}
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan accountCostExportWork)
	var workers sync.WaitGroup
	var processedCount atomic.Int64
	processedCount.Store(processed)
	var callbackMu sync.Mutex
	var fatalMu sync.Mutex
	var fatalErr error
	setFatal := func(err error) {
		if err == nil {
			return
		}
		fatalMu.Lock()
		if fatalErr == nil {
			fatalErr = err
			cancel()
		}
		fatalMu.Unlock()
	}
	reportTerminal := func() error {
		current := processedCount.Add(1)
		callbackMu.Lock()
		defer callbackMu.Unlock()
		return reportUsageRecordExportProgress(onProgress, int(current), total, "collecting_account_costs")
	}
	workerCount := min(accountCostExportGlobalConcurrency, len(works))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for work := range jobs {
				client := clients[work.selection.InstanceID]
				select {
				case client.semaphore <- struct{}{}:
				case <-workerCtx.Done():
					return
				}
				terminal, processErr := processClaudeGatewayAccountCostItem(workerCtx, client, work)
				<-client.semaphore
				if processErr != nil {
					setFatal(processErr)
					return
				}
				if terminal {
					if progressErr := reportTerminal(); progressErr != nil {
						setFatal(progressErr)
						return
					}
				}
			}
		}()
	}
	for _, work := range works {
		select {
		case jobs <- work:
		case <-workerCtx.Done():
			break
		}
		if workerCtx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	fatalMu.Lock()
	err = fatalErr
	fatalMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := reportUsageRecordExportProgress(onProgress, int(total), total, "generating_workbook"); err != nil {
		return nil, err
	}
	items, err = model.ListManagedExportItems(taskID)
	if err != nil {
		return nil, err
	}
	return writeAccountCostExportWorkbook(taskID, locale, items)
}

func processClaudeGatewayAccountCostItem(ctx context.Context, client *accountCostExportClient, work accountCostExportWork) (bool, error) {
	client.once.Do(func() {
		instance, _, connector, credential, err := observationClient(work.selection.InstanceID)
		if err == nil && instance.Kind != model.ManagedInstanceKindClaudeGateway {
			err = ErrUnsupportedCapability
		}
		client.connector, client.credential, client.err = connector, credential, err
	})
	if client.err != nil {
		return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusFailed, work.item.Attempts, "", managedInstanceObservationErrorCode(client.err), client.err.Error())
	}
	client.mu.RLock()
	authCode := client.authErrorCode
	client.mu.RUnlock()
	if authCode != "" {
		return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusFailed, work.item.Attempts, "", authCode, accountCostErrorMessage(authCode, ""))
	}

	attempts := work.item.Attempts
	if attempts >= accountCostExportMaxAttempts {
		return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusFailed, attempts, "", "account_cost_attempts_exhausted", accountCostErrorMessage("account_cost_attempts_exhausted", ""))
	}
	for attempts < accountCostExportMaxAttempts {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		attempts++
		if err := model.MarkManagedExportItemAttempt(work.item.ID, attempts); err != nil {
			return false, err
		}
		requestCtx, cancel := context.WithTimeout(ctx, accountCostExportRequestTimeout)
		result, fetchErr := fetchClaudeGatewayAccountCostDetail(requestCtx, client.connector, client.credential, work.selection)
		cancel()
		if fetchErr == nil {
			encoded, err := json.Marshal(result)
			if err != nil {
				return false, err
			}
			return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusSucceeded, attempts, string(encoded), "", "")
		}
		if ctx.Err() != nil {
			_ = model.ReturnManagedExportItemToPending(work.item.ID)
			return false, ctx.Err()
		}
		var detailErr *accountCostFetchError
		if !errors.As(fetchErr, &detailErr) {
			detailErr = classifyAccountCostFetchError(fetchErr)
		}
		if detailErr.auth {
			client.mu.Lock()
			client.authErrorCode = detailErr.code
			client.mu.Unlock()
		}
		if !detailErr.retryable || attempts >= accountCostExportMaxAttempts {
			return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusFailed, attempts, "", detailErr.code, accountCostErrorMessage(detailErr.code, detailErr.detail))
		}
		if err := model.ReturnManagedExportItemToPending(work.item.ID); err != nil {
			return false, err
		}
		wait := accountCostExportRetryDelay(attempts, detailErr.retryAfter)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	return true, model.FinishManagedExportItem(work.item.ID, model.ManagedExportItemStatusFailed, attempts, "", "account_cost_attempts_exhausted", accountCostErrorMessage("account_cost_attempts_exhausted", ""))
}

func fetchClaudeGatewayAccountCostDetail(ctx context.Context, connector *Connector, credential *CredentialMaterial, selection AccountExportSelection) (*AccountCostExportResult, error) {
	accountID := strings.TrimSpace(selection.Account.IDText)
	if accountID == "" {
		return nil, &accountCostFetchError{code: "account_cost_invalid_account_id", detail: "missing Claude Gateway account ID"}
	}
	response, err := claudeGatewayDoBulkJSON(ctx, connector, credential, http.MethodGet, "/api/admin/oauth-accounts/"+url.PathEscape(accountID), nil, accountCostExportMaxBodyBytes)
	if err != nil {
		return nil, classifyAccountCostFetchError(err)
	}
	if response == nil {
		return nil, &accountCostFetchError{code: "account_cost_invalid_response", detail: "empty Claude Gateway account detail response"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, accountCostHTTPError(response)
	}
	detail, err := decodeClaudeGatewayAccountCostDetail(response.Body)
	if err != nil {
		return nil, &accountCostFetchError{code: "account_cost_invalid_response", detail: "invalid Claude Gateway account detail response"}
	}
	if detail.LifetimeCost == nil && selection.Account.LifetimeCost != nil {
		value := *selection.Account.LifetimeCost
		detail.LifetimeCost = &value
		detail.LifetimeSource = "snapshot"
	}
	if detail.LifetimeCost == nil {
		return nil, &accountCostFetchError{code: "account_cost_lifetime_unavailable", detail: "cumulative account cost is unavailable"}
	}
	if detail.TodayCost == nil {
		return nil, &accountCostFetchError{code: "account_cost_today_unavailable", detail: "today account cost is unavailable"}
	}
	lifetime, today := *detail.LifetimeCost, *detail.TodayCost
	if !validAccountCost(lifetime) || !validAccountCost(today) {
		return nil, &accountCostFetchError{code: "account_cost_invalid_value", detail: "account cost contains an invalid value"}
	}
	difference := lifetime - today
	if difference < -1e-8 {
		return nil, &accountCostFetchError{code: "account_cost_inconsistent", detail: "today account cost exceeds cumulative account cost"}
	}
	if difference < 0 {
		difference = 0
	}
	detail.CostExcludingToday = &difference
	detail.ObservedAt = common.GetTimestamp()
	return detail, nil
}

func decodeClaudeGatewayAccountCostDetail(data []byte) (*AccountCostExportResult, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return nil, errors.New("invalid account detail JSON")
	}
	candidates := []map[string]json.RawMessage{root}
	for _, key := range []string{"account", "data"} {
		if raw := root[key]; len(raw) > 0 {
			var nested map[string]json.RawMessage
			if json.Unmarshal(raw, &nested) == nil {
				candidates = append(candidates, nested)
				if accountRaw := nested["account"]; len(accountRaw) > 0 {
					var account map[string]json.RawMessage
					if json.Unmarshal(accountRaw, &account) == nil {
						candidates = append(candidates, account)
					}
				}
			}
		}
	}
	result := &AccountCostExportResult{}
	for _, candidate := range candidates {
		if result.LifetimeCost == nil {
			result.LifetimeCost = accountCostNumberAt(candidate,
				[]string{"lifetime_cost"}, []string{"total_cost"}, []string{"stats", "total_cost"}, []string{"usage", "total_cost"}, []string{"costs", "total"})
			if result.LifetimeCost != nil {
				result.LifetimeSource = "detail"
			}
		}
		if result.TodayCost == nil {
			result.TodayCost = accountCostNumberAt(candidate,
				[]string{"today_cost"}, []string{"daily_cost"}, []string{"stats", "daily_cost"}, []string{"stats", "today_cost"},
				[]string{"usage", "daily_cost"}, []string{"usage", "today_cost"}, []string{"costs", "today"},
				[]string{"usage_records", "today", "cost"}, []string{"usage_records", "daily", "cost"}, []string{"usage_records", "summary", "today", "cost"})
		}
		if result.TodayRequests == nil {
			result.TodayRequests = accountCostNumberAt(candidate,
				[]string{"today_requests"}, []string{"daily_requests"}, []string{"daily_req"}, []string{"stats", "daily_req"},
				[]string{"stats", "daily_requests"}, []string{"usage_records", "today", "requests"})
		}
		if result.TodayTokens == nil {
			result.TodayTokens = accountCostNumberAt(candidate,
				[]string{"today_tokens"}, []string{"daily_tokens"}, []string{"daily_tok"}, []string{"stats", "daily_tok"},
				[]string{"stats", "daily_tokens"}, []string{"usage_records", "today", "tokens"})
		}
	}
	if result.LifetimeCost == nil && result.TodayCost == nil && result.TodayRequests == nil && result.TodayTokens == nil {
		return nil, errors.New("account detail contains no usage fields")
	}
	return result, nil
}

func accountCostNumberAt(root map[string]json.RawMessage, paths ...[]string) *float64 {
	value, err := claudeGatewayNumberAt(root, paths...)
	if err != nil || value == nil {
		return nil
	}
	result := float64(*value)
	return &result
}

func validAccountCost(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func accountCostHTTPError(response *ConnectorResponse) *accountCostFetchError {
	status := response.StatusCode
	err := &accountCostFetchError{statusCode: status, detail: fmt.Sprintf("Claude Gateway account detail returned HTTP %d", status)}
	switch {
	case status == http.StatusUnauthorized:
		err.code, err.auth = "account_cost_authentication_failed", true
	case status == http.StatusForbidden:
		err.code, err.auth = "account_cost_permission_denied", true
	case status == http.StatusNotFound:
		err.code = "account_cost_account_not_found"
	case status == http.StatusRequestTimeout:
		err.code, err.retryable = "account_cost_timeout", true
	case status == http.StatusTooManyRequests:
		err.code, err.retryable = "account_cost_rate_limited", true
	case status >= 500:
		err.code, err.retryable = "account_cost_upstream_unavailable", true
	default:
		err.code = "account_cost_remote_http"
	}
	retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
	if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil && seconds > 0 {
		err.retryAfter = time.Duration(seconds) * time.Second
	} else if retryAt, parseErr := http.ParseTime(retryAfter); parseErr == nil {
		err.retryAfter = time.Until(retryAt)
		if err.retryAfter < 0 {
			err.retryAfter = 0
		}
	}
	return err
}

func classifyAccountCostFetchError(err error) *accountCostFetchError {
	if err == nil {
		return nil
	}
	var detailErr *accountCostFetchError
	if errors.As(err, &detailErr) {
		return detailErr
	}
	var probeErr *ProbeError
	if errors.As(err, &probeErr) {
		if probeErr.StatusCode > 0 {
			return accountCostHTTPError(&ConnectorResponse{StatusCode: probeErr.StatusCode})
		}
		switch probeErr.Code {
		case ProbeErrorAuthentication, ProbeErrorCredentialExpired:
			return &accountCostFetchError{code: "account_cost_authentication_failed", detail: err.Error(), auth: true}
		case ProbeErrorPermission:
			return &accountCostFetchError{code: "account_cost_permission_denied", detail: err.Error(), auth: true}
		default:
			return &accountCostFetchError{code: probeErr.Code, detail: err.Error()}
		}
	}
	var networkErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &accountCostFetchError{code: "account_cost_timeout", detail: "Claude Gateway account detail request timed out", retryable: true}
	case errors.Is(err, context.Canceled):
		return &accountCostFetchError{code: "account_cost_cancelled", detail: "account cost export was cancelled"}
	case errors.As(err, &networkErr):
		return &accountCostFetchError{code: "account_cost_network_failed", detail: "Claude Gateway account detail network request failed", retryable: true}
	default:
		return &accountCostFetchError{code: managedInstanceObservationErrorCode(err), detail: err.Error()}
	}
}

func accountCostErrorMessage(code string, detail string) string {
	messages := map[string]string{
		"account_cost_invalid_selection":     "冻结的账号信息无效",
		"account_cost_invalid_account_id":    "账号 ID 无效",
		"account_cost_authentication_failed": "实例登录已失效",
		"account_cost_permission_denied":     "实例账号没有读取详情的权限",
		"account_cost_account_not_found":     "账号已不存在",
		"account_cost_timeout":               "账号详情请求超时",
		"account_cost_rate_limited":          "账号详情请求被限流",
		"account_cost_upstream_unavailable":  "Claude Gateway 暂时不可用",
		"account_cost_network_failed":        "无法连接 Claude Gateway",
		"account_cost_invalid_response":      "账号详情响应格式错误",
		"account_cost_lifetime_unavailable":  "累计总消费未提供",
		"account_cost_today_unavailable":     "今日消费未提供",
		"account_cost_invalid_value":         "消费金额不是有效数值",
		"account_cost_inconsistent":          "今日消费大于累计总消费",
		"account_cost_attempts_exhausted":    "账号详情重试 3 次后仍失败",
		"account_cost_remote_http":           "账号详情请求失败",
		"account_cost_cancelled":             "任务已取消",
	}
	if message := messages[code]; message != "" {
		return message
	}
	if strings.TrimSpace(detail) != "" {
		return detail
	}
	return "账号历史消费采集失败"
}

func writeAccountCostExportWorkbook(taskID string, locale string, items []*model.ManagedExportItem) (*UsageRecordExportArtifact, error) {
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
	const sheet = "历史消费"
	workbook.SetSheetName("Sheet1", sheet)
	headers := []string{"实例", "账号 ID", "名称", "邮箱", "账号归属", "供应商", "供应商邮箱", "累计总消费 ($)", "今日消费 ($)", "历史消费（不含今日）($)", "今日请求", "今日 Token", "详情采集时间", "处理状态", "错误原因"}
	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		_ = workbook.SetCellValue(sheet, cell, header)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	if location == nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	warnings := 0
	for index, item := range items {
		var selection AccountExportSelection
		_ = json.Unmarshal([]byte(item.Metadata), &selection)
		var result AccountCostExportResult
		resultErr := error(nil)
		if item.Status == model.ManagedExportItemStatusSucceeded {
			resultErr = json.Unmarshal([]byte(item.Result), &result)
		}
		status := "成功"
		errorMessage := ""
		if item.Status != model.ManagedExportItemStatusSucceeded || resultErr != nil {
			status = "失败"
			warnings++
			if resultErr != nil {
				errorMessage = "已保存的结果无法解析"
			} else {
				errorMessage = accountCostErrorMessage(item.ErrorCode, item.ErrorDetail)
			}
		}
		account := selection.Account
		ownership := firstNonEmpty(selection.SourceName, account.Ownership, account.Group, selection.InstanceName)
		observedAt := ""
		if result.ObservedAt > 0 {
			observedAt = time.Unix(result.ObservedAt, 0).In(location).Format("2006/01/02 15:04:05")
		}
		values := []any{
			selection.InstanceName,
			firstNonEmpty(account.IDText, strconv.FormatInt(account.ID, 10)),
			account.Name, account.Email, ownership, account.VendorName, account.VendorEmail,
			optionalFloat(result.LifetimeCost), optionalFloat(result.TodayCost), optionalFloat(result.CostExcludingToday),
			optionalFloat(result.TodayRequests), optionalFloat(result.TodayTokens), observedAt, status, errorMessage,
		}
		for column, value := range values {
			cell, _ := excelize.CoordinatesToCellName(column+1, index+2)
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}
	headerStyle, _ := workbook.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "#111827"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F3F4F6"}, Pattern: 1}, Border: []excelize.Border{{Type: "bottom", Color: "#D1D5DB", Style: 1}}, Alignment: &excelize.Alignment{Vertical: "center"}})
	numberStyle, _ := workbook.NewStyle(&excelize.Style{NumFmt: 3})
	moneyStyle, _ := workbook.NewStyle(&excelize.Style{CustomNumFmt: stringPointer("$#,##0.00000000")})
	_ = workbook.SetCellStyle(sheet, "A1", "O1", headerStyle)
	if len(items) > 0 {
		end := len(items) + 1
		_ = workbook.SetCellStyle(sheet, "H2", fmt.Sprintf("J%d", end), moneyStyle)
		_ = workbook.SetCellStyle(sheet, "K2", fmt.Sprintf("L%d", end), numberStyle)
	}
	widths := map[string]float64{"A": 22, "B": 38, "C": 24, "D": 32, "E": 22, "F": 22, "G": 30, "H": 22, "I": 20, "J": 28, "K": 16, "L": 20, "M": 22, "N": 14, "O": 34}
	for column, width := range widths {
		_ = workbook.SetColWidth(sheet, column, column, width)
	}
	_ = workbook.SetRowHeight(sheet, 1, 24)
	_ = workbook.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	_ = workbook.AutoFilter(sheet, "A1:O1", nil)
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
	_ = locale
	return &UsageRecordExportArtifact{FileName: "account-costs-" + time.Now().Format("20060102-150405") + ".xlsx", RecordCount: len(items), WarningCount: warnings, Size: info.Size(), ExpiresAt: time.Now().Add(usageRecordExportRetention).Unix()}, nil
}
