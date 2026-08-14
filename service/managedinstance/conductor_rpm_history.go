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
	Timestamp int64   `json:"timestamp"`
	RPM       float64 `json:"rpm"`
	Samples   int     `json:"samples"`
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
	_ = recordConductorRPMSample(ctx, stream.instanceID, common.GetTimestamp(), *state.RPM.Value)
}

func recordConductorRPMSample(ctx context.Context, instanceID int64, observedAt int64, rpm float64) error {
	if model.DB == nil || instanceID <= 0 || observedAt <= 0 || rpm < 0 {
		return ErrInvalidInstance
	}
	bucketStart := observedAt - observedAt%60
	now := common.GetTimestamp()
	update := func() *gorm.DB {
		return model.DB.WithContext(ctx).Model(&model.ManagedRPMHistory{}).
			Where("instance_id = ? AND bucket_start = ?", instanceID, bucketStart).
			Updates(map[string]any{
				"rpm_sum":      gorm.Expr("rpm_sum + ?", rpm),
				"sample_count": gorm.Expr("sample_count + 1"),
				"rpm_last":     rpm,
				"updated_at":   now,
			})
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

func cleanupConductorRPMHistory(ctx context.Context, now int64) {
	if model.DB == nil {
		return
	}
	cutoff := now - int64(conductorRPMRetention/time.Second)
	_ = model.DB.WithContext(ctx).Where("bucket_start < ?", cutoff).Delete(&model.ManagedRPMHistory{}).Error
}

func GetConductorRPMHistory(ctx context.Context, instanceIDs []int64, bucket string, start int64, end int64) (*ConductorRPMHistoryResult, error) {
	bucket = strings.TrimSpace(strings.ToLower(bucket))
	if bucket != ConductorRPMBucketMinute && bucket != ConductorRPMBucketHour {
		return nil, ErrInvalidInstance
	}
	instanceIDs = uniquePositiveInt64s(instanceIDs)
	if len(instanceIDs) == 0 || len(instanceIDs) > 100 {
		return nil, ErrInvalidInstance
	}
	var conductorCount int64
	if err := model.DB.WithContext(ctx).Model(&model.ManagedInstance{}).
		Where("id IN ? AND kind = ?", instanceIDs, model.ManagedInstanceKindConductor).
		Count(&conductorCount).Error; err != nil {
		return nil, err
	}
	if conductorCount != int64(len(instanceIDs)) {
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
	points := aggregateConductorRPMHistory(rows, bucket, start, end)
	return &ConductorRPMHistoryResult{Bucket: bucket, Start: start, End: end, Points: points}, nil
}

func aggregateConductorRPMHistory(rows []model.ManagedRPMHistory, bucket string, start int64, end int64) []ConductorRPMHistoryPoint {
	type aggregate struct {
		sum     float64
		count   int
		samples int
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
		}
	}
	points := make([]ConductorRPMHistoryPoint, 0, len(aggregates))
	for timestamp, value := range aggregates {
		rpm := value.sum
		if bucket == ConductorRPMBucketHour && value.count > 0 {
			rpm /= float64(value.count)
		}
		points = append(points, ConductorRPMHistoryPoint{Timestamp: timestamp, RPM: rpm, Samples: value.samples})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	return points
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
