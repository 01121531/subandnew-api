package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/01121531/subandnew-api/model"
	controlplaneservice "github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

const assistantTimezone = "Asia/Shanghai"

var assistantLocation = time.FixedZone(assistantTimezone, 8*60*60)

type listInstancesInput struct {
	InstanceScope string `json:"instance_scope,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Environment   string `json:"environment,omitempty"`
	Status        string `json:"status,omitempty"`
	Search        string `json:"search,omitempty"`
	Page          int    `json:"page,omitempty"`
	PageSize      int    `json:"page_size,omitempty"`
}

func (input listInstancesInput) Validate() error {
	if err := validateInstanceSelection(nil, input.InstanceScope); err != nil {
		return err
	}
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 100 {
		return errors.New("invalid pagination")
	}
	return nil
}

type instanceSummary struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Environment   string `json:"environment"`
	Status        string `json:"status"`
	Version       string `json:"version,omitempty"`
	LastSeenAt    int64  `json:"last_seen_at"`
	LastCheckedAt int64  `json:"last_checked_at"`
}

type listInstancesOutput struct {
	Items    []instanceSummary `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type instanceIDsInput struct {
	InstanceIDs   []int64 `json:"instance_ids,omitempty"`
	InstanceScope string  `json:"instance_scope,omitempty"`
}

func (input instanceIDsInput) Validate() error {
	return validateInstanceSelection(input.InstanceIDs, input.InstanceScope)
}

type dashboardInput struct {
	InstanceIDs   []int64 `json:"instance_ids,omitempty"`
	InstanceScope string  `json:"instance_scope,omitempty"`
	PresetDays    int     `json:"preset_days,omitempty"`
}

func (input dashboardInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	if input.PresetDays == 0 {
		return nil
	}
	for _, allowed := range []int{1, 7, 14, 30} {
		if input.PresetDays == allowed {
			return nil
		}
	}
	return errors.New("preset_days must be one of 1, 7, 14, or 30")
}

type realtimeItem struct {
	InstanceID int64                                  `json:"instance_id"`
	ObservedAt int64                                  `json:"observed_at,omitempty"`
	Status     string                                 `json:"status"`
	ErrorCode  string                                 `json:"error_code,omitempty"`
	Metrics    *managedinstance.RealtimeMetricsResult `json:"metrics,omitempty"`
}

type realtimeOutput struct {
	Items []realtimeItem `json:"items"`
}

type healthItem struct {
	InstanceID          int64  `json:"instance_id"`
	Name                string `json:"name"`
	Status              string `json:"status"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastSeenAt          int64  `json:"last_seen_at"`
	LastCheckedAt       int64  `json:"last_checked_at"`
}

type healthOutput struct {
	Items []healthItem `json:"items"`
}

type alertItem struct {
	ID          int64  `json:"id"`
	InstanceID  int64  `json:"instance_id"`
	AlertType   string `json:"alert_type"`
	ErrorCode   string `json:"error_code"`
	Occurrences int    `json:"occurrences"`
	FirstSeenAt int64  `json:"first_seen_at"`
	LastSeenAt  int64  `json:"last_seen_at"`
}

type alertsOutput struct {
	Items []alertItem `json:"items"`
}

func NewRegistry(db *gorm.DB) (*tool.Registry, error) {
	registry, err := tool.NewRegistry(access.Authorize(db))
	if err != nil {
		return nil, err
	}
	if err := registerListInstances(registry, db); err != nil {
		return nil, err
	}
	if err := registerDashboardSummary(registry, db); err != nil {
		return nil, err
	}
	if err := registerRealtimeMetrics(registry, db); err != nil {
		return nil, err
	}
	if err := registerMetricHistory(registry, db); err != nil {
		return nil, err
	}
	if err := registerInstanceHealth(registry, db); err != nil {
		return nil, err
	}
	if err := registerOpenAlerts(registry, db); err != nil {
		return nil, err
	}
	return registry, nil
}

func registerListInstances(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "list_instances", Version: "v1", Description: "列出当前用户有权查看的纳管实例及健康状态。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:       tool.RiskLow, ReadOnly: true, Idempotent: true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"instance_scope":{"type":"string","enum":["all"]},"kind":{"type":"string"},"environment":{"type":"string"},"status":{"type":"string"},"search":{"type":"string"},"page":{"type":"integer","minimum":1},"page_size":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`),
	}, func(ctx context.Context, execution tool.ExecutionContext, input listInstancesInput) (tool.Output[listInstancesOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, nil, input.InstanceScope)
		if err != nil {
			return tool.Output[listInstancesOutput]{}, err
		}
		result, err := managedinstance.List(managedinstance.ListFilter{
			IDs: resolution.IDs, Kind: input.Kind, Environment: input.Environment, Status: input.Status,
			Search: input.Search, Page: input.Page, PageSize: input.PageSize,
		})
		if err != nil {
			return tool.Output[listInstancesOutput]{}, err
		}
		items := make([]instanceSummary, 0, len(result.Items))
		observedAt := int64(0)
		provenance := make([]tool.Provenance, 0, len(result.Items))
		for _, view := range result.Items {
			instance := view.ManagedInstance
			items = append(items, instanceSummary{
				ID: instance.Id, Name: instance.Name, Kind: instance.Kind, Environment: instance.Environment,
				Status: instance.Status, Version: instance.Version, LastSeenAt: instance.LastSeenAt, LastCheckedAt: instance.LastCheckedAt,
			})
			observedAt = conservativeTimestamp(observedAt, max(instance.LastCheckedAt, instance.UpdatedAt))
			provenance = append(provenance, tool.Provenance{Source: "managed_instances", Resource: "instance:" + strconv.FormatInt(instance.Id, 10), ObservedAt: unixTime(max(instance.LastCheckedAt, instance.UpdatedAt))})
		}
		freshness := freshnessForSnapshot(observedAt, false)
		if len(provenance) == 0 {
			provenance = []tool.Provenance{{Source: "managed_instances"}}
		}
		return tool.Output[listInstancesOutput]{
			Data:       listInstancesOutput{Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize},
			Provenance: provenance, Freshness: freshness,
		}, nil
	})
}

func registerInstanceHealth(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_instance_health", Version: "v1", Description: "读取有权实例的健康状态、连续失败次数和最近巡检时间。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionView},
		Risk:       tool.RiskLow, ReadOnly: true, Idempotent: true,
		InputSchema: instanceSelectionSchema(false),
	}, func(ctx context.Context, execution tool.ExecutionContext, input instanceIDsInput) (tool.Output[healthOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[healthOutput]{}, err
		}
		result, err := managedinstance.List(managedinstance.ListFilter{IDs: resolution.IDs, Page: 1, PageSize: 100})
		if err != nil {
			return tool.Output[healthOutput]{}, err
		}
		output := healthOutput{Items: make([]healthItem, 0, len(result.Items))}
		provenance := make([]tool.Provenance, 0, len(result.Items))
		observedAt := int64(0)
		for _, view := range result.Items {
			instance := view.ManagedInstance
			output.Items = append(output.Items, healthItem{InstanceID: instance.Id, Name: instance.Name, Status: instance.Status, ConsecutiveFailures: instance.ConsecutiveFailures, LastSeenAt: instance.LastSeenAt, LastCheckedAt: instance.LastCheckedAt})
			observedAt = conservativeTimestamp(observedAt, instance.LastCheckedAt)
			provenance = append(provenance, tool.Provenance{Source: "managed_instances", Resource: "instance:" + strconv.FormatInt(instance.Id, 10), ObservedAt: unixTime(instance.LastCheckedAt)})
		}
		if len(provenance) == 0 {
			provenance = []tool.Provenance{{Source: "managed_instances"}}
		}
		return tool.Output[healthOutput]{Data: output, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, false)}, nil
	})
}

func registerOpenAlerts(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_open_alerts", Version: "v1", Description: "读取有权实例当前未恢复的可用性和凭据告警，不返回通知收件人。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionView},
		Risk:       tool.RiskLow, ReadOnly: true, Idempotent: true,
		InputSchema: instanceSelectionSchema(false),
	}, func(ctx context.Context, execution tool.ExecutionContext, input instanceIDsInput) (tool.Output[alertsOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[alertsOutput]{}, err
		}
		var alerts []model.ManagedInstanceAlert
		if len(resolution.IDs) > 0 {
			err = db.WithContext(ctx).Where("instance_id IN ? AND status = ?", resolution.IDs, model.ManagedInstanceAlertStatusOpen).Order("last_seen_at DESC, id DESC").Limit(100).Find(&alerts).Error
		}
		if err != nil {
			return tool.Output[alertsOutput]{}, err
		}
		output := alertsOutput{Items: make([]alertItem, 0, len(alerts))}
		provenance := make([]tool.Provenance, 0, len(alerts))
		observedAt := int64(0)
		for _, alert := range alerts {
			output.Items = append(output.Items, alertItem{ID: alert.Id, InstanceID: alert.InstanceId, AlertType: alert.AlertType, ErrorCode: alert.ErrorCode, Occurrences: alert.Occurrences, FirstSeenAt: alert.FirstSeenAt, LastSeenAt: alert.LastSeenAt})
			observedAt = conservativeTimestamp(observedAt, alert.LastSeenAt)
			provenance = append(provenance, tool.Provenance{Source: "managed_instance_alerts", Resource: "alert:" + strconv.FormatInt(alert.Id, 10), ObservedAt: unixTime(alert.LastSeenAt)})
		}
		if len(provenance) == 0 {
			provenance = []tool.Provenance{{Source: "managed_instance_alerts"}}
		}
		return tool.Output[alertsOutput]{Data: output, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, false)}, nil
	})
}

func registerDashboardSummary(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_dashboard_summary", Version: "v1", Description: "读取有权实例的 Dashboard 汇总快照，包含采集时间和部分失败状态。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionView},
		Risk:       tool.RiskLow, ReadOnly: true, Idempotent: true,
		InputSchema: instanceSelectionSchema(true),
	}, func(ctx context.Context, execution tool.ExecutionContext, input dashboardInput) (tool.Output[*controlplaneservice.ManagedDashboardSnapshotListView], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[*controlplaneservice.ManagedDashboardSnapshotListView]{}, err
		}
		dashboardRange, err := controlplaneservice.NormalizeManagedDashboardRange(input.PresetDays, 0, 0)
		if err != nil {
			return tool.Output[*controlplaneservice.ManagedDashboardSnapshotListView]{}, err
		}
		result, err := controlplaneservice.GetManagedDashboardSnapshots(resolution.IDs, dashboardRange)
		if err != nil {
			return tool.Output[*controlplaneservice.ManagedDashboardSnapshotListView]{}, err
		}
		provenance := make([]tool.Provenance, 0, len(result.Items))
		observedAt := int64(0)
		stale := false
		for _, item := range result.Items {
			stale = stale || item.Summary.Stale || item.Summary.Observation == nil
			if item.Summary.Observation == nil {
				continue
			}
			observedAt = conservativeTimestamp(observedAt, item.Summary.Observation.ObservedAt)
			provenance = append(provenance, tool.Provenance{Source: "managed_dashboard_snapshots", Resource: "instance:" + strconv.FormatInt(item.InstanceID, 10), ObservedAt: unixTime(item.Summary.Observation.ObservedAt)})
		}
		if len(provenance) == 0 {
			provenance = []tool.Provenance{{Source: "managed_dashboard_snapshots"}}
		}
		return tool.Output[*controlplaneservice.ManagedDashboardSnapshotListView]{Data: result, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, stale)}, nil
	})
}

func registerRealtimeMetrics(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_realtime_metrics", Version: "v1", Description: "读取有权实例的实时 RPM、成功率、并发、成本和账号汇总，不返回账号明细。",
		Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:       tool.RiskMedium, ReadOnly: true, Idempotent: true,
		InputSchema: instanceSelectionSchema(false),
	}, func(ctx context.Context, execution tool.ExecutionContext, input instanceIDsInput) (tool.Output[realtimeOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[realtimeOutput]{}, err
		}
		output := realtimeOutput{Items: make([]realtimeItem, 0, len(resolution.IDs))}
		provenance := make([]tool.Provenance, 0, len(resolution.IDs))
		observedAt := int64(0)
		stale := false
		for _, id := range resolution.IDs {
			state, available, stateErr := managedinstance.CurrentManagedRealtime(id)
			item := realtimeItem{InstanceID: id, Status: "unavailable"}
			if stateErr != nil {
				item.ErrorCode = "unsupported_or_unavailable"
			} else if available {
				item.Status = state.StreamStatus
				item.ObservedAt = state.ObservedAt
				item.ErrorCode = state.ErrorCode
				item.Metrics = state.Metrics()
				observedAt = conservativeTimestamp(observedAt, state.ObservedAt)
				stale = stale || state.Stale
				provenance = append(provenance, tool.Provenance{Source: "managed_realtime", Resource: "instance:" + strconv.FormatInt(id, 10), ObservedAt: unixTime(state.ObservedAt)})
			} else {
				stale = true
			}
			output.Items = append(output.Items, item)
		}
		if len(provenance) == 0 {
			provenance = []tool.Provenance{{Source: "managed_realtime"}}
		}
		return tool.Output[realtimeOutput]{Data: output, Provenance: provenance, Freshness: freshnessForSnapshot(observedAt, stale)}, nil
	})
}

func validateInstanceIDs(ids []int64) error {
	if len(ids) > 100 {
		return errors.New("at most 100 instance ids are allowed")
	}
	for _, id := range ids {
		if id <= 0 {
			return errors.New("instance ids must be positive")
		}
	}
	return nil
}

func validateInstanceSelection(ids []int64, scope string) error {
	if err := validateInstanceIDs(ids); err != nil {
		return err
	}
	if scope != "" && scope != access.InstanceSelectionAll {
		return errors.New("instance_scope must be all")
	}
	if scope == access.InstanceSelectionAll && len(ids) > 0 {
		return errors.New("instance_ids and instance_scope cannot be combined")
	}
	return nil
}

func instanceSelectionSchema(includePreset bool) json.RawMessage {
	preset := ""
	if includePreset {
		preset = `,"preset_days":{"type":"integer","enum":[1,7,14,30]}`
	}
	return json.RawMessage(`{"type":"object","properties":{"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]}` + preset + `},"additionalProperties":false}`)
}

func freshnessForSnapshot(observedAt int64, stale bool) tool.Freshness {
	if observedAt <= 0 {
		return tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}
	}
	state := tool.FreshnessSnapshot
	if stale {
		state = tool.FreshnessStale
	}
	return tool.Freshness{State: state, ObservedAt: unixTime(observedAt), Timezone: assistantTimezone}
}

func conservativeTimestamp(current int64, candidate int64) int64 {
	if candidate <= 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}

func unixTime(timestamp int64) time.Time {
	if timestamp <= 0 {
		return time.Time{}
	}
	return time.Unix(timestamp, 0).In(assistantLocation)
}
