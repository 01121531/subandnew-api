package managedaccount

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	controlplaneservice "github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

const (
	DatasetInventory = "inventory"
	DatasetOutput    = "account_output"
	TimezoneShanghai = "Asia/Shanghai"
	maxPageSize      = 100
	textLimit        = 300
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s]+`),
	regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\bsk-[a-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|token|password|passwd|secret|cookie)\s*[:=]\s*[^\s,;]+`),
}

var quickTermSeparator = regexp.MustCompile(`[,，\n]+`)

type Query struct {
	InstanceIDs        []int64                             `json:"instance_ids"`
	Dataset            string                              `json:"dataset"`
	PresetDays         int                                 `json:"preset_days,omitempty"`
	IncludeTerms       []string                            `json:"include_terms,omitempty"`
	ExcludeTerms       []string                            `json:"exclude_terms,omitempty"`
	MatchMode          string                              `json:"match_mode"`
	Rules              []managedinstance.AccountFilterRule `json:"rules,omitempty"`
	NarrowIncludeTerms []string                            `json:"-"`
	NarrowExcludeTerms []string                            `json:"-"`
	NarrowMatchMode    string                              `json:"-"`
	NarrowRules        []managedinstance.AccountFilterRule `json:"-"`
	NarrowFields       []string                            `json:"-"`
	NarrowSearch       string                              `json:"-"`
	Search             string                              `json:"search,omitempty"`
	SortBy             string                              `json:"sort_by,omitempty"`
	SortOrder          string                              `json:"sort_order,omitempty"`
	Page               int                                 `json:"page,omitempty"`
	PageSize           int                                 `json:"page_size,omitempty"`
	AllowLargePage     bool                                `json:"-"`
	SelectedAccounts   []AccountIdentity                   `json:"-"`
}

type AccountIdentity struct {
	InstanceID int64
	AccountID  string
}

type Item struct {
	InstanceID       int64    `json:"instance_id"`
	InstanceName     string   `json:"instance_name"`
	Platform         string   `json:"platform"`
	AccountID        string   `json:"account_id"`
	Name             string   `json:"name,omitempty"`
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
	CreatedAt        int64    `json:"created_at,omitempty"`
	LastActivityAt   int64    `json:"last_activity_at,omitempty"`
	DisabledAt       int64    `json:"disabled_at,omitempty"`
	ExpiresAt        int64    `json:"expires_at,omitempty"`
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

type SourceStatus struct {
	InstanceID        int64  `json:"instance_id"`
	InstanceName      string `json:"instance_name"`
	Platform          string `json:"platform"`
	Status            string `json:"status"`
	ObservedAt        int64  `json:"observed_at,omitempty"`
	LastAttemptAt     int64  `json:"last_attempt_at,omitempty"`
	LastAttemptStatus string `json:"last_attempt_status,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	Stale             bool   `json:"stale"`
}

type Summary struct {
	Total       int                `json:"total"`
	Available   int                `json:"available"`
	Unavailable int                `json:"unavailable"`
	Unknown     int                `json:"unknown"`
	Requests    float64            `json:"requests"`
	Tokens      float64            `json:"tokens"`
	Amounts     map[string]float64 `json:"amounts"`
}

type Result struct {
	Dataset    string         `json:"dataset"`
	PresetDays int            `json:"preset_days,omitempty"`
	Items      []Item         `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	HasMore    bool           `json:"has_more"`
	Summary    Summary        `json:"summary"`
	Sources    []SourceStatus `json:"sources"`
	ObservedAt int64          `json:"observed_at,omitempty"`
	Stale      bool           `json:"stale"`
	Partial    bool           `json:"partial"`
	NoData     bool           `json:"no_data"`
}

type row struct {
	item Item
	doc  map[string][]string
}

func NormalizeQuery(input Query) (Query, error) {
	if len(input.InstanceIDs) == 0 || len(input.InstanceIDs) > 100 {
		return input, errors.New("account query requires 1 to 100 instance ids")
	}
	seen := make(map[int64]struct{}, len(input.InstanceIDs))
	ids := make([]int64, 0, len(input.InstanceIDs))
	for _, id := range input.InstanceIDs {
		if id <= 0 {
			return input, errors.New("instance ids must be positive")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	input.InstanceIDs = ids
	if input.Dataset == "" {
		input.Dataset = DatasetInventory
	}
	if input.Dataset != DatasetInventory && input.Dataset != DatasetOutput {
		return input, errors.New("dataset must be inventory or account_output")
	}
	if input.PresetDays == 0 {
		input.PresetDays = 7
	}
	if input.PresetDays != 1 && input.PresetDays != 7 && input.PresetDays != 14 && input.PresetDays != 30 {
		return input, errors.New("preset_days must be one of 1, 7, 14, or 30")
	}
	matchMode, rules, err := managedinstance.NormalizeAccountFilter(input.MatchMode, input.Rules, false)
	if err != nil {
		return input, err
	}
	input.MatchMode, input.Rules = matchMode, rules
	input.IncludeTerms, err = normalizeTerms(input.IncludeTerms)
	if err != nil {
		return input, err
	}
	input.ExcludeTerms, err = normalizeTerms(input.ExcludeTerms)
	if err != nil {
		return input, err
	}
	narrowMatchMode, narrowRules, err := managedinstance.NormalizeAccountFilter(input.NarrowMatchMode, input.NarrowRules, false)
	if err != nil {
		return input, err
	}
	input.NarrowMatchMode, input.NarrowRules = narrowMatchMode, narrowRules
	input.NarrowIncludeTerms, err = normalizeTerms(input.NarrowIncludeTerms)
	if err != nil {
		return input, err
	}
	input.NarrowExcludeTerms, err = normalizeTerms(input.NarrowExcludeTerms)
	if err != nil {
		return input, err
	}
	input.NarrowSearch = strings.TrimSpace(input.NarrowSearch)
	if utf8.RuneCountInString(input.NarrowSearch) > 200 {
		return input, errors.New("narrow search is too long")
	}
	input.Search = strings.TrimSpace(input.Search)
	if utf8.RuneCountInString(input.Search) > 200 {
		return input, errors.New("search is too long")
	}
	validSort := map[string]bool{"": true, "name": true, "created_at": true, "last_activity_at": true, "status": true, "requests": true, "tokens": true, "amount": true}
	if !validSort[input.SortBy] || (input.SortOrder != "" && input.SortOrder != "asc" && input.SortOrder != "desc") {
		return input, errors.New("invalid account sort")
	}
	if input.SortBy == "" {
		input.SortBy = "created_at"
	}
	if input.SortOrder == "" {
		input.SortOrder = "desc"
	}
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	pageLimit := maxPageSize
	if input.AllowLargePage {
		pageLimit = 10000
	}
	if input.PageSize > pageLimit {
		return input, fmt.Errorf("page_size cannot exceed %d", pageLimit)
	}
	if len(input.SelectedAccounts) > 10000 {
		return input, errors.New("selected account count cannot exceed 10000")
	}
	selected := make([]AccountIdentity, 0, len(input.SelectedAccounts))
	selectedSeen := make(map[string]struct{}, len(input.SelectedAccounts))
	for _, identity := range input.SelectedAccounts {
		identity.AccountID = strings.TrimSpace(identity.AccountID)
		if identity.InstanceID <= 0 || identity.AccountID == "" {
			return input, errors.New("selected accounts require a valid instance id and account id")
		}
		key := strconv.FormatInt(identity.InstanceID, 10) + "\x00" + identity.AccountID
		if _, ok := selectedSeen[key]; ok {
			continue
		}
		selectedSeen[key] = struct{}{}
		selected = append(selected, identity)
	}
	input.SelectedAccounts = selected
	return input, nil
}

func Execute(ctx context.Context, input Query) (*Result, error) {
	input, err := NormalizeQuery(input)
	if err != nil {
		return nil, err
	}
	accountRange, err := controlplaneservice.NormalizeManagedAccountRange(input.PresetDays, 0, 0, TimezoneShanghai)
	if err != nil {
		return nil, err
	}
	rows := make([]row, 0)
	statuses := make([]SourceStatus, 0, len(input.InstanceIDs))
	observedAt := int64(0)
	successfulSources := 0
	stale := false

	for _, instanceID := range input.InstanceIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		instance, getErr := managedinstance.Get(instanceID)
		if getErr != nil {
			statuses = append(statuses, SourceStatus{InstanceID: instanceID, Status: "missing", ErrorCode: errorCode(getErr), Stale: true})
			stale = true
			continue
		}
		status := SourceStatus{InstanceID: instanceID, InstanceName: SanitizeText(instance.Name), Platform: instance.Kind, Status: "no_data", Stale: true}
		snapshot, snapshotErr := controlplaneservice.GetManagedAccountSnapshot(instanceID, accountRange)
		if snapshotErr != nil {
			status.ErrorCode = errorCode(snapshotErr)
			statuses = append(statuses, status)
			stale = true
			continue
		}
		section := snapshot.Inventory
		if input.Dataset == DatasetOutput {
			section = snapshot.AccountOutput
		}
		status.LastAttemptAt = section.LastAttemptAt
		status.LastAttemptStatus = section.LastAttemptStatus
		status.ErrorCode = section.LastErrorCode
		if section.Observation == nil {
			statuses = append(statuses, status)
			stale = true
			continue
		}
		status.Status = section.Observation.CollectionStatus
		status.ObservedAt = section.Observation.ObservedAt
		status.Stale = section.LastAttemptStatus == model.ManagedInstanceCollectionFailed || time.Now().Unix()-status.ObservedAt > int64(65*time.Minute/time.Second)
		statuses = append(statuses, status)
		stale = stale || status.Stale
		observedAt = conservativeTimestamp(observedAt, status.ObservedAt)
		if input.Dataset == DatasetInventory {
			inventory, inventoryErr := controlplaneservice.GetManagedAccountInventorySnapshot(instanceID)
			if inventoryErr != nil {
				statuses[len(statuses)-1].Status = model.ManagedInstanceCollectionFailed
				statuses[len(statuses)-1].ErrorCode = errorCode(inventoryErr)
				statuses[len(statuses)-1].Stale = true
				stale = true
				continue
			}
			successfulSources++
			if inventory != nil {
				sourceNames := sourceNameMap(inventory)
				for _, account := range inventory.Items {
					candidate := inventoryRow(instance, account, sourceNames)
					if matches(candidate.doc, input) && selectedAccountMatches(candidate.item, input.SelectedAccounts) {
						rows = append(rows, candidate)
					}
				}
			}
			continue
		}
		inventory, _ := controlplaneservice.GetManagedAccountInventorySnapshot(instanceID)
		sourceNames := sourceNameMap(inventory)
		output, outputErr := controlplaneservice.GetManagedAccountOutputSnapshot(instanceID, accountRange.RangeKey)
		if outputErr != nil {
			statuses[len(statuses)-1].Status = model.ManagedInstanceCollectionFailed
			statuses[len(statuses)-1].ErrorCode = errorCode(outputErr)
			statuses[len(statuses)-1].Stale = true
			stale = true
			continue
		}
		successfulSources++
		if output != nil {
			for _, account := range output.Items {
				candidate := outputRow(instance, account, sourceNames)
				if matches(candidate.doc, input) && selectedAccountMatches(candidate.item, input.SelectedAccounts) {
					rows = append(rows, candidate)
				}
			}
		}
	}

	sortRows(rows, input.SortBy, input.SortOrder)
	summary := summarize(rows)
	start := (input.Page - 1) * input.PageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := min(start+input.PageSize, len(rows))
	items := make([]Item, 0, end-start)
	for _, candidate := range rows[start:end] {
		items = append(items, candidate.item)
	}
	return &Result{
		Dataset: input.Dataset, PresetDays: input.PresetDays, Items: items, Total: len(rows), Page: input.Page,
		PageSize: input.PageSize, HasMore: end < len(rows), Summary: summary, Sources: statuses,
		ObservedAt: observedAt, Stale: stale, Partial: successfulSources > 0 && successfulSources < len(input.InstanceIDs), NoData: successfulSources == 0,
	}, nil
}

func inventoryRow(instance *managedinstance.InstanceView, account managedinstance.InventoryItem, sources map[string]string) row {
	item := itemFromInventory(instance, account, sources)
	item.Requests, item.Tokens, item.Amount, item.Currency = account.Requests, account.Tokens, account.Cost, SanitizeText(account.CostUnit)
	item.CollectionStatus = model.ManagedInstanceCollectionSucceeded
	return row{item: item, doc: document(item)}
}

func outputRow(instance *managedinstance.InstanceView, output managedinstance.AccountOutputItem, sources map[string]string) row {
	item := itemFromInventory(instance, output.Account, sources)
	if output.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		requests, tokens, amount := output.TotalRequests, output.TotalTokens, output.Amount
		item.Requests, item.Tokens, item.Amount = &requests, &tokens, &amount
	}
	item.Currency = SanitizeText(output.Currency)
	item.CollectionStatus = output.CollectionStatus
	item.ErrorCode = safeErrorCode(output.ErrorCode)
	return row{item: item, doc: document(item)}
}

func itemFromInventory(instance *managedinstance.InstanceView, account managedinstance.InventoryItem, sources map[string]string) Item {
	accountID := strings.TrimSpace(account.IDText)
	if accountID == "" {
		accountID = strconv.FormatInt(account.ID, 10)
	}
	return Item{
		InstanceID: instance.Id, InstanceName: SanitizeText(instance.Name), Platform: SanitizeText(instance.Kind),
		AccountID: SanitizeText(accountID), Name: SanitizeText(account.Name), Email: SanitizeText(account.Email),
		Note: SanitizeText(account.Note), Ownership: SanitizeText(account.Ownership), Type: SanitizeText(account.Type),
		Group: SanitizeText(account.Group), Status: SanitizeText(account.Status), Available: account.Enabled,
		RateLimited: account.RateLimited, SourceID: SanitizeText(account.SourceID), SourceName: SanitizeText(sources[account.SourceID]),
		CreatedAt: account.CreatedAt, LastActivityAt: account.LastActivityAt, DisabledAt: account.DisabledAt,
		ExpiresAt: account.ExpiresAt, RPM: account.RPM, ActiveSessions: account.ActiveSessions,
		Utilization5H: account.Utilization5H, Utilization7D: account.Utilization7D,
	}
}

func sourceNameMap(inventory *managedinstance.InventoryPage) map[string]string {
	result := map[string]string{}
	if inventory == nil {
		return result
	}
	for _, source := range inventory.Sources {
		result[source.ID] = SanitizeText(source.Name)
	}
	return result
}

func document(item Item) map[string][]string {
	available := "unknown"
	if item.Available != nil {
		if *item.Available {
			available = "available"
		} else {
			available = "unavailable"
		}
	}
	return map[string][]string{
		"name": {item.Name}, "email": {item.Email}, "account_id": {item.AccountID}, "note": {item.Note},
		"ownership": {item.Ownership}, "instance": {item.InstanceName, strconv.FormatInt(item.InstanceID, 10)},
		"platform": {item.Platform}, "type": {item.Type}, "group": {item.Group}, "status": {item.Status},
		"source": {item.SourceID, item.SourceName}, "available": {available},
	}
}

func matches(doc map[string][]string, input Query) bool {
	if !matchesFilter(doc, input.IncludeTerms, input.ExcludeTerms, input.MatchMode, input.Rules) {
		return false
	}
	searchable := strings.ToLower(strings.Join(flatten(doc), " "))
	if input.Search != "" && !strings.Contains(searchable, strings.ToLower(input.Search)) {
		return false
	}
	narrowDocument := restrictDocument(doc, input.NarrowFields)
	narrowSearchable := strings.ToLower(strings.Join(flatten(narrowDocument), " "))
	if input.NarrowSearch != "" && !strings.Contains(narrowSearchable, strings.ToLower(input.NarrowSearch)) {
		return false
	}
	return matchesFilter(narrowDocument, input.NarrowIncludeTerms, input.NarrowExcludeTerms, input.NarrowMatchMode, input.NarrowRules)
}

func selectedAccountMatches(item Item, selected []AccountIdentity) bool {
	if len(selected) == 0 {
		return true
	}
	for _, identity := range selected {
		if identity.InstanceID == item.InstanceID && identity.AccountID == item.AccountID {
			return true
		}
	}
	return false
}

func restrictDocument(doc map[string][]string, fields []string) map[string][]string {
	if len(fields) == 0 {
		return doc
	}
	result := make(map[string][]string, len(fields))
	for _, field := range fields {
		result[field] = doc[field]
	}
	return result
}

func matchesFilter(doc map[string][]string, include, exclude []string, matchMode string, rules []managedinstance.AccountFilterRule) bool {
	searchable := strings.ToLower(strings.Join(flatten(doc), " "))
	if len(include) > 0 && !anyContained(searchable, include) {
		return false
	}
	if anyContained(searchable, exclude) {
		return false
	}
	if len(rules) == 0 {
		return true
	}
	matched := 0
	for _, rule := range rules {
		if ruleMatches(doc[rule.Field], rule) {
			matched++
		}
	}
	if matchMode == managedinstance.AccountFilterMatchAny {
		return matched > 0
	}
	return matched == len(rules)
}

func ruleMatches(fields []string, rule managedinstance.AccountFilterRule) bool {
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(field)))
	}
	hasValue := false
	for _, field := range normalized {
		hasValue = hasValue || field != ""
	}
	if rule.Operator == "is_empty" {
		return !hasValue
	}
	if rule.Operator == "is_not_empty" {
		return hasValue
	}
	matched := 0
	for _, raw := range rule.Values {
		target := strings.ToLower(strings.TrimSpace(raw))
		valueMatched := false
		for _, field := range normalized {
			if rule.Operator == "contains" || rule.Operator == "not_contains" {
				valueMatched = valueMatched || strings.Contains(field, target)
			} else {
				valueMatched = valueMatched || field == target
			}
		}
		if valueMatched {
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

func sortRows(rows []row, field, order string) {
	sort.SliceStable(rows, func(i, j int) bool {
		comparison := compareItems(rows[i].item, rows[j].item, field)
		if comparison == 0 {
			comparison = strings.Compare(rows[i].item.AccountID, rows[j].item.AccountID)
		}
		if order == "asc" {
			return comparison < 0
		}
		return comparison > 0
	})
}

func compareItems(left, right Item, field string) int {
	switch field {
	case "name":
		return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
	case "status":
		return strings.Compare(strings.ToLower(left.Status), strings.ToLower(right.Status))
	case "requests":
		return compareNumber(left.Requests, right.Requests)
	case "tokens":
		return compareNumber(left.Tokens, right.Tokens)
	case "amount":
		return compareNumber(left.Amount, right.Amount)
	case "last_activity_at":
		return cmpInt64(left.LastActivityAt, right.LastActivityAt)
	default:
		return cmpInt64(left.CreatedAt, right.CreatedAt)
	}
}

func compareNumber(left, right *float64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
}

func cmpInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func summarize(rows []row) Summary {
	result := Summary{Total: len(rows), Amounts: map[string]float64{}}
	for _, candidate := range rows {
		item := candidate.item
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

func normalizeTerms(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, term := range quickTermSeparator.Split(value, -1) {
			term = strings.ToLower(strings.TrimSpace(term))
			if term == "" {
				continue
			}
			if utf8.RuneCountInString(term) > 200 {
				return nil, errors.New("account filter values cannot exceed 200 characters")
			}
			if _, duplicate := seen[term]; duplicate {
				continue
			}
			seen[term] = struct{}{}
			result = append(result, term)
			if len(result) > 50 {
				return nil, errors.New("account quick filters cannot exceed 50 values")
			}
		}
	}
	return result, nil
}

func flatten(doc map[string][]string) []string {
	result := make([]string, 0)
	for _, values := range doc {
		result = append(result, values...)
	}
	return result
}

func anyContained(searchable string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(searchable, term) {
			return true
		}
	}
	return false
}

func conservativeTimestamp(current, candidate int64) int64 {
	if candidate <= 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var probe *managedinstance.ProbeError
	if errors.As(err, &probe) && probe.Code != "" {
		return safeErrorCode(probe.Code)
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

func safeErrorCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return -1
	}, value)
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned
}

func SanitizeText(value string) string {
	value = strings.TrimSpace(value)
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[已隐藏]")
	}
	if utf8.RuneCountInString(value) <= textLimit {
		return value
	}
	return string([]rune(value)[:textLimit]) + "..."
}

func FormatTime(timestamp int64) string {
	if timestamp <= 0 {
		return ""
	}
	location, _ := time.LoadLocation(TimezoneShanghai)
	return time.Unix(timestamp, 0).In(location).Format(time.RFC3339)
}

func FieldValue(item Item, field string) (any, bool) {
	switch field {
	case "instance_id":
		return item.InstanceID, true
	case "account_id":
		return item.AccountID, true
	case "instance_name":
		return item.InstanceName, item.InstanceName != ""
	case "platform":
		return item.Platform, item.Platform != ""
	case "name":
		return item.Name, item.Name != ""
	case "email":
		return item.Email, item.Email != ""
	case "note":
		return item.Note, item.Note != ""
	case "ownership":
		return item.Ownership, item.Ownership != ""
	case "type":
		return item.Type, item.Type != ""
	case "group":
		return item.Group, item.Group != ""
	case "status":
		return item.Status, item.Status != ""
	case "available":
		return item.Available, item.Available != nil
	case "rate_limited":
		return item.RateLimited, true
	case "source_id":
		return item.SourceID, item.SourceID != ""
	case "source_name":
		return item.SourceName, item.SourceName != ""
	case "created_at":
		return FormatTime(item.CreatedAt), item.CreatedAt > 0
	case "last_activity_at":
		return FormatTime(item.LastActivityAt), item.LastActivityAt > 0
	case "disabled_at":
		return FormatTime(item.DisabledAt), item.DisabledAt > 0
	case "expires_at":
		return FormatTime(item.ExpiresAt), item.ExpiresAt > 0
	case "requests":
		return item.Requests, item.Requests != nil
	case "tokens":
		return item.Tokens, item.Tokens != nil
	case "amount":
		return item.Amount, item.Amount != nil
	case "currency":
		return item.Currency, item.Currency != ""
	case "rpm":
		return item.RPM, item.RPM != nil
	case "active_sessions":
		return item.ActiveSessions, item.ActiveSessions != nil
	case "utilization_5h":
		return item.Utilization5H, item.Utilization5H != nil
	case "utilization_7d":
		return item.Utilization7D, item.Utilization7D != nil
	case "collection_status":
		return item.CollectionStatus, item.CollectionStatus != ""
	case "error_code":
		return item.ErrorCode, item.ErrorCode != ""
	default:
		return nil, false
	}
}

func ValidateFields(fields []string) ([]string, error) {
	allowed := map[string]bool{
		"instance_name": true, "platform": true, "name": true, "email": true, "note": true, "ownership": true,
		"type": true, "group": true, "status": true, "available": true, "rate_limited": true, "source_id": true,
		"source_name": true, "created_at": true, "last_activity_at": true, "disabled_at": true, "expires_at": true,
		"requests": true, "tokens": true, "amount": true, "currency": true, "rpm": true, "active_sessions": true,
		"utilization_5h": true, "utilization_7d": true, "collection_status": true, "error_code": true,
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if !allowed[field] {
			return nil, fmt.Errorf("unsupported account field %q", field)
		}
		if _, duplicate := seen[field]; duplicate {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	if len(result) == 0 {
		return nil, errors.New("at least one optional field is required")
	}
	return result, nil
}
