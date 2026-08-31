package builtin

import (
	"context"
	"encoding/json"
	"errors"
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
	queryMetricsPeriodRealtime  = "realtime"
	queryMetricsPeriodToday     = "today"
	queryMetricsPeriodYesterday = "yesterday"
	queryMetricsPeriodLast7     = "last_7_days"
	queryMetricsPeriodLast14    = "last_14_days"
	queryMetricsPeriodLast30    = "last_30_days"
	queryMetricsPeriodCustom    = "custom"
	queryMetricsPeriodPoint     = "point"

	queryMetricsModeSummary = "summary"
	queryMetricsModeSeries  = "series"
	queryMetricsModePoint   = "point"
)

var queryMetricDefinitions = map[string]string{
	"cost":               "actual_cost",
	"requests":           "requests",
	"tokens":             "tokens",
	"rpm":                "rpm",
	"rpm_capacity":       "rpm_capacity",
	"success_rate":       "success_rate",
	"concurrency_used":   "concurrency_used",
	"concurrency_max":    "concurrency_max",
	"accounts_available": "accounts_available",
	"accounts_total":     "accounts_total",
	"active_sessions":    "active_sessions",
}

type queryMetricsInput struct {
	InstanceIDs   []int64  `json:"instance_ids,omitempty"`
	InstanceScope string   `json:"instance_scope,omitempty"`
	Metrics       []string `json:"metrics"`
	Period        string   `json:"period"`
	Mode          string   `json:"mode,omitempty"`
	StartAt       string   `json:"start_at,omitempty"`
	EndAt         string   `json:"end_at,omitempty"`
	PointAt       string   `json:"point_at,omitempty"`
	Granularity   string   `json:"granularity,omitempty"`
}

func (input queryMetricsInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	if len(input.Metrics) == 0 || len(input.Metrics) > len(queryMetricDefinitions) {
		return errors.New("metrics must contain between 1 and 11 values")
	}
	seen := make(map[string]struct{}, len(input.Metrics))
	for _, raw := range input.Metrics {
		metric := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := queryMetricDefinitions[metric]; !ok {
			return errors.New("unsupported metric " + strconv.Quote(metric))
		}
		if _, duplicate := seen[metric]; duplicate {
			return errors.New("metrics must not contain duplicates")
		}
		seen[metric] = struct{}{}
	}
	period := strings.ToLower(strings.TrimSpace(input.Period))
	switch period {
	case queryMetricsPeriodRealtime, queryMetricsPeriodToday, queryMetricsPeriodYesterday,
		queryMetricsPeriodLast7, queryMetricsPeriodLast14, queryMetricsPeriodLast30,
		queryMetricsPeriodCustom, queryMetricsPeriodPoint:
	default:
		return errors.New("unsupported period")
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = queryMetricsModeSummary
		if period == queryMetricsPeriodPoint {
			mode = queryMetricsModePoint
		}
	}
	if mode != queryMetricsModeSummary && mode != queryMetricsModeSeries && mode != queryMetricsModePoint {
		return errors.New("mode must be summary, series, or point")
	}
	if period == queryMetricsPeriodRealtime && mode != queryMetricsModeSummary {
		return errors.New("realtime period only supports summary mode")
	}
	if period == queryMetricsPeriodPoint {
		if mode != queryMetricsModePoint || strings.TrimSpace(input.PointAt) == "" || input.StartAt != "" || input.EndAt != "" {
			return errors.New("point period requires point mode and point_at")
		}
	} else if mode == queryMetricsModePoint || input.PointAt != "" {
		return errors.New("point mode requires point period")
	}
	if period == queryMetricsPeriodCustom {
		if strings.TrimSpace(input.StartAt) == "" || strings.TrimSpace(input.EndAt) == "" {
			return errors.New("custom period requires start_at and end_at")
		}
	} else if input.StartAt != "" || input.EndAt != "" {
		return errors.New("start_at and end_at require custom period")
	}
	granularity := strings.ToLower(strings.TrimSpace(input.Granularity))
	if granularity != "" && granularity != "auto" && granularity != managedinstance.ConductorRPMBucketMinute && granularity != managedinstance.ConductorRPMBucketHour && granularity != managedinstance.ConductorRPMBucketDay {
		return errors.New("granularity must be auto, minute, hour, or day")
	}
	return nil
}

type queryMetricsWindow struct {
	StartAt  string `json:"start_at,omitempty"`
	EndAt    string `json:"end_at,omitempty"`
	PointAt  string `json:"point_at,omitempty"`
	Timezone string `json:"timezone"`
}

type queryMetricUnitValue struct {
	Unit  string   `json:"unit"`
	Value *float64 `json:"value,omitempty"`
}

type queryMetricResult struct {
	Value       *float64               `json:"value,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	Values      []queryMetricUnitValue `json:"values,omitempty"`
	Status      string                 `json:"status"`
	Aggregation string                 `json:"aggregation"`
	Count       int                    `json:"count,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	Average     *float64               `json:"average,omitempty"`
	Sum         *float64               `json:"sum,omitempty"`
	Latest      *float64               `json:"latest,omitempty"`
	LatestAt    string                 `json:"latest_at,omitempty"`
	ObservedAt  string                 `json:"observed_at,omitempty"`
}

type queryMetricsPoint struct {
	Time   string              `json:"time"`
	Values map[string]*float64 `json:"values"`
}

type queryMetricsOutput struct {
	InstanceIDs     []int64                      `json:"instance_ids"`
	Instances       []instanceSummary            `json:"instances"`
	SelectionSource string                       `json:"selection_source"`
	DefaultFallback bool                         `json:"default_fallback"`
	Period          string                       `json:"period"`
	Mode            string                       `json:"mode"`
	Granularity     string                       `json:"granularity,omitempty"`
	Window          queryMetricsWindow           `json:"window"`
	Metrics         map[string]queryMetricResult `json:"metrics"`
	Points          []queryMetricsPoint          `json:"points,omitempty"`
	Complete        bool                         `json:"complete"`
	ObservedAt      string                       `json:"observed_at,omitempty"`
	Sources         []string                     `json:"sources"`
}

type queryMetricsRange struct {
	start int64
	end   int64
	point int64
}

func registerQueryMetrics(registry *tool.Registry, db *gorm.DB) error {
	schema := json.RawMessage(`{"type":"object","properties":{"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]},"metrics":{"type":"array","minItems":1,"maxItems":11,"uniqueItems":true,"items":{"type":"string","enum":["cost","requests","tokens","rpm","rpm_capacity","success_rate","concurrency_used","concurrency_max","accounts_available","accounts_total","active_sessions"]}},"period":{"type":"string","enum":["realtime","today","yesterday","last_7_days","last_14_days","last_30_days","custom","point"]},"mode":{"type":"string","enum":["summary","series","point"]},"start_at":{"type":"string"},"end_at":{"type":"string"},"point_at":{"type":"string"},"granularity":{"type":"string","enum":["auto","minute","hour","day"]}},"required":["metrics","period"],"additionalProperties":false}`)
	return tool.Register(registry, tool.ToolSpec{
		Name: "query_metrics", Version: "v1",
		Description: "统一查询实例指标。period 直接使用 realtime、today、yesterday、last_7_days、last_14_days、last_30_days、custom 或 point；服务端按 Asia/Shanghai 解析时间和默认实例，无需先查询当前时间或实例。",
		Permission:  tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:        tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: schema,
	}, func(ctx context.Context, execution tool.ExecutionContext, input queryMetricsInput) (tool.Output[queryMetricsOutput], error) {
		return executeQueryMetrics(ctx, db, execution, input, time.Now().In(assistantLocation))
	})
}

func executeQueryMetrics(ctx context.Context, db *gorm.DB, execution tool.ExecutionContext, input queryMetricsInput, now time.Time) (tool.Output[queryMetricsOutput], error) {
	resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
	if err != nil {
		return tool.Output[queryMetricsOutput]{}, err
	}
	metrics := normalizedQueryMetrics(input.Metrics)
	period := strings.ToLower(strings.TrimSpace(input.Period))
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = queryMetricsModeSummary
		if period == queryMetricsPeriodPoint {
			mode = queryMetricsModePoint
		}
	}
	instances, err := metricHistoryInstances(ctx, db, resolution.IDs)
	if err != nil {
		return tool.Output[queryMetricsOutput]{}, err
	}
	window, queryRange, err := normalizeQueryMetricsRange(input, now)
	if err != nil {
		return tool.Output[queryMetricsOutput]{}, err
	}
	output := queryMetricsOutput{
		InstanceIDs: append([]int64(nil), resolution.IDs...), Instances: instances,
		SelectionSource: resolution.Source, DefaultFallback: resolution.Fallback,
		Period: period, Mode: mode, Window: window, Metrics: make(map[string]queryMetricResult, len(metrics)), Complete: true,
	}
	if period == queryMetricsPeriodRealtime {
		return queryRealtimeMetrics(ctx, resolution.IDs, instances, metrics, output)
	}

	provenance := make([]tool.Provenance, 0, len(resolution.IDs)+2)
	sources := make(map[string]struct{})
	observedAt := int64(0)
	stale := false
	historyMetrics := append([]string(nil), metrics...)
	if mode == queryMetricsModeSummary {
		if presetDays := queryMetricsPresetDays(period); presetDays > 0 {
			dashboardMetrics, remaining := splitDashboardMetrics(metrics)
			historyMetrics = remaining
			if len(dashboardMetrics) > 0 {
				results, dashboardObservedAt, dashboardStale, dashboardProvenance, dashboardErr := queryDashboardMetricSummary(resolution.IDs, dashboardMetrics, presetDays)
				if dashboardErr != nil {
					return tool.Output[queryMetricsOutput]{}, dashboardErr
				}
				for metric, result := range results {
					output.Metrics[metric] = result
					output.Complete = output.Complete && result.Status == model.ManagedInstanceCollectionSucceeded
				}
				observedAt = conservativeTimestamp(observedAt, dashboardObservedAt)
				stale = stale || dashboardStale
				provenance = append(provenance, dashboardProvenance...)
				sources["managed_dashboard_snapshots"] = struct{}{}
			}
		}
	}
	if len(historyMetrics) > 0 {
		historyInput := metricHistoryInput{
			InstanceIDs: resolution.IDs, Metrics: queryHistoryMetricNames(historyMetrics), Mode: mode,
			Granularity: input.Granularity,
		}
		if mode == queryMetricsModePoint {
			historyInput.PointAt = assistantTime(queryRange.point)
		} else {
			historyInput.StartAt = assistantTime(queryRange.start)
			historyInput.EndAt = assistantTime(queryRange.end)
		}
		historyQuery, historyErr := normalizeMetricHistoryQuery(historyInput)
		if historyErr != nil {
			return tool.Output[queryMetricsOutput]{}, historyErr
		}
		history, historyObservedAt, historyErr := queryMetricHistory(ctx, db, resolution.IDs, historyInput.Metrics, historyQuery)
		if historyErr != nil {
			return tool.Output[queryMetricsOutput]{}, historyErr
		}
		for historyMetric, status := range history.MetricStatus {
			if status.Status == "no_data" && !metricSupportedByAnyInstance(historyMetric, instances) {
				status.Status = model.ManagedInstanceCollectionUnsupported
				history.MetricStatus[historyMetric] = status
			}
		}
		for metric, result := range queryMetricResultsFromHistory(historyMetrics, history) {
			output.Metrics[metric] = result
			output.Complete = output.Complete && result.Status == model.ManagedInstanceCollectionSucceeded
		}
		output.Granularity = history.Granularity
		output.Points = queryMetricPointsFromHistory(metrics, history.Points)
		observedAt = conservativeTimestamp(observedAt, historyObservedAt)
		stale = stale || !history.Complete
		if queryHistoryUsesRealtimeStore(historyInput.Metrics) {
			sources["managed_rpm_history"] = struct{}{}
			provenance = append(provenance, tool.Provenance{Source: "managed_rpm_history", ObservedAt: unixTime(historyObservedAt)})
		}
		if containsDailyMetric(historyInput.Metrics) {
			sources["managed_dashboard_snapshots"] = struct{}{}
			provenance = append(provenance, tool.Provenance{Source: "managed_dashboard_snapshots", ObservedAt: unixTime(historyObservedAt)})
		}
	}
	for _, metric := range metrics {
		if _, ok := output.Metrics[metric]; !ok {
			output.Metrics[metric] = queryMetricResult{Status: "no_data", Aggregation: queryMetricAggregation(metric, mode)}
			output.Complete = false
		}
	}
	output.ObservedAt = assistantTime(observedAt)
	output.Sources = sortedQueryMetricSources(sources)
	if len(provenance) == 0 {
		provenance = []tool.Provenance{{Source: "managed_metrics"}}
	}
	return tool.Output[queryMetricsOutput]{Data: output, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, stale || !output.Complete)}, nil
}

func normalizeQueryMetricsRange(input queryMetricsInput, now time.Time) (queryMetricsWindow, queryMetricsRange, error) {
	now = now.In(assistantLocation)
	period := strings.ToLower(strings.TrimSpace(input.Period))
	window := queryMetricsWindow{Timezone: assistantTimezone}
	rangeValue := queryMetricsRange{}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, assistantLocation)
	switch period {
	case queryMetricsPeriodRealtime:
		window.StartAt, window.EndAt = now.Format(time.RFC3339), now.Format(time.RFC3339)
		rangeValue.start, rangeValue.end = now.Unix(), now.Unix()
	case queryMetricsPeriodToday:
		rangeValue.start, rangeValue.end = dayStart.Unix(), now.Unix()
	case queryMetricsPeriodYesterday:
		start := dayStart.AddDate(0, 0, -1)
		rangeValue.start, rangeValue.end = start.Unix(), dayStart.Add(-time.Second).Unix()
	case queryMetricsPeriodLast7, queryMetricsPeriodLast14, queryMetricsPeriodLast30:
		days := map[string]int{queryMetricsPeriodLast7: 7, queryMetricsPeriodLast14: 14, queryMetricsPeriodLast30: 30}[period]
		rangeValue.start, rangeValue.end = dayStart.AddDate(0, 0, -(days-1)).Unix(), now.Unix()
	case queryMetricsPeriodCustom:
		start, _, err := parseAssistantHistoryTime(input.StartAt, false)
		if err != nil {
			return queryMetricsWindow{}, queryMetricsRange{}, err
		}
		end, _, err := parseAssistantHistoryTime(input.EndAt, true)
		if err != nil {
			return queryMetricsWindow{}, queryMetricsRange{}, err
		}
		rangeValue.start, rangeValue.end = start.Unix(), end.Unix()
	case queryMetricsPeriodPoint:
		point, _, err := parseAssistantHistoryTime(input.PointAt, true)
		if err != nil {
			return queryMetricsWindow{}, queryMetricsRange{}, err
		}
		rangeValue.point = point.Unix()
		window.PointAt = point.Format(time.RFC3339)
	}
	if period != queryMetricsPeriodPoint {
		window.StartAt, window.EndAt = assistantTime(rangeValue.start), assistantTime(rangeValue.end)
	}
	return window, rangeValue, nil
}

func queryRealtimeMetrics(_ context.Context, ids []int64, instances []instanceSummary, metrics []string, output queryMetricsOutput) (tool.Output[queryMetricsOutput], error) {
	type total struct {
		value      float64
		count      int
		observedAt int64
	}
	totals := make(map[string]map[string]*total, len(metrics))
	validCounts := make(map[string]int, len(metrics))
	unsupportedCounts := make(map[string]int, len(metrics))
	provenance := make([]tool.Provenance, 0, len(ids))
	observedAt := int64(0)
	stale := false
	for _, id := range ids {
		state, available, err := managedinstance.CurrentManagedRealtime(id)
		if err != nil || !available {
			stale = true
			continue
		}
		observedAt = conservativeTimestamp(observedAt, state.ObservedAt)
		stale = stale || state.Stale
		provenance = append(provenance, tool.Provenance{Source: "managed_realtime", Resource: "instance:" + strconv.FormatInt(id, 10), ObservedAt: unixTime(state.ObservedAt)})
		for _, metric := range metrics {
			value, unit, status, metricObservedAt := realtimeQueryMetric(state, metric)
			if status == model.ManagedInstanceCollectionUnsupported {
				unsupportedCounts[metric]++
				continue
			}
			if value == nil || status != model.ManagedInstanceCollectionSucceeded {
				continue
			}
			if totals[metric] == nil {
				totals[metric] = map[string]*total{}
			}
			entry := totals[metric][unit]
			if entry == nil {
				entry = &total{}
				totals[metric][unit] = entry
			}
			entry.value += *value
			entry.count++
			entry.observedAt = conservativeTimestamp(entry.observedAt, metricObservedAt)
			validCounts[metric]++
		}
	}
	for _, metric := range metrics {
		status := "no_data"
		if validCounts[metric] == len(ids) && len(ids) > 0 {
			status = model.ManagedInstanceCollectionSucceeded
		} else if validCounts[metric] > 0 {
			status = "partial"
		} else if len(ids) > 0 && (unsupportedCounts[metric] == len(ids) || !realtimeQueryMetricSupported(metric, instances)) {
			status = model.ManagedInstanceCollectionUnsupported
		}
		result := queryMetricResult{Status: status, Aggregation: queryMetricAggregation(metric, queryMetricsModeSummary), Count: validCounts[metric]}
		units := totals[metric]
		unitNames := make([]string, 0, len(units))
		for unit := range units {
			unitNames = append(unitNames, unit)
		}
		sort.Strings(unitNames)
		for _, unit := range unitNames {
			entry := units[unit]
			value := entry.value
			if metric == "success_rate" && entry.count > 0 {
				value /= float64(entry.count)
			}
			if len(unitNames) == 1 {
				result.Value, result.Unit, result.ObservedAt = &value, unit, assistantTime(entry.observedAt)
			} else {
				result.Values = append(result.Values, queryMetricUnitValue{Unit: unit, Value: &value})
			}
		}
		output.Metrics[metric] = result
		output.Complete = output.Complete && status == model.ManagedInstanceCollectionSucceeded
	}
	output.ObservedAt = assistantTime(observedAt)
	output.Sources = []string{"managed_realtime"}
	if len(provenance) == 0 {
		provenance = []tool.Provenance{{Source: "managed_realtime"}}
	}
	return tool.Output[queryMetricsOutput]{Data: output, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, stale || !output.Complete)}, nil
}

func realtimeQueryMetric(state managedinstance.ManagedRealtimeState, metric string) (*float64, string, string, int64) {
	sample := managedinstance.MetricSample{}
	observedAt := state.ObservedAt
	switch metric {
	case "rpm":
		sample = state.RPM
	case "rpm_capacity":
		sample = state.RPMCapacity
	case "success_rate":
		sample, observedAt = state.SuccessRate, state.SuccessRateObservedAt
	case "concurrency_used":
		sample, observedAt = state.ConcurrencyUsed, state.ConcurrencyObservedAt
	case "concurrency_max":
		sample, observedAt = state.ConcurrencyMax, state.ConcurrencyObservedAt
	case "cost":
		sample, observedAt = state.TodayCost, state.TodayCostObservedAt
	case "accounts_available", "accounts_total":
		if state.AccountsCollectionStatus != model.ManagedInstanceCollectionSucceeded || state.AccountsObservedAt <= 0 {
			return nil, "account", state.AccountsCollectionStatus, state.AccountsObservedAt
		}
		value := float64(state.AccountsAvailable)
		if metric == "accounts_total" {
			value = float64(state.AccountsTotal)
		}
		return &value, "account", model.ManagedInstanceCollectionSucceeded, state.AccountsObservedAt
	case "active_sessions":
		if state.ActiveSessionsObservedAt <= 0 {
			return nil, "session", "", 0
		}
		value := float64(state.ActiveSessions)
		return &value, "session", model.ManagedInstanceCollectionSucceeded, state.ActiveSessionsObservedAt
	default:
		return nil, "", model.ManagedInstanceCollectionUnsupported, 0
	}
	return sample.Value, sample.Unit, sample.CollectionStatus, observedAt
}

func realtimeQueryMetricSupported(metric string, instances []instanceSummary) bool {
	if metric == "requests" || metric == "tokens" {
		return false
	}
	return metricSupportedByAnyInstance(queryHistoryMetricName(metric, true), instances)
}

func queryDashboardMetricSummary(ids []int64, metrics []string, presetDays int) (map[string]queryMetricResult, int64, bool, []tool.Provenance, error) {
	dashboardRange, err := controlplaneservice.NormalizeManagedDashboardRange(presetDays, 0, 0)
	if err != nil {
		return nil, 0, false, nil, err
	}
	snapshots, err := controlplaneservice.GetManagedDashboardSnapshots(ids, dashboardRange)
	if err != nil {
		return nil, 0, false, nil, err
	}
	type total struct {
		value      float64
		count      int
		observedAt int64
	}
	totals := make(map[string]map[string]*total, len(metrics))
	unsupported := make(map[string]int, len(metrics))
	observedAt := int64(0)
	stale := false
	provenance := make([]tool.Provenance, 0, len(snapshots.Items))
	for _, item := range snapshots.Items {
		section := item.Summary
		if section.Observation == nil {
			stale = true
			continue
		}
		summary, ok := section.Observation.Data.(*managedinstance.SummaryResult)
		if !ok || summary == nil {
			stale = true
			continue
		}
		observedAt = conservativeTimestamp(observedAt, section.Observation.ObservedAt)
		stale = stale || section.Stale
		provenance = append(provenance, tool.Provenance{Source: "managed_dashboard_snapshots", Resource: "instance:" + strconv.FormatInt(item.InstanceID, 10), ObservedAt: unixTime(section.Observation.ObservedAt)})
		for _, metric := range metrics {
			sample := summary.Cost
			if metric == "requests" {
				sample = summary.Requests
			} else if metric == "tokens" {
				sample = summary.Tokens
			}
			if sample.CollectionStatus == model.ManagedInstanceCollectionUnsupported {
				unsupported[metric]++
				continue
			}
			if sample.Value == nil || sample.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
				continue
			}
			if totals[metric] == nil {
				totals[metric] = map[string]*total{}
			}
			entry := totals[metric][sample.Unit]
			if entry == nil {
				entry = &total{}
				totals[metric][sample.Unit] = entry
			}
			entry.value += *sample.Value
			entry.count++
			entry.observedAt = conservativeTimestamp(entry.observedAt, section.Observation.ObservedAt)
		}
	}
	results := make(map[string]queryMetricResult, len(metrics))
	for _, metric := range metrics {
		count := 0
		for _, entry := range totals[metric] {
			count += entry.count
		}
		status := "no_data"
		if count == len(ids) && len(ids) > 0 {
			status = model.ManagedInstanceCollectionSucceeded
		} else if count > 0 {
			status = "partial"
		} else if len(ids) > 0 && unsupported[metric] == len(ids) {
			status = model.ManagedInstanceCollectionUnsupported
		}
		result := queryMetricResult{Status: status, Aggregation: "total", Count: count}
		units := make([]string, 0, len(totals[metric]))
		for unit := range totals[metric] {
			units = append(units, unit)
		}
		sort.Strings(units)
		for _, unit := range units {
			entry := totals[metric][unit]
			value := entry.value
			if len(units) == 1 {
				result.Value, result.Unit, result.ObservedAt = &value, unit, assistantTime(entry.observedAt)
			} else {
				result.Values = append(result.Values, queryMetricUnitValue{Unit: unit, Value: &value})
			}
		}
		results[metric] = result
	}
	return results, observedAt, stale, provenance, nil
}

func queryMetricResultsFromHistory(metrics []string, history metricHistoryOutput) map[string]queryMetricResult {
	results := make(map[string]queryMetricResult, len(metrics))
	for _, metric := range metrics {
		historyName := queryHistoryMetricName(metric, false)
		status := history.MetricStatus[historyName]
		statistics := history.Statistics[historyName]
		result := queryMetricResult{
			Unit: status.Unit, Status: status.Status, Aggregation: status.Aggregation,
			Count: statistics.Count, Minimum: statistics.Minimum, Maximum: statistics.Maximum,
			Average: statistics.Average, Sum: statistics.Sum, Latest: statistics.Latest, LatestAt: statistics.LatestAt,
		}
		if history.Mode == metricHistoryModePoint || status.Aggregation == "period_end" {
			result.Value = statistics.Latest
		} else if status.Aggregation == "daily_total" {
			result.Value = statistics.Sum
		} else {
			result.Value = statistics.Average
		}
		result.ObservedAt = statistics.LatestAt
		results[metric] = result
	}
	return results
}

func queryMetricPointsFromHistory(metrics []string, points []metricHistoryPoint) []queryMetricsPoint {
	result := make([]queryMetricsPoint, 0, len(points))
	for _, point := range points {
		values := make(map[string]*float64, len(metrics))
		for _, metric := range metrics {
			values[metric] = point.Values[queryHistoryMetricName(metric, false)]
		}
		result = append(result, queryMetricsPoint{Time: point.Time, Values: values})
	}
	return result
}

func normalizedQueryMetrics(metrics []string) []string {
	result := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		result = append(result, strings.ToLower(strings.TrimSpace(metric)))
	}
	return result
}

func queryHistoryMetricNames(metrics []string) []string {
	result := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		result = append(result, queryHistoryMetricName(metric, false))
	}
	return result
}

func queryHistoryUsesRealtimeStore(metrics []string) bool {
	for _, metric := range metrics {
		if !metricHistoryDefinitions[metric].dailyOnly {
			return true
		}
	}
	return false
}

func queryHistoryMetricName(metric string, realtime bool) string {
	if metric == "cost" {
		if realtime {
			return "today_cost"
		}
		return "actual_cost"
	}
	return metric
}

func splitDashboardMetrics(metrics []string) ([]string, []string) {
	dashboard, history := make([]string, 0, len(metrics)), make([]string, 0, len(metrics))
	for _, metric := range metrics {
		if metric == "cost" || metric == "requests" || metric == "tokens" {
			dashboard = append(dashboard, metric)
		} else {
			history = append(history, metric)
		}
	}
	return dashboard, history
}

func queryMetricsPresetDays(period string) int {
	switch period {
	case queryMetricsPeriodToday:
		return 1
	case queryMetricsPeriodLast7:
		return 7
	case queryMetricsPeriodLast14:
		return 14
	case queryMetricsPeriodLast30:
		return 30
	default:
		return 0
	}
}

func queryMetricAggregation(metric, mode string) string {
	if mode == queryMetricsModePoint || metric == "accounts_available" || metric == "accounts_total" || metric == "concurrency_used" || metric == "concurrency_max" || metric == "active_sessions" {
		return "period_end"
	}
	if metric == "cost" || metric == "requests" || metric == "tokens" {
		return "total"
	}
	return "average"
}

func sortedQueryMetricSources(sourceSet map[string]struct{}) []string {
	result := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
}
