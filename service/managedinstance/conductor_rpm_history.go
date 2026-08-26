package managedinstance

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	ConductorRPMBucketMinute = "minute"
	ConductorRPMBucketHour   = "hour"

	conductorRPMSampleInterval = 10 * time.Second
	conductorRPMRetention      = 31 * 24 * time.Hour
	conductorRPMMinuteMaxRange = 24 * time.Hour
	conductorRPMHourMaxRange   = 31 * 24 * time.Hour
)

type ConductorRPMHistoryPoint struct {
	Timestamp          int64    `json:"timestamp"`
	RPM                float64  `json:"rpm"`
	Capacity           *float64 `json:"capacity"`
	Samples            int      `json:"samples"`
	SuccessRate        *float64 `json:"success_rate"`
	SuccessRateSamples int      `json:"success_rate_samples"`
	AccountsAvailable  *int     `json:"accounts_available"`
	AccountsTotal      *int     `json:"accounts_total"`
	AccountSamples     int      `json:"account_samples"`
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
	if state.StreamStatus != "connected" || state.Stale || state.RPM.Value == nil || state.RPM.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
		return
	}
	if state.RPMCapacity.Value != nil && state.RPMCapacity.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		_ = recordConductorRPMSample(ctx, stream.instanceID, common.GetTimestamp(), *state.RPM.Value, *state.RPMCapacity.Value)
		return
	}
	_ = recordConductorRPMSample(ctx, stream.instanceID, common.GetTimestamp(), *state.RPM.Value)
}

func recordConductorRPMSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64, capacities ...float64) error {
	var capacity *float64
	if len(capacities) > 0 {
		capacity = &capacities[0]
	}
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, rpm, capacity, nil, 0, nil, nil)
}

func recordManagedRealtimeSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64, capacity *float64, successRate *float64, successRateWeight float64, accountsAvailable *int, accountsTotal *int) error {
	if model.DB == nil || instanceID <= 0 || observedAt <= 0 || rpm < 0 {
		return ErrInvalidInstance
	}
	hasCapacity := capacity != nil && *capacity >= 0
	hasSuccessRate := successRate != nil && *successRate >= 0 && *successRate <= 1
	hasAccounts := accountsAvailable != nil && accountsTotal != nil && *accountsAvailable >= 0 && *accountsTotal >= 0 && *accountsAvailable <= *accountsTotal
	if successRate != nil && !hasSuccessRate {
		return ErrInvalidInstance
	}
	if accountsAvailable != nil || accountsTotal != nil {
		if !hasAccounts {
			return ErrInvalidInstance
		}
	}
	if hasSuccessRate && successRateWeight <= 0 {
		successRateWeight = 1
	}
	bucketStart := observedAt - observedAt%60
	now := common.GetTimestamp()
	update := func() *gorm.DB {
		updates := map[string]any{
			"rpm_sum":      gorm.Expr("rpm_sum + ?", rpm),
			"sample_count": gorm.Expr("sample_count + 1"),
			"rpm_last":     rpm,
			"updated_at":   now,
		}
		if hasCapacity {
			updates["capacity_sum"] = gorm.Expr("capacity_sum + ?", *capacity)
			updates["capacity_sample_count"] = gorm.Expr("capacity_sample_count + 1")
			updates["capacity_last"] = *capacity
		}
		if hasSuccessRate {
			updates["success_rate_weighted_sum"] = gorm.Expr("success_rate_weighted_sum + ?", *successRate*successRateWeight)
			updates["success_rate_weight_sum"] = gorm.Expr("success_rate_weight_sum + ?", successRateWeight)
			updates["success_rate_sample_count"] = gorm.Expr("success_rate_sample_count + 1")
			updates["success_rate_last"] = *successRate
		}
		if hasAccounts {
			updates["accounts_available_last"] = *accountsAvailable
			updates["accounts_total_last"] = *accountsTotal
			updates["account_sample_count"] = gorm.Expr("account_sample_count + 1")
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
	history := &model.ManagedRPMHistory{
		InstanceID: instanceID, BucketStart: bucketStart, RPMSum: rpm, SampleCount: 1, RPMLast: rpm,
	}
	if hasCapacity {
		history.CapacitySum = *capacity
		history.CapacitySampleCount = 1
		history.CapacityLast = *capacity
	}
	if hasSuccessRate {
		history.SuccessRateWeightedSum = *successRate * successRateWeight
		history.SuccessRateWeightSum = successRateWeight
		history.SuccessRateSampleCount = 1
		history.SuccessRateLast = *successRate
	}
	if hasAccounts {
		history.AccountsAvailableLast = *accountsAvailable
		history.AccountsTotalLast = *accountsTotal
		history.AccountSampleCount = 1
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
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, rpm, nil, successRate, successRateWeight, nil, nil)
}

func RecordManagedRealtimeSampleWithAccounts(ctx context.Context, instanceID int64, observedAt int64, rpm float64, successRate *float64, successRateWeight float64, accountsAvailable int, accountsTotal int) error {
	return recordManagedRealtimeSample(ctx, instanceID, observedAt, rpm, nil, successRate, successRateWeight, &accountsAvailable, &accountsTotal)
}

func cleanupConductorRPMHistory(ctx context.Context, now int64) {
	if model.DB == nil {
		return
	}
	cutoff := now - int64(conductorRPMRetention/time.Second)
	_ = model.DB.WithContext(ctx).Where("bucket_start < ?", cutoff).Delete(&model.ManagedRPMHistory{}).Error
}

func CleanupManagedRPMHistory(ctx context.Context, now int64) {
	cleanupConductorRPMHistory(ctx, now)
}

func GetConductorRPMHistory(ctx context.Context, instanceIDs []int64, bucket string, start int64, end int64) (*ConductorRPMHistoryResult, error) {
	return GetManagedRPMHistory(ctx, instanceIDs, bucket, start, end)
}

func GetManagedRPMHistory(ctx context.Context, instanceIDs []int64, bucket string, start int64, end int64) (*ConductorRPMHistoryResult, error) {
	bucket = strings.TrimSpace(strings.ToLower(bucket))
	if bucket != ConductorRPMBucketMinute && bucket != ConductorRPMBucketHour {
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
		Where("instance_id IN ? AND bucket_start >= ? AND bucket_start <= ? AND sample_count > 0", instanceIDs, queryStart, end).
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
	}
	expected := 0
	if len(expectedInstances) > 0 {
		expected = expectedInstances[0]
	}
	minutes := map[int64]*aggregate{}
	for _, row := range rows {
		if row.SampleCount <= 0 || row.BucketStart < start || row.BucketStart > end {
			continue
		}
		minute := minutes[row.BucketStart]
		if minute == nil {
			minute = &aggregate{}
			minutes[row.BucketStart] = minute
		}
		minute.sum += row.RPMSum / float64(row.SampleCount)
		minute.samples += row.SampleCount
		if row.CapacitySampleCount > 0 {
			minute.capacitySum += row.CapacitySum / float64(row.CapacitySampleCount)
			minute.capacityCount++
		}
		if row.SuccessRateSampleCount > 0 && row.SuccessRateWeightSum > 0 {
			minute.successRateWeightedSum += row.SuccessRateWeightedSum
			minute.successRateWeightSum += row.SuccessRateWeightSum
			minute.successRateSamples += row.SuccessRateSampleCount
		}
	}
	for _, minute := range minutes {
		minute.capacityComplete = minute.capacityCount > 0 && (expected == 0 || minute.capacityCount == expected)
	}
	aggregates := minutes
	if bucket == ConductorRPMBucketHour {
		aggregates = map[int64]*aggregate{}
		for timestamp, minute := range minutes {
			hour := timestamp - timestamp%3600
			value := aggregates[hour]
			if value == nil {
				value = &aggregate{}
				aggregates[hour] = value
			}
			value.sum += minute.sum
			value.count++
			value.samples += minute.samples
			if minute.capacityComplete {
				value.capacitySum += minute.capacitySum
				value.capacityCount++
			}
			value.successRateWeightedSum += minute.successRateWeightedSum
			value.successRateWeightSum += minute.successRateWeightSum
			value.successRateSamples += minute.successRateSamples
		}
	}
	points := make([]ConductorRPMHistoryPoint, 0, len(aggregates))
	accountPoints := aggregateManagedAccountHistory(rows, bucket, start, end, expected)
	for timestamp, value := range aggregates {
		rpm := value.sum
		if bucket == ConductorRPMBucketHour && value.count > 0 {
			rpm /= float64(value.count)
		}
		var capacity *float64
		if bucket == ConductorRPMBucketMinute && value.capacityComplete {
			capacityValue := value.capacitySum
			capacity = &capacityValue
		} else if bucket == ConductorRPMBucketHour && value.capacityCount > 0 {
			capacityValue := value.capacitySum / float64(value.capacityCount)
			capacity = &capacityValue
		}
		var successRate *float64
		if value.successRateSamples > 0 && value.successRateWeightSum > 0 {
			rate := value.successRateWeightedSum / value.successRateWeightSum
			successRate = &rate
		}
		point := ConductorRPMHistoryPoint{
			Timestamp: timestamp, RPM: rpm, Capacity: capacity, Samples: value.samples,
			SuccessRate: successRate, SuccessRateSamples: value.successRateSamples,
		}
		if accounts, ok := accountPoints[timestamp]; ok {
			point.AccountsAvailable = accounts.available
			point.AccountsTotal = accounts.total
			point.AccountSamples = accounts.samples
		}
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	return points
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
		if bucket == ConductorRPMBucketHour {
			timestamp -= timestamp % 3600
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
