package managedinstance

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
)

type OperationTaskView struct {
	Operation *OperationView            `json:"operation"`
	Task      *model.SystemTaskResponse `json:"task,omitempty"`
}

// ProjectManagedInstanceDTO preserves only the structure of these reviewed DTOs.
// It must not be used as a generic escape hatch around DataAccess.Project.
// Callers must check Current and the endpoint's instance/owner scope first.
func ProjectManagedInstanceDTO(access *authz.DataAccess, value any) (any, error) {
	if access == nil || access.Role >= common.RoleRootUser || value == nil {
		return value, nil
	}
	p := managedDTOProjection{access: access}
	var project func(any) any
	switch value.(type) {
	case *OperationView:
		project = p.operation
	case *BatchOperationView:
		project = p.batch
	case OperationTaskView:
		project = func(v any) any {
			n := dtoObject(v)
			out := map[string]any{"operation": p.operation(n["operation"])}
			if task, ok := n["task"]; ok {
				out["task"] = p.task(task)
			}
			return out
		}
	case model.SystemTaskResponse:
		project = p.task
	case *ConfigTemplateView:
		project = p.template
	case *ConfigTemplateList:
		project = func(v any) any { return map[string]any{"items": dtoList(dtoObject(v)["items"], p.template)} }
	case *ConfigBindingView:
		project = p.binding
	case *ConfigPreview:
		project = p.preview
	case []ConfigSchema:
		project = func(v any) any { return dtoList(v, p.schema) }
	case *AccountFilterTemplateView:
		project = p.filterTemplate
	case []*AccountFilterTemplateView:
		project = func(v any) any { return dtoList(v, p.filterTemplate) }
	default:
		return nil, authz.ErrDataForbidden
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
	if node == nil {
		return nil, nil
	}
	return project(node), nil
}

type managedDTOProjection struct{ access *authz.DataAccess }

func dtoObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func dtoList(value any, project func(any) any) []any {
	out := []any{}
	items, _ := value.([]any)
	for _, item := range items {
		if projected := project(item); projected != nil {
			out = append(out, projected)
		}
	}
	return out
}

// Dynamic containers must be objects, never opaque (or double-encoded) JSON.
func dtoScalar(value any) bool {
	switch v := value.(type) {
	case nil, bool, json.Number:
		return true
	case string:
		v = strings.TrimSpace(v)
		return !strings.HasPrefix(v, "{") && !strings.HasPrefix(v, "[") && !strings.HasPrefix(v, `"`)
	default:
		return false
	}
}

func (p managedDTOProjection) fields(value any, keys string) map[string]any {
	out := map[string]any{}
	node := dtoObject(value)
	for _, key := range strings.Fields(keys) {
		if item, ok := node[key]; ok && dtoScalar(item) {
			switch key {
			case "id", "instance_id", "actor_id", "executed_by", "resource_id", "template_id", "schema_version", "created_by", "updated_by", "position", "version",
				"planned_at", "executed_at", "finished_at", "created_at", "updated_at", "observed_at", "last_checked_at", "last_applied_at", "start_time", "checked_at", "latency_ms",
				"target_count", "count", "total", "planned", "active", "failed", "unknown", "min", "max", "min_length", "max_length":
				if _, ok := item.(json.Number); !ok {
					continue
				}
			case "writes_remote", "idempotent_replay", "enabled", "verified", "compensated":
				if _, ok := item.(bool); !ok {
					continue
				}
			case "succeeded":
				switch item.(type) {
				case bool, json.Number:
				default:
					continue
				}
			}
			out[key] = item
		}
	}
	return out
}

func (p managedDTOProjection) group(out map[string]any, value any, group, keys string) {
	if !p.access.HasField(group) {
		return
	}
	for key, item := range p.fields(value, keys) {
		out[key] = item
	}
}

func (p managedDTOProjection) operation(value any) any {
	if value == nil {
		return nil
	}
	n := dtoObject(value)
	out := p.fields(n, "id operation_id instance_id task_id actor_id executed_by action status risk_level writes_remote required_capability idempotent_replay")
	p.group(out, n, "time", "planned_at executed_at finished_at created_at updated_at")
	out["parameters"] = p.parameters(n["parameters"])
	out["plan"] = p.plan(n["plan"])
	if result, ok := n["result"]; ok {
		out["result"] = p.result(result)
	}
	return out
}

func (p managedDTOProjection) parameters(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "resource_id template_id schema_version")
	p.group(out, n, "status", "enabled")
	if ids, ok := n["resource_ids"].([]any); ok {
		out["resource_ids"] = dtoList(ids, func(id any) any {
			if _, ok := id.(json.Number); ok {
				return id
			}
			return nil
		})
	}
	for _, key := range []string{"desired", "rollback"} {
		if v, ok := n[key]; ok {
			out[key] = p.configValues(v)
		}
	}
	return out
}

func (p managedDTOProjection) plan(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "action risk_level writes_remote required_capability template_id")
	p.group(out, n, "accounts", "target_count")
	if differences, ok := n["differences"]; ok {
		visible := dtoList(differences, p.diff)
		out["differences"] = visible
		if p.access.HasField("accounts") {
			out["target_count"] = len(visible)
		}
	}
	return out
}

func (p managedDTOProjection) result(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "action resource_kind")
	p.group(out, n, "accounts", "count")
	p.group(out, n, "status", "verified compensated")
	if items, ok := n["items"]; ok {
		out["items"] = dtoList(items, func(v any) any {
			item := p.fields(v, "resource_id")
			p.group(item, v, "status", "succeeded enabled")
			return item
		})
	}
	if fields, ok := n["changed_fields"]; ok {
		out["changed_fields"] = dtoList(fields, func(v any) any {
			key, ok := v.(string)
			if ok && p.configField(key) {
				return key
			}
			return nil
		})
	}
	return out
}

func (p managedDTOProjection) batch(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "id batch_id actor_id executed_by action status idempotent_replay")
	p.group(out, n, "accounts", "target_count")
	p.group(out, n, "time", "planned_at executed_at finished_at created_at updated_at")
	out["summary"] = map[string]any{}
	p.group(out["summary"].(map[string]any), n["summary"], "accounts", "total planned active succeeded failed unknown")
	out["items"] = dtoList(n["items"], func(v any) any {
		item := p.fields(v, "instance_id position status")
		child := dtoObject(v)
		item["parameters"] = p.parameters(child["parameters"])
		if op, ok := child["operation"]; ok {
			item["operation"] = p.operation(op)
		}
		return item
	})
	return out
}

func (p managedDTOProjection) task(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "id task_id type status")
	p.group(out, n, "time", "created_at updated_at")
	out["payload"] = p.fields(n["payload"], "operation_id instance_id actor_id batch_id")
	// State, errors, and worker bookkeeping have no stable public data schema.
	out["state"] = map[string]any{}
	out["result"] = nil
	if result := dtoObject(n["result"]); result != nil {
		switch n["type"] {
		case model.SystemTaskTypeManagedInstanceOperation:
			if _, ok := result["operation_id"]; !ok {
				return out
			}
			out["result"] = p.operation(result)
		case model.SystemTaskTypeManagedInstanceProbe:
			probe := p.fields(result, "kind")
			p.group(probe, result, "status", "status")
			p.group(probe, result, "time", "start_time checked_at latency_ms")
			out["result"] = probe
		}
	}
	return out
}

// Public config keys are deliberately enumerated: adding a remote config field
// does not automatically make its before/after values visible to delegates.
func (p managedDTOProjection) configField(key string) bool {
	switch key {
	case "ui.site_name", "ui.logo_url", "ui.site_subtitle", "ui.docs_url", "ui.compact_home", "ui.table_page_size":
		return true
	default:
		return false
	}
}

func (p managedDTOProjection) configValues(value any) any {
	out := map[string]any{}
	for key, v := range dtoObject(value) {
		if p.configField(key) && dtoScalar(v) {
			out[key] = v
		}
	}
	return out
}

func (p managedDTOProjection) diff(value any) any {
	n := dtoObject(value)
	key, _ := n["key"].(string)
	if !p.configField(key) || !dtoScalar(n["current"]) || !dtoScalar(n["desired"]) {
		return nil
	}
	return p.fields(n, "key current desired")
}

func (p managedDTOProjection) template(value any) any {
	if value == nil {
		return nil
	}
	n := dtoObject(value)
	out := p.fields(n, "id name kind schema_version created_by updated_by")
	p.group(out, n, "time", "created_at updated_at")
	out["values"] = p.configValues(n["values"])
	return out
}

func (p managedDTOProjection) binding(value any) any {
	if value == nil {
		return nil
	}
	n := dtoObject(value)
	out := p.fields(n, "id instance_id template_id mode created_by updated_by")
	values := dtoObject(dtoObject(n["template"])["values"])
	if values != nil && len(values) == len(p.configValues(values).(map[string]any)) {
		p.group(out, n, "status", "drift_status")
	}
	p.group(out, n, "time", "last_checked_at last_applied_at created_at updated_at")
	out["template"] = p.template(n["template"])
	return out
}

func (p managedDTOProjection) preview(value any) any {
	n := dtoObject(value)
	differences := dtoList(n["differences"], p.diff)
	out := map[string]any{"binding": p.binding(n["binding"]), "observed": p.configValues(n["observed"]),
		"desired": p.configValues(n["desired"]), "differences": differences}
	p.group(out, n, "time", "observed_at")
	if p.access.HasField("status") {
		out["drifted"] = len(differences) > 0
	}
	// Hashes describe the entire input, including any removed fields.
	return out
}

func (p managedDTOProjection) schema(value any) any {
	n := dtoObject(value)
	out := p.fields(n, "kind version")
	out["fields"] = dtoList(n["fields"], func(v any) any {
		field := dtoObject(v)
		key, _ := field["key"].(string)
		if !p.configField(key) {
			return nil
		}
		out := p.fields(field, "key remote_key type description min max min_length max_length format")
		if values, ok := field["enum"]; ok {
			out["enum"] = dtoList(values, func(v any) any {
				if dtoScalar(v) {
					return v
				}
				return nil
			})
		}
		return out
	})
	return out
}

func (p managedDTOProjection) filterTemplate(value any) any {
	n := dtoObject(value)
	rules, ok := n["rules"].([]any)
	if !ok {
		return nil
	}
	visible := []any{}
	for _, v := range rules {
		rule := dtoObject(v)
		field, _ := rule["field"].(string)
		if _, known := accountFilterFieldType[field]; !known || p.access.CheckDataField(field) != nil {
			return nil
		}
		out := p.fields(rule, "field operator value_mode")
		values := []any{}
		if input, ok := rule["values"].([]any); ok {
			for _, v := range input {
				if _, ok := v.(string); !ok || !dtoScalar(v) {
					return nil
				}
				values = append(values, v)
			}
		}
		out["values"] = values
		visible = append(visible, out)
	}
	out := p.fields(n, "id name match_mode")
	p.group(out, n, "time", "created_at updated_at")
	out["rules"] = visible
	return out
}
