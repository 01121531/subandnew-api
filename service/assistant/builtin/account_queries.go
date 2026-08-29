package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	controlplaneservice "github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
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
	validSort := map[string]bool{"": true, "name": true, "created_at": true, "last_activity_at": true, "status": true, "requests": true, "tokens": true, "amount": true}
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
	schema := json.RawMessage(`{"type":"object","properties":{"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]},"dataset":{"type":"string","enum":["inventory","account_output"]},"preset_days":{"type":"integer","enum":[1,7,14,30]},"match_mode":{"type":"string","enum":["all","any"]},"rules":{"type":"array","maxItems":20,"items":{"type":"object","properties":{"field":{"type":"string","enum":["name","email","account_id","note","ownership","instance","platform","type","group","status","source","available"]},"operator":{"type":"string","enum":["contains","not_contains","is_empty","is_not_empty","is","is_not"]},"values":{"type":"array","maxItems":50,"items":{"type":"string","maxLength":200}},"value_mode":{"type":"string","enum":["any","all"]}},"required":["field","operator","value_mode"],"additionalProperties":false}},"sort_by":{"type":"string","enum":["name","created_at","last_activity_at","status","requests","tokens","amount"]},"sort_order":{"type":"string","enum":["asc","desc"]},"page":{"type":"integer","minimum":1},"page_size":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	return tool.Register(registry, tool.ToolSpec{
		Name: "query_managed_accounts", Version: "v1", Description: "从账号管理后台快照查询账号明细或新增账号产出，支持高级筛选、排序和分页；不会刷新目标实例。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:       tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: schema,
	}, func(ctx context.Context, execution tool.ExecutionContext, input managedAccountsInput) (tool.Output[managedAccountsOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[managedAccountsOutput]{}, err
		}
		return executeManagedAccountQuery(resolution.IDs, input)
	})
}

func executeManagedAccountQuery(instanceIDs []int64, input managedAccountsInput) (tool.Output[managedAccountsOutput], error) {
	dataset := input.Dataset
	if dataset == "" {
		dataset = managedAccountDatasetInventory
	}
	presetDays := input.PresetDays
	if presetDays == 0 {
		presetDays = 7
	}
	matchMode := input.MatchMode
	if matchMode == "" {
		matchMode = managedinstance.AccountFilterMatchAll
	}
	page, pageSize := input.Page, input.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	rows := make([]managedAccountRow, 0)
	statuses := make([]managedAccountSourceStatus, 0, len(instanceIDs))
	provenance := make([]tool.Provenance, 0, len(instanceIDs))
	observedAt := int64(0)
	stale := false

	accountRange, err := controlplaneservice.NormalizeManagedAccountRange(presetDays, 0, 0, assistantTimezone)
	if err != nil {
		return tool.Output[managedAccountsOutput]{}, err
	}
	for _, instanceID := range instanceIDs {
		view, getErr := managedinstance.Get(instanceID)
		if getErr != nil {
			return tool.Output[managedAccountsOutput]{}, getErr
		}
		snapshot, snapshotErr := controlplaneservice.GetManagedAccountSnapshot(instanceID, accountRange)
		status := managedAccountSourceStatus{InstanceID: instanceID, InstanceName: safeBusinessText(view.Name), Platform: view.Kind, Status: "no_data", Stale: true}
		if snapshotErr != nil {
			status.ErrorCode = assistantErrorCode(snapshotErr)
			statuses = append(statuses, status)
			stale = true
			continue
		}
		section := snapshot.Inventory
		if dataset == managedAccountDatasetOutput {
			section = snapshot.AccountOutput
		}
		status.LastAttemptAt = assistantTime(section.LastAttemptAt)
		status.LastAttemptStatus = section.LastAttemptStatus
		status.ErrorCode = section.LastErrorCode
		status.Stale = section.LastAttemptStatus == model.ManagedInstanceCollectionFailed || section.Observation == nil
		if section.Observation == nil {
			statuses = append(statuses, status)
			stale = true
			continue
		}
		status.Status = section.Observation.CollectionStatus
		status.ObservedAt = assistantTime(section.Observation.ObservedAt)
		status.Stale = status.Stale || time.Now().Unix()-section.Observation.ObservedAt > int64(65*time.Minute/time.Second)
		observedAt = conservativeTimestamp(observedAt, section.Observation.ObservedAt)
		stale = stale || status.Stale
		provenance = append(provenance, tool.Provenance{Source: "managed_account_snapshots", Resource: "instance:" + strconv.FormatInt(instanceID, 10) + ":" + dataset, ObservedAt: unixTime(section.Observation.ObservedAt)})

		var inventory *managedinstance.InventoryPage
		var output *managedinstance.AccountOutputResult
		if dataset == managedAccountDatasetInventory {
			inventory, getErr = controlplaneservice.GetManagedAccountInventorySnapshot(instanceID)
		} else {
			output, getErr = controlplaneservice.GetManagedAccountOutputSnapshot(instanceID, accountRange.RangeKey)
		}
		if getErr != nil {
			status.Status = model.ManagedInstanceCollectionFailed
			status.ErrorCode = assistantErrorCode(getErr)
			status.Stale = true
			statuses = append(statuses, status)
			stale = true
			continue
		}
		if inventory != nil {
			sourceNames := inventorySourceNames(inventory.Sources)
			for _, account := range inventory.Items {
				row := accountInventoryRow(view, account, sourceNames)
				if managedAccountMatches(row.doc, matchMode, input.Rules) {
					rows = append(rows, row)
				}
			}
		}
		if output != nil {
			sourceNames := map[string]string{}
			if inventorySnapshot, inventoryErr := controlplaneservice.GetManagedAccountInventorySnapshot(instanceID); inventoryErr == nil && inventorySnapshot != nil {
				sourceNames = inventorySourceNames(inventorySnapshot.Sources)
			}
			for _, accountOutput := range output.Items {
				row := accountOutputRow(view, accountOutput, sourceNames)
				if managedAccountMatches(row.doc, matchMode, input.Rules) {
					rows = append(rows, row)
				}
			}
		}
		statuses = append(statuses, status)
	}
	if len(provenance) == 0 {
		provenance = []tool.Provenance{{Source: "managed_account_snapshots"}}
	}
	sortManagedAccountRows(rows, input.SortBy, input.SortOrder)
	summary := summarizeManagedAccountRows(rows)
	start := (page - 1) * pageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := min(start+pageSize, len(rows))
	items := make([]managedAccountItem, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, row.item)
	}
	return tool.Output[managedAccountsOutput]{
		Data:       managedAccountsOutput{Dataset: dataset, PresetDays: presetDays, Items: items, Total: len(rows), Page: page, PageSize: pageSize, Summary: summary, Sources: statuses},
		Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, stale),
	}, nil
}

func validateManagedAccountRule(rule managedinstance.AccountFilterRule) error {
	textFields := map[string]bool{"name": true, "email": true, "account_id": true, "note": true, "ownership": true}
	categoryFields := map[string]bool{"instance": true, "platform": true, "type": true, "group": true, "status": true, "source": true, "available": true}
	empty := rule.Operator == "is_empty" || rule.Operator == "is_not_empty"
	if textFields[rule.Field] {
		if rule.Operator != "contains" && rule.Operator != "not_contains" && !empty {
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
	item := managedAccountItem{InstanceID: instance.Id, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, AccountID: accountIdentifier(account), Name: safeBusinessText(account.Name), Email: safeBusinessText(account.Email), Note: safeBusinessText(account.Note), Ownership: safeBusinessText(account.Ownership), Type: safeBusinessText(account.Type), Group: safeBusinessText(account.Group), Status: safeBusinessText(account.Status), Available: account.Enabled, RateLimited: account.RateLimited, SourceID: safeBusinessText(account.SourceID), SourceName: sources[account.SourceID], CreatedAt: assistantTime(account.CreatedAt), LastActivityAt: assistantTime(account.LastActivityAt), DisabledAt: assistantTime(account.DisabledAt), ExpiresAt: assistantTime(account.ExpiresAt), Requests: account.Requests, Tokens: account.Tokens, Amount: amount, Currency: safeBusinessText(account.CostUnit), RPM: account.RPM, ActiveSessions: account.ActiveSessions, Utilization5H: account.Utilization5H, Utilization7D: account.Utilization7D, CollectionStatus: model.ManagedInstanceCollectionSucceeded}
	return managedAccountRow{item: item, doc: managedAccountDocument(item)}
}

func accountOutputRow(instance *managedinstance.InstanceView, output managedinstance.AccountOutputItem, sources map[string]string) managedAccountRow {
	account := output.Account
	var requests, tokens, amount *float64
	if output.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		requestsValue, tokensValue, amountValue := output.TotalRequests, output.TotalTokens, output.Amount
		requests, tokens, amount = &requestsValue, &tokensValue, &amountValue
	}
	item := managedAccountItem{InstanceID: instance.Id, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, AccountID: accountIdentifier(account), Name: safeBusinessText(account.Name), Email: safeBusinessText(account.Email), Note: safeBusinessText(account.Note), Ownership: safeBusinessText(account.Ownership), Type: safeBusinessText(account.Type), Group: safeBusinessText(account.Group), Status: safeBusinessText(account.Status), Available: account.Enabled, RateLimited: account.RateLimited, SourceID: safeBusinessText(account.SourceID), SourceName: sources[account.SourceID], CreatedAt: assistantTime(account.CreatedAt), LastActivityAt: assistantTime(account.LastActivityAt), DisabledAt: assistantTime(account.DisabledAt), ExpiresAt: assistantTime(account.ExpiresAt), Requests: requests, Tokens: tokens, Amount: amount, Currency: safeBusinessText(output.Currency), CollectionStatus: output.CollectionStatus, ErrorCode: output.ErrorCode}
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
	return map[string][]string{"name": {item.Name}, "email": {item.Email}, "account_id": {item.AccountID}, "note": {item.Note}, "ownership": {item.Ownership}, "instance": {item.InstanceName, strconv.FormatInt(item.InstanceID, 10)}, "platform": {item.Platform}, "type": {item.Type}, "group": {item.Group}, "status": {item.Status}, "source": {item.SourceID, item.SourceName}, "available": {available}}
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
			if rule.Operator == "contains" || rule.Operator == "not_contains" {
				if strings.Contains(field, target) {
					return true
				}
			} else if field == target {
				return true
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
