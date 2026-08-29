package builtin

const (
	ToolListInstances      = "list_instances"
	ToolRuntimeContext     = "get_runtime_context"
	ToolDashboardSummary   = "get_dashboard_summary"
	ToolRealtimeMetrics    = "get_realtime_metrics"
	ToolMetricHistory      = "get_metric_history"
	ToolManagedAccounts    = "query_managed_accounts"
	ToolUsageFilterOptions = "get_usage_record_filter_options"
	ToolUsageRecords       = "query_usage_records"
	ToolUsageSummary       = "get_usage_record_summary"
	ToolInstanceHealth     = "get_instance_health"
	ToolOpenAlerts         = "get_open_alerts"
	ToolGuide              = "get_tool_guide"
)

var dataToolNames = []string{
	ToolListInstances,
	ToolRuntimeContext,
	ToolDashboardSummary,
	ToolRealtimeMetrics,
	ToolMetricHistory,
	ToolManagedAccounts,
	ToolUsageFilterOptions,
	ToolUsageRecords,
	ToolUsageSummary,
	ToolInstanceHealth,
	ToolOpenAlerts,
}

// AllDataToolNames returns the stable fallback set for operational questions
// that cannot be assigned to a narrower capability group.
func AllDataToolNames() []string {
	return append([]string(nil), dataToolNames...)
}
