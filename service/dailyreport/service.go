package dailyreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidRange = errors.New("invalid daily report range")
	ErrUnsupported  = errors.New("daily report source is not supported")
)

type Metrics struct {
	Requests     float64 `json:"requests"`
	InputTokens  float64 `json:"input_tokens"`
	OutputTokens float64 `json:"output_tokens"`
	CacheTokens  float64 `json:"cache_tokens"`
	TotalTokens  float64 `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency"`
}

type Row struct {
	Snapshot model.DailyReportSnapshot `json:"snapshot"`
	Full     Metrics                   `json:"full"`
	Filtered Metrics                   `json:"filtered"`
}

type Overview struct {
	Date        string `json:"date"`
	Timezone    string `json:"timezone"`
	Items       []Row  `json:"items"`
	CollectedAt int64  `json:"collected_at"`
}

type Window struct {
	Start, End int64
	Date, Mode string
}

func BeijingLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func NaturalDay(date string) (Window, error) {
	loc := BeijingLocation()
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return Window{}, ErrInvalidRange
	}
	return Window{Start: parsed.Unix(), End: parsed.Add(24*time.Hour).Unix() - 1, Date: date, Mode: model.DailyReportModeNaturalDay}, nil
}

func Rolling(end time.Time, duration time.Duration) Window {
	loc := BeijingLocation()
	end = end.In(loc)
	return Window{Start: end.Add(-duration).Unix(), End: end.Unix(), Date: end.Format("2006-01-02"), Mode: model.DailyReportModeRolling}
}

func ListRules(instanceID int64) ([]map[string]any, error) {
	var rules []model.DailyReportRule
	query := model.DB.Order("id desc")
	if instanceID > 0 {
		query = query.Where("instance_id = ?", instanceID)
	}
	if err := query.Find(&rules).Error; err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		var filter any
		_ = json.Unmarshal([]byte(rule.FilterJSON), &filter)
		items = append(items, map[string]any{"id": rule.ID, "instance_id": rule.InstanceID, "supplier_code": rule.SupplierCode, "supplier_name": rule.SupplierName, "filter": filter, "source_template_id": rule.SourceTemplateID, "source_template_name": rule.SourceTemplateName, "enabled": rule.Enabled, "version": rule.Version, "created_at": rule.CreatedAt, "updated_at": rule.UpdatedAt})
	}
	return items, nil
}

func ListRulesForInstances(instanceIDs []int64) ([]map[string]any, error) {
	if len(instanceIDs) == 0 {
		return []map[string]any{}, nil
	}
	var rules []model.DailyReportRule
	if err := model.DB.Where("instance_id IN ?", instanceIDs).Order("id desc").Find(&rules).Error; err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		var filter any
		_ = json.Unmarshal([]byte(rule.FilterJSON), &filter)
		items = append(items, map[string]any{"id": rule.ID, "instance_id": rule.InstanceID, "supplier_code": rule.SupplierCode, "supplier_name": rule.SupplierName, "filter": filter, "source_template_id": rule.SourceTemplateID, "source_template_name": rule.SourceTemplateName, "enabled": rule.Enabled, "version": rule.Version, "created_at": rule.CreatedAt, "updated_at": rule.UpdatedAt})
	}
	return items, nil
}

func SaveRule(input model.DailyReportRule) (*model.DailyReportRule, error) {
	if input.InstanceID <= 0 || strings.TrimSpace(input.SupplierCode) == "" {
		return nil, ErrInvalidRange
	}
	if input.FilterJSON == "" {
		input.FilterJSON = `{}`
	}
	input.SupplierCode = strings.TrimSpace(input.SupplierCode)
	input.SupplierName = strings.TrimSpace(input.SupplierName)
	now := common.GetTimestamp()
	var result model.DailyReportRule
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.DailyReportRule
		err := tx.Where("instance_id = ? AND supplier_code = ?", input.InstanceID, input.SupplierCode).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			input.CreatedAt, input.UpdatedAt = now, now
			input.Version = 1
			if err := tx.Create(&input).Error; err != nil {
				return err
			}
			result = input
			return nil
		}
		if err != nil {
			return err
		}
		if input.ID > 0 && input.Version > 0 && existing.Version != input.Version {
			return gorm.ErrInvalidData
		}
		updates := map[string]any{"supplier_name": input.SupplierName, "filter_json": input.FilterJSON, "source_template_id": input.SourceTemplateID, "source_template_name": input.SourceTemplateName, "enabled": input.Enabled, "updated_by": input.UpdatedBy, "updated_at": now, "version": gorm.Expr("version + 1")}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&result, existing.ID).Error
	})
	return &result, err
}

func DeleteRule(id int64) error { return model.DB.Delete(&model.DailyReportRule{}, id).Error }

func collectManaged(ctx context.Context, instanceID int64, window Window, rule *model.DailyReportRule) (Metrics, Metrics, int64, int64, bool, string, error) {
	summary, err := managedinstance.CollectSummaryData(ctx, instanceID, managedinstance.TimeWindow{Start: window.Start, End: window.End, Timezone: "Asia/Shanghai"})
	if err != nil {
		return Metrics{}, Metrics{}, 0, 0, true, "usage_summary_failed", err
	}
	full := Metrics{}
	if summary.Requests.Value != nil {
		full.Requests = *summary.Requests.Value
	}
	if summary.Cost.Value != nil {
		full.Cost = *summary.Cost.Value
		full.Currency = summary.Cost.Unit
		if full.Currency == "" {
			full.Currency = "USD"
		}
	}
	if summary.Tokens.Value != nil {
		full.TotalTokens = *summary.Tokens.Value
	}
	filtered := full
	var accountCount int64
	query := managedaccount.Query{InstanceIDs: []int64{instanceID}, Dataset: managedaccount.DatasetOutput, PresetDays: 1, Page: 1, PageSize: 10000, AllowLargePage: true}
	claudeGateway := false
	if instance, getErr := managedinstance.Get(instanceID); getErr == nil && instance.Kind == model.ManagedInstanceKindClaudeGateway {
		claudeGateway = true
		query.Dataset = managedaccount.DatasetInventory
		query.Range = "all"
		query.PresetDays = 0
	}
	if rule != nil && strings.TrimSpace(rule.FilterJSON) != "" && rule.FilterJSON != "{}" {
		var raw struct {
			MatchMode    string                              `json:"match_mode"`
			Rules        []managedinstance.AccountFilterRule `json:"rules"`
			IncludeTerms []string                            `json:"include_terms"`
			ExcludeTerms []string                            `json:"exclude_terms"`
		}
		if err := json.Unmarshal([]byte(rule.FilterJSON), &raw); err == nil {
			query.MatchMode, query.Rules = raw.MatchMode, raw.Rules
			query.IncludeTerms, query.ExcludeTerms = raw.IncludeTerms, raw.ExcludeTerms
		}
	}
	if result, qerr := managedaccount.Execute(ctx, query); qerr == nil {
		accountCount = int64(result.Total)
		if rule != nil && strings.TrimSpace(rule.FilterJSON) != "" && rule.FilterJSON != "{}" {
			filtered = Metrics{Currency: full.Currency}
			for _, item := range result.Items {
				requests, tokens, amount := item.Requests, item.Tokens, item.Amount
				if claudeGateway {
					requests, tokens, amount = item.TodayRequests, item.TodayTokens, item.TodayCost
				}
				if requests != nil {
					filtered.Requests += *requests
				}
				if tokens != nil {
					filtered.TotalTokens += *tokens
				}
				if amount != nil {
					filtered.Cost += *amount
				}
			}
		}
	}
	return full, filtered, accountCount, accountCount, false, "", nil
}

func Collect(ctx context.Context, instanceID int64, date string, source string) (Row, error) {
	return CollectWithRule(ctx, instanceID, date, source, nil)
}

func CollectWithRule(ctx context.Context, instanceID int64, date string, source string, rule *model.DailyReportRule) (Row, error) {
	window, err := NaturalDay(date)
	if err != nil {
		return Row{}, err
	}
	if source == "" {
		source = model.DailyReportSourceManagedAccounts
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		return Row{}, err
	}
	row := Row{Snapshot: model.DailyReportSnapshot{InstanceID: instanceID, SnapshotDate: date, WindowStart: window.Start, WindowEnd: window.End, Mode: window.Mode, Source: source, Status: "succeeded"}}
	if rule != nil {
		row.Snapshot.SupplierCode, row.Snapshot.SupplierName = rule.SupplierCode, rule.SupplierName
	}
	if source == model.DailyReportSourceManagedAccounts {
		full, filtered, accounts, active, stale, code, err := collectManaged(ctx, instanceID, window, rule)
		if err != nil {
			row.Snapshot.Status = "failed"
			row.Snapshot.ErrorCode = code
			row.Snapshot.Stale = stale
			return row, err
		}
		row.Full, row.Filtered = full, filtered
		row.Snapshot.AccountCount, row.Snapshot.ActiveCount = accounts, active
		row.Snapshot.Stale = stale
	} else if source == model.DailyReportSourceNevermore || source == model.DailyReportSourceRouter {
		full, err := collectExternal(ctx, &instance, window, source)
		if err != nil {
			row.Snapshot.Status = "failed"
			row.Snapshot.ErrorCode = "upstream_failed"
			return row, err
		}
		row.Full = full
	} else {
		return Row{}, ErrUnsupported
	}
	row.Snapshot.ObservedAt = common.GetTimestamp()
	row.Snapshot.CreatedAt = row.Snapshot.ObservedAt
	row.Snapshot.UpdatedAt = row.Snapshot.ObservedAt
	fullJSON, _ := json.Marshal(row.Full)
	filteredJSON, _ := json.Marshal(row.Filtered)
	row.Snapshot.FullMetricsJSON, row.Snapshot.FilterMetricsJSON = string(fullJSON), string(filteredJSON)
	return row, model.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instance_id"}, {Name: "snapshot_date"}, {Name: "source"}, {Name: "supplier_code"}}, DoUpdates: clause.AssignmentColumns([]string{"window_start", "window_end", "mode", "full_metrics_json", "filter_metrics_json", "account_count", "active_count", "channel_count", "upload_count", "status", "error_code", "error_message", "stale", "observed_at", "updated_at"})}).Create(&row.Snapshot).Error
}

func collectExternal(ctx context.Context, instance *model.ManagedInstance, window Window, source string) (Metrics, error) {
	credential, err := managedinstance.LoadCredentialForInstance(instance.Id)
	if err != nil {
		return Metrics{}, err
	}
	if credential == nil || credential.AuthType != "account_password" {
		return Metrics{}, errors.New("account password credential required")
	}
	_, connector, err := managedinstance.NewConnectorForInstance(instance.Id)
	if err != nil {
		return Metrics{}, err
	}
	if source == model.DailyReportSourceNevermore {
		login, err := connector.DoJSON(ctx, http.MethodPost, "/admin/v1/auth/login", nil, map[string]string{"username": credential.UserID, "password": credential.Secret})
		if err != nil || login.StatusCode >= 300 {
			return Metrics{}, fmt.Errorf("nevermore login failed")
		}
		resp, err := connector.DoJSON(ctx, http.MethodGet, "/supplier/v1/deliveries?limit=100&offset=0", nil, nil)
		if err != nil || resp.StatusCode >= 300 {
			return Metrics{}, fmt.Errorf("nevermore deliveries failed")
		}
		var payload struct {
			Items []struct {
				CreatedAt string `json:"created_at"`
			} `json:"items"`
		}
		if json.Unmarshal(resp.Body, &payload) != nil {
			return Metrics{}, errors.New("invalid nevermore response")
		}
		count := 0
		loc := BeijingLocation()
		for _, item := range payload.Items {
			t, _ := time.Parse(time.RFC3339Nano, item.CreatedAt)
			if t.In(loc).Unix() >= window.Start && t.In(loc).Unix() <= window.End {
				count++
			}
		}
		return Metrics{Requests: float64(count)}, nil
	}
	login, err := connector.DoJSON(ctx, http.MethodPost, "/api/user/login", nil, map[string]string{"username": credential.UserID, "password": credential.Secret})
	if err != nil || login.StatusCode >= 300 {
		return Metrics{}, errors.New("router login failed")
	}
	path := "/api/maas/supplier-bills/?start_date=" + url.QueryEscape(time.Unix(window.Start, 0).In(BeijingLocation()).Format("2006-01-02")) + "&end_date=" + url.QueryEscape(time.Unix(window.End, 0).In(BeijingLocation()).Format("2006-01-02"))
	resp, err := connector.DoJSON(ctx, http.MethodGet, path, nil, nil)
	if err != nil || resp.StatusCode >= 300 {
		return Metrics{}, errors.New("router bills failed")
	}
	var payload struct {
		Data []struct {
			RequestCount           float64 `json:"request_count"`
			PromptTokens           float64 `json:"prompt_tokens"`
			CompletionTokens       float64 `json:"completion_tokens"`
			CacheTokens            float64 `json:"cache_tokens"`
			ActualSettlementAmount float64 `json:"actual_settlement_amount"`
			SettlementAmount       float64 `json:"settlement_amount"`
			RawAmount              float64 `json:"raw_amount"`
		} `json:"data"`
		Items []struct {
			RequestCount           float64 `json:"request_count"`
			PromptTokens           float64 `json:"prompt_tokens"`
			CompletionTokens       float64 `json:"completion_tokens"`
			CacheTokens            float64 `json:"cache_tokens"`
			ActualSettlementAmount float64 `json:"actual_settlement_amount"`
			SettlementAmount       float64 `json:"settlement_amount"`
			RawAmount              float64 `json:"raw_amount"`
		} `json:"items"`
	}
	if json.Unmarshal(resp.Body, &payload) != nil {
		return Metrics{}, errors.New("invalid router response")
	}
	rows := payload.Data
	if len(rows) == 0 {
		rows = payload.Items
	}
	result := Metrics{Currency: "USD"}
	for _, item := range rows {
		result.Requests += item.RequestCount
		result.InputTokens += item.PromptTokens
		result.OutputTokens += item.CompletionTokens
		result.CacheTokens += item.CacheTokens
		result.TotalTokens += item.PromptTokens + item.CompletionTokens + item.CacheTokens
		if item.ActualSettlementAmount != 0 {
			result.Cost += item.ActualSettlementAmount
		} else if item.SettlementAmount != 0 {
			result.Cost += item.SettlementAmount
		} else {
			result.Cost += item.RawAmount
		}
	}
	return result, nil
}
