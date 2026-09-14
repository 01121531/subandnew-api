package authz

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/01121531/subandnew-api/common"
)

// This registry names normalized API fields, not arbitrary upstream payloads.
// Restricted callers only receive known fields; new DTO fields are closed by default.
var dataFieldGroups = map[string]string{}
var dataSafeKeys = map[string]bool{}

var dataFieldDependencies = map[string][]string{
	"vendor_email":            {"vendor", "email"},
	"requests_per_account":    {"requests", "accounts"},
	"average_requests":        {"requests", "accounts"},
	"average_cost":            {"amount", "accounts"},
	"avg_cost":                {"amount", "accounts"},
	"amount_per_request":      {"amount", "requests"},
	"cost_per_request":        {"amount", "requests"},
	"successful_requests_24h": {"requests", "rates"},
	"limited_requests_24h":    {"requests", "status"},
}

func init() {
	groups := map[string]string{
		"amount":      "amount amounts currency cost cost_unit cost_7d cost_30d today_cost actual_cost total_cost total_cost_7d total_cost_30d lifetime_cost cost_excluding_today costs_excluding_today balance quota used_quota remaining_quota total_quota amount_usd actual_amount quota_consumed input_price_per_m output_price_per_m cache_read_price_per_m cache_create_price_per_m cost_samples today_cost_samples cost_7d_samples cost_30d_samples cost_observed_at today_cost_observed_at cost_7d_observed_at cost_30d_observed_at cost_stale today_cost_stale cost_7d_stale cost_30d_stale amount_status total_amount total_amount_usd daily_cost real_cost",
		"requests":    "requests total_requests request_count requests_24h successful_requests_24h limited_requests_24h today_requests request_total total_calls",
		"tokens":      "tokens total_tokens prompt_tokens completion_tokens input_tokens output_tokens cache_creation_input_tokens cache_read_input_tokens cache_tokens today_tokens token_count",
		"rpm":         "rpm rpm_capacity rpm_max rpm_sum rpm_samples rpm_observed_at rpm_stale max_rpm rpm_limit current_rpm",
		"concurrency": "concurrency concurrency_used concurrency_max concurrency_collection_status concurrency_observed_at active_sessions max_sessions max_concurrent sessions concurrency_samples",
		"accounts":    "accounts_total accounts_available accounts_rate_limited accounts_reporting accounts_collection_status account_count total_accounts available_accounts total_rows total total_pages available unavailable unknown enabled_count unhealthy_count selected_count matched_count count counts resource_count enabled unhealthy",
		"rates":       "success_rate error_rate success_rate_sample_count success_rate_samples utilization_5h utilization_7d utilization_7d_oi coverage metric_coverage instance_availability availability_percentage",
		"email":       "email account_email user_email",
		"vendor":      "vendor_id vendor_name vendor_email owner_user_id ownership vendor_collection_status vendor_observed_at vendor_stale vendor_error_code",
		"group":       "group groups group_ids user_group token_group",
		"status":      "status available rate_limited health_status last_error error_message error_code consecutive_failures alert_failure_threshold enabled disabled_at recovery_window fable_recovery_window",
		"time":        "created_at last_activity_at last_used_at last_seen_at last_checked_at expires_at updated_at observed_at collected_at last_attempt_at last_success_at first_seen_at recovered_at response_time_ms latency duration_ms elapsed_time",
	}
	for group, keys := range groups {
		for _, key := range strings.Fields(keys) {
			dataFieldGroups[key] = group
		}
	}
	// Availability booleans are status fields; numeric resource counts also
	// require the account-count grant in projectNode.
	dataFieldGroups["available"], dataFieldGroups["enabled"] = "status", "status"
	for group, keys := range map[string]string{
		"amount":   "account_stats_cost account_billed_cost account_rate_multiplier amount_per_request cost_per_request average_cost avg_cost",
		"requests": "requests_per_account average_requests",
		"tokens":   "cache_read_tokens cache_creation_tokens cache_5m_tokens cache_1h_tokens token_total cache_creation_5m_input_tokens cache_creation_1h_input_tokens",
		"group":    "group_id",
		"rates":    "success_rate_summary",
		"time":     "duration elapsed timestamp_ms started_at finished_at first_token_ms use_time total_is_exact",
	} {
		for _, key := range strings.Fields(keys) {
			dataFieldGroups[key] = group
		}
	}
	dataFieldGroups["total_is_exact"] = "accounts"
	for group, keys := range map[string]string{
		"rpm":         "capacity samples",
		"rates":       "success_rate_observed_at",
		"accounts":    "added_accounts collected_accounts account_samples record_count processed target_count selection_count",
		"amount":      "today_cost_complete",
		"concurrency": "active_session_samples",
	} {
		for _, key := range strings.Fields(keys) {
			dataFieldGroups[key] = group
		}
	}
	for _, key := range strings.Fields("observation snapshot account accounts task enqueued range_key operation batch_id operation_id actions action parameters result status_message stream_status last_error_code refresh_recommended frozen available_fields data_policy authorization_version source snapshot_updated updated_at_ms model model_name token_name user_name username request_id request_type billing_type duration elapsed timestamp_ms account_name instance source_name actor_id executed_by queue_position file_format export_kind file_size warning_count started_at finished_at") {
		dataSafeKeys[key] = true
	}
	for _, key := range strings.Fields("id id_text account_id instance_id source_instance_id instance_ids instance_name name platform kind type resource_kind source_id source_name environment display_name dataset preset_days page page_size has_more next_cursor total_is_exact usage_window_days date timestamp bucket_start bucket_end bucket window start end timezone granularity mode items data result summary resources trend points sources inventory account_output ranges snapshots metrics today success_rate_summary value unit collection_status last_attempt_status refresh_recommended next_refresh_at stale partial no_data sample_count completeness incomplete partial_data error message success pending task_id progress stage status_code file_name capabilities version etag filters filter_options fields options label label_key source snapshot_version schema_version updated_ranges source_range range_start range_end missing_instances unsupported_instances range status_message collected_instances") {
		dataSafeKeys[key] = true
	}
}

func DataFieldGroup(field string) string { return dataFieldGroups[field] }

func KnownDataField(field string) bool { return dataFieldGroups[field] != "" || dataSafeKeys[field] }

func (a *DataAccess) CheckDataField(field string) error {
	for _, dependency := range dataFieldDependencies[field] {
		if !a.HasField(dependency) {
			return ErrDataForbidden
		}
	}
	if field != "" && !a.AllFields() && !KnownDataField(field) {
		return ErrDataForbidden
	}
	if group := DataFieldGroup(field); group != "" && !a.HasField(group) {
		return ErrDataForbidden
	}
	return nil
}

func (a *DataAccess) Project(value any) (any, error) {
	if a.Role >= common.RoleRootUser {
		return value, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	return a.projectNode(data), nil
}

func (a *DataAccess) projectNode(value any) any {
	switch value := value.(type) {
	case []any:
		result := make([]any, 0, len(value))
		for _, item := range value {
			result = append(result, a.projectNode(item))
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for key, item := range value {
			if len(dataFieldDependencies[key]) > 0 && a.CheckDataField(key) != nil {
				continue
			}
			if !a.AllFields() {
				switch key {
				case "message", "error", "status_message", "error_message", "last_error":
					continue
				}
			}
			switch strings.ToLower(key) {
			case "password", "secret", "token", "access_token", "refresh_token", "session_key", "cookie", "cookies", "authorization", "base_url", "url", "credential", "api_key":
				continue
			}
			if strings.HasSuffix(key, "_observed_at") && !a.HasField("time") {
				continue
			}
			if key == "status" && value["task_id"] != nil {
				result[key] = a.projectNode(item)
				continue
			}
			if (key == "available" || key == "enabled") && !a.HasField("accounts") {
				if _, number := item.(json.Number); number {
					continue
				}
			}
			if group := dataFieldGroups[key]; group != "" {
				if !a.HasField(group) {
					continue
				}
				// Currency-keyed amount aggregates do not contain schema field names.
				if key == "amounts" || key == "costs_excluding_today" {
					if amounts, ok := item.(map[string]any); ok {
						filtered := map[string]any{}
						for unit, amount := range amounts {
							if _, valid := amount.(json.Number); valid || amount == nil {
								filtered[unit] = amount
							}
						}
						result[key] = filtered
					} else {
						result[key] = a.projectNode(item)
					}
					continue
				}
			} else if !dataSafeKeys[key] && !a.AllFields() {
				continue
			}
			result[key] = a.projectNode(item)
		}
		return result
	default:
		return value
	}
}
