package accountdataapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"gorm.io/gorm"
)

const (
	DefaultKeyLifetime = 90 * 24 * time.Hour
	MaxActiveKeys      = 2
	ExternalPath       = "/open-api/v1/accounts"
)

var (
	ErrInvalid       = errors.New("invalid account data api")
	ErrNotFound      = errors.New("account data api not found")
	ErrUnauthorized  = errors.New("invalid or expired account data api key")
	ErrDisabled      = errors.New("account data api is disabled")
	ErrIPDenied      = errors.New("client ip is not allowed")
	ErrTooManyKeys   = errors.New("at most two active keys are allowed")
	ErrRateLimited   = errors.New("account data api rate limited")
	ErrSnapshotEmpty = errors.New("account snapshot unavailable")
)

var defaultFields = []string{"instance_name", "platform", "name", "vendor_name", "type", "status", "available"}

var accessLogCleanup struct {
	sync.Mutex
	last    time.Time
	running bool
}

type ConfigInput struct {
	Name               string                              `json:"name"`
	Description        string                              `json:"description"`
	Status             string                              `json:"status"`
	Dataset            string                              `json:"dataset"`
	PresetDays         int                                 `json:"preset_days"`
	InstanceIDs        []int64                             `json:"instance_ids"`
	IncludeTerms       []string                            `json:"include_terms"`
	ExcludeTerms       []string                            `json:"exclude_terms"`
	MatchMode          string                              `json:"match_mode"`
	Rules              []managedinstance.AccountFilterRule `json:"rules"`
	Fields             []string                            `json:"fields"`
	SortBy             string                              `json:"sort_by"`
	SortOrder          string                              `json:"sort_order"`
	PageSize           int                                 `json:"page_size"`
	RateLimitPerMinute int                                 `json:"rate_limit_per_minute"`
	AllowedCIDRs       []string                            `json:"allowed_cidrs"`
	PortalEnabled      bool                                `json:"portal_enabled"`
	PortalPassword     string                              `json:"portal_password"`
	ResetPortalSlug    bool                                `json:"reset_portal_slug"`
}

type KeyView struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	ExpiresAt  int64  `json:"expires_at"`
	RevokedAt  int64  `json:"revoked_at"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

type View struct {
	ID                 int64                               `json:"id"`
	Name               string                              `json:"name"`
	Description        string                              `json:"description"`
	Status             string                              `json:"status"`
	Dataset            string                              `json:"dataset"`
	PresetDays         int                                 `json:"preset_days"`
	Timezone           string                              `json:"timezone"`
	InstanceIDs        []int64                             `json:"instance_ids"`
	IncludeTerms       []string                            `json:"include_terms"`
	ExcludeTerms       []string                            `json:"exclude_terms"`
	MatchMode          string                              `json:"match_mode"`
	Rules              []managedinstance.AccountFilterRule `json:"rules"`
	Fields             []string                            `json:"fields"`
	SortBy             string                              `json:"sort_by"`
	SortOrder          string                              `json:"sort_order"`
	PageSize           int                                 `json:"page_size"`
	RateLimitPerMinute int                                 `json:"rate_limit_per_minute"`
	AllowedCIDRs       []string                            `json:"allowed_cidrs"`
	PortalEnabled      bool                                `json:"portal_enabled"`
	PortalConfigured   bool                                `json:"portal_configured"`
	PortalURL          string                              `json:"portal_url"`
	PortalPasswordAt   int64                               `json:"portal_password_at"`
	MatchedCount       int                                 `json:"matched_count"`
	LastObservedAt     int64                               `json:"last_observed_at"`
	LastAccessedAt     int64                               `json:"last_accessed_at"`
	RequestCount       int64                               `json:"request_count"`
	CreatedBy          int                                 `json:"created_by"`
	UpdatedBy          int                                 `json:"updated_by"`
	CreatedAt          int64                               `json:"created_at"`
	UpdatedAt          int64                               `json:"updated_at"`
	Stale              bool                                `json:"stale"`
	Endpoint           string                              `json:"endpoint"`
	Keys               []KeyView                           `json:"keys"`
}

type CreateResult struct {
	API       *View  `json:"api"`
	Secret    string `json:"secret"`
	KeyPrefix string `json:"key_prefix"`
}

type PreviewResult struct {
	Total      int                           `json:"total"`
	Summary    managedaccount.Summary        `json:"summary"`
	Sample     []map[string]any              `json:"sample"`
	Sources    []managedaccount.SourceStatus `json:"sources"`
	ObservedAt int64                         `json:"observed_at"`
	Stale      bool                          `json:"stale"`
	Partial    bool                          `json:"partial"`
}

type ListResult struct {
	Items    []*View `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

type InstanceOption struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type Authenticated struct {
	API  *model.ManagedAccountAPI
	Key  *model.ManagedAccountAPIKey
	View *View
}

func Create(ctx context.Context, input ConfigInput, actorID int) (*CreateResult, error) {
	prepared, query, err := prepareInput(input)
	if err != nil {
		return nil, err
	}
	preview, err := managedaccount.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	if preview.NoData {
		return nil, ErrSnapshotEmpty
	}
	entry, err := modelFromInput(prepared, actorID)
	if err != nil {
		return nil, err
	}
	entry.MatchedCount = preview.Total
	entry.LastObservedAt = preview.ObservedAt
	if _, err := applyPortalInput(entry, prepared, true); err != nil {
		return nil, err
	}
	var secret string
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		if err := replaceInstances(tx, entry.ID, prepared.InstanceIDs); err != nil {
			return err
		}
		key, plaintext, keyErr := newKey(entry.ID, "Primary", actorID, time.Now().Add(DefaultKeyLifetime).Unix())
		if keyErr != nil {
			return keyErr
		}
		if err := tx.Create(key).Error; err != nil {
			return err
		}
		secret = plaintext
		return nil
	})
	if err != nil {
		return nil, err
	}
	view, err := Get(entry.ID)
	if err != nil {
		return nil, err
	}
	return &CreateResult{API: view, Secret: secret, KeyPrefix: view.Keys[0].Prefix}, nil
}

func Update(ctx context.Context, id int64, input ConfigInput, actorID int) (*View, error) {
	prepared, query, err := prepareInput(input)
	if err != nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalid
	}
	var current model.ManagedAccountAPI
	if err := model.DB.First(&current, id).Error; err != nil {
		return nil, mapNotFound(err)
	}
	matchedCount, observedAt := current.MatchedCount, current.LastObservedAt
	if prepared.Status == model.ManagedAccountAPIEnabled {
		preview, err := managedaccount.Execute(ctx, query)
		if err != nil {
			return nil, err
		}
		if preview.NoData {
			return nil, ErrSnapshotEmpty
		}
		matchedCount, observedAt = preview.Total, preview.ObservedAt
	}
	updates, err := modelFromInput(prepared, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var entry model.ManagedAccountAPI
		if err := tx.First(&entry, id).Error; err != nil {
			return mapNotFound(err)
		}
		revokePortalSessions, portalErr := applyPortalInput(&entry, prepared, false)
		if portalErr != nil {
			return portalErr
		}
		entry.Name, entry.Description, entry.Status = updates.Name, updates.Description, updates.Status
		entry.Dataset, entry.PresetDays, entry.Timezone = updates.Dataset, updates.PresetDays, updates.Timezone
		entry.IncludeTerms, entry.ExcludeTerms, entry.MatchMode, entry.Rules = updates.IncludeTerms, updates.ExcludeTerms, updates.MatchMode, updates.Rules
		entry.Fields, entry.SortBy, entry.SortOrder = updates.Fields, updates.SortBy, updates.SortOrder
		entry.PageSize, entry.RateLimitPerMinute, entry.AllowedCIDRs = updates.PageSize, updates.RateLimitPerMinute, updates.AllowedCIDRs
		entry.MatchedCount, entry.LastObservedAt, entry.UpdatedBy = matchedCount, observedAt, actorID
		if err := tx.Save(&entry).Error; err != nil {
			return err
		}
		if revokePortalSessions {
			if err := tx.Where("api_id = ?", id).Delete(&model.ManagedAccountAPIPortalSession{}).Error; err != nil {
				return err
			}
		}
		return replaceInstances(tx, id, prepared.InstanceIDs)
	})
	if err != nil {
		return nil, err
	}
	return Get(id)
}

func Preview(ctx context.Context, input ConfigInput) (*PreviewResult, error) {
	prepared, query, err := prepareInput(input)
	if err != nil {
		return nil, err
	}
	query.Page, query.PageSize = 1, 5
	result, err := managedaccount.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	fields := append([]string{"instance_id", "account_id"}, prepared.Fields...)
	sample := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		sample = append(sample, Project(item, fields))
	}
	return &PreviewResult{Total: result.Total, Summary: result.Summary, Sample: sample, Sources: result.Sources, ObservedAt: result.ObservedAt, Stale: result.Stale, Partial: result.Partial}, nil
}

func Get(id int64) (*View, error) {
	if id <= 0 {
		return nil, ErrInvalid
	}
	var entry model.ManagedAccountAPI
	if err := model.DB.First(&entry, id).Error; err != nil {
		return nil, mapNotFound(err)
	}
	return viewFor(&entry)
}

func List(search, status string, page, pageSize int) (*ListResult, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := model.DB.Model(&model.ManagedAccountAPI{})
	if search = strings.TrimSpace(search); search != "" {
		like := "%" + search + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var entries []*model.ManagedAccountAPI
	if err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&entries).Error; err != nil {
		return nil, err
	}
	items := make([]*View, 0, len(entries))
	for _, entry := range entries {
		view, err := viewFor(entry)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return &ListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func ListInstanceOptions() ([]InstanceOption, error) {
	var entries []model.ManagedInstance
	if err := model.DB.Select("id", "name", "kind", "status").Order("name, id").Find(&entries).Error; err != nil {
		return nil, err
	}
	result := make([]InstanceOption, 0, len(entries))
	for _, entry := range entries {
		result = append(result, InstanceOption{ID: entry.Id, Name: entry.Name, Kind: entry.Kind, Status: entry.Status})
	}
	return result, nil
}

func Delete(id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var entry model.ManagedAccountAPI
		if err := tx.First(&entry, id).Error; err != nil {
			return mapNotFound(err)
		}
		now := common.GetTimestamp()
		if err := tx.Model(&model.ManagedAccountAPIKey{}).Where("api_id = ? AND revoked_at = 0", id).Update("revoked_at", now).Error; err != nil {
			return err
		}
		if err := tx.Where("api_id = ?", id).Delete(&model.ManagedAccountAPIPortalSession{}).Error; err != nil {
			return err
		}
		return tx.Delete(&entry).Error
	})
}

func CreateKey(apiID int64, name string, expiresAt int64, actorID int) (*KeyView, string, error) {
	if apiID <= 0 {
		return nil, "", ErrInvalid
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Rotated key"
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, "", ErrInvalid
	}
	if expiresAt == 0 {
		expiresAt = time.Now().Add(DefaultKeyLifetime).Unix()
	}
	if expiresAt <= time.Now().Unix() || expiresAt > time.Now().Add(DefaultKeyLifetime).Unix() {
		return nil, "", ErrInvalid
	}
	var entry model.ManagedAccountAPI
	if err := model.DB.First(&entry, apiID).Error; err != nil {
		return nil, "", mapNotFound(err)
	}
	var active int64
	if err := model.DB.Model(&model.ManagedAccountAPIKey{}).Where("api_id = ? AND revoked_at = 0 AND expires_at > ?", apiID, common.GetTimestamp()).Count(&active).Error; err != nil {
		return nil, "", err
	}
	if active >= MaxActiveKeys {
		return nil, "", ErrTooManyKeys
	}
	key, secret, err := newKey(apiID, name, actorID, expiresAt)
	if err != nil {
		return nil, "", err
	}
	if err := model.DB.Create(key).Error; err != nil {
		return nil, "", err
	}
	view := keyView(key)
	return &view, secret, nil
}

func RevokeKey(apiID, keyID int64) error {
	if apiID <= 0 || keyID <= 0 {
		return ErrInvalid
	}
	result := model.DB.Model(&model.ManagedAccountAPIKey{}).Where("id = ? AND api_id = ? AND revoked_at = 0", keyID, apiID).Update("revoked_at", common.GetTimestamp())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func Authenticate(token, clientIP string) (*Authenticated, error) {
	maybeCleanupAccessLogs()
	prefix, ok := tokenPrefix(token)
	if !ok {
		return nil, ErrUnauthorized
	}
	var key model.ManagedAccountAPIKey
	if err := model.DB.Where("prefix = ?", prefix).First(&key).Error; err != nil {
		return nil, ErrUnauthorized
	}
	now := common.GetTimestamp()
	if key.RevokedAt > 0 || key.ExpiresAt <= now || !constantHashEqual(key.SecretHash, hashToken(token)) {
		return nil, ErrUnauthorized
	}
	var entry model.ManagedAccountAPI
	if err := model.DB.First(&entry, key.APIID).Error; err != nil {
		return nil, ErrUnauthorized
	}
	view, err := viewFor(&entry)
	if err != nil {
		return nil, err
	}
	auth := &Authenticated{API: &entry, Key: &key, View: view}
	if entry.Status != model.ManagedAccountAPIEnabled {
		return auth, ErrDisabled
	}
	if !ipAllowed(clientIP, view.AllowedCIDRs) {
		return auth, ErrIPDenied
	}
	return auth, nil
}

func QueryExternal(ctx context.Context, auth *Authenticated, page, pageSize int, search, sortBy, sortOrder string) (*managedaccount.Result, error) {
	if auth == nil || auth.View == nil {
		return nil, ErrUnauthorized
	}
	if pageSize <= 0 {
		pageSize = auth.View.PageSize
	}
	if pageSize > auth.View.PageSize {
		return nil, ErrInvalid
	}
	if sortBy != "" && !containsString(auth.View.Fields, sortBy) {
		return nil, ErrInvalid
	}
	query := managedaccount.Query{InstanceIDs: auth.View.InstanceIDs, Dataset: auth.View.Dataset, PresetDays: auth.View.PresetDays,
		IncludeTerms: auth.View.IncludeTerms, ExcludeTerms: auth.View.ExcludeTerms, MatchMode: auth.View.MatchMode, Rules: auth.View.Rules,
		Search: search, SortBy: sortBy, SortOrder: sortOrder, Page: page, PageSize: pageSize}
	if query.SortBy == "" {
		query.SortBy = auth.View.SortBy
	}
	if query.SortOrder == "" {
		query.SortOrder = auth.View.SortOrder
	}
	result, err := managedaccount.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	if result.NoData {
		return nil, ErrSnapshotEmpty
	}
	return result, nil
}

func Project(item managedaccount.Item, fields []string) map[string]any {
	result := map[string]any{"instance_id": item.InstanceID, "account_id": item.AccountID}
	for _, field := range fields {
		if field == "instance_id" || field == "account_id" {
			continue
		}
		if value, ok := managedaccount.FieldValue(item, field); ok {
			result[field] = value
		}
	}
	return result
}

func MarkUsed(auth *Authenticated, result *managedaccount.Result) {
	if auth == nil || auth.API == nil || auth.Key == nil || result == nil {
		return
	}
	now := common.GetTimestamp()
	if err := model.DB.Session(&gorm.Session{SkipHooks: true}).Model(&model.ManagedAccountAPI{}).Where("id = ?", auth.API.ID).UpdateColumns(map[string]any{
		"last_accessed_at": now, "request_count": gorm.Expr("request_count + 1"), "matched_count": result.Total, "last_observed_at": result.ObservedAt,
	}).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("account data authorization usage write failed: api_id=%d error=%v", auth.API.ID, err))
	}
	if err := model.DB.Session(&gorm.Session{SkipHooks: true}).Model(&model.ManagedAccountAPIKey{}).Where("id = ?", auth.Key.ID).UpdateColumn("last_used_at", now).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("account data API key usage write failed: key_id=%d error=%v", auth.Key.ID, err))
	}
}

func RecordAccess(entry *model.ManagedAccountAPIAccessLog) {
	if entry == nil || entry.APIID <= 0 {
		return
	}
	maybeCleanupAccessLogs()
	if err := model.DB.Create(entry).Error; err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("account data access log write failed: api_id=%d action=%s status=%d error=%v", entry.APIID, entry.Action, entry.StatusCode, err))
	}
}

func ListAccessLogs(apiID int64, page, pageSize int) ([]*model.ManagedAccountAPIAccessLog, int64, error) {
	if apiID <= 0 {
		return nil, 0, ErrInvalid
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := model.DB.Model(&model.ManagedAccountAPIAccessLog{}).Where("api_id = ?", apiID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.ManagedAccountAPIAccessLog
	err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func CleanupAccessLogs(now int64) error {
	return cleanupAccessLogs(model.DB, now)
}

func cleanupAccessLogs(db *gorm.DB, now int64) error {
	if db == nil {
		return errors.New("account data access log database is unavailable")
	}
	cutoff := now - int64((90*24*time.Hour)/time.Second)
	const batchSize = 1000
	for {
		var ids []int64
		if err := db.Model(&model.ManagedAccountAPIAccessLog{}).
			Where("created_at < ?", cutoff).Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.Where("id IN ?", ids).Delete(&model.ManagedAccountAPIAccessLog{}).Error; err != nil {
			return err
		}
		if len(ids) < batchSize {
			return nil
		}
	}
}

func maybeCleanupAccessLogs() {
	accessLogCleanup.Lock()
	if accessLogCleanup.running || time.Since(accessLogCleanup.last) < time.Hour {
		accessLogCleanup.Unlock()
		return
	}
	accessLogCleanup.running = true
	db := model.DB
	accessLogCleanup.Unlock()
	go func() {
		err := cleanupAccessLogs(db, common.GetTimestamp())
		accessLogCleanup.Lock()
		accessLogCleanup.running = false
		if err == nil {
			accessLogCleanup.last = time.Now()
		}
		accessLogCleanup.Unlock()
		if err != nil {
			logger.LogError(context.Background(), "account data access log cleanup failed: "+err.Error())
		}
	}()
}

func prepareInput(input ConfigInput) (ConfigInput, managedaccount.Query, error) {
	input.Name, input.Description = strings.TrimSpace(input.Name), strings.TrimSpace(input.Description)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 96 || utf8.RuneCountInString(input.Description) > 500 {
		return input, managedaccount.Query{}, ErrInvalid
	}
	if input.Status == "" {
		input.Status = model.ManagedAccountAPIEnabled
	}
	if input.Status != model.ManagedAccountAPIEnabled && input.Status != model.ManagedAccountAPIDisabled {
		return input, managedaccount.Query{}, ErrInvalid
	}
	if input.Dataset == "" {
		input.Dataset = managedaccount.DatasetInventory
	}
	if input.PresetDays == 0 {
		input.PresetDays = 7
	}
	if input.PageSize == 0 {
		input.PageSize = 50
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		return input, managedaccount.Query{}, ErrInvalid
	}
	if input.RateLimitPerMinute == 0 {
		input.RateLimitPerMinute = 60
	}
	if input.RateLimitPerMinute < 1 || input.RateLimitPerMinute > 6000 {
		return input, managedaccount.Query{}, ErrInvalid
	}
	if len(input.Fields) == 0 {
		input.Fields = append([]string(nil), defaultFields...)
	}
	fields, err := managedaccount.ValidateFields(input.Fields)
	if err != nil {
		return input, managedaccount.Query{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	input.Fields = fields
	cidrs, err := normalizeCIDRs(input.AllowedCIDRs)
	if err != nil {
		return input, managedaccount.Query{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	input.AllowedCIDRs = cidrs
	query, err := managedaccount.NormalizeQuery(managedaccount.Query{InstanceIDs: input.InstanceIDs, Dataset: input.Dataset,
		PresetDays: input.PresetDays, IncludeTerms: input.IncludeTerms, ExcludeTerms: input.ExcludeTerms,
		MatchMode: input.MatchMode, Rules: input.Rules, SortBy: input.SortBy, SortOrder: input.SortOrder, Page: 1, PageSize: 5})
	if err != nil {
		return input, managedaccount.Query{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	input.InstanceIDs, input.Dataset, input.PresetDays = query.InstanceIDs, query.Dataset, query.PresetDays
	input.IncludeTerms, input.ExcludeTerms, input.MatchMode, input.Rules = query.IncludeTerms, query.ExcludeTerms, query.MatchMode, query.Rules
	input.SortBy, input.SortOrder = query.SortBy, query.SortOrder
	input = normalizeConfigCollections(input)
	var instanceCount int64
	if err := model.DB.Model(&model.ManagedInstance{}).Where("id IN ?", input.InstanceIDs).Count(&instanceCount).Error; err != nil {
		return input, managedaccount.Query{}, err
	}
	if instanceCount != int64(len(input.InstanceIDs)) {
		return input, managedaccount.Query{}, fmt.Errorf("%w: one or more managed instances do not exist", ErrInvalid)
	}
	return input, query, nil
}

func modelFromInput(input ConfigInput, actorID int) (*model.ManagedAccountAPI, error) {
	input = normalizeConfigCollections(input)
	include, _ := json.Marshal(input.IncludeTerms)
	exclude, _ := json.Marshal(input.ExcludeTerms)
	rules, _ := json.Marshal(input.Rules)
	fields, _ := json.Marshal(input.Fields)
	cidrs, _ := json.Marshal(input.AllowedCIDRs)
	return &model.ManagedAccountAPI{Name: input.Name, Description: input.Description, Status: input.Status,
		Dataset: input.Dataset, PresetDays: input.PresetDays, Timezone: managedaccount.TimezoneShanghai,
		IncludeTerms: string(include), ExcludeTerms: string(exclude), MatchMode: input.MatchMode, Rules: string(rules), Fields: string(fields),
		SortBy: input.SortBy, SortOrder: input.SortOrder, PageSize: input.PageSize, RateLimitPerMinute: input.RateLimitPerMinute,
		AllowedCIDRs: string(cidrs), CreatedBy: actorID, UpdatedBy: actorID}, nil
}

func viewFor(entry *model.ManagedAccountAPI) (*View, error) {
	var instances []model.ManagedAccountAPIInstance
	if err := model.DB.Where("api_id = ?", entry.ID).Order("instance_id").Find(&instances).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(instances))
	for _, instance := range instances {
		ids = append(ids, instance.InstanceID)
	}
	var include, exclude, fields, cidrs []string
	var rules []managedinstance.AccountFilterRule
	if err := json.Unmarshal([]byte(entry.IncludeTerms), &include); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(entry.ExcludeTerms), &exclude); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(entry.Fields), &fields); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(entry.AllowedCIDRs), &cidrs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(entry.Rules), &rules); err != nil {
		return nil, err
	}
	include = nonNilSlice(include)
	exclude = nonNilSlice(exclude)
	fields = nonNilSlice(fields)
	cidrs = nonNilSlice(cidrs)
	rules = normalizeFilterRuleCollections(rules)
	var keys []model.ManagedAccountAPIKey
	if err := model.DB.Where("api_id = ?", entry.ID).Order("created_at DESC, id DESC").Find(&keys).Error; err != nil {
		return nil, err
	}
	keyViews := make([]KeyView, 0, len(keys))
	for index := range keys {
		keyViews = append(keyViews, keyView(&keys[index]))
	}
	return &View{ID: entry.ID, Name: entry.Name, Description: entry.Description, Status: entry.Status, Dataset: entry.Dataset,
		PresetDays: entry.PresetDays, Timezone: entry.Timezone, InstanceIDs: ids, IncludeTerms: include, ExcludeTerms: exclude,
		MatchMode: entry.MatchMode, Rules: rules, Fields: fields, SortBy: entry.SortBy, SortOrder: entry.SortOrder,
		PageSize: entry.PageSize, RateLimitPerMinute: entry.RateLimitPerMinute, AllowedCIDRs: cidrs,
		PortalEnabled: entry.PortalEnabled, PortalConfigured: entry.PortalPasswordHash != "", PortalURL: portalURL(entry), PortalPasswordAt: entry.PortalPasswordAt,
		MatchedCount: entry.MatchedCount, LastObservedAt: entry.LastObservedAt, LastAccessedAt: entry.LastAccessedAt,
		RequestCount: entry.RequestCount, CreatedBy: entry.CreatedBy, UpdatedBy: entry.UpdatedBy,
		CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
		Stale:    entry.LastObservedAt == 0 || common.GetTimestamp()-entry.LastObservedAt > int64((20*time.Minute)/time.Second),
		Endpoint: ExternalPath, Keys: keyViews}, nil
}

func normalizeConfigCollections(input ConfigInput) ConfigInput {
	input.InstanceIDs = nonNilSlice(input.InstanceIDs)
	input.IncludeTerms = nonNilSlice(input.IncludeTerms)
	input.ExcludeTerms = nonNilSlice(input.ExcludeTerms)
	input.Rules = normalizeFilterRuleCollections(input.Rules)
	input.Fields = nonNilSlice(input.Fields)
	input.AllowedCIDRs = nonNilSlice(input.AllowedCIDRs)
	return input
}

func normalizeFilterRuleCollections(rules []managedinstance.AccountFilterRule) []managedinstance.AccountFilterRule {
	rules = nonNilSlice(rules)
	for index := range rules {
		rules[index].Values = nonNilSlice(rules[index].Values)
	}
	return rules
}

func nonNilSlice[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func applyPortalInput(entry *model.ManagedAccountAPI, input ConfigInput, creating bool) (bool, error) {
	if entry == nil {
		return false, ErrInvalid
	}
	revoke := !creating && entry.PortalEnabled != input.PortalEnabled
	password := strings.TrimSpace(input.PortalPassword)
	if password != "" {
		if utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128 {
			return false, fmt.Errorf("%w: portal password must contain 8 to 128 characters", ErrInvalid)
		}
		hash, err := common.Password2Hash(password)
		if err != nil {
			return false, err
		}
		entry.PortalPasswordHash = hash
		entry.PortalPasswordAt = common.GetTimestamp()
		revoke = !creating
	}
	if input.ResetPortalSlug || (input.PortalEnabled && entry.PortalSlug == nil) {
		slug, err := randomPortalSlug()
		if err != nil {
			return false, err
		}
		entry.PortalSlug = &slug
		revoke = !creating
	}
	if input.PortalEnabled && entry.PortalPasswordHash == "" {
		return false, fmt.Errorf("%w: portal password is required", ErrInvalid)
	}
	entry.PortalEnabled = input.PortalEnabled
	return revoke, nil
}

func randomPortalSlug() (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func portalURL(entry *model.ManagedAccountAPI) string {
	if entry == nil || entry.PortalSlug == nil || strings.TrimSpace(*entry.PortalSlug) == "" {
		return ""
	}
	return "/account-data/" + strings.TrimSpace(*entry.PortalSlug)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func replaceInstances(tx *gorm.DB, apiID int64, ids []int64) error {
	if err := tx.Where("api_id = ?", apiID).Delete(&model.ManagedAccountAPIInstance{}).Error; err != nil {
		return err
	}
	rows := make([]model.ManagedAccountAPIInstance, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, model.ManagedAccountAPIInstance{APIID: apiID, InstanceID: id})
	}
	return tx.Create(&rows).Error
}

func newKey(apiID int64, name string, actorID int, expiresAt int64) (*model.ManagedAccountAPIKey, string, error) {
	prefixBytes, secretBytes := make([]byte, 8), make([]byte, 32)
	if _, err := rand.Read(prefixBytes); err != nil {
		return nil, "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", err
	}
	prefix := hex.EncodeToString(prefixBytes)
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	plaintext := "acct_live_" + prefix + "." + secret
	return &model.ManagedAccountAPIKey{APIID: apiID, Name: name, Prefix: prefix, SecretHash: hashToken(plaintext), ExpiresAt: expiresAt, CreatedBy: actorID}, plaintext, nil
}

func keyView(key *model.ManagedAccountAPIKey) KeyView {
	return KeyView{ID: key.ID, Name: key.Name, Prefix: key.Prefix, ExpiresAt: key.ExpiresAt, RevokedAt: key.RevokedAt, LastUsedAt: key.LastUsedAt, CreatedAt: key.CreatedAt}
}

func tokenPrefix(token string) (string, bool) {
	if !strings.HasPrefix(token, "acct_live_") {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(token, "acct_live_"), ".", 2)
	return parts[0], len(parts) == 2 && len(parts[0]) == 16 && len(parts[1]) >= 40
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func constantHashEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func normalizeCIDRs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if ip := net.ParseIP(value); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				value += "/" + strconv.Itoa(bits)
			}
			_, network, err := net.ParseCIDR(value)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed CIDR %q", value)
			}
			normalized := network.String()
			if _, duplicate := seen[normalized]; duplicate {
				continue
			}
			seen[normalized] = struct{}{}
			result = append(result, normalized)
		}
	}
	return result, nil
}

func ipAllowed(raw string, cidrs []string) bool {
	if len(cidrs) == 0 {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
