package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

const (
	metricHistoryModePoint   = "point"
	metricHistoryModeSeries  = "series"
	metricHistoryModeSummary = "summary"
	metricHistoryMaxPoints   = 200
)

var metricHistoryDefinitions = map[string]struct {
	unit      string
	dailyOnly bool
}{
	"rpm":                {unit: "request/min"},
	"rpm_capacity":       {unit: "request/min"},
	"success_rate":       {unit: "ratio"},
	"concurrency_used":   {unit: "concurrency"},
	"concurrency_max":    {unit: "concurrency"},
	"accounts_available": {unit: "account"},
	"accounts_total":     {unit: "account"},
	"active_sessions":    {unit: "session"},
	"today_cost":         {unit: "USD"},
	"requests":           {unit: "request", dailyOnly: true},
	"tokens":             {unit: "token", dailyOnly: true},
	"actual_cost":        {unit: "USD", dailyOnly: true},
}

type metricHistoryInput struct {
	InstanceIDs   []int64  `json:"instance_ids,omitempty"`
	InstanceScope string   `json:"instance_scope,omitempty"`
	Metrics       []string `json:"metrics"`
	Mode          string   `json:"mode,omitempty"`
	StartAt       string   `json:"start_at,omitempty"`
	EndAt         string   `json:"end_at,omitempty"`
	PointAt       string   `json:"point_at,omitempty"`
	Granularity   string   `json:"granularity,omitempty"`
}

func (input metricHistoryInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	if len(input.Metrics) == 0 || len(input.Metrics) > len(metricHistoryDefinitions) {
		return errors.New("metrics must contain between 1 and 12 values")
	}
	seen := map[string]struct{}{}
	for _, metric := range input.Metrics {
		metric = strings.TrimSpace(strings.ToLower(metric))
		if _, ok := metricHistoryDefinitions[metric]; !ok {
			return fmt.Errorf("unsupported metric %q", metric)
		}
		if _, duplicate := seen[metric]; duplicate {
			return errors.New("metrics must not contain duplicates")
		}
		seen[metric] = struct{}{}
	}
	mode := strings.TrimSpace(strings.ToLower(input.Mode))
	if mode == "" {
		mode = metricHistoryModeSeries
	}
	if mode != metricHistoryModePoint && mode != metricHistoryModeSeries && mode != metricHistoryModeSummary {
		return errors.New("mode must be point, series, or summary")
	}
	if mode == metricHistoryModePoint {
		if strings.TrimSpace(input.PointAt) == "" || input.StartAt != "" || input.EndAt != "" {
			return errors.New("point mode requires only point_at")
		}
	} else if strings.TrimSpace(input.StartAt) == "" || strings.TrimSpace(input.EndAt) == "" || input.PointAt != "" {
		return errors.New("series and summary modes require start_at and end_at")
	}
	granularity := strings.TrimSpace(strings.ToLower(input.Granularity))
	if granularity != "" && granularity != "auto" && granularity != managedinstance.ConductorRPMBucketMinute && granularity != managedinstance.ConductorRPMBucketHour && granularity != managedinstance.ConductorRPMBucketDay {
		return errors.New("granularity must be auto, minute, hour, or day")
	}
	return nil
}

type metricHistoryPoint struct {
	timestamp int64
	Time      string              `json:"time"`
	Values    map[string]*float64 `json:"values"`
}

type metricHistoryStatistics struct {
	Count    int      `json:"count"`
	Minimum  *float64 `json:"minimum"`
	Maximum  *float64 `json:"maximum"`
	Average  *float64 `json:"average"`
	Sum      *float64 `json:"sum"`
	Latest   *float64 `json:"latest"`
	LatestAt string   `json:"latest_at,omitempty"`
}

type metricHistoryMetricStatus struct {
	Unit                 string  `json:"unit"`
	Status               string  `json:"status"`
	Aggregation          string  `json:"aggregation"`
	SupportedInstances   []int64 `json:"supported_instances,omitempty"`
	UnsupportedInstances []int64 `json:"unsupported_instances,omitempty"`
	MissingInstances     []int64 `json:"missing_instances,omitempty"`
}

type metricHistoryOutput struct {
	InstanceIDs   []int64                              `json:"instance_ids"`
	Instances     []instanceSummary                    `json:"instances"`
	Mode          string                               `json:"mode"`
	Granularity   string                               `json:"granularity"`
	StartAt       string                               `json:"start_at"`
	EndAt         string                               `json:"end_at"`
	Timezone      string                               `json:"timezone"`
	Points        []metricHistoryPoint                 `json:"points,omitempty"`
	Statistics    map[string]metricHistoryStatistics   `json:"statistics"`
	MetricStatus  map[string]metricHistoryMetricStatus `json:"metric_status"`
	Complete      bool                                 `json:"complete"`
	ObservedAt    string                               `json:"observed_at,omitempty"`
	TimeSemantics string                               `json:"time_semantics,omitempty"`
}

type metricHistoryQuery struct {
	mode        string
	granularity string
	start       int64
	end         int64
}

func registerMetricHistory(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_metric_history", Version: "v1",
		Description: "查询有权实例过去 31 天的历史指标。支持指定时间点、区间趋势和区间统计；所有时间按 Asia/Shanghai 解释。",
		Permission:  tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:        tool.RiskMedium, ReadOnly: true, Idempotent: true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]},"metrics":{"type":"array","minItems":1,"maxItems":12,"uniqueItems":true,"items":{"type":"string","enum":["rpm","rpm_capacity","success_rate","concurrency_used","concurrency_max","accounts_available","accounts_total","active_sessions","today_cost","requests","tokens","actual_cost"]}},"mode":{"type":"string","enum":["point","series","summary"]},"start_at":{"type":"string"},"end_at":{"type":"string"},"point_at":{"type":"string"},"granularity":{"type":"string","enum":["auto","minute","hour","day"]}},"required":["metrics"],"additionalProperties":false}`),
	}, func(ctx context.Context, execution tool.ExecutionContext, input metricHistoryInput) (tool.Output[metricHistoryOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[metricHistoryOutput]{}, err
		}
		query, err := normalizeMetricHistoryQuery(input)
		if err != nil {
			return tool.Output[metricHistoryOutput]{}, err
		}
		metrics := normalizedMetricNames(input.Metrics)
		instances, err := metricHistoryInstances(ctx, db, resolution.IDs)
		if err != nil {
			return tool.Output[metricHistoryOutput]{}, err
		}
		result, observedAt, err := queryMetricHistory(ctx, db, resolution.IDs, metrics, query)
		if err != nil {
			return tool.Output[metricHistoryOutput]{}, err
		}
		result.Instances = instances
		result.InstanceIDs = append([]int64(nil), resolution.IDs...)
		for metric, status := range result.MetricStatus {
			if status.Status != "no_data" || metricSupportedByAnyInstance(metric, instances) {
				continue
			}
			status.Status = "unsupported"
			result.MetricStatus[metric] = status
		}
		result.ObservedAt = assistantTime(observedAt)
		provenance := []tool.Provenance{{Source: "managed_rpm_history", ObservedAt: unixTime(observedAt)}}
		if containsDailyMetric(metrics) {
			provenance = append(provenance, tool.Provenance{Source: "managed_dashboard_snapshots", ObservedAt: unixTime(observedAt)})
		}
		freshness := freshnessForSnapshot(observedAt, !result.Complete)
		return tool.Output[metricHistoryOutput]{Data: result, Provenance: provenance, Freshness: freshness}, nil
	})
}

func normalizeMetricHistoryQuery(input metricHistoryInput) (metricHistoryQuery, error) {
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = metricHistoryModeSeries
	}
	granularity := strings.ToLower(strings.TrimSpace(input.Granularity))
	if granularity == "" {
		granularity = "auto"
	}
	dailyOnly := containsDailyMetric(normalizedMetricNames(input.Metrics))
	var start, end int64
	var pointDateOnly, startDateOnly, endDateOnly bool
	var err error
	if mode == metricHistoryModePoint {
		var point time.Time
		point, pointDateOnly, err = parseAssistantHistoryTime(input.PointAt, true)
		if err != nil {
			return metricHistoryQuery{}, err
		}
		start, end = point.Unix(), point.Unix()
	} else {
		var startTime, endTime time.Time
		startTime, startDateOnly, err = parseAssistantHistoryTime(input.StartAt, false)
		if err == nil {
			endTime, endDateOnly, err = parseAssistantHistoryTime(input.EndAt, true)
		}
		if err != nil {
			return metricHistoryQuery{}, err
		}
		start, end = startTime.Unix(), endTime.Unix()
	}
	if start <= 0 || end < start || end-start > int64(31*24*time.Hour/time.Second) {
		return metricHistoryQuery{}, errors.New("history range must be valid and no longer than 31 days")
	}
	if granularity == "auto" {
		duration := time.Duration(end-start) * time.Second
		switch {
		case dailyOnly || pointDateOnly || duration > 200*time.Hour:
			granularity = managedinstance.ConductorRPMBucketDay
		case duration > 200*time.Minute:
			granularity = managedinstance.ConductorRPMBucketHour
		default:
			granularity = managedinstance.ConductorRPMBucketMinute
		}
	}
	if dailyOnly && granularity != managedinstance.ConductorRPMBucketDay {
		return metricHistoryQuery{}, errors.New("requests, tokens, and actual_cost require day granularity")
	}
	if dailyOnly && mode != metricHistoryModePoint && !(startDateOnly && endDateOnly) {
		startTime := time.Unix(start, 0).In(assistantLocation)
		endTime := time.Unix(end, 0).In(assistantLocation)
		if startTime.Hour() != 0 || startTime.Minute() != 0 || startTime.Second() != 0 || endTime.Hour() != 23 || endTime.Minute() != 59 || endTime.Second() != 59 {
			return metricHistoryQuery{}, errors.New("daily metrics require complete Asia/Shanghai natural days")
		}
	}
	if estimatedHistoryPoints(start, end, granularity) > metricHistoryMaxPoints {
		return metricHistoryQuery{}, errors.New("selected granularity would return more than 200 points")
	}
	if mode == metricHistoryModePoint {
		start = historyBucketStart(start, granularity)
		end = historyBucketEnd(start, granularity)
	}
	return metricHistoryQuery{mode: mode, granularity: granularity, start: start, end: end}, nil
}

func parseAssistantHistoryTime(raw string, endOfDate bool) (time.Time, bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false, errors.New("history time is required")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(assistantLocation), false, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, assistantLocation); err == nil {
			return parsed, false, nil
		}
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, assistantLocation)
	if err != nil {
		return time.Time{}, false, errors.New("time must be RFC3339 or YYYY-MM-DD[ HH:mm[:ss]]")
	}
	if endOfDate {
		parsed = parsed.Add(24*time.Hour - time.Second)
	}
	return parsed, true, nil
}

func estimatedHistoryPoints(start, end int64, granularity string) int {
	seconds := int64(60)
	if granularity == managedinstance.ConductorRPMBucketHour {
		seconds = 3600
	} else if granularity == managedinstance.ConductorRPMBucketDay {
		seconds = 86400
	}
	return int((end-start)/seconds) + 1
}

func historyBucketStart(timestamp int64, granularity string) int64 {
	if granularity == managedinstance.ConductorRPMBucketMinute {
		return timestamp - timestamp%60
	}
	if granularity == managedinstance.ConductorRPMBucketHour {
		return timestamp - timestamp%3600
	}
	local := time.Unix(timestamp, 0).In(assistantLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, assistantLocation).Unix()
}

func historyBucketEnd(start int64, granularity string) int64 {
	if granularity == managedinstance.ConductorRPMBucketMinute {
		return start + 59
	}
	if granularity == managedinstance.ConductorRPMBucketHour {
		return start + 3599
	}
	return time.Unix(start, 0).In(assistantLocation).AddDate(0, 0, 1).Add(-time.Second).Unix()
}

func normalizedMetricNames(metrics []string) []string {
	result := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		result = append(result, strings.ToLower(strings.TrimSpace(metric)))
	}
	return result
}

func containsDailyMetric(metrics []string) bool {
	for _, metric := range metrics {
		if metricHistoryDefinitions[metric].dailyOnly {
			return true
		}
	}
	return false
}

func metricHistoryInstances(ctx context.Context, db *gorm.DB, ids []int64) ([]instanceSummary, error) {
	var instances []model.ManagedInstance
	if err := db.WithContext(ctx).Where("id IN ?", ids).Order("id asc").Find(&instances).Error; err != nil {
		return nil, err
	}
	result := make([]instanceSummary, 0, len(instances))
	for _, instance := range instances {
		result = append(result, instanceSummary{ID: instance.Id, Name: instance.Name, Kind: instance.Kind, Status: instance.Status})
	}
	return result, nil
}

func queryMetricHistory(ctx context.Context, db *gorm.DB, ids []int64, metrics []string, query metricHistoryQuery) (metricHistoryOutput, int64, error) {
	result := metricHistoryOutput{
		Mode: query.mode, Granularity: query.granularity,
		StartAt:  time.Unix(query.start, 0).In(assistantLocation).Format(time.RFC3339),
		EndAt:    time.Unix(query.end, 0).In(assistantLocation).Format(time.RFC3339),
		Timezone: assistantTimezone, Statistics: map[string]metricHistoryStatistics{},
		MetricStatus: map[string]metricHistoryMetricStatus{}, Complete: true,
	}
	if containsDailyMetric(metrics) {
		result.TimeSemantics = "china_natural_day"
	}
	instances, err := metricHistoryInstances(ctx, db, ids)
	if err != nil {
		return metricHistoryOutput{}, 0, err
	}
	instanceKinds := make(map[int64]string, len(instances))
	for _, instance := range instances {
		instanceKinds[instance.ID] = instance.Kind
	}
	points := map[int64]map[string]*float64{}
	observedAt := int64(0)
	realtimeMetrics := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		definition := metricHistoryDefinitions[metric]
		aggregation := "average"
		if metric == "accounts_available" || metric == "accounts_total" || metric == "concurrency_used" || metric == "concurrency_max" || metric == "active_sessions" || metric == "today_cost" {
			aggregation = "period_end"
		} else if definition.dailyOnly {
			aggregation = "daily_total"
		}
		result.MetricStatus[metric] = metricHistoryMetricStatus{Unit: definition.unit, Status: "no_data", Aggregation: aggregation}
		if !definition.dailyOnly {
			realtimeMetrics = append(realtimeMetrics, metric)
		}
		supported, unsupported := splitMetricInstanceSupport(metric, ids, instanceKinds)
		status := result.MetricStatus[metric]
		status.SupportedInstances = supported
		status.UnsupportedInstances = unsupported
		result.MetricStatus[metric] = status
	}
	for _, metric := range realtimeMetrics {
		status := result.MetricStatus[metric]
		if len(status.SupportedInstances) == 0 {
			continue
		}
		history, err := managedinstance.GetManagedRPMHistory(ctx, status.SupportedInstances, query.granularity, query.start, query.end)
		if err != nil {
			return metricHistoryOutput{}, 0, err
		}
		for _, point := range history.Points {
			values := ensureMetricPoint(points, point.Timestamp, metrics)
			values[metric] = realtimeHistoryValue(point, metric)
			if point.Timestamp > observedAt {
				observedAt = point.Timestamp
			}
		}
		if err := mergeRealtimePeriodEndHistory(ctx, db, status.SupportedInstances, []string{metric}, query, points); err != nil {
			return metricHistoryOutput{}, 0, err
		}
	}
	if containsDailyMetric(metrics) {
		dailyIDs := supportedMetricInstanceUnion(metrics, result.MetricStatus)
		dashboardObservedAt, err := mergeDashboardHistory(ctx, db, dailyIDs, metrics, query, points)
		if err != nil {
			return metricHistoryOutput{}, 0, err
		}
		if dashboardObservedAt > observedAt {
			observedAt = dashboardObservedAt
		}
	}
	coverage, err := loadMetricHistoryCoverage(ctx, db, ids, metrics, query, instanceKinds)
	if err != nil {
		return metricHistoryOutput{}, 0, err
	}
	missingByMetric := enforceMetricHistoryCoverage(points, metrics, query, result.MetricStatus, coverage)
	timestamps := make([]int64, 0, len(points))
	for timestamp := range points {
		timestamps = append(timestamps, timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	for _, timestamp := range timestamps {
		result.Points = append(result.Points, metricHistoryPoint{
			timestamp: timestamp, Time: assistantTime(timestamp), Values: points[timestamp],
		})
	}
	if query.mode == metricHistoryModePoint && len(result.Points) > 1 {
		result.Points = result.Points[len(result.Points)-1:]
	}
	for _, metric := range metrics {
		statistics := calculateMetricHistoryStatistics(result.Points, metric)
		result.Statistics[metric] = statistics
		status := result.MetricStatus[metric]
		expectedPoints := estimatedHistoryPoints(query.start, query.end, query.granularity)
		if query.mode == metricHistoryModePoint {
			expectedPoints = 1
		}
		status.MissingInstances = missingByMetric[metric]
		switch {
		case len(status.SupportedInstances) == 0:
			status.Status = model.ManagedInstanceCollectionUnsupported
			result.Complete = false
		case statistics.Count == expectedPoints && len(status.UnsupportedInstances) == 0 && len(status.MissingInstances) == 0:
			status.Status = "succeeded"
		case statistics.Count > 0:
			status.Status = "partial"
			result.Complete = false
		default:
			if len(status.UnsupportedInstances) > 0 || (len(status.MissingInstances) > 0 && len(status.MissingInstances) < len(status.SupportedInstances)) {
				status.Status = "partial"
			}
			result.Complete = false
		}
		result.MetricStatus[metric] = status
	}
	if query.mode == metricHistoryModeSummary {
		result.Points = nil
	}
	return result, observedAt, nil
}

func mergeRealtimePeriodEndHistory(ctx context.Context, db *gorm.DB, ids []int64, metrics []string, query metricHistoryQuery, points map[int64]map[string]*float64) error {
	periodEndMetrics := map[string]struct{}{
		"accounts_available": {}, "accounts_total": {}, "concurrency_used": {}, "concurrency_max": {},
		"active_sessions": {}, "today_cost": {},
	}
	if query.mode == metricHistoryModePoint {
		periodEndMetrics["rpm"] = struct{}{}
		periodEndMetrics["rpm_capacity"] = struct{}{}
		periodEndMetrics["success_rate"] = struct{}{}
	}
	selected := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		if _, ok := periodEndMetrics[metric]; ok {
			selected = append(selected, metric)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	var rows []model.ManagedRPMHistory
	if err := db.WithContext(ctx).Where("instance_id IN ? AND bucket_start >= ? AND bucket_start <= ?", ids, query.start-query.start%60, query.end).
		Order("bucket_start asc").Find(&rows).Error; err != nil {
		return err
	}
	type lastValue struct {
		bucketStart int64
		value       float64
	}
	byPeriod := map[int64]map[string]map[int64]lastValue{}
	for _, row := range rows {
		period := historyBucketStart(row.BucketStart, query.granularity)
		byMetric := byPeriod[period]
		if byMetric == nil {
			byMetric = map[string]map[int64]lastValue{}
			byPeriod[period] = byMetric
		}
		for _, metric := range selected {
			value, ok := managedHistoryRowValue(row, metric)
			if !ok {
				continue
			}
			byInstance := byMetric[metric]
			if byInstance == nil {
				byInstance = map[int64]lastValue{}
				byMetric[metric] = byInstance
			}
			previous, exists := byInstance[row.InstanceID]
			if !exists || row.BucketStart >= previous.bucketStart {
				byInstance[row.InstanceID] = lastValue{bucketStart: row.BucketStart, value: value}
			}
		}
	}
	for timestamp, byMetric := range byPeriod {
		values := ensureMetricPoint(points, timestamp, metrics)
		for _, metric := range selected {
			byInstance := byMetric[metric]
			if len(byInstance) != len(ids) {
				continue
			}
			total := 0.0
			for _, item := range byInstance {
				total += item.value
			}
			if metric == "success_rate" && len(byInstance) > 0 {
				total /= float64(len(byInstance))
			}
			value := total
			values[metric] = &value
		}
	}
	return nil
}

func managedHistoryRowValue(row model.ManagedRPMHistory, metric string) (float64, bool) {
	switch metric {
	case "rpm":
		return row.RPMLast, row.SampleCount > 0
	case "rpm_capacity":
		return row.CapacityLast, row.CapacitySampleCount > 0
	case "success_rate":
		return row.SuccessRateLast, row.SuccessRateSampleCount > 0
	case "accounts_available":
		return float64(row.AccountsAvailableLast), row.AccountSampleCount > 0
	case "accounts_total":
		return float64(row.AccountsTotalLast), row.AccountSampleCount > 0
	case "concurrency_used":
		return row.ConcurrencyUsedLast, row.ConcurrencyUsedSamples > 0
	case "concurrency_max":
		return row.ConcurrencyMaxLast, row.ConcurrencyMaxSamples > 0
	case "today_cost":
		return row.TodayCostLast, row.TodayCostSampleCount > 0
	case "active_sessions":
		return float64(row.ActiveSessionsLast), row.ActiveSessionSamples > 0
	default:
		return 0, false
	}
}

type metricHistoryCoverage map[string]map[int64]map[int64]struct{}

func splitMetricInstanceSupport(metric string, ids []int64, kinds map[int64]string) ([]int64, []int64) {
	supported := make([]int64, 0, len(ids))
	unsupported := make([]int64, 0)
	for _, id := range ids {
		if metricSupportedByInstance(metric, kinds[id]) {
			supported = append(supported, id)
		} else {
			unsupported = append(unsupported, id)
		}
	}
	return supported, unsupported
}

func metricSupportedByInstance(metric, kind string) bool {
	switch metric {
	case "rpm", "requests", "tokens", "actual_cost":
		return kind != "" && kind != model.ManagedInstanceKindGeneric
	case "rpm_capacity":
		return kind == model.ManagedInstanceKindConductor
	case "success_rate":
		return kind == model.ManagedInstanceKindClaudeGateway
	case "concurrency_used", "concurrency_max":
		return kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindClaudeGateway
	case "accounts_available", "accounts_total":
		return kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor || kind == model.ManagedInstanceKindClaudeGateway
	case "active_sessions":
		return kind == model.ManagedInstanceKindConductor || kind == model.ManagedInstanceKindClaudeGateway
	case "today_cost":
		return kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor || kind == model.ManagedInstanceKindClaudeGateway
	default:
		return false
	}
}

func supportedMetricInstanceUnion(metrics []string, statuses map[string]metricHistoryMetricStatus) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0)
	for _, metric := range metrics {
		if !metricHistoryDefinitions[metric].dailyOnly {
			continue
		}
		for _, id := range statuses[metric].SupportedInstances {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result
}

func addMetricCoverage(coverage metricHistoryCoverage, metric string, timestamp, instanceID int64) {
	byTime := coverage[metric]
	if byTime == nil {
		byTime = map[int64]map[int64]struct{}{}
		coverage[metric] = byTime
	}
	instances := byTime[timestamp]
	if instances == nil {
		instances = map[int64]struct{}{}
		byTime[timestamp] = instances
	}
	instances[instanceID] = struct{}{}
}

func loadMetricHistoryCoverage(ctx context.Context, db *gorm.DB, ids []int64, metrics []string, query metricHistoryQuery, kinds map[int64]string) (metricHistoryCoverage, error) {
	coverage := metricHistoryCoverage{}
	if len(ids) == 0 {
		return coverage, nil
	}
	if !containsOnlyDailyMetrics(metrics) {
		var rows []model.ManagedRPMHistory
		if err := db.WithContext(ctx).Where("instance_id IN ? AND bucket_start >= ? AND bucket_start <= ?", ids, query.start-query.start%60, query.end).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			timestamp := historyBucketStart(row.BucketStart, query.granularity)
			for _, metric := range metrics {
				if metricHistoryDefinitions[metric].dailyOnly || !metricSupportedByInstance(metric, kinds[row.InstanceID]) {
					continue
				}
				if _, ok := managedHistoryRowValue(row, metric); ok {
					addMetricCoverage(coverage, metric, timestamp, row.InstanceID)
				}
			}
		}
	}
	if containsDailyMetric(metrics) {
		var snapshots []model.ManagedDashboardSnapshot
		if err := db.WithContext(ctx).Where("instance_id IN ? AND range_key = ? AND observed_at > 0", ids, "preset-30").Find(&snapshots).Error; err != nil {
			return nil, err
		}
		for _, snapshot := range snapshots {
			var summary managedinstance.SummaryResult
			if json.Unmarshal([]byte(snapshot.Payload), &summary) != nil {
				continue
			}
			for _, trend := range summary.Trend {
				day, err := time.ParseInLocation("2006-01-02", trend.Date, assistantLocation)
				if err != nil || day.Unix() < historyBucketStart(query.start, managedinstance.ConductorRPMBucketDay) || day.Unix() > query.end {
					continue
				}
				for _, metric := range metrics {
					if metricHistoryDefinitions[metric].dailyOnly && metricSupportedByInstance(metric, kinds[snapshot.InstanceID]) {
						addMetricCoverage(coverage, metric, day.Unix(), snapshot.InstanceID)
					}
				}
			}
		}
	}
	return coverage, nil
}

func containsOnlyDailyMetrics(metrics []string) bool {
	for _, metric := range metrics {
		if !metricHistoryDefinitions[metric].dailyOnly {
			return false
		}
	}
	return true
}

func enforceMetricHistoryCoverage(points map[int64]map[string]*float64, metrics []string, query metricHistoryQuery, statuses map[string]metricHistoryMetricStatus, coverage metricHistoryCoverage) map[string][]int64 {
	missing := make(map[string][]int64, len(metrics))
	timestamps := expectedHistoryTimestamps(query)
	for _, timestamp := range timestamps {
		ensureMetricPoint(points, timestamp, metrics)
	}
	for _, metric := range metrics {
		status := statuses[metric]
		missingSet := map[int64]struct{}{}
		for _, timestamp := range timestamps {
			present := coverage[metric][timestamp]
			for _, id := range status.SupportedInstances {
				if _, ok := present[id]; !ok {
					missingSet[id] = struct{}{}
				}
			}
			if len(present) != len(status.SupportedInstances) {
				points[timestamp][metric] = nil
			}
		}
		for _, id := range status.SupportedInstances {
			if _, ok := missingSet[id]; ok {
				missing[metric] = append(missing[metric], id)
			}
		}
	}
	return missing
}

func expectedHistoryTimestamps(query metricHistoryQuery) []int64 {
	start := historyBucketStart(query.start, query.granularity)
	end := historyBucketStart(query.end, query.granularity)
	result := make([]int64, 0, estimatedHistoryPoints(query.start, query.end, query.granularity))
	for current := start; current <= end; {
		result = append(result, current)
		if query.granularity == managedinstance.ConductorRPMBucketDay {
			current = time.Unix(current, 0).In(assistantLocation).AddDate(0, 0, 1).Unix()
		} else if query.granularity == managedinstance.ConductorRPMBucketHour {
			current += 3600
		} else {
			current += 60
		}
	}
	return result
}

func metricSupportedByAnyInstance(metric string, instances []instanceSummary) bool {
	for _, instance := range instances {
		switch metric {
		case "rpm", "requests", "tokens", "actual_cost":
			if instance.Kind != model.ManagedInstanceKindGeneric {
				return true
			}
		case "rpm_capacity":
			if instance.Kind == model.ManagedInstanceKindConductor {
				return true
			}
		case "success_rate":
			if instance.Kind == model.ManagedInstanceKindClaudeGateway {
				return true
			}
		case "concurrency_used", "concurrency_max":
			if instance.Kind == model.ManagedInstanceKindSub2API || instance.Kind == model.ManagedInstanceKindClaudeGateway {
				return true
			}
		case "accounts_available", "accounts_total":
			if instance.Kind == model.ManagedInstanceKindSub2API || instance.Kind == model.ManagedInstanceKindConductor || instance.Kind == model.ManagedInstanceKindClaudeGateway {
				return true
			}
		case "active_sessions":
			if instance.Kind == model.ManagedInstanceKindConductor || instance.Kind == model.ManagedInstanceKindClaudeGateway {
				return true
			}
		case "today_cost":
			if instance.Kind == model.ManagedInstanceKindSub2API || instance.Kind == model.ManagedInstanceKindConductor || instance.Kind == model.ManagedInstanceKindClaudeGateway {
				return true
			}
		}
	}
	return false
}

func ensureMetricPoint(points map[int64]map[string]*float64, timestamp int64, metrics []string) map[string]*float64 {
	values := points[timestamp]
	if values == nil {
		values = make(map[string]*float64, len(metrics))
		for _, metric := range metrics {
			values[metric] = nil
		}
		points[timestamp] = values
	}
	return values
}

func realtimeHistoryValue(point managedinstance.ConductorRPMHistoryPoint, metric string) *float64 {
	switch metric {
	case "rpm":
		if point.Samples > 0 && point.RPMComplete {
			value := point.RPM
			return &value
		}
	case "rpm_capacity":
		return point.Capacity
	case "success_rate":
		return point.SuccessRate
	case "concurrency_used":
		return point.ConcurrencyUsed
	case "concurrency_max":
		return point.ConcurrencyMax
	case "accounts_available":
		if point.AccountsAvailable != nil {
			value := float64(*point.AccountsAvailable)
			return &value
		}
	case "accounts_total":
		if point.AccountsTotal != nil {
			value := float64(*point.AccountsTotal)
			return &value
		}
	case "active_sessions":
		if point.ActiveSessions != nil {
			value := float64(*point.ActiveSessions)
			return &value
		}
	case "today_cost":
		return point.TodayCost
	}
	return nil
}

func mergeDashboardHistory(ctx context.Context, db *gorm.DB, ids []int64, metrics []string, query metricHistoryQuery, points map[int64]map[string]*float64) (int64, error) {
	var snapshots []model.ManagedDashboardSnapshot
	if err := db.WithContext(ctx).Where("instance_id IN ? AND range_key = ? AND observed_at > 0", ids, "preset-30").Find(&snapshots).Error; err != nil {
		return 0, err
	}
	observedAt := int64(0)
	for _, snapshot := range snapshots {
		var summary managedinstance.SummaryResult
		if json.Unmarshal([]byte(snapshot.Payload), &summary) != nil {
			continue
		}
		if snapshot.ObservedAt > observedAt {
			observedAt = snapshot.ObservedAt
		}
		for _, trend := range summary.Trend {
			day, err := time.ParseInLocation("2006-01-02", trend.Date, assistantLocation)
			if err != nil || day.Unix() < historyBucketStart(query.start, managedinstance.ConductorRPMBucketDay) || day.Unix() > query.end {
				continue
			}
			values := ensureMetricPoint(points, day.Unix(), metrics)
			for _, metric := range metrics {
				var value float64
				switch metric {
				case "requests":
					value = trend.Requests
				case "tokens":
					value = trend.Tokens
				case "actual_cost":
					value = trend.Cost
				default:
					continue
				}
				if values[metric] == nil {
					values[metric] = new(float64)
				}
				*values[metric] += value
			}
		}
	}
	return observedAt, nil
}

func calculateMetricHistoryStatistics(points []metricHistoryPoint, metric string) metricHistoryStatistics {
	result := metricHistoryStatistics{}
	var sum float64
	for _, point := range points {
		value := point.Values[metric]
		if value == nil {
			continue
		}
		if result.Count == 0 || *value < *result.Minimum {
			minimum := *value
			result.Minimum = &minimum
		}
		if result.Count == 0 || *value > *result.Maximum {
			maximum := *value
			result.Maximum = &maximum
		}
		sum += *value
		latest := *value
		result.Latest = &latest
		result.LatestAt = point.Time
		result.Count++
	}
	if result.Count > 0 {
		average, total := sum/float64(result.Count), sum
		result.Average, result.Sum = &average, &total
	}
	return result
}
