package builtin

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
)

//go:embed guides/*.md
var toolGuideFiles embed.FS

var toolGuidePaths = map[string]string{
	"realtime":      "guides/realtime.md",
	"dashboard":     "guides/dashboard.md",
	"accounts":      "guides/accounts.md",
	"usage_records": "guides/usage-records.md",
	"health_alerts": "guides/health-alerts.md",
}

type toolGuideInput struct {
	Topic string `json:"topic"`
}

func (input toolGuideInput) Validate() error {
	if _, exists := toolGuidePaths[strings.TrimSpace(input.Topic)]; !exists {
		return errors.New("unsupported tool guide topic")
	}
	return nil
}

type toolGuideOutput struct {
	Topic   string `json:"topic"`
	Content string `json:"content"`
}

func registerToolGuide(registry *tool.Registry) error {
	schema := json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","enum":["realtime","dashboard","accounts","usage_records","health_alerts"]}},"required":["topic"],"additionalProperties":false}`)
	return tool.Register(registry, tool.ToolSpec{
		Name: ToolGuide, Version: "v1",
		Description: "按需读取控制台数据工具的业务口径与选择指南；只用于解释能力或复杂查询，不返回业务数据。",
		Permission:  tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionView},
		Risk:        tool.RiskLow, ReadOnly: true, Idempotent: true, InputSchema: schema,
	}, func(_ context.Context, _ tool.ExecutionContext, input toolGuideInput) (tool.Output[toolGuideOutput], error) {
		topic := strings.TrimSpace(input.Topic)
		content, err := toolGuideFiles.ReadFile(toolGuidePaths[topic])
		if err != nil {
			return tool.Output[toolGuideOutput]{}, err
		}
		now := time.Now().In(assistantLocation)
		return tool.Output[toolGuideOutput]{
			Data:       toolGuideOutput{Topic: topic, Content: strings.TrimSpace(string(content))},
			Provenance: []tool.Provenance{{Source: "assistant_tool_guides", Resource: topic, ObservedAt: now}},
			Freshness:  tool.Freshness{State: tool.FreshnessSnapshot, ObservedAt: now, Timezone: assistantTimezone},
		}, nil
	})
}
