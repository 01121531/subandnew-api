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
	usersResponse, usersErr := conductorDoJSON(ctx, client.connector, client.credential, http.MethodGet, "/api/v1/users", nil)
	if usersErr == nil {
		usersData, decodeErr := conductorEnvelopeData(usersResponse)
		var users []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		}
		if decodeErr == nil && json.Unmarshal(usersData, &users) == nil {
			for _, user := range users {
				if user.ID <= 0 {
					continue
				}
				result.Fields["user_id"] = append(result.Fields["user_id"], UsageRecordFilterOption{Value: strconv.FormatInt(user.ID, 10), Label: fmt.Sprintf("%s (#%d)", user.Username, user.ID)})
			}
		}
	}
	prices, err := conductorPrices(ctx, client.connector, client.credential)
	if err == nil {
		for _, price := range prices {
			if strings.TrimSpace(price.Model) != "" {
				result.Fields["model"] = append(result.Fields["model"], UsageRecordFilterOption{Value: price.Model, Label: price.Model})
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
