package billingalert

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

func OptionalAccess(values []*authz.DataAccess) *authz.DataAccess {
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

func MetricField(metric string) string {
	switch metric {
	case "today_cost":
		return "amount"
	case "requests", "tokens", "rpm":
		return metric
	case "rpm_capacity", "rpm_utilization":
		return "rpm"
	case "accounts_available", "accounts_total", "accounts_availability":
		return "accounts"
	case "concurrency_used", "concurrency_max", "concurrency_utilization", "active_sessions":
		return "concurrency"
	case "success_rate":
		return "rates"
	case "instance_connected", "unhealthy_instances":
		return "status"
	}
	return ""
}

func MetricAllowed(a *authz.DataAccess, metric string) bool {
	return a == nil || (MetricField(metric) != "" && a.HasField(MetricField(metric)))
}

// Exclude the entire rule when even one binding is outside the principal's scope.
// Table names are internal constants, never request input.
func ScopeRules(q *gorm.DB, a *authz.DataAccess, table, bindings string) *gorm.DB {
	if a == nil || a.Policy.InstanceScope == "all" {
		return q
	}
	if len(a.Policy.InstanceIDs) == 0 {
		return q.Where("1 = 0")
	}
	return q.Where("EXISTS (SELECT 1 FROM "+bindings+" b WHERE b.rule_id = "+table+".id)").
		Where("NOT EXISTS (SELECT 1 FROM "+bindings+" b WHERE b.rule_id = "+table+".id AND b.instance_id NOT IN ?)", a.Policy.InstanceIDs)
}

func CheckRuleAccess(a *authz.DataAccess, id int64, table, bindings string) error {
	if a == nil {
		return nil
	}
	var count int64
	if err := ScopeRules(model.DB.Table(table), a, table, bindings).Where(table+".id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return authz.ErrDataForbidden
	}
	return nil
}

// Updating a shared template also resets every rule using it.
func CheckTemplateAccess(a *authz.DataAccess, id int64) error {
	if a == nil {
		return nil
	}
	if err := a.Current(model.DB); err != nil {
		return err
	}
	if !a.AllFields() {
		return authz.ErrDataForbidden
	}
	var ids []int64
	if err := model.DB.Model(&model.BillingAlertRule{}).Where("template_id = ?", id).Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, ruleID := range ids {
		if err := CheckRuleAccess(a, ruleID, "billing_alert_rules", "billing_alert_rule_instances"); err != nil {
			return err
		}
	}
	return nil
}

func ValidateRecordFilter(filter AlertRecordFilter, a *authz.DataAccess) error {
	if a == nil {
		return nil
	}
	if filter.InstanceID > 0 && !a.HasInstance(filter.InstanceID) {
		return authz.ErrDataForbidden
	}
	if filter.MetricKey != "" && !MetricAllowed(a, filter.MetricKey) {
		return authz.ErrDataForbidden
	}
	for field, used := range map[string]bool{"amount": filter.Currency != "", "email": filter.Recipient != "", "time": filter.StartTime != 0 || filter.EndTime != 0, "status": filter.EventType != ""} {
		if used && !a.HasField(field) {
			return authz.ErrDataForbidden
		}
	}
	if filter.RuleID > 0 {
		table, bindings := "billing_alert_rules", "billing_alert_rule_instances"
		switch filter.SourceType {
		case model.AlertSourceMetric:
			table, bindings = "metric_alert_rules", "metric_alert_rule_instances"
		case model.AlertSourceInstance:
			table, bindings = "managed_instance_alert_rules", "managed_instance_alert_rule_instances"
		case model.AlertSourceBilling:
		default:
			return authz.ErrDataForbidden
		}
		return CheckRuleAccess(a, filter.RuleID, table, bindings)
	}
	return nil
}

// Aggregate historical events do not freeze their contributing instance IDs.
// Selected-scope readers must not receive those unverifiable snapshots.
func ScopeEvents(q *gorm.DB, a *authz.DataAccess) *gorm.DB {
	if a != nil {
		return a.ScopeQuery(q, "billing_alert_events.instance_id")
	}
	return q
}

// ProjectAlertData is a closed-schema DTO projection. Opaque human text and
// serialized detail blobs are not safe substitutes for metric-aware fields.
func ProjectAlertData(value any, a *authz.DataAccess) (any, error) {
	if a == nil || (a.AllFields() && a.Policy.InstanceScope == "all") {
		return value, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var node any
	if err := decoder.Decode(&node); err != nil {
		return nil, err
	}
	return projectAlertNode(node, a, ""), nil
}

func projectAlertNode(node any, a *authz.DataAccess, metric string) any {
	switch node := node.(type) {
	case []any:
		result := make([]any, 0, len(node))
		for _, item := range node {
			if projected := projectAlertNode(item, a, metric); projected != nil {
				result = append(result, projected)
			}
		}
		return result
	case map[string]any:
		if !alertNodeInScope(node, a) {
			return nil
		}
		if m, ok := node["metric"].(string); ok {
			metric = m
		}
		if m, ok := node["metric_key"].(string); ok {
			metric = m
		}
		if m, ok := node["key"].(string); ok && MetricField(m) != "" {
			metric = m
		}
		if source, _ := node["source_type"].(string); source == model.AlertSourceBilling {
			metric = "today_cost"
		}
		result := map[string]any{}
		for key, item := range node {
			if key == "status" && node["task_id"] != nil {
				result[key] = item
				continue
			}
			if (strings.HasPrefix(key, "email_") || strings.HasPrefix(key, "recovery_email_")) && !a.HasField("email") {
				continue
			}
			field := ""
			switch key {
			case "description", "conditions", "observed_values", "last_values", "filters", "query", "error", "last_error", "email_error", "recovery_email_error", "error_message", "message", "threshold_name", "threshold_name_snapshot", "event_key":
				// Structured conditions are projected; old serialized/human detail is withheld.
				if key == "conditions" {
					if _, ok := item.([]any); ok {
						result[key] = projectAlertNode(item, a, "")
					}
				}
				continue
			case "threshold", "recovery_threshold", "current_value", "value", "unit", "operator":
				if !MetricAllowed(a, metric) {
					continue
				}
			case "metric", "metric_key":
				if _, scalar := item.(string); scalar && !MetricAllowed(a, metric) {
					continue
				}
			case "scope_key":
				if a.Policy.InstanceScope != "all" {
					continue
				}
			case "currency", "usd_total", "cny_total", "discount_rate", "exchange_rate", "exchange_source", "exchange_mode", "exchange_observed_date", "manual_exchange_rate", "exchange_override", "amount", "repeat_increment", "thresholds":
				field = "amount"
			case "recipients", "recipient", "effective_recipients", "email_recipients", "recovery_email_recipients", "deliveries", "email_status", "email_attempts", "recovery_email_status", "recovery_email_attempts", "host", "port", "security", "username", "from_address", "from_name", "password_set", "alert_recipients":
				field = "email"
			case "status", "enabled", "active", "event_type", "alert_type", "alert_types", "error_code", "last_error_code", "failure_threshold", "effective_failure_threshold", "consecutive_failures", "consecutive_violations", "consecutive_recoveries", "occurrences", "failure_notified":
				field = "status"
			default:
				if strings.HasSuffix(key, "_at") || key == "cycle_start" || key == "cycle_end" {
					field = "time"
				} else if !alertSafeKey(key) {
					continue
				}
			}
			if field != "" && !a.HasField(field) {
				continue
			}
			result[key] = projectAlertNode(item, a, metric)
		}
		return result
	default:
		return node
	}
}

func alertSafeKey(key string) bool {
	if key == "sources" || key == "source_instance_id" || key == "task" || key == "created" || key == "type" {
		return true
	}
	return strings.Contains(" id task_id actor_id file_name file_size record_count instance_id instance_ids instance_name instance_name_snapshot instance_kind instance_kind_snapshot source_type source_record_id rule_id rule_name rule_name_snapshot name page page_size total items billing metric scope_mode match_mode template_id template bindings states conditions sort_order created_by updated_by current_version system_kind timezone cycle_type cycle_config schedule_type schedule_config reminder_mode repeat_interval_seconds evaluation_interval_seconds check_interval_seconds trigger_count recovery_count phase attempts open_event_id failure_event_id scope_key cycle_id threshold_id evaluation_id key label kinds aggregatable ", " "+key+" ")
}

func alertNodeInScope(node map[string]any, a *authz.DataAccess) bool {
	if a.Policy.InstanceScope == "all" {
		return true
	}
	for _, key := range []string{"instance_id", "source_instance_id"} {
		if value, ok := node[key].(json.Number); ok {
			id, err := value.Int64()
			if err != nil || !a.HasInstance(id) {
				return false
			}
		}
	}
	if ids, ok := node["instance_ids"].([]any); ok {
		for _, value := range ids {
			number, ok := value.(json.Number)
			if !ok {
				return false
			}
			id, err := number.Int64()
			if err != nil || !a.HasInstance(id) {
				return false
			}
		}
	}
	return true
}
