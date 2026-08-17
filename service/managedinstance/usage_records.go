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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
)

const (
	usageRecordPageSize        = 20
	usageRecordMaxPageSize     = 100
	usageRecordExportLimit     = 1000000
	usageRecordMaxTextValue    = 512
	usageRecordMaxValues       = 20
	usageRecordMaxVariants     = 64
	usageRecordExportRetention = 30 * 24 * time.Hour
)

var ErrUsageExportTooLarge = errors.New("usage record export exceeds the 1000000 row limit")
var ErrUsageExportIncomplete = errors.New("usage record export ended before all rows were returned")

type UsageRecordPage struct {
	SourceInstanceID int64             `json:"source_instance_id"`
	Kind             string            `json:"kind"`
	Items            []json.RawMessage `json:"items"`
	Total            int64             `json:"total"`
	Page             int               `json:"page"`
	PageSize         int               `json:"page_size"`
	HasMore          bool              `json:"has_more"`
	TotalIsExact     bool              `json:"total_is_exact"`
	pages            int
}

type UsageRecordSummary struct {
	SourceInstanceID int64   `json:"source_instance_id"`
	Kind             string  `json:"kind"`
	TotalRequests    float64 `json:"total_requests"`
	TotalTokens      float64 `json:"total_tokens"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
}

type UsageRecordFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type UsageRecordFilterOptions struct {
	SourceInstanceID int64                                `json:"source_instance_id"`
	Kind             string                               `json:"kind"`
	Fields           map[string][]UsageRecordFilterOption `json:"fields"`
}

type usageRecordClient struct {
	instance         *model.ManagedInstance
	connector        *Connector
	credential       *CredentialMaterial
	conductorCacheMu sync.Mutex
	conductorCache   map[string][]json.RawMessage
}

type UsageRecordCSVExport struct {
	client *usageRecordClient
	query  url.Values
	first  *UsageRecordPage
}

type UsageRecordExportProgress struct {
	Progress  int    `json:"progress"`
	Processed int64  `json:"processed"`
	Total     int64  `json:"total"`
	Stage     string `json:"stage"`
}

type UsageRecordExportArtifact struct {
	FileName    string `json:"file_name"`
	RecordCount int    `json:"record_count"`
	Size        int64  `json:"size"`
	ExpiresAt   int64  `json:"expires_at"`
}

type UsageRecordExportProgressCallback func(UsageRecordExportProgress) error

func ListUsageRecords(ctx context.Context, instanceID int64, input url.Values) (*UsageRecordPage, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, err
	}
	query, err := normalizeUsageRecordQuery(client.instance.Kind, input)
	if err != nil {
		return nil, err
	}
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		query.Set("exact_total", "false")
	}
	return client.list(ctx, query)
}

func GetUsageRecordFilterOptions(ctx context.Context, instanceID int64, input url.Values) (*UsageRecordFilterOptions, error) {
	client, err := newUsageRecordClient(instanceID)
	if err != nil {
		return nil, err
	}
	query, err := normalizeUsageRecordQuery(client.instance.Kind, input)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{"start_timestamp": true, "end_timestamp": true, "start_date": true, "end_date": true, "timezone": true}
	for key := range query {
		if !keep[key] {
			query.Del(key)
		}
	}
	if client.instance.Kind == model.ManagedInstanceKindConductor {
		return conductorUsageFilterOptions(ctx, client, query)
	}
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		query.Set("exact_total", "false")
	}
	setUsageRecordPage(query, client.instance.Kind, 1, usageRecordMaxPageSize)
	page, err := client.listTarget(ctx, query)
	if err != nil {
		return nil, err
	}
	options := usageRecordOptionsFromPage(client.instance, page)
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		if models, modelErr := client.sub2ModelFilterOptions(ctx, query); modelErr == nil {
			options.Fields["model"] = mergeUsageRecordFilterOptions(options.Fields["model"], models)
		}
	}
	return options, nil
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
	return export.WriteWithProgress(ctx, writer, nil)
}

func (export *UsageRecordCSVExport) WriteWithProgress(ctx context.Context, writer io.Writer, onProgress UsageRecordExportProgressCallback) (int, error) {
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
	total := page.Total
	nextPage := 2
	if err := reportUsageRecordExportProgress(onProgress, written, total, "exporting"); err != nil {
		return written, err
	}
	for {
		if len(page.Items) == 0 && int64(written) < total {
			return written, ErrUsageExportIncomplete
		}
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
		if err := reportUsageRecordExportProgress(onProgress, written, total, "exporting"); err != nil {
			return written, err
		}
		if int64(written) >= total || written >= usageRecordExportLimit {
			break
		}
		setUsageRecordPage(export.query, export.client.instance.Kind, nextPage, usageRecordMaxPageSize)
		nextPage++
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
	if err := reportUsageRecordExportProgress(onProgress, written, total, "completed"); err != nil {
		return written, err
	}
	return written, nil
}

func reportUsageRecordExportProgress(callback UsageRecordExportProgressCallback, written int, total int64, stage string) error {
	if callback == nil {
		return nil
	}
	progress := 100
	if total > 0 && int64(written) < total {
		progress = int((int64(written) * 100) / total)
		if progress > 99 {
			progress = 99
		}
	}
	return callback(UsageRecordExportProgress{
		Progress: progress, Processed: int64(written), Total: total, Stage: stage,
	})
}

func ExportUsageRecordsCSVToTaskFile(ctx context.Context, instanceID int64, taskID string, input url.Values, onProgress UsageRecordExportProgressCallback) (*UsageRecordExportArtifact, error) {
	path, err := usageRecordExportTaskPath(taskID, false)
	if err != nil {
		return nil, err
	}
	temporaryPath, err := usageRecordExportTaskPath(taskID, true)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(temporaryPath)
	_ = os.Remove(path)
	export, err := PrepareUsageRecordsCSV(ctx, instanceID, input)
	if err != nil {
		return nil, err
	}
	temporary, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	count, exportErr := export.WriteWithProgress(ctx, temporary, onProgress)
	closeErr := temporary.Close()
	if exportErr != nil {
		_ = os.Remove(temporaryPath)
		return nil, exportErr
	}
	if closeErr != nil {
		_ = os.Remove(temporaryPath)
		return nil, closeErr
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &UsageRecordExportArtifact{
		FileName:    "usage-records-" + strconv.FormatInt(instanceID, 10) + "-" + time.Now().Format("20060102-150405") + ".csv",
		RecordCount: count,
		Size:        info.Size(),
		ExpiresAt:   time.Now().Add(usageRecordExportRetention).Unix(),
	}, nil
}

func OpenUsageRecordExportArtifact(taskID string) (*os.File, error) {
	path, err := usageRecordExportTaskPath(taskID, false)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func RemoveUsageRecordExportArtifact(taskID string) {
	if path, err := usageRecordExportTaskPath(taskID, false); err == nil {
		_ = os.Remove(path)
	}
	if path, err := usageRecordExportTaskPath(taskID, true); err == nil {
		_ = os.Remove(path)
	}
}

func usageRecordExportTaskPath(taskID string, temporary bool) (string, error) {
	if !strings.HasPrefix(taskID, "systask_") || strings.IndexFunc(taskID, func(character rune) bool {
		return character != '_' && character != '-' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z')
	}) >= 0 {
		return "", ErrInvalidInstance
	}
	directory := strings.TrimSpace(os.Getenv("MANAGED_USAGE_EXPORT_DIR"))
	if directory == "" {
		directory = filepath.Join(".", "exports", "usage-records")
	}
	extension := ".csv"
	if temporary {
		extension = ".part"
	}
	return filepath.Join(directory, "managed-usage-"+taskID+extension), nil
}

func CleanupStaleUsageRecordExportParts() {
	directory := strings.TrimSpace(os.Getenv("MANAGED_USAGE_EXPORT_DIR"))
	if directory == "" {
		directory = filepath.Join(".", "exports", "usage-records")
	}
	paths, _ := filepath.Glob(filepath.Join(directory, "managed-usage-systask_*.part"))
	cutoff := time.Now().Add(-2 * time.Hour)
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
	}
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
	if instance.Kind != model.ManagedInstanceKindNewAPI && instance.Kind != model.ManagedInstanceKindHuichuan && instance.Kind != model.ManagedInstanceKindSub2API && instance.Kind != model.ManagedInstanceKindConductor {
		return nil, ErrUnsupportedCapability
	}
	return &usageRecordClient{instance: instance, connector: connector, credential: credential}, nil
}

func (client *usageRecordClient) list(ctx context.Context, query url.Values) (*UsageRecordPage, error) {
	variants, err := usageRecordQueryVariants(query)
	if err != nil {
		return nil, err
	}
	if len(variants) == 1 {
		return client.listTarget(ctx, variants[0])
	}

	requestedPage := integerValue(query.Get(pageField(client.instance.Kind)), 1)
	requestedSize := integerValue(query.Get("page_size"), usageRecordPageSize)
	needed := requestedPage * requestedSize
	type variantResult struct {
		items        []json.RawMessage
		total        int64
		totalIsExact bool
		err          error
	}
	results := make([]variantResult, len(variants))
	semaphore := make(chan struct{}, 6)
	var group sync.WaitGroup
	for index, variant := range variants {
		group.Add(1)
		go func(index int, variant url.Values) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index].items, results[index].total, results[index].totalIsExact, results[index].err = client.collectUsageVariant(ctx, variant, needed)
		}(index, variant)
	}
	group.Wait()

	items := make([]json.RawMessage, 0, needed*len(variants))
	var total int64
	totalIsExact := true
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		items = append(items, result.items...)
		total += result.total
		totalIsExact = totalIsExact && result.totalIsExact
	}
	sortUsageRecordItems(items, client.instance.Kind, query.Get("sort_by"), query.Get("sort_order"))
	start := (requestedPage - 1) * requestedSize
	if start > len(items) {
		start = len(items)
	}
	end := start + requestedSize
	if end > len(items) {
		end = len(items)
	}
	return &UsageRecordPage{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		Items:            items[start:end], Total: total, Page: requestedPage, PageSize: requestedSize,
		HasMore: end < len(items) || total > int64(end), TotalIsExact: totalIsExact,
	}, nil
}

func (client *usageRecordClient) collectUsageVariant(ctx context.Context, query url.Values, needed int) ([]json.RawMessage, int64, bool, error) {
	items := make([]json.RawMessage, 0, needed)
	var total int64
	totalIsExact := true
	for pageNumber := 1; len(items) < needed; pageNumber++ {
		pageSize := needed - len(items)
		if pageSize > usageRecordMaxPageSize {
			pageSize = usageRecordMaxPageSize
		}
		pageQuery := cloneURLValues(query)
		setUsageRecordPage(pageQuery, client.instance.Kind, pageNumber, pageSize)
		page, err := client.listTarget(ctx, pageQuery)
		if err != nil {
			return nil, 0, false, err
		}
		total = page.Total
		totalIsExact = page.TotalIsExact
		items = append(items, page.Items...)
		if len(page.Items) == 0 || !page.HasMore {
			break
		}
	}
	return items, total, totalIsExact, nil
}

func (client *usageRecordClient) listTarget(ctx context.Context, query url.Values) (*UsageRecordPage, error) {
	if client.instance.Kind == model.ManagedInstanceKindConductor {
		page, err := conductorUsageRecordPage(ctx, client, query)
		if err != nil {
			return nil, err
		}
		page.TotalIsExact = true
		page.HasMore = page.Total > int64(page.Page*page.PageSize)
		return page, nil
	}
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
		response, err = newAPIDoJSON(ctx, client.connector, client.instance.Kind, client.credential, http.MethodGet, endpoint, nil)
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
	page.TotalIsExact = client.instance.Kind != model.ManagedInstanceKindSub2API || query.Get("exact_total") == "true"
	page.HasMore = page.pages > page.Page || page.Total > int64(page.Page*page.PageSize)
	return page, nil
}

func (client *usageRecordClient) summary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		if sub2StatsSupportsQuery(query) {
			summary, err := client.sub2StatsSummary(ctx, query)
			if err == nil {
				return summary, nil
			}
			if !sub2StatsFallbackAllowed(err) {
				return nil, err
			}
		}
		if hasUsageRecordDetailFilters(query) {
			return client.filteredSummary(ctx, query)
		}
		return client.sub2SnapshotSummary(ctx, query)
	}
	if hasUsageRecordDetailFilters(query) {
		return client.filteredSummary(ctx, query)
	}
	if client.instance.Kind == model.ManagedInstanceKindConductor {
		return client.filteredSummary(ctx, query)
	}
	return client.newAPISummary(ctx, query)
}

func hasUsageRecordDetailFilters(query url.Values) bool {
	ignored := map[string]bool{
		"p": true, "page": true, "page_size": true,
		"start_timestamp": true, "end_timestamp": true,
		"start_date": true, "end_date": true, "timezone": true,
		"sort_by": true, "sort_order": true, "exact_total": true,
	}
	for key, values := range query {
		if !ignored[key] && len(values) > 0 {
			return true
		}
	}
	return false
}

func (client *usageRecordClient) filteredSummary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	variants, err := usageRecordQueryVariants(query)
	if err != nil {
		return nil, err
	}
	totalTokens := 0.0
	totalRequests := 0.0
	amount := 0.0
	processed := 0
	for _, variant := range variants {
		variant.Del("exact_total")
		for pageNumber := 1; ; pageNumber++ {
			setUsageRecordPage(variant, client.instance.Kind, pageNumber, usageRecordMaxPageSize)
			page, err := client.listTarget(ctx, variant)
			if err != nil {
				return nil, err
			}
			if len(page.Items) == 0 {
				break
			}
			processed += len(page.Items)
			if processed > usageRecordExportLimit {
				return nil, ErrUsageExportTooLarge
			}
			for _, raw := range page.Items {
				totalRequests += usageRecordRequestCount(client.instance.Kind, raw)
				tokens, cost, err := usageRecordTotals(client.instance.Kind, raw)
				if err != nil {
					return nil, err
				}
				totalTokens += tokens
				amount += cost
			}
			if !page.HasMore {
				break
			}
		}
	}
	currency := "USD"
	if client.instance.Kind != model.ManagedInstanceKindSub2API && client.instance.Kind != model.ManagedInstanceKindConductor {
		amount, currency = client.newAPIQuotaAmount(ctx, amount)
	}
	return &UsageRecordSummary{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		TotalRequests:    totalRequests,
		TotalTokens:      totalTokens,
		Amount:           amount,
		Currency:         currency,
	}, nil
}

func usageRecordRequestCount(kind string, raw json.RawMessage) float64 {
	if kind != model.ManagedInstanceKindConductor {
		return 1
	}
	var item map[string]any
	if json.Unmarshal(raw, &item) != nil {
		return 0
	}
	requests, _ := usageNumber(item["requests"])
	return requests
}

func usageRecordTotals(kind string, raw json.RawMessage) (float64, float64, error) {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	value := func(key string) float64 {
		number, _ := usageNumber(item[key])
		return number
	}
	if kind == model.ManagedInstanceKindSub2API {
		tokens := value("input_tokens") + value("output_tokens") + value("cache_read_tokens") + value("cache_creation_tokens")
		return tokens, value("actual_cost"), nil
	}
	if kind == model.ManagedInstanceKindConductor {
		return value("total_tokens"), value("actual_cost"), nil
	}
	return value("prompt_tokens") + value("completion_tokens"), value("quota"), nil
}

func (client *usageRecordClient) newAPISummary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
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
	response, err := newAPIDoJSON(ctx, client.connector, client.instance.Kind, client.credential, http.MethodGet, endpoint+"?"+summaryQuery.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			Count     float64 `json:"count"`
			TokenUsed float64 `json:"token_used"`
			Quota     float64 `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !payload.Success {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}

	totalTokens := 0.0
	totalRequests := 0.0
	quota := 0.0
	for _, item := range payload.Data {
		totalRequests += item.Count
		totalTokens += item.TokenUsed
		quota += item.Quota
	}
	amount, currency := client.newAPIQuotaAmount(ctx, quota)
	return &UsageRecordSummary{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		TotalRequests:    totalRequests,
		TotalTokens:      totalTokens,
		Amount:           amount,
		Currency:         currency,
	}, nil
}

func (client *usageRecordClient) newAPIQuotaAmount(ctx context.Context, quota float64) (float64, string) {
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
	return amount, currency
}

func (client *usageRecordClient) sub2SnapshotSummary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
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
				Requests    float64 `json:"requests"`
				TotalTokens float64 `json:"total_tokens"`
				ActualCost  float64 `json:"actual_cost"`
			} `json:"trend"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !sub2SuccessCode(payload.Code) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	totalTokens := 0.0
	totalRequests := 0.0
	amount := 0.0
	for _, item := range payload.Data.Trend {
		totalRequests += item.Requests
		totalTokens += item.TotalTokens
		amount += item.ActualCost
	}
	return &UsageRecordSummary{
		SourceInstanceID: client.instance.Id,
		Kind:             client.instance.Kind,
		TotalRequests:    totalRequests,
		TotalTokens:      totalTokens,
		Amount:           amount,
		Currency:         "USD",
	}, nil
}

type sub2UsageStatsData struct {
	TotalRequests   float64 `json:"total_requests"`
	TotalTokens     float64 `json:"total_tokens"`
	TotalActualCost float64 `json:"total_actual_cost"`
}

func (client *usageRecordClient) sub2StatsSummary(ctx context.Context, query url.Values) (*UsageRecordSummary, error) {
	variants, err := usageRecordQueryVariants(query)
	if err != nil {
		return nil, err
	}
	result := &UsageRecordSummary{SourceInstanceID: client.instance.Id, Kind: client.instance.Kind, Currency: "USD"}
	for _, variant := range variants {
		stats, statsErr := fetchSub2UsageStats(ctx, client.connector, client.credential, variant)
		if statsErr != nil {
			return nil, statsErr
		}
		result.TotalRequests += stats.TotalRequests
		result.TotalTokens += stats.TotalTokens
		result.Amount += stats.TotalActualCost
	}
	return result, nil
}

func fetchSub2UsageStats(ctx context.Context, connector *Connector, credential *CredentialMaterial, query url.Values) (*sub2UsageStatsData, error) {
	statsQuery := cloneURLValues(query)
	for _, key := range []string{"p", "page", "page_size", "sort_by", "sort_order", "exact_total"} {
		statsQuery.Del(key)
	}
	endpoint := "/api/v1/admin/usage/stats"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/v1/usage/stats"
	}
	response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, endpoint+"?"+statsQuery.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var payload struct {
		Code any                `json:"code"`
		Data sub2UsageStatsData `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !sub2SuccessCode(payload.Code) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return &payload.Data, nil
}

func sub2StatsSupportsQuery(query url.Values) bool {
	for _, key := range []string{"group_id", "request_id"} {
		if len(query[key]) > 0 {
			return false
		}
	}
	return true
}

func sub2StatsFallbackAllowed(err error) bool {
	var probeErr *ProbeError
	if !errors.As(err, &probeErr) {
		return false
	}
	return probeErr.StatusCode == http.StatusNotFound || probeErr.StatusCode == http.StatusMethodNotAllowed || probeErr.StatusCode == http.StatusNotImplemented
}

func (client *usageRecordClient) sub2ModelFilterOptions(ctx context.Context, query url.Values) ([]UsageRecordFilterOption, error) {
	modelQuery := url.Values{"model_source": {"requested"}}
	for _, key := range []string{"start_date", "end_date", "timezone"} {
		if value := query.Get(key); value != "" {
			modelQuery.Set(key, value)
		}
	}
	endpoint := "/api/v1/admin/dashboard/models"
	if credentialAccessScope(client.credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/v1/usage/dashboard/models"
	}
	response, err := sub2APIDoJSON(ctx, client.connector, client.credential, http.MethodGet, endpoint+"?"+modelQuery.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var payload struct {
		Code any             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !sub2SuccessCode(payload.Code) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	type modelItem struct {
		Model string `json:"model"`
	}
	items := make([]modelItem, 0)
	if err := json.Unmarshal(payload.Data, &items); err != nil {
		var container struct {
			Models []modelItem `json:"models"`
		}
		if containerErr := json.Unmarshal(payload.Data, &container); containerErr != nil {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		items = container.Models
	}
	options := make([]UsageRecordFilterOption, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item.Model)
		if value != "" {
			options = append(options, UsageRecordFilterOption{Value: value, Label: value})
		}
	}
	return options, nil
}

func mergeUsageRecordFilterOptions(groups ...[]UsageRecordFilterOption) []UsageRecordFilterOption {
	values := map[string]string{}
	for _, group := range groups {
		for _, option := range group {
			if value := strings.TrimSpace(option.Value); value != "" {
				values[value] = option.Label
			}
		}
	}
	result := make([]UsageRecordFilterOption, 0, len(values))
	for value, label := range values {
		result = append(result, UsageRecordFilterOption{Value: value, Label: label})
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].Label < result[right].Label })
	return result
}

func decodeUsageRecordPage(kind string, body []byte) (*UsageRecordPage, error) {
	data := struct {
		Items    []json.RawMessage `json:"items"`
		Total    int64             `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
		Pages    int               `json:"pages"`
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
	return &UsageRecordPage{Items: data.Items, Total: data.Total, Page: data.Page, PageSize: data.PageSize, pages: data.Pages}, nil
}

func normalizeUsageRecordQuery(kind string, input url.Values) (url.Values, error) {
	allowed := newAPIUsageQueryFields
	if kind == model.ManagedInstanceKindSub2API {
		allowed = sub2UsageQueryFields
	} else if kind == model.ManagedInstanceKindConductor {
		allowed = conductorUsageQueryFields
	}
	query := make(url.Values)
	for key, values := range input {
		validator, ok := allowed[key]
		if !ok || len(values) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range values {
			value := strings.TrimSpace(raw)
			if value == "" || seen[value] {
				continue
			}
			if len(value) > usageRecordMaxTextValue || !validator(value) {
				return nil, fmt.Errorf("%w: invalid usage record filter %s", ErrInvalidInstance, key)
			}
			seen[value] = true
			query.Add(key, value)
			if len(query[key]) > usageRecordMaxValues {
				return nil, fmt.Errorf("%w: too many values for usage record filter %s", ErrInvalidInstance, key)
			}
		}
	}
	page := integerValue(query.Get(pageField(kind)), 1)
	pageSize := integerValue(query.Get("page_size"), usageRecordPageSize)
	if page <= 0 || pageSize <= 0 || pageSize > usageRecordMaxPageSize {
		return nil, fmt.Errorf("%w: invalid usage record pagination", ErrInvalidInstance)
	}
	setUsageRecordPage(query, kind, page, pageSize)
	if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
		location, _ := time.LoadLocation("Asia/Shanghai")
		now := time.Now().In(location)
		if kind == model.ManagedInstanceKindConductor && query.Get("end_date") == "" {
			query.Set("end_date", now.Format("2006-01-02"))
		}
		if kind == model.ManagedInstanceKindConductor && query.Get("start_date") == "" {
			query.Set("start_date", now.AddDate(0, 0, -1).Format("2006-01-02"))
		}
		if query.Get("timezone") == "" {
			query.Set("timezone", "Asia/Shanghai")
		}
	}
	if err := validateUsageRecordRange(kind, query); err != nil {
		return nil, err
	}
	return query, nil
}

// UnsupportedUsageRecordFilterFields returns fields that the target system
// cannot apply. Callers that calculate money must not silently drop them.
func UnsupportedUsageRecordFilterFields(kind string, input url.Values) []string {
	allowed := newAPIUsageQueryFields
	if kind == model.ManagedInstanceKindSub2API {
		allowed = sub2UsageQueryFields
	} else if kind == model.ManagedInstanceKindConductor {
		allowed = conductorUsageQueryFields
	}
	unsupported := make([]string, 0)
	for key, values := range input {
		if len(values) == 0 {
			continue
		}
		if _, exists := allowed[key]; !exists {
			unsupported = append(unsupported, key)
		}
	}
	sort.Strings(unsupported)
	return unsupported
}

func usageRecordQueryVariants(query url.Values) ([]url.Values, error) {
	variants := []url.Values{cloneURLValues(query)}
	for key, values := range query {
		if len(values) < 2 || !usageRecordMultiValueFields[key] {
			continue
		}
		if len(variants)*len(values) > usageRecordMaxVariants {
			return nil, fmt.Errorf("%w: usage record filter combinations exceed %d", ErrInvalidInstance, usageRecordMaxVariants)
		}
		next := make([]url.Values, 0, len(variants)*len(values))
		for _, variant := range variants {
			for _, value := range values {
				copy := cloneURLValues(variant)
				copy.Set(key, value)
				next = append(next, copy)
			}
		}
		variants = next
	}
	for _, variant := range variants {
		for key, values := range variant {
			if len(values) > 1 {
				variant.Set(key, values[0])
			}
		}
	}
	return variants, nil
}

func cloneURLValues(source url.Values) url.Values {
	copy := make(url.Values, len(source))
	for key, values := range source {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

func validateUsageRecordRange(kind string, query url.Values) error {
	invalid := func() error {
		return fmt.Errorf("%w: usage record start must not be after end", ErrInvalidInstance)
	}
	if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
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
	if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
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

var conductorUsageQueryFields = map[string]usageQueryValidator{
	"page": positiveInteger, "page_size": positiveInteger,
	"user_id": positiveInteger, "model": textValue,
	"start_date": dateValue, "end_date": dateValue, "timezone": timezoneValue,
	"sort_by":    oneOf("date", "created_at", "id", "user_id", "model", "requests", "total_tokens", "actual_cost"),
	"sort_order": oneOf("asc", "desc"),
}

var usageRecordMultiValueFields = map[string]bool{
	"type": true, "username": true, "token_name": true, "model_name": true,
	"channel": true, "group": true, "request_id": true, "upstream_request_id": true, "proxy_id": true,
	"user_id": true, "api_key_id": true, "account_id": true, "group_id": true, "model": true,
	"request_type": true, "stream": true, "billing_type": true, "billing_mode": true,
	"upstream_model_mismatch": true,
}

func sortUsageRecordItems(items []json.RawMessage, kind string, sortBy string, sortOrder string) {
	if sortBy == "" {
		sortBy = "created_at"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}
	sort.SliceStable(items, func(left int, right int) bool {
		leftValue := usageRecordSortValue(items[left], kind, sortBy)
		rightValue := usageRecordSortValue(items[right], kind, sortBy)
		comparison := compareUsageRecordValues(leftValue, rightValue)
		if sortOrder == "asc" {
			return comparison < 0
		}
		return comparison > 0
	})
}

func usageRecordSortValue(raw json.RawMessage, kind string, sortBy string) any {
	var item map[string]any
	if json.Unmarshal(raw, &item) != nil {
		return nil
	}
	if sortBy == "model" {
		if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
			return item["model"]
		}
		return item["model_name"]
	}
	return item[sortBy]
}

func compareUsageRecordValues(left any, right any) int {
	leftNumber, leftIsNumber := usageNumber(left)
	rightNumber, rightIsNumber := usageNumber(right)
	if leftIsNumber && rightIsNumber {
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	}
	return strings.Compare(strings.ToLower(fmt.Sprint(left)), strings.ToLower(fmt.Sprint(right)))
}

func usageRecordOptionsFromPage(instance *model.ManagedInstance, page *UsageRecordPage) *UsageRecordFilterOptions {
	fields := map[string]map[string]string{}
	add := func(field string, value any, label any) {
		normalized := strings.TrimSpace(fmt.Sprint(value))
		if normalized == "" || normalized == "<nil>" {
			return
		}
		if fields[field] == nil {
			fields[field] = map[string]string{}
		}
		display := strings.TrimSpace(fmt.Sprint(label))
		if display == "" || display == "<nil>" || display == normalized {
			display = normalized
		} else {
			display += " (#" + normalized + ")"
		}
		fields[field][normalized] = display
	}

	for _, raw := range page.Items {
		var item map[string]any
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if instance.Kind == model.ManagedInstanceKindSub2API {
			add("user_id", item["user_id"], nestedUsageValue(item, []string{"user", "email"}))
			add("api_key_id", item["api_key_id"], nestedUsageValue(item, []string{"api_key", "name"}))
			add("account_id", item["account_id"], nestedUsageValue(item, []string{"account", "name"}))
			add("group_id", item["group_id"], nestedUsageValue(item, []string{"group", "name"}))
			add("model", item["model"], item["model"])
			add("request_id", item["request_id"], item["request_id"])
			continue
		}
		if instance.Kind == model.ManagedInstanceKindConductor {
			add("user_id", item["user_id"], item["username"])
			add("model", item["model"], item["model"])
			continue
		}
		add("username", item["username"], item["username"])
		add("token_name", item["token_name"], item["token_name"])
		add("model_name", item["model_name"], item["model_name"])
		add("channel", item["channel"], item["channel_name"])
		add("group", item["group"], item["group"])
		add("request_id", item["request_id"], item["request_id"])
		add("upstream_request_id", item["upstream_request_id"], item["upstream_request_id"])
		add("proxy_id", item["proxy_id"], item["proxy_id"])
	}

	result := &UsageRecordFilterOptions{SourceInstanceID: instance.Id, Kind: instance.Kind, Fields: map[string][]UsageRecordFilterOption{}}
	for field, values := range fields {
		options := make([]UsageRecordFilterOption, 0, len(values))
		for value, label := range values {
			options = append(options, UsageRecordFilterOption{Value: value, Label: label})
		}
		sort.Slice(options, func(left int, right int) bool { return options[left].Label < options[right].Label })
		result.Fields[field] = options
	}
	return result
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
	if kind == model.ManagedInstanceKindConductor {
		return []string{"日期", "用户", "用户 ID", "模型", "请求数", "输入 Token", "输出 Token", "缓存读取 Token", "缓存创建 Token", "5 分钟缓存 Token", "1 小时缓存 Token", "总 Token", "消费金额 (USD)"}, []usageCSVField{
			field("date"), field("username"), field("user_id"), field("model"), field("requests"),
			field("input_tokens"), field("output_tokens"), field("cache_read_tokens"), field("cache_creation_tokens"),
			field("cache_5m_tokens"), field("cache_1h_tokens"), field("total_tokens"), field("actual_cost"),
		}
	}
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
