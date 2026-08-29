# 使用记录

使用 `get_usage_record_summary` 回答时间段请求数、Token 和费用。只有需要逐条记录时才使用 `query_usage_records`，默认每页 20 条。

筛选值不明确时先调用 `get_usage_record_filter_options`。记录明细一次只能查询一个实例；Claude Gateway 不支持时应明确返回 unsupported。
