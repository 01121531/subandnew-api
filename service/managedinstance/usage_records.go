package managedinstance

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
)

const (
	usageRecordPageSize     = 20
	usageRecordMaxPageSize  = 100
	usageRecordExportLimit  = 100000
	usageRecordMaxTextValue = 512
)

var ErrUsageExportTooLarge = errors.New("usage record export exceeds the 100000 row limit")

type UsageRecordPage struct {
	SourceInstanceID int64             `json:"source_instance_id"`
	Kind             string            `json:"kind"`
	Items            []json.RawMessage `json:"items"`
	Total            int64             `json:"total"`
	Page             int               `json:"page"`
	PageSize         int               `json:"page_size"`
}

type UsageRecordSummary struct {
	SourceInstanceID int64   `json:"source_instance_id"`
	Kind             string  `json:"kind"`
	TotalTokens      float64 `json:"total_tokens"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
}

type usageRecordClient struct {
	instance   *model.ManagedInstance
	connector  *Connector
	credential *CredentialMaterial
}

type UsageRecordCSVExport struct {
	client *usageRecordClient
	query  url.Values
	first  *UsageRecordPage
}

func ListUsageRecords(ctx context.Context, instanceID int64, input url.Values) (*UsageRecordPage, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, err
	}
	query, err := normalizeUsageRecordQuery(client.instance.Kind, input)
	if err != nil {
		return nil, err
	}
	return client.list(ctx, query)
}

func GetUsageRecordSummary(ctx context.Context, instanceID int64, input url.Values) (*UsageRecordSummary, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, err
	}
	query, err := normalizeUsageRecordQuery(client.instance.Kind, input)
	if err != nil {
		return nil, err
	}
	return client.summary(ctx, query)
}

func StreamUsageRecordsCSV(ctx context.Context, instanceID int64, input url.Values, writer io.Writer) (int, error) {
	export, err := PrepareUsageRecordsCSV(ctx, instanceID, input)
	if err != nil {
		return 0, err
	}
	return export.Write(ctx, writer)
}

func PrepareUsageRecordsCSV(ctx context.Context, instanceID int64, input url.Values) (*UsageRecordCSVExport, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, err
	}
	query, err := normalizeUsageRecordQuery(client.instance.Kind, input)
	if err != nil {
		return nil, err
	}
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		query.Set("exact_total", "true")
	}
	setUsageRecordPage(query, client.instance.Kind, 1, usageRecordMaxPageSize)
	first, err := client.list(ctx, query)
	if err != nil {
		return nil, err
	}
	if first.Total > usageRecordExportLimit {
		return nil, ErrUsageExportTooLarge
	}
	return &UsageRecordCSVExport{client: client, query: query, first: first}, nil
}

func (export *UsageRecordCSVExport) Write(ctx context.Context, writer io.Writer) (int, error) {
	if export == nil || export.client == nil || export.first == nil {
		return 0, ErrInvalidInstance
	}

	if _, err := io.WriteString(writer, "\xEF\xBB\xBF"); err != nil {
		return 0, err
	}
	csvWriter := csv.NewWriter(writer)
	headers, fields := usageCSVSchema(export.client.instance.Kind)
	if err := csvWriter.Write(headers); err != nil {
		return 0, err
	}

	written := 0
	page := export.first
	for {
		for _, raw := range page.Items {
			row, err := usageCSVRow(raw, fields)
			if err != nil {
				return written, err
			}
			if err := csvWriter.Write(row); err != nil {
				return written, err
			}
			written++
			if written >= usageRecordExportLimit {
				break
			}
		}
		if written >= int(page.Total) || len(page.Items) < usageRecordMaxPageSize || written >= usageRecordExportLimit {
			break
		}
		setUsageRecordPage(export.query, export.client.instance.Kind, page.Page+1, usageRecordMaxPageSize)
		var err error
		page, err = export.client.list(ctx, export.query)
		if err != nil {
			return written, err
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return written, err
	}
	return written, nil
}

func RecordUsageRecordExportAudit(instanceID int64, actorID int, count int, exportErr error) {
	outcome := "succeeded"
	details := map[string]any{"format": "csv", "record_count": count}
	if exportErr != nil {
		outcome = "failed"
		details["error_code"] = managedInstanceObservationErrorCode(exportErr)
	}
	_ = writeAuditOutcome(model.DB, instanceID, actorID, "usage_records_export", outcome, details)
}

func newUsageRecordClient(instanceID int64) (*usageRecordClient, error) {
	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	if instance.Kind != model.ManagedInstanceKindNewAPI && instance.Kind != model.ManagedInstanceKindHuichuan && instance.Kind != model.ManagedInstanceKindSub2API {
		return nil, ErrUnsupportedCapability
	}
	return &usageRecordClient{instance: instance, connector: connector, credential: credential}, nil
}

func (client *usageRecordClient) list(ctx context.Context, query url.Values) (*UsageRecordPage, error) {
	var endpoint string
	var response *ConnectorResponse
	var err error
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		endpoint = "/api/v1/admin/usage?" + query.Encode()
		if credentialAccessScope(client.credential) == model.ManagedInstanceAccessUser {
			endpoint = "/api/v1/usage?" + query.Encode()
		}
		response, err = sub2APIDoJSON(ctx, client.connector, client.credential, http.MethodGet, endpoint, nil)
	} else {
		endpoint = "/api/log/?" + query.Encode()
		if credentialAccessScope(client.credential) == model.ManagedInstanceAccessUser {
			endpoint = "/api/log/self?" + query.Encode()
		}
		var headers http.Header
		headers, err = newAPIAuthHeaders(ctx, client.connector, client.instance.Kind, client.credential)
		if err == nil {
			response, err = client.connector.DoJSON(ctx, http.MethodGet, endpoint, headers, nil)
		}
	}
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	page, err := decodeUsageRecordPage(client.instance.Kind, response.Body)
	if err != nil {
		return nil, err
	}
	page.SourceInstanceID = client.instance.Id
	page.Kind = client.instance.Kind
	return page, nil
}

func (client *usageRecordClient) summary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		return client.sub2Summary(ctx, query)
	}
	return client.newAPISummary(ctx, query)
}

func (client *usageRecordClient) newAPISummary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	headers, err := newAPIAuthHeaders(ctx, client.connector, client.instance.Kind, client.credential)
	if err != nil {
		return nil, err
	}
	summaryQuery := url.Values{}
	for _, key := range []string{"start_timestamp", "end_timestamp"} {
		if value := query.Get(key); value != "" {
			summaryQuery.Set(key, value)
		}
	}
	endpoint := "/api/data/"
	if credentialAccessScope(client.credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/data/self"
	}
	response, err := client.connector.DoJSON(ctx, http.MethodGet, endpoint+"?"+summaryQuery.Encode(), headers, nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			TokenUsed float64 `json:"token_used"`
			Quota     float64 `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !payload.Success {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}

	totalTokens := 0.0
	quota := 0.0
	for _, item := range payload.Data {
		totalTokens += item.TokenUsed
		quota += item.Quota
	}
	amount := quota
	currency := "quota"
	statusResponse, statusErr := client.connector.DoJSON(ctx, http.MethodGet, "/api/status", nil, nil)
	if statusErr == nil && statusResponse.StatusCode == http.StatusOK {
		var status struct {
			Success bool `json:"success"`
			Data    struct {
				QuotaPerUnit float64 `json:"quota_per_unit"`
			} `json:"data"`
		}
		if json.Unmarshal(statusResponse.Body, &status) == nil && status.Success && status.Data.QuotaPerUnit > 0 {
			amount = quota / status.Data.QuotaPerUnit
			currency = "USD"
		}
	}
	return &UsageRecordSummary{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		TotalTokens:      totalTokens,
		Amount:           amount,
		Currency:         currency,
	}, nil
}

func (client *usageRecordClient) sub2Summary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	summaryQuery := url.Values{}
	for _, key := range []string{"start_date", "end_date", "timezone"} {
		if value := query.Get(key); value != "" {
			summaryQuery.Set(key, value)
		}
	}
	granularity := "day"
	start, startErr := time.Parse("2006-01-02", summaryQuery.Get("start_date"))
	end, endErr := time.Parse("2006-01-02", summaryQuery.Get("end_date"))
	if startErr == nil && endErr == nil && end.Sub(start) <= 7*24*time.Hour {
		granularity = "hour"
	}
	summaryQuery.Set("granularity", granularity)
	summaryQuery.Set("include_stats", "false")
	summaryQuery.Set("include_model_stats", "false")
	summaryQuery.Set("include_group_stats", "false")
	endpoint := "/api/v1/admin/dashboard/snapshot-v2"
	if credentialAccessScope(client.credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/v1/usage/dashboard/snapshot-v2"
		summaryQuery.Del("include_stats")
		summaryQuery.Set("include_trend", "true")
	}
	response, err := sub2APIDoJSON(ctx, client.connector, client.credential, http.MethodGet, endpoint+"?"+summaryQuery.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var payload struct {
		Code any `json:"code"`
		Data struct {
			Trend []struct {
				TotalTokens float64 `json:"total_tokens"`
				ActualCost  float64 `json:"actual_cost"`
			} `json:"trend"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !sub2SuccessCode(payload.Code) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	totalTokens := 0.0
	amount := 0.0
	for _, item := range payload.Data.Trend {
		totalTokens += item.TotalTokens
		amount += item.ActualCost
	}
	return &UsageRecordSummary{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		TotalTokens:      totalTokens,
		Amount:           amount,
		Currency:         "USD",
	}, nil
}

func decodeUsageRecordPage(kind string, body []byte) (*UsageRecordPage, error) {
	data := struct {
		Items    []json.RawMessage `json:"items"`
		Total    int64             `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
	}{}
	if kind == model.ManagedInstanceKindSub2API {
		var envelope struct {
			Code any             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || !sub2SuccessCode(envelope.Code) || json.Unmarshal(envelope.Data, &data) != nil {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
		}
	} else {
		var envelope struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Success || json.Unmarshal(envelope.Data, &data) != nil {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
		}
	}
	if data.Items == nil || data.Total < 0 || data.Page <= 0 || data.PageSize <= 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	return &UsageRecordPage{Items: data.Items, Total: data.Total, Page: data.Page, PageSize: data.PageSize}, nil
}

func normalizeUsageRecordQuery(kind string, input url.Values) (url.Values, error) {
	allowed := newAPIUsageQueryFields
	if kind == model.ManagedInstanceKindSub2API {
		allowed = sub2UsageQueryFields
	}
	query := make(url.Values)
	for key, values := range input {
		validator, ok := allowed[key]
		if !ok || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			continue
		}
		value := strings.TrimSpace(values[0])
		if len(value) > usageRecordMaxTextValue || !validator(value) {
			return nil, fmt.Errorf("%w: invalid usage record filter %s", ErrInvalidInstance, key)
		}
		query.Set(key, value)
	}
	page := integerValue(query.Get(pageField(kind)), 1)
	pageSize := integerValue(query.Get("page_size"), usageRecordPageSize)
	if page <= 0 || pageSize <= 0 || pageSize > usageRecordMaxPageSize {
		return nil, fmt.Errorf("%w: invalid usage record pagination", ErrInvalidInstance)
	}
	setUsageRecordPage(query, kind, page, pageSize)
	if err := validateUsageRecordRange(kind, query); err != nil {
		return nil, err
	}
	return query, nil
}

func validateUsageRecordRange(kind string, query url.Values) error {
	invalid := func() error {
		return fmt.Errorf("%w: usage record start must not be after end", ErrInvalidInstance)
	}
	if kind == model.ManagedInstanceKindSub2API {
		start, startErr := time.Parse("2006-01-02", query.Get("start_date"))
		end, endErr := time.Parse("2006-01-02", query.Get("end_date"))
		if startErr == nil && endErr == nil && start.After(end) {
			return invalid()
		}
		return nil
	}
	start, startErr := strconv.ParseInt(query.Get("start_timestamp"), 10, 64)
	end, endErr := strconv.ParseInt(query.Get("end_timestamp"), 10, 64)
	if startErr == nil && endErr == nil && start > end {
		return invalid()
	}
	return nil
}

func pageField(kind string) string {
	if kind == model.ManagedInstanceKindSub2API {
		return "page"
	}
	return "p"
}

func setUsageRecordPage(query url.Values, kind string, page int, pageSize int) {
	query.Del("p")
	query.Del("page")
	query.Set(pageField(kind), strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))
}

func integerValue(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}

type usageQueryValidator func(string) bool

var newAPIUsageQueryFields = map[string]usageQueryValidator{
	"p": positiveInteger, "page_size": positiveInteger, "type": integer,
	"username": textValue, "token_name": textValue, "model_name": textValue,
	"start_timestamp": nonNegativeInteger, "end_timestamp": nonNegativeInteger,
	"channel": nonNegativeInteger, "group": textValue, "request_id": textValue,
	"upstream_request_id": textValue, "proxy_id": nonNegativeInteger,
}

var sub2UsageQueryFields = map[string]usageQueryValidator{
	"page": positiveInteger, "page_size": positiveInteger,
	"user_id": positiveInteger, "api_key_id": positiveInteger, "account_id": positiveInteger, "group_id": positiveInteger,
	"model": textValue, "request_id": textValue,
	"request_type": oneOf("ws_v2", "live", "stream", "sync", "cyber"),
	"stream":       booleanValue, "billing_type": oneOf("0", "1"),
	"billing_mode":            oneOf("token", "per_request", "image", "video"),
	"upstream_model_mismatch": booleanValue, "start_date": dateValue, "end_date": dateValue,
	"timezone": timezoneValue, "sort_by": oneOf("created_at", "model", "id"),
	"sort_order": oneOf("asc", "desc"), "exact_total": booleanValue,
}

func textValue(value string) bool { return strings.TrimSpace(value) != "" }
func integer(value string) bool   { _, err := strconv.ParseInt(value, 10, 64); return err == nil }
func nonNegativeInteger(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed >= 0
}
func positiveInteger(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0
}
func booleanValue(value string) bool { _, err := strconv.ParseBool(value); return err == nil }
func dateValue(value string) bool    { _, err := time.Parse("2006-01-02", value); return err == nil }
func timezoneValue(value string) bool {
	_, err := time.LoadLocation(value)
	return err == nil
}
func oneOf(values ...string) usageQueryValidator {
	return func(value string) bool {
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
}

type usageCSVField struct {
	paths   [][]string
	derived string
}

func derivedField(name string) usageCSVField { return usageCSVField{derived: name} }

func field(paths ...string) usageCSVField {
	result := usageCSVField{}
	for _, path := range paths {
		result.paths = append(result.paths, strings.Split(path, "."))
	}
	return result
}

func usageCSVSchema(kind string) ([]string, []usageCSVField) {
	if kind == model.ManagedInstanceKindSub2API {
		return []string{"时间", "用户", "API Key", "账号", "请求模型", "上游模型", "响应模型", "上游模型不一致", "分组", "请求类型", "输入Token", "输出Token", "缓存读取Token", "缓存创建Token", "原始成本", "用户计费", "账号计费", "首字延迟(ms)", "耗时(ms)", "请求ID", "IP"}, []usageCSVField{
			field("created_at"), field("user.email", "user_id"), field("api_key.name", "api_key_id"), field("account.name", "account_id"),
			field("model"), field("upstream_model", "model"), field("upstream_response_model"), field("upstream_model_mismatch"), field("group.name", "group_id"), field("request_type"),
			field("input_tokens"), field("output_tokens"), field("cache_read_tokens"), field("cache_creation_tokens"), field("total_cost"), field("actual_cost"), derivedField("account_billed_cost"),
			field("first_token_ms"), field("duration_ms"), field("request_id"), field("ip_address"),
		}
	}
	return []string{"时间", "用户", "类型", "令牌", "模型", "渠道", "分组", "提示Token", "补全Token", "额度", "耗时(秒)", "流式", "请求ID", "上游请求ID", "内容"}, []usageCSVField{
		field("created_at"), field("username"), field("type"), field("token_name"), field("model_name"), field("channel_name", "channel"), field("group"),
		field("prompt_tokens"), field("completion_tokens"), field("quota"), field("use_time"), field("is_stream"), field("request_id"), field("upstream_request_id"), field("content"),
	}
}

func usageCSVRow(raw json.RawMessage, fields []usageCSVField) ([]string, error) {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	row := make([]string, 0, len(fields))
	for _, csvField := range fields {
		var value any
		if csvField.derived == "account_billed_cost" {
			value = sub2AccountBilledCost(item)
		} else {
			for _, path := range csvField.paths {
				value = nestedUsageValue(item, path)
				if value != nil && fmt.Sprint(value) != "" {
					break
				}
			}
		}
		row = append(row, usageCSVCell(value))
	}
	return row, nil
}

func sub2AccountBilledCost(item map[string]any) any {
	base, ok := usageNumber(item["account_stats_cost"])
	if !ok {
		base, ok = usageNumber(item["total_cost"])
	}
	if !ok {
		return nil
	}
	multiplier, ok := usageNumber(item["account_rate_multiplier"])
	if !ok {
		multiplier = 1
	}
	return strconv.FormatFloat(base*multiplier, 'f', 6, 64)
}

func usageNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}

func nestedUsageValue(item map[string]any, path []string) any {
	var current any = item
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func usageCSVCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		trimmed := strings.TrimLeft(typed, " \t\r\n")
		if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
			return "'" + typed
		}
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}
