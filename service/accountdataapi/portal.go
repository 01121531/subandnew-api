package accountdataapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	PortalSessionLifetime  = 12 * time.Hour
	PortalExportLimit      = 10000
	portalLoginWindow      = 15 * time.Minute
	portalLoginMaxAttempts = 5
	portalLoginLocalLimit  = 10000
)

var (
	ErrPortalUnauthorized = errors.New("invalid or expired portal session")
	ErrPortalLoginLimited = errors.New("too many portal login attempts")
	ErrPortalExportLarge  = errors.New("portal export exceeds 10000 accounts")
)

type PortalLoginResult struct {
	Token     string      `json:"-"`
	SessionID int64       `json:"-"`
	CSRFToken string      `json:"csrf_token"`
	Session   *PortalView `json:"session"`
}

type PortalView struct {
	Authenticated  bool     `json:"authenticated"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Dataset        string   `json:"dataset"`
	PresetDays     int      `json:"preset_days"`
	Timezone       string   `json:"timezone"`
	Fields         []string `json:"fields"`
	FilterFields   []string `json:"filter_fields"`
	PageSize       int      `json:"page_size"`
	ExpiresAt      int64    `json:"expires_at"`
	CSRFToken      string   `json:"csrf_token,omitempty"`
	LastObservedAt int64    `json:"last_observed_at"`
	Stale          bool     `json:"stale"`
}

type PortalAuthenticated struct {
	API     *model.ManagedAccountAPI
	View    *View
	Session *model.ManagedAccountAPIPortalSession
	Token   string
}

type PortalQueryInput struct {
	IncludeTerms     []string                            `json:"include_terms"`
	ExcludeTerms     []string                            `json:"exclude_terms"`
	MatchMode        string                              `json:"match_mode"`
	Rules            []managedinstance.AccountFilterRule `json:"rules"`
	Search           string                              `json:"search"`
	SortBy           string                              `json:"sort_by"`
	SortOrder        string                              `json:"sort_order"`
	Page             int                                 `json:"page"`
	PageSize         int                                 `json:"page_size"`
	SelectedAccounts []managedaccount.AccountIdentity    `json:"-"`
}

type PortalSelection struct {
	InstanceID int64  `json:"instance_id"`
	AccountID  string `json:"account_id"`
}

type PortalExportInput struct {
	Query      PortalQueryInput  `json:"query"`
	Mode       string            `json:"mode"`
	Selections []PortalSelection `json:"selections"`
	Exclusions []PortalSelection `json:"exclusions"`
}

type PortalExport struct {
	FileName string
	Data     []byte
	Count    int
}

func LoginPortal(slug, password, clientIP string) (*PortalLoginResult, error) {
	password = strings.TrimSpace(password)
	if utf8.RuneCountInString(password) == 0 || utf8.RuneCountInString(password) > 128 {
		return nil, ErrPortalUnauthorized
	}
	entry, view, err := loadPortal(slug, clientIP)
	if err != nil {
		return nil, ErrPortalUnauthorized
	}
	if blocked, _ := portalLoginBlocked(slug, clientIP); blocked {
		return nil, ErrPortalLoginLimited
	}
	if !common.ValidatePasswordAndHash(password, entry.PortalPasswordHash) {
		portalLoginFailed(slug, clientIP)
		return nil, ErrPortalUnauthorized
	}
	portalLoginSucceeded(slug, clientIP)
	token, err := randomPortalToken()
	if err != nil {
		return nil, err
	}
	csrf := portalCSRFToken(token)
	now := common.GetTimestamp()
	session := &model.ManagedAccountAPIPortalSession{APIID: entry.ID, TokenHash: hashToken(token), CSRFHash: hashToken(csrf),
		IPAddress: clientIP, ExpiresAt: now + int64(PortalSessionLifetime/time.Second), LastUsedAt: now}
	if err := model.DB.Create(session).Error; err != nil {
		return nil, err
	}
	maybeCleanupPortalSessions(now)
	return &PortalLoginResult{Token: token, SessionID: session.ID, CSRFToken: csrf, Session: portalView(view, session.ExpiresAt, "")}, nil
}

func AuthenticatePortal(slug, token, csrf, clientIP string, requireCSRF bool) (*PortalAuthenticated, error) {
	entry, view, err := loadPortal(slug, clientIP)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, ErrPortalUnauthorized
	}
	var session model.ManagedAccountAPIPortalSession
	if err := model.DB.Where("api_id = ? AND token_hash = ?", entry.ID, hashToken(token)).First(&session).Error; err != nil {
		return nil, ErrPortalUnauthorized
	}
	now := common.GetTimestamp()
	if session.RevokedAt > 0 || session.ExpiresAt <= now {
		return nil, ErrPortalUnauthorized
	}
	if requireCSRF && !constantHashEqual(session.CSRFHash, hashToken(strings.TrimSpace(csrf))) {
		return nil, ErrPortalUnauthorized
	}
	_ = model.DB.Model(&model.ManagedAccountAPIPortalSession{}).Where("id = ?", session.ID).Update("last_used_at", now).Error
	return &PortalAuthenticated{API: entry, View: view, Session: &session, Token: token}, nil
}

func RefreshPortalCSRF(auth *PortalAuthenticated) (string, error) {
	if auth == nil || auth.Session == nil {
		return "", ErrPortalUnauthorized
	}
	csrf := portalCSRFToken(auth.Token)
	if csrf == "" {
		return "", ErrPortalUnauthorized
	}
	csrfHash := hashToken(csrf)
	if constantHashEqual(auth.Session.CSRFHash, csrfHash) {
		return csrf, nil
	}
	if err := model.DB.Model(&model.ManagedAccountAPIPortalSession{}).Where("id = ?", auth.Session.ID).Update("csrf_hash", csrfHash).Error; err != nil {
		return "", err
	}
	return csrf, nil
}

func PortalSessionView(auth *PortalAuthenticated, csrf string) *PortalView {
	if auth == nil || auth.View == nil || auth.Session == nil {
		return nil
	}
	return portalView(auth.View, auth.Session.ExpiresAt, csrf)
}

func LogoutPortal(auth *PortalAuthenticated) error {
	if auth == nil || auth.Session == nil {
		return nil
	}
	return model.DB.Delete(&model.ManagedAccountAPIPortalSession{}, auth.Session.ID).Error
}

func PortalAPIID(slug string) int64 {
	var entry model.ManagedAccountAPI
	if err := model.DB.Select("id").Where("portal_slug = ?", strings.TrimSpace(slug)).First(&entry).Error; err != nil {
		return 0
	}
	return entry.ID
}

func MarkPortalUsed(auth *PortalAuthenticated, result *managedaccount.Result) {
	if auth == nil || auth.API == nil || result == nil {
		return
	}
	now := common.GetTimestamp()
	if err := model.DB.Session(&gorm.Session{SkipHooks: true}).Model(&model.ManagedAccountAPI{}).Where("id = ?", auth.API.ID).UpdateColumns(map[string]any{
		"last_accessed_at": now, "request_count": gorm.Expr("request_count + 1"), "matched_count": result.Total, "last_observed_at": result.ObservedAt,
	}).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("account data portal usage write failed: api_id=%d error=%v", auth.API.ID, err))
	}
}

func QueryPortal(ctx context.Context, auth *PortalAuthenticated, input PortalQueryInput) (*managedaccount.Result, error) {
	if auth == nil || auth.View == nil {
		return nil, ErrPortalUnauthorized
	}
	filterFields := PortalFilterFields(auth.View.Fields)
	if err := validatePortalRules(input.Rules, filterFields); err != nil {
		return nil, err
	}
	if input.PageSize <= 0 {
		input.PageSize = min(auth.View.PageSize, 50)
	}
	if input.PageSize > auth.View.PageSize || input.PageSize > 100 {
		return nil, ErrInvalid
	}
	if input.SortBy != "" && !containsString(auth.View.Fields, input.SortBy) {
		return nil, ErrInvalid
	}
	query := portalManagedQuery(auth.View, input)
	result, err := managedaccount.Execute(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if result.NoData {
		return nil, ErrSnapshotEmpty
	}
	MarkPortalUsed(auth, result)
	return result, nil
}

func ExportPortal(ctx context.Context, auth *PortalAuthenticated, input PortalExportInput) (*PortalExport, error) {
	if input.Mode != "filtered" && input.Mode != "selected" {
		return nil, ErrInvalid
	}
	if input.Mode == "selected" && (len(input.Selections) == 0 || len(input.Selections) > PortalExportLimit) {
		return nil, ErrInvalid
	}
	if input.Mode != "filtered" && len(input.Exclusions) > 0 {
		return nil, ErrInvalid
	}
	exclusions, err := portalSelectionKeys(input.Exclusions)
	if err != nil || len(exclusions) > PortalExportLimit {
		return nil, ErrInvalid
	}
	query := input.Query
	query.Page, query.PageSize = 1, PortalExportLimit
	if input.Mode == "selected" {
		querySelections := make([]managedaccount.AccountIdentity, 0, len(input.Selections))
		for _, item := range input.Selections {
			querySelections = append(querySelections, managedaccount.AccountIdentity{InstanceID: item.InstanceID, AccountID: item.AccountID})
		}
		query.SelectedAccounts = querySelections
	}
	result, err := queryPortalLarge(ctx, auth, query)
	if err != nil {
		return nil, err
	}
	if input.Mode == "filtered" && result.Total > PortalExportLimit {
		return nil, ErrPortalExportLarge
	}
	items := result.Items
	if len(exclusions) > 0 {
		filtered := make([]managedaccount.Item, 0, len(items))
		for _, item := range items {
			if _, excluded := exclusions[portalSelectionKey(item.InstanceID, item.AccountID)]; !excluded {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if len(items) == 0 {
		return nil, ErrInvalid
	}
	data, err := writePortalWorkbook(auth.View.Fields, items)
	if err != nil {
		return nil, err
	}
	MarkPortalUsed(auth, result)
	return &PortalExport{FileName: "accounts-" + time.Now().Format("20060102-150405") + ".xlsx", Data: data, Count: len(items)}, nil
}

func portalSelectionKeys(items []PortalSelection) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.AccountID = strings.TrimSpace(item.AccountID)
		if item.InstanceID <= 0 || item.AccountID == "" {
			return nil, ErrInvalid
		}
		result[portalSelectionKey(item.InstanceID, item.AccountID)] = struct{}{}
	}
	return result, nil
}

func portalSelectionKey(instanceID int64, accountID string) string {
	return strconv.FormatInt(instanceID, 10) + "\x00" + strings.TrimSpace(accountID)
}

func queryPortalLarge(ctx context.Context, auth *PortalAuthenticated, input PortalQueryInput) (*managedaccount.Result, error) {
	if auth == nil || auth.View == nil {
		return nil, ErrPortalUnauthorized
	}
	filterFields := PortalFilterFields(auth.View.Fields)
	if err := validatePortalRules(input.Rules, filterFields); err != nil {
		return nil, err
	}
	if input.SortBy != "" && !containsString(auth.View.Fields, input.SortBy) {
		return nil, ErrInvalid
	}
	query := portalManagedQuery(auth.View, input)
	query.AllowLargePage = true
	result, err := managedaccount.Execute(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if result.NoData {
		return nil, ErrSnapshotEmpty
	}
	return result, nil
}

func portalManagedQuery(view *View, input PortalQueryInput) managedaccount.Query {
	query := managedaccount.Query{InstanceIDs: view.InstanceIDs, Dataset: view.Dataset, PresetDays: view.PresetDays,
		IncludeTerms: view.IncludeTerms, ExcludeTerms: view.ExcludeTerms, MatchMode: view.MatchMode, Rules: view.Rules,
		NarrowIncludeTerms: input.IncludeTerms, NarrowExcludeTerms: input.ExcludeTerms, NarrowMatchMode: input.MatchMode,
		NarrowRules: input.Rules, NarrowFields: PortalFilterFields(view.Fields), NarrowSearch: input.Search,
		SortBy: input.SortBy, SortOrder: input.SortOrder, Page: input.Page, PageSize: input.PageSize,
		SelectedAccounts: input.SelectedAccounts}
	if query.SortBy == "" {
		query.SortBy = view.SortBy
	}
	if query.SortOrder == "" {
		query.SortOrder = view.SortOrder
	}
	return query
}

func PortalFilterFields(fields []string) []string {
	allowed := map[string]bool{"account_id": true}
	for _, field := range fields {
		switch field {
		case "name", "email", "note", "ownership", "vendor_name", "vendor_email", "platform", "type", "group", "status", "available":
			allowed[field] = true
		case "instance_name":
			allowed["instance"] = true
		case "source_name":
			allowed["source"] = true
		case "requests", "tokens", "amount", "rpm", "active_sessions", "utilization_5h", "utilization_7d", "created_at", "last_activity_at":
			allowed[field] = true
		}
	}
	result := make([]string, 0, len(allowed))
	for _, field := range []string{"name", "email", "account_id", "note", "ownership", "vendor_name", "vendor_email", "instance", "platform", "type", "group", "status", "source", "available", "requests", "tokens", "amount", "rpm", "active_sessions", "utilization_5h", "utilization_7d", "created_at", "last_activity_at"} {
		if allowed[field] {
			result = append(result, field)
		}
	}
	return result
}

func validatePortalRules(rules []managedinstance.AccountFilterRule, fields []string) error {
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	for _, rule := range rules {
		if !allowed[rule.Field] {
			return ErrInvalid
		}
	}
	return nil
}

func loadPortal(slug, clientIP string) (*model.ManagedAccountAPI, *View, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 48 {
		return nil, nil, ErrPortalUnauthorized
	}
	var entry model.ManagedAccountAPI
	if err := model.DB.Where("portal_slug = ?", slug).First(&entry).Error; err != nil {
		return nil, nil, ErrPortalUnauthorized
	}
	if entry.Status != model.ManagedAccountAPIEnabled || !entry.PortalEnabled || entry.PortalPasswordHash == "" {
		return nil, nil, ErrPortalUnauthorized
	}
	view, err := viewFor(&entry)
	if err != nil || !ipAllowed(clientIP, view.AllowedCIDRs) {
		return nil, nil, ErrPortalUnauthorized
	}
	return &entry, view, nil
}

func portalView(view *View, expiresAt int64, csrf string) *PortalView {
	return &PortalView{Authenticated: true, Name: view.Name, Description: view.Description, Dataset: view.Dataset, PresetDays: view.PresetDays,
		Timezone: managedaccount.TimezoneShanghai, Fields: append([]string{"instance_id", "account_id"}, view.Fields...),
		FilterFields: PortalFilterFields(view.Fields), PageSize: view.PageSize, ExpiresAt: expiresAt, CSRFToken: csrf,
		LastObservedAt: view.LastObservedAt, Stale: view.Stale}
}

func randomPortalToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func portalCSRFToken(sessionToken string) string {
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return ""
	}
	return hashToken("portal-csrf\x00" + sessionToken)
}

var portalSessionCleanup struct {
	sync.Mutex
	last time.Time
}

func maybeCleanupPortalSessions(now int64) {
	portalSessionCleanup.Lock()
	defer portalSessionCleanup.Unlock()
	if time.Since(portalSessionCleanup.last) < time.Hour {
		return
	}
	if err := model.DB.Where("expires_at <= ? OR revoked_at > 0", now).Delete(&model.ManagedAccountAPIPortalSession{}).Error; err == nil {
		portalSessionCleanup.last = time.Now()
	} else {
		logger.LogWarn(context.Background(), "account data portal session cleanup failed: "+err.Error())
	}
}

type portalLoginAttempt struct {
	count     int
	until     time.Time
	updatedAt time.Time
}

var portalLoginAttempts = struct {
	sync.Mutex
	items     map[string]portalLoginAttempt
	lastPrune time.Time
}{items: map[string]portalLoginAttempt{}}

func portalLoginAttemptKey(slug, ip string) string { return hashToken(slug + "\x00" + ip) }

func portalLoginRedisKey(slug, ip string) string {
	return "accountDataPortalLogin:" + portalLoginAttemptKey(slug, ip)
}

func PortalRateLimitIdentity(apiID int64, ip string) string {
	return fmt.Sprintf("portal:%d:%s", apiID, strings.TrimSpace(ip))
}

func portalLoginBlocked(slug, ip string) (bool, int) {
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		key := portalLoginRedisKey(slug, ip)
		count, err := common.RDB.Get(ctx, key).Int()
		if err == nil {
			ttl, ttlErr := common.RDB.TTL(ctx, key).Result()
			if ttlErr == nil && count >= portalLoginMaxAttempts {
				return true, max(1, int(ttl.Seconds()))
			}
			return false, 0
		}
	}
	key := portalLoginAttemptKey(slug, ip)
	now := time.Now()
	portalLoginAttempts.Lock()
	defer portalLoginAttempts.Unlock()
	prunePortalLoginAttemptsLocked(now)
	attempt := portalLoginAttempts.items[key]
	if now.After(attempt.until) {
		delete(portalLoginAttempts.items, key)
		return false, 0
	}
	return attempt.count >= portalLoginMaxAttempts, max(1, int(time.Until(attempt.until).Seconds()))
}

func portalLoginFailed(slug, ip string) {
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		key := portalLoginRedisKey(slug, ip)
		count, err := common.RDB.Incr(ctx, key).Result()
		if err == nil {
			if count == 1 {
				_ = common.RDB.Expire(ctx, key, portalLoginWindow).Err()
			}
			return
		}
	}
	key := portalLoginAttemptKey(slug, ip)
	now := time.Now()
	portalLoginAttempts.Lock()
	defer portalLoginAttempts.Unlock()
	prunePortalLoginAttemptsLocked(now)
	attempt := portalLoginAttempts.items[key]
	if now.After(attempt.until) {
		attempt = portalLoginAttempt{until: now.Add(portalLoginWindow)}
	}
	attempt.count++
	attempt.updatedAt = now
	portalLoginAttempts.items[key] = attempt
}

func portalLoginSucceeded(slug, ip string) {
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = common.RDB.Del(ctx, portalLoginRedisKey(slug, ip)).Err()
		cancel()
	}
	portalLoginAttempts.Lock()
	delete(portalLoginAttempts.items, portalLoginAttemptKey(slug, ip))
	portalLoginAttempts.Unlock()
}

func prunePortalLoginAttemptsLocked(now time.Time) {
	if len(portalLoginAttempts.items) < portalLoginLocalLimit && now.Sub(portalLoginAttempts.lastPrune) < time.Minute {
		return
	}
	portalLoginAttempts.lastPrune = now
	for key, attempt := range portalLoginAttempts.items {
		if now.After(attempt.until) {
			delete(portalLoginAttempts.items, key)
		}
	}
	for len(portalLoginAttempts.items) >= portalLoginLocalLimit {
		oldestKey := ""
		var oldest time.Time
		for key, attempt := range portalLoginAttempts.items {
			if oldestKey == "" || attempt.updatedAt.Before(oldest) {
				oldestKey, oldest = key, attempt.updatedAt
			}
		}
		if oldestKey == "" {
			break
		}
		delete(portalLoginAttempts.items, oldestKey)
	}
}

func PortalFilterOptions(options map[string][]string, fields []string) map[string][]string {
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
	}
	result := make(map[string][]string)
	for field, values := range options {
		if _, ok := allowed[field]; ok {
			result[field] = append([]string(nil), values...)
		}
	}
	return result
}

var portalFieldLabels = map[string]string{
	"instance_id": "实例 ID", "account_id": "账号 ID", "instance_name": "实例", "platform": "平台", "name": "名称",
	"email": "邮箱", "note": "备注", "ownership": "账号归属", "type": "账号类型", "group": "分组", "status": "状态",
	"vendor_name": "供应商", "vendor_email": "供应商邮箱",
	"available": "可用状态", "rate_limited": "限流状态", "source_id": "节点 ID", "source_name": "工作节点",
	"created_at": "录入时间", "last_activity_at": "最后活动", "disabled_at": "停用时间", "expires_at": "过期时间",
	"requests": "请求数", "tokens": "Token", "amount": "消费金额", "currency": "币种", "rpm": "RPM",
	"active_sessions": "活跃会话", "utilization_5h": "5h 利用率", "utilization_7d": "7d 利用率",
	"collection_status": "统计状态", "error_code": "错误代码",
}

func writePortalWorkbook(fields []string, items []managedaccount.Item) ([]byte, error) {
	columns := append([]string{"instance_id", "account_id"}, fields...)
	workbook := excelize.NewFile()
	defer workbook.Close()
	const sheet = "账号数据"
	workbook.SetSheetName("Sheet1", sheet)
	dateFormat, amountFormat, numberFormat := "yyyy-mm-dd hh:mm:ss", "#,##0.00000000", "#,##0.00"
	dateStyle, _ := workbook.NewStyle(&excelize.Style{CustomNumFmt: &dateFormat})
	amountStyle, _ := workbook.NewStyle(&excelize.Style{CustomNumFmt: &amountFormat})
	numberStyle, _ := workbook.NewStyle(&excelize.Style{CustomNumFmt: &numberFormat})
	shanghai, _ := time.LoadLocation(managedaccount.TimezoneShanghai)
	for index, field := range columns {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		_ = workbook.SetCellValue(sheet, cell, portalFieldLabels[field])
	}
	for rowIndex, item := range items {
		for columnIndex, field := range columns {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			value, ok := managedaccount.FieldValue(item, field)
			if !ok {
				continue
			}
			value, ok = portalScalarValue(value)
			if !ok {
				continue
			}
			if field == "instance_id" || field == "account_id" {
				_ = workbook.SetCellStr(sheet, cell, fmt.Sprint(value))
			} else if isPortalTimeField(field) {
				if timestamp, ok := portalTimestamp(value); ok && timestamp > 0 {
					_ = workbook.SetCellValue(sheet, cell, time.Unix(timestamp, 0).In(shanghai))
					_ = workbook.SetCellStyle(sheet, cell, cell, dateStyle)
				}
			} else {
				_ = workbook.SetCellValue(sheet, cell, value)
				if field == "amount" {
					_ = workbook.SetCellStyle(sheet, cell, cell, amountStyle)
				} else if field == "requests" || field == "tokens" {
					_ = workbook.SetCellStyle(sheet, cell, cell, numberStyle)
				}
			}
		}
	}
	lastColumn, _ := excelize.ColumnNumberToName(len(columns))
	headerStyle, _ := workbook.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "#111827"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F3F4F6"}, Pattern: 1}, Border: []excelize.Border{{Type: "bottom", Color: "#D1D5DB", Style: 1}}, Alignment: &excelize.Alignment{Vertical: "center"}})
	_ = workbook.SetCellStyle(sheet, "A1", lastColumn+"1", headerStyle)
	for index, field := range columns {
		column, _ := excelize.ColumnNumberToName(index + 1)
		width := 18.0
		if field == "email" || field == "vendor_email" || field == "note" || field == "ownership" {
			width = 28
		}
		if field == "account_id" {
			width = 24
		}
		_ = workbook.SetColWidth(sheet, column, column, width)
	}
	_ = workbook.SetRowHeight(sheet, 1, 24)
	_ = workbook.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	_ = workbook.AutoFilter(sheet, "A1:"+lastColumn+"1", nil)
	var buffer bytes.Buffer
	if err := workbook.Write(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func isPortalTimeField(field string) bool {
	return field == "created_at" || field == "last_activity_at" || field == "disabled_at" || field == "expires_at"
}

func portalTimestamp(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return normalizePortalTimestamp(typed), true
	case int:
		return normalizePortalTimestamp(int64(typed)), true
	case float64:
		return normalizePortalTimestamp(int64(typed)), true
	case string:
		value := strings.TrimSpace(typed)
		if value == "" {
			return 0, false
		}
		if numeric, err := strconv.ParseFloat(value, 64); err == nil {
			return normalizePortalTimestamp(int64(numeric)), true
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return 0, false
		}
		return parsed.Unix(), true
	case time.Time:
		return typed.Unix(), true
	default:
		return 0, false
	}
}

func normalizePortalTimestamp(value int64) int64 {
	for value > 100_000_000_000 {
		value /= 1000
	}
	return value
}

func portalScalarValue(value any) (any, bool) {
	switch typed := value.(type) {
	case *float64:
		if typed == nil {
			return nil, false
		}
		return *typed, true
	case *int:
		if typed == nil {
			return nil, false
		}
		return *typed, true
	case *int64:
		if typed == nil {
			return nil, false
		}
		return *typed, true
	case *bool:
		if typed == nil {
			return nil, false
		}
		return *typed, true
	case *string:
		if typed == nil {
			return nil, false
		}
		return *typed, true
	default:
		return value, value != nil
	}
}

func RevokePortalSessions(apiID int64) error {
	if apiID <= 0 {
		return ErrInvalid
	}
	return model.DB.Where("api_id = ?", apiID).Delete(&model.ManagedAccountAPIPortalSession{}).Error
}
