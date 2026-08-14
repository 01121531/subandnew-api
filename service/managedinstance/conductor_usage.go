package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
)

type conductorPrice struct {
	ID                   int64   `json:"id"`
	Model                string  `json:"model"`
	InputPricePerM       float64 `json:"input_price_per_m"`
	OutputPricePerM      float64 `json:"output_price_per_m"`
	CacheReadPricePerM   float64 `json:"cache_read_price_per_m"`
	CacheCreatePricePerM float64 `json:"cache_create_price_per_m"`
	Note                 string  `json:"note"`
}

type conductorUsageMetrics struct {
	Requests            float64 `json:"requests"`
	InputTokens         float64 `json:"input_tokens"`
	OutputTokens        float64 `json:"output_tokens"`
	CacheReadTokens     float64 `json:"cache_read_tokens"`
	CacheCreationTokens float64 `json:"cache_creation_tokens"`
	Cache5MTokens       float64 `json:"cache_5m_tokens"`
	Cache1HTokens       float64 `json:"cache_1h_tokens"`
}

type conductorUsagePayload struct {
	Labels map[string]string                                      `json:"labels"`
	Total  int                                                    `json:"total"`
	Usage  map[string]map[string]map[string]conductorUsageMetrics `json:"usage"`
}

type conductorUsageRow struct {
	ID                  int64   `json:"id"`
	Date                string  `json:"date"`
	CreatedAt           string  `json:"created_at"`
	UserID              int64   `json:"user_id"`
	Username            string  `json:"username"`
	Model               string  `json:"model"`
	Requests            float64 `json:"requests"`
	InputTokens         float64 `json:"input_tokens"`
	OutputTokens        float64 `json:"output_tokens"`
	CacheReadTokens     float64 `json:"cache_read_tokens"`
	CacheCreationTokens float64 `json:"cache_creation_tokens"`
	Cache5MTokens       float64 `json:"cache_5m_tokens"`
	Cache1HTokens       float64 `json:"cache_1h_tokens"`
	TotalTokens         float64 `json:"total_tokens"`
	ActualCost          float64 `json:"actual_cost"`
}

type conductorUsageAggregate struct {
	Requests float64
	Tokens   float64
	Cost     float64
	Trend    []UsageTrendPoint
}

type conductorReportMetrics struct {
	Requests            float64 `json:"requests"`
	InputTokens         float64 `json:"input_tokens"`
	OutputTokens        float64 `json:"output_tokens"`
	CacheReadTokens     float64 `json:"cache_read_tokens"`
	CacheCreationTokens float64 `json:"cache_creation_tokens"`
	Cache5MTokens       float64 `json:"cache_5m_tokens"`
	Cache1HTokens       float64 `json:"cache_1h_tokens"`
	Cost                float64 `json:"cost"`
}

func (metrics conductorReportMetrics) totalTokens() float64 {
	return metrics.InputTokens + metrics.OutputTokens + metrics.CacheReadTokens + metrics.CacheCreationTokens
}

type conductorReportRow struct {
	conductorReportMetrics
	AccountID string `json:"account_id"`
	Label     string `json:"label"`
	Email     string `json:"email"`
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Model     string `json:"model"`
	Date      string `json:"date"`
	KeyID     int64  `json:"key_id"`
	KeyName   string `json:"key_name"`
}

type conductorReportPayload struct {
	Rows    []conductorReportRow   `json:"rows"`
	Summary conductorReportMetrics `json:"summary"`
	Total   int                    `json:"total"`
}

type ConductorKeyUsageResult struct {
	SourceInstanceID int64      `json:"source_instance_id"`
	Kind             string     `json:"kind"`
	Window           TimeWindow `json:"window"`
	Timezone         string     `json:"timezone"`
	KeyID            int64      `json:"key_id"`
	KeyName          string     `json:"key_name"`
	UserID           int64      `json:"user_id"`
	Username         string     `json:"username"`
	TotalRequests    float64    `json:"total_requests"`
	TotalTokens      float64    `json:"total_tokens"`
	Amount           float64    `json:"amount"`
	Currency         string     `json:"currency"`
}

func conductorUsageReport(ctx context.Context, connector *Connector, credential *CredentialMaterial, groupBy, from, to, timezone, search string) (*conductorReportPayload, error) {
	query := url.Values{
		"group_by": {groupBy},
		"from":     {from},
		"to":       {to},
		"tz":       {timezone},
		"search":   {search},
	}
	path := "/api/v1/reports/usage?" + query.Encode()
	var response *ConnectorResponse
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		response, err = conductorDoJSON(ctx, connector, credential, http.MethodGet, path, nil)
		if err == nil && response != nil && response.StatusCode != http.StatusBadGateway && response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusGatewayTimeout {
			break
		}
		if attempt == 0 {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var payload conductorReportPayload
	if json.Unmarshal(data, &payload) != nil || payload.Total < 0 || payload.Total > 0 && payload.Rows == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return &payload, nil
}

func CollectConductorKeyUsage(ctx context.Context, instanceID int64, keyID int64, window TimeWindow, timezone string) (*ConductorKeyUsageResult, error) {
	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	if instance.Kind != model.ManagedInstanceKindConductor || keyID <= 0 {
		return nil, ErrInvalidInstance
	}
	if window.End == 0 {
		window.End = time.Now().Unix()
	}
	if window.Start == 0 {
		window.Start = window.End - 7*86400
	}
	if window.Start < 0 || window.Start >= window.End {
		return nil, ErrInvalidInstance
	}
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, ErrInvalidInstance
	}
	query := url.Values{
		"group_by": {"total"}, "key_id": {strconv.FormatInt(keyID, 10)},
		"from": {time.Unix(window.Start, 0).In(location).Format("2006-01-02")},
		"to":   {time.Unix(window.End, 0).In(location).Format("2006-01-02")}, "tz": {timezone},
	}
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/reports/keys?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var payload conductorReportPayload
	if json.Unmarshal(data, &payload) != nil || payload.Total < 0 || len(payload.Rows) > 1 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	row := conductorReportRow{KeyID: keyID}
	if len(payload.Rows) == 1 {
		row = payload.Rows[0]
	}
	return &ConductorKeyUsageResult{
		SourceInstanceID: instance.Id, Kind: instance.Kind, Window: window, Timezone: timezone,
		KeyID: keyID, KeyName: row.KeyName, UserID: row.UserID, Username: row.Username,
		TotalRequests: row.Requests, TotalTokens: row.totalTokens(), Amount: row.Cost, Currency: "USD",
	}, nil
}

func conductorPrices(ctx context.Context, connector *Connector, credential *CredentialMaterial) ([]conductorPrice, error) {
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/prices", nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var prices []conductorPrice
	if json.Unmarshal(data, &prices) != nil || prices == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return prices, nil
}

func conductorUsageRows(ctx context.Context, connector *Connector, credential *CredentialMaterial, from, to, timezone, userFilter, modelFilter string) ([]conductorUsageRow, error) {
	prices, err := conductorPrices(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	priceByModel := make(map[string]conductorPrice, len(prices))
	for _, price := range prices {
		priceByModel[price.Model] = price
	}

	const limit = 100
	allLabels := map[string]string{}
	allUsage := map[string]map[string]map[string]conductorUsageMetrics{}
	for offset := 0; ; offset += limit {
		query := url.Values{
			"from": {from}, "to": {to}, "timezone": {timezone},
			"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "group_by": {"model"},
		}
		if userFilter != "" {
			query.Set("user_id", userFilter)
		} else {
			query.Set("user_id", "all")
		}
		response, requestErr := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/usage?"+query.Encode(), nil)
		if requestErr != nil {
			return nil, requestErr
		}
		data, decodeErr := conductorEnvelopeData(response)
		if decodeErr != nil {
			return nil, decodeErr
		}
		var payload conductorUsagePayload
		if json.Unmarshal(data, &payload) != nil || payload.Usage == nil || payload.Total < 0 {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		for id, label := range payload.Labels {
			allLabels[id] = label
		}
		for id, dates := range payload.Usage {
			allUsage[id] = dates
		}
		if userFilter != "" || len(payload.Usage) == 0 || offset+limit >= payload.Total {
			break
		}
		if offset+limit >= managedInstanceInventoryMaxItems {
			return nil, ErrUsageExportTooLarge
		}
	}

	userIDs := sortedMapKeys(allUsage)
	rows := make([]conductorUsageRow, 0)
	for _, userKey := range userIDs {
		userID, _ := strconv.ParseInt(userKey, 10, 64)
		dates := allUsage[userKey]
		for _, date := range sortedMapKeys(dates) {
			models := dates[date]
			for _, modelName := range sortedMapKeys(models) {
				if modelName == "_total" || (modelFilter != "" && modelName != modelFilter) {
					continue
				}
				metrics := models[modelName]
				price, hasPrice := priceByModel[modelName]
				if !hasPrice {
					return nil, fmt.Errorf("conductor price is missing for model %q", modelName)
				}
				totalTokens := metrics.InputTokens + metrics.OutputTokens + metrics.CacheReadTokens + metrics.CacheCreationTokens
				cost := (metrics.InputTokens*price.InputPricePerM + metrics.OutputTokens*price.OutputPricePerM + metrics.CacheReadTokens*price.CacheReadPricePerM + metrics.CacheCreationTokens*price.CacheCreatePricePerM) / 1_000_000
				rows = append(rows, conductorUsageRow{
					Date: date, CreatedAt: date, UserID: userID, Username: allLabels[userKey], Model: modelName,
					Requests: metrics.Requests, InputTokens: metrics.InputTokens, OutputTokens: metrics.OutputTokens,
					CacheReadTokens: metrics.CacheReadTokens, CacheCreationTokens: metrics.CacheCreationTokens,
					Cache5MTokens: metrics.Cache5MTokens, Cache1HTokens: metrics.Cache1HTokens,
					TotalTokens: totalTokens, ActualCost: cost,
				})
			}
		}
	}
	for index := range rows {
		rows[index].ID = int64(index + 1)
	}
	return rows, nil
}

func conductorUsageAggregateForWindow(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*conductorUsageAggregate, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.UTC
	}
	from := time.Unix(window.Start, 0).In(location).Format("2006-01-02")
	to := time.Unix(window.End, 0).In(location).Format("2006-01-02")
	if report, reportErr := conductorUsageReport(ctx, connector, credential, "date", from, to, location.String(), ""); reportErr == nil {
		result := &conductorUsageAggregate{
			Requests: report.Summary.Requests,
			Tokens:   report.Summary.totalTokens(),
			Cost:     report.Summary.Cost,
		}
		for _, row := range report.Rows {
			if row.Date == "" {
				continue
			}
			result.Trend = append(result.Trend, UsageTrendPoint{
				Date: row.Date, Requests: row.Requests, Tokens: row.totalTokens(), Cost: row.Cost,
			})
		}
		sort.Slice(result.Trend, func(left, right int) bool { return result.Trend[left].Date < result.Trend[right].Date })
		return result, nil
	}
	rows, err := conductorUsageRows(ctx, connector, credential, from, to, location.String(), "", "")
	if err != nil {
		return nil, err
	}
	byDate := map[string]*UsageTrendPoint{}
	result := &conductorUsageAggregate{}
	for _, row := range rows {
		result.Requests += row.Requests
		result.Tokens += row.TotalTokens
		result.Cost += row.ActualCost
		point := byDate[row.Date]
		if point == nil {
			point = &UsageTrendPoint{Date: row.Date}
			byDate[row.Date] = point
		}
		point.Requests += row.Requests
		point.Tokens += row.TotalTokens
		point.Cost += row.ActualCost
	}
	for _, date := range sortedMapKeys(byDate) {
		result.Trend = append(result.Trend, *byDate[date])
	}
	return result, nil
}

func conductorUsageRecordPage(ctx context.Context, client *usageRecordClient, query url.Values) (*UsageRecordPage, error) {
	items, err := conductorUsageRecordItems(ctx, client, query)
	if err != nil {
		return nil, err
	}
	sortUsageRecordItems(items, client.instance.Kind, query.Get("sort_by"), query.Get("sort_order"))
	page := integerValue(query.Get("page"), 1)
	pageSize := integerValue(query.Get("page_size"), usageRecordPageSize)
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return &UsageRecordPage{SourceInstanceID: client.instance.Id, Kind: client.instance.Kind, Items: items[start:end], Total: int64(len(items)), Page: page, PageSize: pageSize}, nil
}

func conductorUsageRecordItems(ctx context.Context, client *usageRecordClient, query url.Values) ([]json.RawMessage, error) {
	cacheQuery := cloneURLValues(query)
	for _, key := range []string{"page", "p", "page_size", "sort_by", "sort_order"} {
		cacheQuery.Del(key)
	}
	cacheKey := cacheQuery.Encode()
	client.conductorCacheMu.Lock()
	defer client.conductorCacheMu.Unlock()
	if cached, ok := client.conductorCache[cacheKey]; ok {
		return append([]json.RawMessage(nil), cached...), nil
	}
	rows, err := conductorUsageRows(ctx, client.connector, client.credential, query.Get("start_date"), query.Get("end_date"), query.Get("timezone"), query.Get("user_id"), query.Get("model"))
	if err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		raw, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return nil, marshalErr
		}
		items = append(items, raw)
	}
	if client.conductorCache == nil {
		client.conductorCache = map[string][]json.RawMessage{}
	}
	client.conductorCache[cacheKey] = append([]json.RawMessage(nil), items...)
	return items, nil
}

func conductorUsageFilterOptions(ctx context.Context, client *usageRecordClient, query url.Values) (*UsageRecordFilterOptions, error) {
	result := &UsageRecordFilterOptions{SourceInstanceID: client.instance.Id, Kind: client.instance.Kind, Fields: map[string][]UsageRecordFilterOption{}}
	timezone := query.Get("timezone")
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	ownerReport, ownerErr := conductorUsageReport(ctx, client.connector, client.credential, "owner", query.Get("start_date"), query.Get("end_date"), timezone, "")
	if ownerErr == nil {
		for _, owner := range ownerReport.Rows {
			if owner.UserID > 0 {
				result.Fields["user_id"] = append(result.Fields["user_id"], UsageRecordFilterOption{Value: strconv.FormatInt(owner.UserID, 10), Label: fmt.Sprintf("%s (#%d)", owner.Username, owner.UserID)})
			}
		}
	} else {
		usersResponse, usersErr := conductorDoJSON(ctx, client.connector, client.credential, http.MethodGet, "/api/v1/users", nil)
		if usersErr == nil {
			usersData, decodeErr := conductorEnvelopeData(usersResponse)
			var users []struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			}
			if decodeErr == nil && json.Unmarshal(usersData, &users) == nil {
				for _, user := range users {
					if user.ID > 0 {
						result.Fields["user_id"] = append(result.Fields["user_id"], UsageRecordFilterOption{Value: strconv.FormatInt(user.ID, 10), Label: fmt.Sprintf("%s (#%d)", user.Username, user.ID)})
					}
				}
			}
		}
	}
	modelReport, modelErr := conductorUsageReport(ctx, client.connector, client.credential, "model", query.Get("start_date"), query.Get("end_date"), timezone, "")
	if modelErr == nil {
		for _, modelRow := range modelReport.Rows {
			if modelName := strings.TrimSpace(modelRow.Model); modelName != "" {
				result.Fields["model"] = append(result.Fields["model"], UsageRecordFilterOption{Value: modelName, Label: modelName})
			}
		}
	} else if prices, priceErr := conductorPrices(ctx, client.connector, client.credential); priceErr == nil {
		for _, price := range prices {
			if modelName := strings.TrimSpace(price.Model); modelName != "" {
				result.Fields["model"] = append(result.Fields["model"], UsageRecordFilterOption{Value: modelName, Label: modelName})
			}
		}
	}
	for field := range result.Fields {
		sort.Slice(result.Fields[field], func(left, right int) bool {
			return result.Fields[field][left].Label < result.Fields[field][right].Label
		})
	}
	return result, nil
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
