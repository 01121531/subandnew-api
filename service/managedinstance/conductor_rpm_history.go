package managedinstance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	ConductorRPMBucketMinute = "minute"
	ConductorRPMBucketHour   = "hour"
	ConductorRPMBucketDay    = "day"

	conductorRPMSampleInterval = 10 * time.Second
	conductorRPMRetention      = 31 * 24 * time.Hour
	conductorRPMMinuteMaxRange = 24 * time.Hour
	conductorRPMHourMaxRange   = 31 * 24 * time.Hour
)

var managedHistoryWriteFailures = struct {
	sync.Mutex
	last map[int64]time.Time
}{last: make(map[int64]time.Time)}

type ConductorRPMHistoryPoint struct {
	Timestamp            int64    `json:"timestamp"`
	RPM                  float64  `json:"rpm"`
	Capacity             *float64 `json:"capacity"`
	Samples              int      `json:"samples"`
	SuccessRate          *float64 `json:"success_rate"`
	SuccessRateSamples   int      `json:"success_rate_samples"`
	AccountsAvailable    *int     `json:"accounts_available"`
	AccountsTotal        *int     `json:"accounts_total"`
	AccountSamples       int      `json:"account_samples"`
	ConcurrencyUsed      *float64 `json:"concurrency_used"`
	ConcurrencyMax       *float64 `json:"concurrency_max"`
	ConcurrencySamples   int      `json:"concurrency_samples"`
	TodayCost            *float64 `json:"today_cost"`
	TodayCostSamples     int      `json:"today_cost_samples"`
	TodayCostComplete    bool     `json:"today_cost_complete"`
	ActiveSessions       *int     `json:"active_sessions"`
	ActiveSessionSamples int      `json:"active_session_samples"`
	RPMComplete          bool     `json:"-"`
	SuccessRateComplete  bool     `json:"-"`
}

type ConductorRPMHistoryResult struct {
	Bucket string                     `json:"bucket"`
	Start  int64                      `json:"start"`
	End    int64                      `json:"end"`
	Points []ConductorRPMHistoryPoint `json:"points"`
}

func (stream *conductorRealtimeStream) runHistorySampler(ctx context.Context) {
	cleanupConductorRPMHistory(ctx, common.GetTimestamp())
	sampleTicker := time.NewTicker(conductorRPMSampleInterval)
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer sampleTicker.Stop()
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sampleTicker.C:
			stream.sampleRPM(ctx)
		case <-cleanupTicker.C:
			cleanupConductorRPMHistory(ctx, common.GetTimestamp())
		}
	}
}

func (stream *conductorRealtimeStream) sampleRPM(ctx context.Context) {
	stream.mu.Lock()
	state := stream.snapshotLocked()
	stream.mu.Unlock()
	if state.StreamStatus != "connected" || state.Stale {
		return
	}
	sample := ManagedRealtimeHistorySample{
		RPM: metricHistoryValue(state.RPM), RPMCapacity: metricHistoryValue(state.RPMCapacity),
		TodayCost: metricHistoryValue(state.TodayCost), AccountsAvailable: &state.AccountsAvailable,
		AccountsTotal: &state.AccountsTotal, ActiveSessions: &state.ActiveSessions,
	}
	if err := RecordManagedRealtimeHistorySample(ctx, stream.instanceID, common.GetTimestamp(), sample); err != nil {
		ReportManagedRealtimeHistoryWriteError(ctx, stream.instanceID, err)
	}
}

func ReportManagedRealtimeHistoryWriteError(ctx context.Context, instanceID int64, err error) {
	if err == nil {
		return
	}
	now := time.Now()
	managedHistoryWriteFailures.Lock()
	last := managedHistoryWriteFailures.last[instanceID]
	if now.Sub(last) < time.Minute {
		managedHistoryWriteFailures.Unlock()
		return
	}
	managedHistoryWriteFailures.last[instanceID] = now
	managedHistoryWriteFailures.Unlock()
	logger.LogError(ctx, fmt.Sprintf("managed realtime history write failed: instance_id=%d error=%v", instanceID, err))
}

func recordConductorRPMSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64, capacities ...float64) error {
	var capacity *float64
	if len(capacities) > 0 {
		capacity = &capacities[0]
	}
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, ManagedRealtimeHistorySample{RPM: &rpm, RPMCapacity: capacity})
}

type ManagedRealtimeHistorySample struct {
	RPM               *float64
	RPMCapacity       *float64
	SuccessRate       *float64
	SuccessRateWeight float64
	AccountsAvailable *int
	AccountsTotal     *int
	ConcurrencyUsed   *float64
	ConcurrencyMax    *float64
	TodayCost         *float64
	Cost7D            *float64
	Cost30D           *float64
	ActiveSessions    *int
}

func metricHistoryValue(sample MetricSample) *float64 {
	if sample.CollectionStatus != model.ManagedInstanceCollectionSucceeded || sample.Value == nil {
		return nil
	}
	return sample.Value
}

func recordManagedRealtimeSample(ctx context.Context, instanceID int64, observedAt int64, sample ManagedRealtimeHistorySample) error {
	if model.DB == nil || instanceID <= 0 || observedAt <= 0 {
		return ErrInvalidInstance
	}
	hasRPM := sample.RPM != nil && *sample.RPM >= 0
	hasCapacity := sample.RPMCapacity != nil && *sample.RPMCapacity >= 0
	hasSuccessRate := sample.SuccessRate != nil && *sample.SuccessRate >= 0 && *sample.SuccessRate <= 1
	hasAccounts := sample.AccountsAvailable != nil && sample.AccountsTotal != nil && *sample.AccountsAvailable >= 0 && *sample.AccountsTotal >= 0 && *sample.AccountsAvailable <= *sample.AccountsTotal
	hasConcurrencyUsed := sample.ConcurrencyUsed != nil && *sample.ConcurrencyUsed >= 0
	hasConcurrencyMax := sample.ConcurrencyMax != nil && *sample.ConcurrencyMax >= 0
	hasTodayCost := sample.TodayCost != nil && *sample.TodayCost >= 0
	hasCost7D := sample.Cost7D != nil && *sample.Cost7D >= 0
	hasCost30D := sample.Cost30D != nil && *sample.Cost30D >= 0
	hasActiveSessions := sample.ActiveSessions != nil && *sample.ActiveSessions >= 0
	if sample.RPM != nil && !hasRPM || sample.RPMCapacity != nil && !hasCapacity || sample.SuccessRate != nil && !hasSuccessRate {
		return ErrInvalidInstance
	}
	if sample.AccountsAvailable != nil || sample.AccountsTotal != nil {
		if !hasAccounts {
			return ErrInvalidInstance
		}
	}
	if sample.ConcurrencyUsed != nil && !hasConcurrencyUsed || sample.ConcurrencyMax != nil && !hasConcurrencyMax {
		return ErrInvalidInstance
	}
	if sample.TodayCost != nil && !hasTodayCost || sample.Cost7D != nil && !hasCost7D || sample.Cost30D != nil && !hasCost30D || sample.ActiveSessions != nil && !hasActiveSessions {
		return ErrInvalidInstance
	}
	if !hasRPM && !hasCapacity && !hasSuccessRate && !hasAccounts && !hasConcurrencyUsed && !hasConcurrencyMax && !hasTodayCost && !hasCost7D && !hasCost30D && !hasActiveSessions {
		return ErrInvalidInstance
	}
	if hasSuccessRate && sample.SuccessRateWeight <= 0 {
		sample.SuccessRateWeight = 1
	}
	bucketStart := observedAt - observedAt%60
	now := common.GetTimestamp()
	update := func() *gorm.DB {
		updates := map[string]any{"updated_at": now}
		if hasRPM {
			updates["rpm_sum"] = gorm.Expr("rpm_sum + ?", *sample.RPM)
			updates["sample_count"] = gorm.Expr("sample_count + 1")
			updates["rpm_last"] = *sample.RPM
		}
		if hasCapacity {
			updates["capacity_sum"] = gorm.Expr("capacity_sum + ?", *sample.RPMCapacity)
			updates["capacity_sample_count"] = gorm.Expr("capacity_sample_count + 1")
			updates["capacity_last"] = *sample.RPMCapacity
		}
		if hasSuccessRate {
			updates["success_rate_weighted_sum"] = gorm.Expr("success_rate_weighted_sum + ?", *sample.SuccessRate*sample.SuccessRateWeight)
			updates["success_rate_weight_sum"] = gorm.Expr("success_rate_weight_sum + ?", sample.SuccessRateWeight)
			updates["success_rate_sample_count"] = gorm.Expr("success_rate_sample_count + 1")
			updates["success_rate_last"] = *sample.SuccessRate
		}
		if hasAccounts {
			updates["accounts_available_last"] = *sample.AccountsAvailable
			updates["accounts_total_last"] = *sample.AccountsTotal
			updates["account_sample_count"] = gorm.Expr("account_sample_count + 1")
		}
		if hasConcurrencyUsed {
			updates["concurrency_used_last"] = *sample.ConcurrencyUsed
			updates["concurrency_used_samples"] = gorm.Expr("concurrency_used_samples + 1")
		}
		if hasConcurrencyMax {
			updates["concurrency_max_last"] = *sample.ConcurrencyMax
			updates["concurrency_max_samples"] = gorm.Expr("concurrency_max_samples + 1")
		}
		if hasConcurrencyUsed || hasConcurrencyMax {
			updates["concurrency_sample_count"] = gorm.Expr("concurrency_sample_count + 1")
		}
		if hasTodayCost {
			updates["today_cost_last"] = *sample.TodayCost
			updates["today_cost_sample_count"] = gorm.Expr("today_cost_sample_count + 1")
		}
		if hasCost7D {
			updates["cost_7d_last"] = *sample.Cost7D
			updates["cost_7d_sample_count"] = gorm.Expr("cost_7d_sample_count + 1")
		}
		if hasCost30D {
			updates["cost_30d_last"] = *sample.Cost30D
			updates["cost_30d_sample_count"] = gorm.Expr("cost_30d_sample_count + 1")
		}
		if hasActiveSessions {
			updates["active_sessions_last"] = *sample.ActiveSessions
			updates["active_session_samples"] = gorm.Expr("active_session_samples + 1")
		}
		return model.DB.WithContext(ctx).Model(&model.ManagedRPMHistory{}).
			Where("instance_id = ? AND bucket_start = ?", instanceID, bucketStart).
			Updates(updates)
	}
	query := update()
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected > 0 {
		return nil
	}
	history := &model.ManagedRPMHistory{InstanceID: instanceID, BucketStart: bucketStart}
	if hasRPM {
		history.RPMSum = *sample.RPM
		history.SampleCount = 1
		history.RPMLast = *sample.RPM
	}
	if hasCapacity {
		history.CapacitySum = *sample.RPMCapacity
		history.CapacitySampleCount = 1
		history.CapacityLast = *sample.RPMCapacity
	}
	if hasSuccessRate {
		history.SuccessRateWeightedSum = *sample.SuccessRate * sample.SuccessRateWeight
		history.SuccessRateWeightSum = sample.SuccessRateWeight
		history.SuccessRateSampleCount = 1
		history.SuccessRateLast = *sample.SuccessRate
	}
	if hasAccounts {
		history.AccountsAvailableLast = *sample.AccountsAvailable
		history.AccountsTotalLast = *sample.AccountsTotal
		history.AccountSampleCount = 1
	}
	if hasConcurrencyUsed {
		history.ConcurrencyUsedLast = *sample.ConcurrencyUsed
		history.ConcurrencyUsedSamples = 1
	}
	if hasConcurrencyMax {
		history.ConcurrencyMaxLast = *sample.ConcurrencyMax
		history.ConcurrencyMaxSamples = 1
	}
	if hasConcurrencyUsed || hasConcurrencyMax {
		history.ConcurrencySampleCount = 1
	}
	if hasTodayCost {
		history.TodayCostLast = *sample.TodayCost
		history.TodayCostSampleCount = 1
	}
	if hasCost7D {
		history.Cost7DLast = *sample.Cost7D
		history.Cost7DSampleCount = 1
	}
	if hasCost30D {
		history.Cost30DLast = *sample.Cost30D
		history.Cost30DSampleCount = 1
	}
	if hasActiveSessions {
		history.ActiveSessionsLast = *sample.ActiveSessions
		history.ActiveSessionSamples = 1
	}
	createErr := model.DB.WithContext(ctx).Create(history).Error
	if createErr == nil {
		return nil
	}
	retry := update()
	if retry.Error != nil {
		return retry.Error
	}
	if retry.RowsAffected == 0 {
		return createErr
	}
	return nil
}

func RecordManagedRPMSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64) error {
	return recordConductorRPMSample(ctx, instanceID, observedAt, rpm)
}

func RecordManagedRealtimeSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64, successRate *float64, successRateWeight float64) error {
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, ManagedRealtimeHistorySample{RPM: &rpm, SuccessRate: successRate, SuccessRateWeight: successRateWeight})
}

func RecordManagedRealtimeSampleWithAccounts(ctx context.Context, instanceID int64, observedAt int64, rpm float64, successRate *float64, successRateWeight float64, accountsAvailable int, accountsTotal int) error {
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, ManagedRealtimeHistorySample{
		RPM: &rpm, SuccessRate: successRate, SuccessRateWeight: successRateWeight,
		AccountsAvailable: &accountsAvailable, AccountsTotal: &accountsTotal,
	})
}

func RecordManagedRealtimeHistorySample(ctx context.Context, instanceID int64, observedAt int64, sample ManagedRealtimeHistorySample) error {
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, sample)
}

func cleanupConductorRPMHistory(ctx context.Context, now int64) {
	if model.DB == nil {
		return
	}
	cutoff := now - int64(conductorRPMRetention/time.Second)
	if err := model.DB.WithContext(ctx).Where("bucket_start < ?", cutoff).Delete(&model.ManagedRPMHistory{}).Error; err != nil {
		logger.LogError(ctx, "managed realtime history cleanup failed: "+err.Error())
	}
}

func CleanupManagedRPMHistory(ctx context.Context, now int64) {
	cleanupConductorRPMHistory(ctx, now)
}

func GetConductorRPMHistory(ctx context.Context, instanceIDs []int64, bucket string, start int64, end int64) (*ConductorRPMHistoryResult, error) {
	return GetManagedRPMHistory(ctx, instanceIDs, bucket, start, end)
}

func GetManagedRPMHistory(ctx context.Context, instanceIDs []int64, bucket string, start int64, end int64) (*ConductorRPMHistoryResult, error) {
	bucket = strings.TrimSpace(strings.ToLower(bucket))
	if bucket != ConductorRPMBucketMinute && bucket != ConductorRPMBucketHour && bucket != ConductorRPMBucketDay {
		return nil, ErrInvalidInstance
	}
	instanceIDs = uniquePositiveInt64s(instanceIDs)
	if len(instanceIDs) == 0 || len(instanceIDs) > 100 {
		return nil, ErrInvalidInstance
	}
	var supportedCount int64
	if err := model.DB.WithContext(ctx).Model(&model.ManagedInstance{}).
		Where("id IN ? AND kind IN ?", instanceIDs, []string{
			model.ManagedInstanceKindConductor,
			model.ManagedInstanceKindSub2API,
			model.ManagedInstanceKindNewAPI,
			model.ManagedInstanceKindHuichuan,
			model.ManagedInstanceKindClaudeGateway,
		}).Count(&supportedCount).Error; err != nil {
		return nil, err
	}
	if supportedCount != int64(len(instanceIDs)) {
		return nil, ErrUnsupportedCapability
	}
	if end <= 0 {
		end = common.GetTimestamp()
	}
	maxRange := int64(conductorRPMMinuteMaxRange / time.Second)
	defaultRange := int64(time.Hour / time.Second)
	if bucket == ConductorRPMBucketHour {
		maxRange = int64(conductorRPMHourMaxRange / time.Second)
		defaultRange = int64(24 * time.Hour / time.Second)
	} else if bucket == ConductorRPMBucketDay {
		maxRange = int64(conductorRPMHourMaxRange / time.Second)
		defaultRange = int64(7 * 24 * time.Hour / time.Second)
	}
	if start <= 0 {
		start = end - defaultRange
	}
	if start < 0 || start >= end || end-start > maxRange {
		return nil, ErrInvalidInstance
	}
	var rows []model.ManagedRPMHistory
	queryStart := start - start%60
	if err := model.DB.WithContext(ctx).
		Where("instance_id IN ? AND bucket_start >= ? AND bucket_start <= ?", instanceIDs, queryStart, end).
		Order("bucket_start asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	points := aggregateConductorRPMHistory(rows, bucket, start, end, len(instanceIDs))
	return &ConductorRPMHistoryResult{Bucket: bucket, Start: start, End: end, Points: points}, nil
}

func aggregateConductorRPMHistory(rows []model.ManagedRPMHistory, bucket string, start int64, end int64, expectedInstances ...int) []ConductorRPMHistoryPoint {
	type aggregate struct {
		sum                    float64
		count                  int
		samples                int
		capacitySum            float64
		capacityCount          int
		capacityComplete       bool
		successRateWeightedSum float64
		successRateWeightSum   float64
		successRateSamples     int
		rpmInstances           map[int64]struct{}
		capacityInstances      map[int64]struct{}
		successRateInstances   map[int64]struct{}
	}
	expected := 0
	if len(expectedInstances) > 0 {
		expected = expectedInstances[0]
	}
	minutes := map[int64]*aggregate{}
	for _, row := range rows {
		if row.BucketStart < start || row.BucketStart > end {
			continue
		}
		minute := minutes[row.BucketStart]
		if minute == nil {
			minute = &aggregate{rpmInstances: map[int64]struct{}{}, capacityInstances: map[int64]struct{}{}, successRateInstances: map[int64]struct{}{}}
			minutes[row.BucketStart] = minute
		}
		if row.SampleCount > 0 {
			minute.sum += row.RPMSum / float64(row.SampleCount)
			minute.samples += row.SampleCount
			minute.rpmInstances[row.InstanceID] = struct{}{}
		}
		if row.CapacitySampleCount > 0 {
			minute.capacitySum += row.CapacitySum / float64(row.CapacitySampleCount)
			minute.capacityCount++
			minute.capacityInstances[row.InstanceID] = struct{}{}
		}
		if row.SuccessRateSampleCount > 0 && row.SuccessRateWeightSum > 0 {
			minute.successRateWeightedSum += row.SuccessRateWeightedSum
			minute.successRateWeightSum += row.SuccessRateWeightSum
			minute.successRateSamples += row.SuccessRateSampleCount
			minute.successRateInstances[row.InstanceID] = struct{}{}
		}
	}
	for _, minute := range minutes {
		minute.capacityComplete = minute.capacityCount > 0 && (expected == 0 || minute.capacityCount == expected)
	}
	aggregates := minutes
	if bucket == ConductorRPMBucketHour || bucket == ConductorRPMBucketDay {
		aggregates = map[int64]*aggregate{}
		for timestamp, minute := range minutes {
			period := managedHistoryBucketStart(timestamp, bucket)
			value := aggregates[period]
			if value == nil {
				value = &aggregate{rpmInstances: map[int64]struct{}{}, capacityInstances: map[int64]struct{}{}, successRateInstances: map[int64]struct{}{}}
				aggregates[period] = value
			}
			if minute.samples > 0 {
				value.sum += minute.sum
				value.count++
			}
			value.samples += minute.samples
			if minute.capacityComplete {
				value.capacitySum += minute.capacitySum
				value.capacityCount++
			}
			value.successRateWeightedSum += minute.successRateWeightedSum
			value.successRateWeightSum += minute.successRateWeightSum
			value.successRateSamples += minute.successRateSamples
			for id := range minute.rpmInstances {
				value.rpmInstances[id] = struct{}{}
			}
			for id := range minute.capacityInstances {
				value.capacityInstances[id] = struct{}{}
			}
			for id := range minute.successRateInstances {
				value.successRateInstances[id] = struct{}{}
			}
		}
	}
	points := make([]ConductorRPMHistoryPoint, 0, len(aggregates))
	accountPoints := aggregateManagedAccountHistory(rows, bucket, start, end, expected)
	auxiliaryPoints := aggregateManagedAuxiliaryHistory(rows, bucket, start, end, expected)
	for timestamp, value := range aggregates {
		rpm := value.sum
		if bucket != ConductorRPMBucketMinute && value.count > 0 {
			rpm /= float64(value.count)
		}
		var capacity *float64
		if bucket == ConductorRPMBucketMinute && value.capacityComplete {
			capacityValue := value.capacitySum
			capacity = &capacityValue
		} else if bucket != ConductorRPMBucketMinute && value.capacityCount > 0 {
			capacityValue := value.capacitySum / float64(value.capacityCount)
			capacity = &capacityValue
		}
		var successRate *float64
		successComplete := value.successRateSamples > 0 && (expected == 0 || len(value.successRateInstances) == expected)
		if value.successRateSamples > 0 && value.successRateWeightSum > 0 {
			rate := value.successRateWeightedSum / value.successRateWeightSum
			successRate = &rate
		}
		point := ConductorRPMHistoryPoint{
			Timestamp: timestamp, RPM: rpm, Capacity: capacity, Samples: value.samples,
			SuccessRate: successRate, SuccessRateSamples: value.successRateSamples,
			RPMComplete: value.samples > 0 && (expected == 0 || len(value.rpmInstances) == expected), SuccessRateComplete: successComplete,
		}
		if accounts, ok := accountPoints[timestamp]; ok {
			point.AccountsAvailable = accounts.available
			point.AccountsTotal = accounts.total
			point.AccountSamples = accounts.samples
		}
		if auxiliary, ok := auxiliaryPoints[timestamp]; ok {
			point.ConcurrencyUsed = auxiliary.concurrencyUsed
			point.ConcurrencyMax = auxiliary.concurrencyMax
			point.ConcurrencySamples = auxiliary.concurrencySamples
			point.TodayCost = auxiliary.todayCost
			point.TodayCostSamples = auxiliary.todayCostSamples
			point.TodayCostComplete = auxiliary.todayCost != nil
			point.ActiveSessions = auxiliary.activeSessions
			point.ActiveSessionSamples = auxiliary.activeSessionSamples
		}
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	return points
}

type managedAuxiliaryHistoryPoint struct {
	concurrencyUsed      *float64
	concurrencyMax       *float64
	concurrencySamples   int
	todayCost            *float64
	todayCostSamples     int
	activeSessions       *int
	activeSessionSamples int
}

func aggregateManagedAuxiliaryHistory(rows []model.ManagedRPMHistory, bucket string, start int64, end int64, expectedInstances int) map[int64]managedAuxiliaryHistoryPoint {
	type value struct {
		bucketStart int64
		used        *float64
		maximum     *float64
		todayCost   *float64
		sessions    *int
	}
	buckets := map[int64]map[int64]value{}
	sampleCounts := map[int64]managedAuxiliaryHistoryPoint{}
	for _, row := range rows {
		if row.BucketStart < start || row.BucketStart > end {
			continue
		}
		timestamp := row.BucketStart
		if bucket != ConductorRPMBucketMinute {
			timestamp = managedHistoryBucketStart(timestamp, bucket)
		}
		byInstance := buckets[timestamp]
		if byInstance == nil {
			byInstance = map[int64]value{}
			buckets[timestamp] = byInstance
		}
		current, exists := byInstance[row.InstanceID]
		if exists && current.bucketStart > row.BucketStart {
			continue
		}
		current.bucketStart = row.BucketStart
		if row.ConcurrencyUsedSamples > 0 {
			used := row.ConcurrencyUsedLast
			current.used = &used
		}
		if row.ConcurrencyMaxSamples > 0 {
			maximum := row.ConcurrencyMaxLast
			current.maximum = &maximum
		}
		if row.TodayCostSampleCount > 0 {
			cost := row.TodayCostLast
			current.todayCost = &cost
		}
		if row.ActiveSessionSamples > 0 {
			sessions := row.ActiveSessionsLast
			current.sessions = &sessions
		}
		byInstance[row.InstanceID] = current
		counts := sampleCounts[timestamp]
		counts.concurrencySamples += max(row.ConcurrencyUsedSamples, row.ConcurrencyMaxSamples)
		counts.todayCostSamples += row.TodayCostSampleCount
		counts.activeSessionSamples += row.ActiveSessionSamples
		sampleCounts[timestamp] = counts
	}
	result := make(map[int64]managedAuxiliaryHistoryPoint, len(buckets))
	for timestamp, byInstance := range buckets {
		point := sampleCounts[timestamp]
		used, maximum, cost := 0.0, 0.0, 0.0
		sessions := 0
		usedCount, maxCount, costCount, sessionCount := 0, 0, 0, 0
		for _, item := range byInstance {
			if item.used != nil {
				used += *item.used
				usedCount++
			}
			if item.maximum != nil {
				maximum += *item.maximum
				maxCount++
			}
			if item.todayCost != nil {
				cost += *item.todayCost
				costCount++
			}
			if item.sessions != nil {
				sessions += *item.sessions
				sessionCount++
			}
		}
		complete := func(count int) bool { return count > 0 && (expectedInstances == 0 || count == expectedInstances) }
		if complete(usedCount) {
			point.concurrencyUsed = &used
		}
		if complete(maxCount) {
			point.concurrencyMax = &maximum
		}
		if complete(costCount) {
			point.todayCost = &cost
		}
		if complete(sessionCount) {
			point.activeSessions = &sessions
		}
		result[timestamp] = point
	}
	return result
}

type managedAccountHistoryPoint struct {
	available *int
	total     *int
	samples   int
}

func aggregateManagedAccountHistory(rows []model.ManagedRPMHistory, bucket string, start int64, end int64, expectedInstances int) map[int64]managedAccountHistoryPoint {
	type accountValue struct {
		available   int
		total       int
		bucketStart int64
	}
	type accountBucket struct {
		lastByInstance map[int64]accountValue
		samples        int
	}
	buckets := map[int64]*accountBucket{}
	for _, row := range rows {
		if row.AccountSampleCount <= 0 || row.BucketStart < start || row.BucketStart > end {
			continue
		}
		timestamp := row.BucketStart
		if bucket != ConductorRPMBucketMinute {
			timestamp = managedHistoryBucketStart(timestamp, bucket)
		}
		value := buckets[timestamp]
		if value == nil {
			value = &accountBucket{lastByInstance: map[int64]accountValue{}}
			buckets[timestamp] = value
		}
		value.samples += row.AccountSampleCount
		previous, exists := value.lastByInstance[row.InstanceID]
		if !exists || row.BucketStart >= previous.bucketStart {
			value.lastByInstance[row.InstanceID] = accountValue{
				available:   row.AccountsAvailableLast,
				total:       row.AccountsTotalLast,
				bucketStart: row.BucketStart,
			}
		}
	}
	result := make(map[int64]managedAccountHistoryPoint, len(buckets))
	for timestamp, bucketValue := range buckets {
		point := managedAccountHistoryPoint{samples: bucketValue.samples}
		if len(bucketValue.lastByInstance) > 0 && (expectedInstances == 0 || len(bucketValue.lastByInstance) == expectedInstances) {
			available, total := 0, 0
			for _, instanceValue := range bucketValue.lastByInstance {
				available += instanceValue.available
				total += instanceValue.total
			}
			point.available = &available
			point.total = &total
		}
		result[timestamp] = point
	}
	return result
}

func managedHistoryBucketStart(timestamp int64, bucket string) int64 {
	if bucket == ConductorRPMBucketHour {
		return timestamp - timestamp%3600
	}
	if bucket == ConductorRPMBucketDay {
		location := time.FixedZone("Asia/Shanghai", 8*60*60)
		local := time.Unix(timestamp, 0).In(location)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).Unix()
	}
	return timestamp - timestamp%60
}

func uniquePositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
