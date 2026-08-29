package processor

import (
	"sort"
	"strings"

	"github.com/01121531/subandnew-api/service/assistant/builtin"
	"github.com/01121531/subandnew-api/service/assistant/provider"
)

// selectAssistantToolNames performs a cheap deterministic capability routing
// pass over the current user message. Ambiguous operational questions fall
// back to the complete data tool set, while casual conversation carries no
// tool schemas at all.
func selectAssistantToolNames(message string) []string {
	text := strings.ToLower(strings.TrimSpace(message))
	selected := make(map[string]struct{})
	add := func(names ...string) {
		for _, name := range names {
			selected[name] = struct{}{}
		}
	}
	if text == "" {
		return []string{}
	}

	wantsGuide := containsAny(text, "怎么查", "如何查", "如何查询", "能查什么", "支持什么", "工具说明", "工具介绍", "查询口径", "数据口径", "字段含义", "有什么区别")
	wantsRuntime := containsAny(text, "今天", "昨日", "昨天", "前天", "上周", "本周", "上个月", "本月", "最近", "过去", "时间段", "日期范围", "从今天", "截至今天")
	wantsRealtime := containsAny(text, "rpm", "实时", "当前指标", "此刻", "并发", "成功率", "活跃会话", "容量")
	wantsHistory := containsAny(text, "历史", "趋势", "最高", "最低", "最大值", "最小值", "平均值", "某时刻", "当时", "过去", "昨天", "前天", "上周")
	wantsAccounts := containsAny(text, "账号", "账户", "邮箱", "email", "备注", "归属", "存活", "工作节点", "可用", "不可用", "限流", "账号产出", "账户产出")
	wantsUsage := containsAny(text, "使用记录", "调用记录", "请求记录", "请求日志", "request id", "request_id", "api key", "apikey", "渠道", "筛选项", "记录明细")
	wantsDashboard := containsAny(text, "数据面板", "用量", "消费", "费用", "成本", "请求数", "token", "账单", "每日趋势", "今日消费")
	wantsHealth := containsAny(text, "巡检", "健康", "离线", "连接失败", "连接状态", "故障", "异常实例", "实例异常")
	wantsAlerts := containsAny(text, "告警", "预警", "报警", "通知记录")
	wantsInstances := containsAny(text, "实例", "站点", "平台", "全部", "所有")
	modelScopedUsage := containsAny(text, "模型", "model") && (wantsDashboard || containsAny(text, "调用", "统计"))

	if wantsRealtime {
		add(builtin.ToolRealtimeMetrics)
	}
	if wantsHistory {
		add(builtin.ToolMetricHistory)
	}
	if wantsAccounts {
		add(builtin.ToolManagedAccounts)
		if containsAny(text, "可用", "不可用", "总数", "数量") {
			add(builtin.ToolRealtimeMetrics)
		}
	}
	if wantsUsage || modelScopedUsage {
		add(builtin.ToolUsageFilterOptions, builtin.ToolUsageRecords, builtin.ToolUsageSummary)
	}
	if wantsDashboard {
		add(builtin.ToolDashboardSummary)
	}
	if wantsHealth {
		add(builtin.ToolInstanceHealth)
	}
	if wantsAlerts {
		add(builtin.ToolOpenAlerts, builtin.ToolInstanceHealth)
	}
	if wantsRuntime {
		add(builtin.ToolRuntimeContext)
	}
	if wantsGuide {
		add(builtin.ToolGuide)
	}

	if len(selected) > 0 || wantsInstances {
		add(builtin.ToolListInstances)
	}
	if len(selected) == 0 && looksLikeOperationalQuery(text) {
		add(builtin.AllDataToolNames()...)
		if wantsGuide {
			add(builtin.ToolGuide)
		}
	}

	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func selectAssistantToolsForConversation(message string, history []provider.Message) []string {
	selected := selectAssistantToolNames(message)
	if hasDomainTool(selected) || !looksLikeFollowUp(message) {
		return selected
	}
	merged := make(map[string]struct{}, len(selected)+4)
	for _, name := range selected {
		merged[name] = struct{}{}
	}
	for index := len(history) - 2; index >= 0; index-- {
		if history[index].Role != provider.RoleUser {
			continue
		}
		for _, name := range selectAssistantToolNames(history[index].Content) {
			if name != builtin.ToolRuntimeContext && name != builtin.ToolGuide {
				merged[name] = struct{}{}
			}
		}
		break
	}
	result := make([]string, 0, len(merged))
	for name := range merged {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func hasDomainTool(names []string) bool {
	for _, name := range names {
		if name != builtin.ToolListInstances && name != builtin.ToolRuntimeContext && name != builtin.ToolGuide {
			return true
		}
	}
	return false
}

func looksLikeFollowUp(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return len([]rune(text)) <= 24 && containsAny(text, "那", "这个", "这些", "它", "还有", "继续", "分别", "再查", "呢")
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func looksLikeOperationalQuery(text string) bool {
	return containsAny(text, "查询", "查看", "多少", "数据", "指标", "状态", "统计", "汇总", "明细", "列表", "情况", "为什么", "是否")
}
