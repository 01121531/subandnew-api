package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

const (
	managedAccountDatasetInventory = "inventory"
	managedAccountDatasetOutput    = "account_output"
)

type managedAccountsInput struct {
	InstanceIDs   []int64                             `json:"instance_ids,omitempty"`
	InstanceScope string                              `json:"instance_scope,omitempty"`
	Dataset       string                              `json:"dataset,omitempty"`
	PresetDays    int                                 `json:"preset_days,omitempty"`
	MatchMode     string                              `json:"match_mode,omitempty"`
	Rules         []managedinstance.AccountFilterRule `json:"rules,omitempty"`
	SortBy        string                              `json:"sort_by,omitempty"`
	SortOrder     string                              `json:"sort_order,omitempty"`
	Page          int                                 `json:"page,omitempty"`
	PageSize      int                                 `json:"page_size,omitempty"`
}

func (input managedAccountsInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	if input.Dataset != "" && input.Dataset != managedAccountDatasetInventory && input.Dataset != managedAccountDatasetOutput {
		return errors.New("dataset must be inventory or account_output")
	}
	if input.PresetDays != 0 && input.PresetDays != 1 && input.PresetDays != 7 && input.PresetDays != 14 && input.PresetDays != 30 {
		return errors.New("preset_days must be one of 1, 7, 14, or 30")
	}
	if input.MatchMode != "" && input.MatchMode != managedinstance.AccountFilterMatchAll && input.MatchMode != managedinstance.AccountFilterMatchAny {
		return errors.New("match_mode must be all or any")
	}
	if len(input.Rules) > 20 || input.Page < 0 || input.PageSize < 0 || input.PageSize > 100 {
		return errors.New("invalid account query limits")
	}
	for _, rule := range input.Rules {
		if err := validateManagedAccountRule(rule); err != nil {
			return err
		}
	}
	validSort := map[string]bool{"": true, "name": true, "vendor_name": true, "created_at": true, "last_activity_at": true, "status": true, "requests": true, "tokens": true, "amount": true}
	if !validSort[input.SortBy] || (input.SortOrder != "" && input.SortOrder != "asc" && input.SortOrder != "desc") {
		return errors.New("invalid account sort")
	}
	return nil
}

type managedAccountItem struct {
	InstanceID       int64    `json:"instance_id"`
	InstanceName     string   `json:"instance_name"`
	Platform         string   `json:"platform"`
	AccountID        string   `json:"account_id"`
	Name             string   `json:"name"`
	Email            string   `json:"email,omitempty"`
	Note             string   `json:"note,omitempty"`
	Ownership        string   `json:"ownership,omitempty"`
	VendorName       string   `json:"vendor_name,omitempty"`
	VendorEmail      string   `json:"vendor_email,omitempty"`
	Type             string   `json:"type,omitempty"`
	Group            string   `json:"group,omitempty"`
	Status           string   `json:"status,omitempty"`
	Available        *bool    `json:"available,omitempty"`
	RateLimited      bool     `json:"rate_limited,omitempty"`
	SourceID         string   `json:"source_id,omitempty"`
	SourceName       string   `json:"source_name,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	LastActivityAt   string   `json:"last_activity_at,omitempty"`
	DisabledAt       string   `json:"disabled_at,omitempty"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
	Requests         *float64 `json:"requests,omitempty"`
	Tokens           *float64 `json:"tokens,omitempty"`
	Amount           *float64 `json:"amount,omitempty"`
	Currency         string   `json:"currency,omitempty"`
	RPM              *int     `json:"rpm,omitempty"`
	ActiveSessions   *int     `json:"active_sessions,omitempty"`
	Utilization5H    *float64 `json:"utilization_5h,omitempty"`
	Utilization7D    *float64 `json:"utilization_7d,omitempty"`
	CollectionStatus string   `json:"collection_status,omitempty"`
	ErrorCode        string   `json:"error_code,omitempty"`
}

type managedAccountSourceStatus struct {
	InstanceID        int64  `json:"instance_id"`
	InstanceName      string `json:"instance_name"`
	Platform          string `json:"platform"`
	Status            string `json:"status"`
	ObservedAt        string `json:"observed_at,omitempty"`
	LastAttemptAt     string `json:"last_attempt_at,omitempty"`
	LastAttemptStatus string `json:"last_attempt_status,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	Stale             bool   `json:"stale"`
	VendorStatus      string `json:"vendor_status,omitempty"`
	VendorObservedAt  string `json:"vendor_observed_at,omitempty"`
	VendorStale       bool   `json:"vendor_stale,omitempty"`
	VendorErrorCode   string `json:"vendor_error_code,omitempty"`
}

type managedAccountSummary struct {
	Total       int                `json:"total"`
	Available   int                `json:"available"`
	Unavailable int                `json:"unavailable"`
	Unknown     int                `json:"unknown"`
	Requests    float64            `json:"requests"`
	Tokens      float64            `json:"tokens"`
	Amounts     map[string]float64 `json:"amounts"`
}

type managedAccountsOutput struct {
	Dataset    string                       `json:"dataset"`
	PresetDays int                          `json:"preset_days,omitempty"`
	Items      []managedAccountItem         `json:"items"`
	Total      int                          `json:"total"`
	Page       int                          `json:"page"`
	PageSize   int                          `json:"page_size"`
	Summary    managedAccountSummary        `json:"summary"`
	Sources    []managedAccountSourceStatus `json:"sources"`
}

type managedAccountRow struct {
	item managedAccountItem
	doc  map[string][]string
}

func registerManagedAccountQuery(registry *tool.Registry, db *gorm.DB) error {
	schema := json.RawMessage(`{"type":"object","properties":{"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]},"dataset":{"type":"string","enum":["inventory","account_output"]},"preset_days":{"type":"integer","enum":[1,7,14,30]},"match_mode":{"type":"string","enum":["all","any"]},"rules":{"type":"array","maxItems":20,"items":{"type":"object","properties":{"field":{"type":"string","enum":["name","email","account_id","note","ownership","vendor_name","vendor_email","instance","platform","type","group","status","source","available"]},"operator":{"type":"string","enum":["contains","starts_with","ends_with","not_contains","is_empty","is_not_empty","is","is_not"]},"values":{"type":"array","maxItems":50,"items":{"type":"string","maxLength":200}},"value_mode":{"type":"string","enum":["any","all"]}},"required":["field","operator","value_mode"],"additionalProperties":false}},"sort_by":{"type":"string","enum":["name","vendor_name","created_at","last_activity_at","status","requests","tokens","amount"]},"sort_order":{"type":"string","enum":["asc","desc"]},"page":{"type":"integer","minimum":1},"page_size":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	return tool.Register(registry, tool.ToolSpec{
		Name: "query_managed_accounts", Version: "v1", Description: "从账号管理后台快照查询账号明细或新增账号产出，支持高级筛选、排序和分页；不会刷新目标实例。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:       tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: schema,
	}, func(ctx context.Context, execution tool.ExecutionContext, input managedAccountsInput) (tool.Output[managedAccountsOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[managedAccountsOutput]{}, err
		}
		return executeManagedAccountQuery(ctx, resolution.IDs, input)
	})
}

func executeManagedAccountQuery(ctx context.Context, instanceIDs []int64, input managedAccountsInput) (tool.Output[managedAccountsOutput], error) {
	result, err := managedaccount.Execute(ctx, managedaccount.Query{
		InstanceIDs: instanceIDs, Dataset: input.Dataset, PresetDays: input.PresetDays,
		MatchMode: input.MatchMode, Rules: input.Rules, SortBy: input.SortBy,
		SortOrder: input.SortOrder, Page: input.Page, PageSize: input.PageSize,
	})
	if err != nil {
		return tool.Output[managedAccountsOutput]{}, err
	}
	items := make([]managedAccountItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, managedAccountItem{
			InstanceID: item.InstanceID, InstanceName: item.InstanceName, Platform: item.Platform,
			AccountID: item.AccountID, Name: item.Name, Email: item.Email, Note: item.Note,
			Ownership: item.Ownership, VendorName: item.VendorName, VendorEmail: item.VendorEmail,
			Type: item.Type, Group: item.Group, Status: item.Status,
			Available: item.Available, RateLimited: item.RateLimited, SourceID: item.SourceID,
			SourceName: item.SourceName, CreatedAt: assistantTime(item.CreatedAt), LastActivityAt: assistantTime(item.LastActivityAt),
			DisabledAt: assistantTime(item.DisabledAt), ExpiresAt: assistantTime(item.ExpiresAt),
			Requests: item.Requests, Tokens: item.Tokens, Amount: item.Amount, Currency: item.Currency,
			RPM: item.RPM, ActiveSessions: item.ActiveSessions, Utilization5H: item.Utilization5H,
			Utilization7D: item.Utilization7D, CollectionStatus: item.CollectionStatus, ErrorCode: item.ErrorCode,
		})
	}
	statuses := make([]managedAccountSourceStatus, 0, len(result.Sources))
	provenance := make([]tool.Provenance, 0, len(result.Sources))
	for _, source := range result.Sources {
		statuses = append(statuses, managedAccountSourceStatus{
			InstanceID: source.InstanceID, InstanceName: source.InstanceName, Platform: source.Platform,
			Status: source.Status, ObservedAt: assistantTime(source.ObservedAt), LastAttemptAt: assistantTime(source.LastAttemptAt),
			LastAttemptStatus: source.LastAttemptStatus, ErrorCode: source.ErrorCode, Stale: source.Stale,
			VendorStatus: source.VendorCollectionStatus, VendorObservedAt: assistantTime(source.VendorObservedAt),
			VendorStale: source.VendorStale, VendorErrorCode: source.VendorErrorCode,
		})
		if source.ObservedAt > 0 {
			provenance = append(provenance, tool.Provenance{Source: "managed_account_snapshots", Resource: "instance:" + strconv.FormatInt(source.InstanceID, 10) + ":" + result.Dataset, ObservedAt: unixTime(source.ObservedAt)})
		}
	}
	if len(provenance) == 0 {
		provenance = []tool.Provenance{{Source: "managed_account_snapshots"}}
	}
	return tool.Output[managedAccountsOutput]{
		Data: managedAccountsOutput{Dataset: result.Dataset, PresetDays: result.PresetDays, Items: items, Total: result.Total,
			Page: result.Page, PageSize: result.PageSize, Summary: managedAccountSummary{Total: result.Summary.Total,
				Available: result.Summary.Available, Unavailable: result.Summary.Unavailable, Unknown: result.Summary.Unknown,
				Requests: result.Summary.Requests, Tokens: result.Summary.Tokens, Amounts: result.Summary.Amounts}, Sources: statuses},
		Provenance: provenance, Freshness: freshnessForSnapshot(result.ObservedAt, result.Stale),
	}, nil
}

func validateManagedAccountRule(rule managedinstance.AccountFilterRule) error {
	textFields := map[string]bool{"name": true, "email": true, "account_id": true, "note": true, "ownership": true, "vendor_name": true, "vendor_email": true}
	categoryFields := map[string]bool{"instance": true, "platform": true, "type": true, "group": true, "status": true, "source": true, "available": true}
	empty := rule.Operator == "is_empty" || rule.Operator == "is_not_empty"
	if textFields[rule.Field] {
		textOperator := rule.Operator == "contains" || rule.Operator == "starts_with" ||
			rule.Operator == "ends_with" || rule.Operator == "not_contains"
		if !textOperator && !empty {
			return errors.New("invalid text account filter operator")
		}
	} else if categoryFields[rule.Field] {
		if rule.Operator != "is" && rule.Operator != "is_not" && !empty {
			return errors.New("invalid category account filter operator")
		}
	} else {
		return errors.New("invalid account filter field")
	}
	if rule.ValueMode != managedinstance.AccountFilterValueAny && rule.ValueMode != managedinstance.AccountFilterValueAll {
		return errors.New("invalid account filter value mode")
	}
	if empty {
		return nil
	}
	if len(rule.Values) == 0 || len(rule.Values) > 50 {
		return errors.New("invalid account filter values")
	}
	for _, value := range rule.Values {
		if strings.TrimSpace(value) == "" || len([]rune(value)) > 200 {
			return errors.New("invalid account filter value")
		}
	}
	return nil
}

func inventorySourceNames(sources []managedinstance.InventorySource) map[string]string {
	result := make(map[string]string, len(sources))
	for _, source := range sources {
		result[source.ID] = safeBusinessText(source.Name)
	}
	return result
}

func accountInventoryRow(instance *managedinstance.InstanceView, account managedinstance.InventoryItem, sources map[string]string) managedAccountRow {
	amount := account.Cost
	item := managedAccountItem{InstanceID: instance.Id, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, AccountID: accountIdentifier(account), Name: safeBusinessText(account.Name), Email: safeBusinessText(account.Email), Note: safeBusinessText(account.Note), Ownership: safeBusinessText(account.Ownership), VendorName: safeBusinessText(account.VendorName), VendorEmail: safeBusinessText(account.VendorEmail), Type: safeBusinessText(account.Type), Group: safeBusinessText(account.Group), Status: safeBusinessText(account.Status), Available: account.Enabled, RateLimited: account.RateLimited, SourceID: safeBusinessText(account.SourceID), SourceName: sources[account.SourceID], CreatedAt: assistantTime(account.CreatedAt), LastActivityAt: assistantTime(account.LastActivityAt), DisabledAt: assistantTime(account.DisabledAt), ExpiresAt: assistantTime(account.ExpiresAt), Requests: account.Requests, Tokens: account.Tokens, Amount: amount, Currency: safeBusinessText(account.CostUnit), RPM: account.RPM, ActiveSessions: account.ActiveSessions, Utilization5H: account.Utilization5H, Utilization7D: account.Utilization7D, CollectionStatus: model.ManagedInstanceCollectionSucceeded}
	return managedAccountRow{item: item, doc: managedAccountDocument(item)}
}

func accountOutputRow(instance *managedinstance.InstanceView, output managedinstance.AccountOutputItem, sources map[string]string) managedAccountRow {
	account := output.Account
	var requests, tokens, amount *float64
	if output.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		requestsValue, tokensValue, amountValue := output.TotalRequests, output.TotalTokens, output.Amount
		requests, tokens, amount = &requestsValue, &tokensValue, &amountValue
	}
	item := managedAccountItem{InstanceID: instance.Id, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, AccountID: accountIdentifier(account), Name: safeBusinessText(account.Name), Email: safeBusinessText(account.Email), Note: safeBusinessText(account.Note), Ownership: safeBusinessText(account.Ownership), VendorName: safeBusinessText(account.VendorName), VendorEmail: safeBusinessText(account.VendorEmail), Type: safeBusinessText(account.Type), Group: safeBusinessText(account.Group), Status: safeBusinessText(account.Status), Available: account.Enabled, RateLimited: account.RateLimited, SourceID: safeBusinessText(account.SourceID), SourceName: sources[account.SourceID], CreatedAt: assistantTime(account.CreatedAt), LastActivityAt: assistantTime(account.LastActivityAt), DisabledAt: assistantTime(account.DisabledAt), ExpiresAt: assistantTime(account.ExpiresAt), Requests: requests, Tokens: tokens, Amount: amount, Currency: safeBusinessText(output.Currency), CollectionStatus: output.CollectionStatus, ErrorCode: output.ErrorCode}
	return managedAccountRow{item: item, doc: managedAccountDocument(item)}
}

func accountIdentifier(account managedinstance.InventoryItem) string {
	if strings.TrimSpace(account.IDText) != "" {
		return safeBusinessText(account.IDText)
	}
	return strconv.FormatInt(account.ID, 10)
}

func managedAccountDocument(item managedAccountItem) map[string][]string {
	available := "unknown"
	if item.Available != nil {
		if *item.Available {
			available = "available"
		} else {
			available = "unavailable"
		}
	}
	return map[string][]string{"name": {item.Name}, "email": {item.Email}, "account_id": {item.AccountID}, "note": {item.Note}, "ownership": {item.Ownership}, "vendor_name": {item.VendorName}, "vendor_email": {item.VendorEmail}, "instance": {item.InstanceName, strconv.FormatInt(item.InstanceID, 10)}, "platform": {item.Platform}, "type": {item.Type}, "group": {item.Group}, "status": {item.Status}, "source": {item.SourceID, item.SourceName}, "available": {available}}
}

func managedAccountMatches(doc map[string][]string, matchMode string, rules []managedinstance.AccountFilterRule) bool {
	if len(rules) == 0 {
		return true
	}
	matched := 0
	for _, rule := range rules {
		if managedAccountRuleMatches(doc[rule.Field], rule) {
			matched++
		}
	}
	if matchMode == managedinstance.AccountFilterMatchAny {
		return matched > 0
	}
	return matched == len(rules)
}

func managedAccountRuleMatches(fields []string, rule managedinstance.AccountFilterRule) bool {
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(field)))
	}
	hasValue := false
	for _, field := range normalized {
		if field != "" {
			hasValue = true
			break
		}
	}
	if rule.Operator == "is_empty" {
		return !hasValue
	}
	if rule.Operator == "is_not_empty" {
		return hasValue
	}
	valueMatches := func(raw string) bool {
		target := strings.ToLower(strings.TrimSpace(raw))
		for _, field := range normalized {
			switch rule.Operator {
			case "starts_with":
				if strings.HasPrefix(field, target) {
					return true
				}
			case "ends_with":
				if strings.HasSuffix(field, target) {
					return true
				}
			case "contains", "not_contains":
				if strings.Contains(field, target) {
					return true
				}
			default:
				if field == target {
					return true
				}
			}
		}
		return false
	}
	matched := 0
	for _, value := range rule.Values {
		if valueMatches(value) {
			matched++
		}
	}
	positive := matched > 0
	if rule.ValueMode == managedinstance.AccountFilterValueAll {
		positive = matched == len(rule.Values)
	}
	if rule.Operator == "not_contains" || rule.Operator == "is_not" {
		return !positive
	}
	return positive
}

func sortManagedAccountRows(rows []managedAccountRow, sortBy, order string) {
	if sortBy == "" {
		sortBy = "created_at"
	}
	if order == "" {
		order = "desc"
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := managedAccountSortValue(rows[i].item, sortBy), managedAccountSortValue(rows[j].item, sortBy)
		comparison := strings.Compare(left, right)
		if order == "asc" {
			return comparison < 0
		}
		return comparison > 0
	})
}

func managedAccountSortValue(item managedAccountItem, field string) string {
	switch field {
	case "name":
		return strings.ToLower(item.Name)
	case "status":
		return strings.ToLower(item.Status)
	case "requests":
		return sortableNumber(item.Requests)
	case "tokens":
		return sortableNumber(item.Tokens)
	case "amount":
		return sortableNumber(item.Amount)
	case "last_activity_at":
		return item.LastActivityAt
	default:
		return item.CreatedAt
	}
}

func sortableNumber(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%030.8f", *value)
}

func summarizeManagedAccountRows(rows []managedAccountRow) managedAccountSummary {
	result := managedAccountSummary{Total: len(rows), Amounts: map[string]float64{}}
	for _, row := range rows {
		item := row.item
		if item.Available == nil {
			result.Unknown++
		} else if *item.Available {
			result.Available++
		} else {
			result.Unavailable++
		}
		if item.Requests != nil {
			result.Requests += *item.Requests
		}
		if item.Tokens != nil {
			result.Tokens += *item.Tokens
		}
		if item.Amount != nil {
			unit := item.Currency
			if unit == "" {
				unit = "unknown"
			}
			result.Amounts[unit] += *item.Amount
		}
	}
	return result
}

func assistantErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var probe *managedinstance.ProbeError
	if errors.As(err, &probe) && probe.Code != "" {
		return probe.Code
	}
	switch {
	case errors.Is(err, managedinstance.ErrUnsupportedCapability):
		return "unsupported"
	case errors.Is(err, managedinstance.ErrInstanceNotFound):
		return "instance_not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "query_failed"
	}
}
