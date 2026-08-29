package processor

import (
	"testing"

	"github.com/01121531/subandnew-api/service/assistant/builtin"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/stretchr/testify/require"
)

func TestSelectAssistantToolNamesUsesNarrowCapabilityGroups(t *testing.T) {
	tests := []struct {
		message  string
		contains []string
		excludes []string
	}{
		{message: "当前 RPM 是多少", contains: []string{builtin.ToolRealtimeMetrics, builtin.ToolListInstances}, excludes: []string{builtin.ToolManagedAccounts, builtin.ToolUsageRecords}},
		{message: "昨天 15 点的 RPM 趋势", contains: []string{builtin.ToolMetricHistory, builtin.ToolRuntimeContext}, excludes: []string{builtin.ToolUsageRecords}},
		{message: "列出不可用账号明细", contains: []string{builtin.ToolManagedAccounts, builtin.ToolRealtimeMetrics}, excludes: []string{builtin.ToolUsageRecords}},
		{message: "查询今天某模型的使用记录", contains: []string{builtin.ToolRuntimeContext, builtin.ToolUsageFilterOptions, builtin.ToolUsageRecords, builtin.ToolUsageSummary}, excludes: []string{builtin.ToolManagedAccounts}},
		{message: "账号产出的统计口径有什么区别", contains: []string{builtin.ToolManagedAccounts, builtin.ToolGuide}, excludes: []string{builtin.ToolUsageRecords}},
	}
	for _, test := range tests {
		names := selectAssistantToolNames(test.message)
		for _, expected := range test.contains {
			require.Contains(t, names, expected, test.message)
		}
		for _, unexpected := range test.excludes {
			require.NotContains(t, names, unexpected, test.message)
		}
	}
	require.Empty(t, selectAssistantToolNames("你好"))
}

func TestSelectAssistantToolNamesFallsBackForAmbiguousOperationalQuestion(t *testing.T) {
	names := selectAssistantToolNames("为什么数据不对")
	require.ElementsMatch(t, builtin.AllDataToolNames(), names)
}

func TestSelectAssistantToolsForConversationReusesOnlyPreviousCapability(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "列出不可用账号"},
		{Role: provider.RoleAssistant, Content: "有 3 个不可用账号"},
		{Role: provider.RoleUser, Content: "那详细信息呢"},
	}
	names := selectAssistantToolsForConversation("那详细信息呢", history)
	require.Contains(t, names, builtin.ToolManagedAccounts)
	require.NotContains(t, names, builtin.ToolUsageRecords)
}

func TestTrimConversationHistoryUsesContiguousRecentBudget(t *testing.T) {
	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older"},
		{Role: provider.RoleAssistant, Content: "middle"},
		{Role: provider.RoleUser, Content: "latest"},
	}
	require.Equal(t, messages[1:], trimConversationHistory(messages, 12))
	require.Equal(t, []provider.Message{{Role: provider.RoleUser, Content: "late"}}, trimConversationHistory(messages[2:], 4))
	require.Empty(t, trimConversationHistory(messages, 0))
}
