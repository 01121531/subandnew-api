# 汇总与历史

使用 `get_dashboard_summary` 查询 1、7、14、30 天的请求数、Token、实际费用和每日趋势。使用 `get_metric_history` 查询过去 31 天的实时指标历史或某个时间点。

相对日期需要转换成明确范围时先调用 `get_runtime_context`。所有工具时间已经是 Asia/Shanghai，不得再次加减 8 小时。
