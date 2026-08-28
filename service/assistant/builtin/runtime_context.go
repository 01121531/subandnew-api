package builtin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

type runtimeContextInput struct{}

type runtimeContextOutput struct {
	CurrentTime         string  `json:"current_time"`
	Timezone            string  `json:"timezone"`
	DefaultSource       string  `json:"default_source"`
	DefaultInstanceID   *int64  `json:"default_instance_id,omitempty"`
	DefaultInstance     string  `json:"default_instance,omitempty"`
	DefaultFallback     bool    `json:"default_fallback"`
	ResolvedInstanceIDs []int64 `json:"resolved_instance_ids"`
}

func registerRuntimeContext(registry *tool.Registry, db *gorm.DB) error {
	return tool.Register(registry, tool.ToolSpec{
		Name: "get_runtime_context", Version: "v1",
		Description: "返回当前中国标准时间和当前身份的默认实例解析结果。仅在问题依赖现在、今天、昨天、上周等相对时间，或需要解释默认实例回退时调用。",
		Permission:  tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView},
		Risk:        tool.RiskLow, ReadOnly: true, Idempotent: true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, func(ctx context.Context, execution tool.ExecutionContext, _ runtimeContextInput) (tool.Output[runtimeContextOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, nil, access.InstanceSelectionDefault)
		if err != nil {
			return tool.Output[runtimeContextOutput]{}, err
		}
		now := time.Now().In(assistantLocation)
		output := runtimeContextOutput{
			CurrentTime: now.Format(time.RFC3339), Timezone: assistantTimezone,
			DefaultSource: resolution.Source, DefaultInstance: resolution.DefaultName,
			DefaultFallback: resolution.Fallback, ResolvedInstanceIDs: append([]int64(nil), resolution.IDs...),
		}
		if resolution.DefaultID > 0 {
			id := resolution.DefaultID
			output.DefaultInstanceID = &id
		}
		return tool.Output[runtimeContextOutput]{
			Data:       output,
			Provenance: []tool.Provenance{{Source: "assistant_runtime", ObservedAt: now}},
			Freshness:  tool.Freshness{State: tool.FreshnessLive, ObservedAt: now, Timezone: assistantTimezone},
		}, nil
	})
}
