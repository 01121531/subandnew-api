package supplier

import (
	"math"
	"strings"
)

func remoteImportBody(m map[string]any, action string) (map[string]any, error) {
	credentials := map[string]any{}
	if action == "import-rt" {
		for _, key := range []string{"refresh_token", "access_token"} {
			value, exists := m[key]
			delete(m, key)
			if !exists && key == "access_token" {
				continue
			}
			text, ok := value.(string)
			if !ok || !validImportSecret(strings.TrimSpace(text)) {
				return nil, remoteErr(400, "invalid_parameters")
			}
			credentials[key] = strings.TrimSpace(text)
		}
	} else {
		values, ok := m["session_keys"].([]any)
		delete(m, "session_keys")
		if !ok || len(values) == 0 || len(values) > 20 {
			return nil, remoteErr(400, "invalid_parameters")
		}
		keys := make([]string, 0, len(values))
		seen := map[string]bool{}
		for _, value := range values {
			text, ok := value.(string)
			if !ok || !validImportSecret(strings.TrimSpace(text)) {
				return nil, remoteErr(400, "invalid_parameters")
			}
			text = strings.TrimSpace(text)
			if !seen[text] {
				keys = append(keys, text)
				seen[text] = true
			}
		}
		credentials["session_keys"] = keys
	}
	payload, err := remoteUploadBody(m, false)
	if err != nil {
		return nil, err
	}
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" || payload["oauth_flow"] != "login" {
		return nil, remoteErr(400, "invalid_parameters")
	}
	delete(payload, "oauth_flow")
	delete(payload, "provider")
	delete(payload, "overwrite")
	for key, value := range credentials {
		payload[key] = value
	}
	return payload, nil
}

// Only normalized, authorized proxy IDs can reach the batch endpoint.
func remoteSKPayload(payload map[string]any) map[string]any {
	source := map[string]any{"mode": "direct"}
	switch payload["outbound_proxy_mode"] {
	case "manual":
		source = map[string]any{"mode": "existing", "existing_ids": []string{payload["outbound_proxy_id"].(string)}}
	case "auto":
		source["mode"] = "auto"
	}
	payload["proxy_source"] = source
	payload["name_prefix"] = payload["name"]
	for _, key := range []string{"outbound_proxy_mode", "outbound_proxy_id", "name", "overwrite_existing"} {
		delete(payload, key)
	}
	return payload
}

func remoteImportStatus(row map[string]any) string {
	ok, hasOK := row["ok"].(bool)
	duplicate, hasDuplicate := row["duplicate"].(bool)
	status, _ := row["status"].(string)
	for _, key := range []string{"ok", "duplicate", "success"} {
		if value, exists := row[key]; exists {
			if _, valid := value.(bool); !valid {
				return "unknown"
			}
		}
	}
	if value, exists := row["status"]; exists {
		if _, valid := value.(string); !valid {
			return "unknown"
		}
	}
	if (ok || status == "imported") && (row["success"] == false || (row["error"] != nil && row["error"] != "")) {
		return "unknown"
	}
	if ok && duplicate {
		return "unknown"
	}
	if ok {
		if status != "" && status != "imported" {
			return "unknown"
		}
		return "imported"
	}
	if duplicate {
		if status != "" && status != "duplicate" {
			return "unknown"
		}
		return "duplicate"
	}
	if status == "imported" && !hasOK {
		return "imported"
	}
	if status == "duplicate" && !hasDuplicate {
		return "duplicate"
	}
	if status == "imported" || status == "duplicate" {
		return "unknown"
	}
	switch status {
	case "proxy_failed", "quota_blocked", "sk_invalid", "reauth_required", "failed":
		return status
	case "paused", "pending", "running":
		return "unknown"
	}
	message, _ := row["error"].(string)
	message = strings.ToLower(message)
	// Map known error categories locally; never expose raw upstream errors.
	switch {
	case strings.Contains(message, "session_stale_relogin"), strings.Contains(message, "session is not fresh enough"):
		return "reauth_required"
	case strings.Contains(message, "sessionkey invalid"), strings.Contains(message, "account_session_invalid"), strings.Contains(message, "account has no organizations"):
		return "sk_invalid"
	case strings.Contains(message, "license_quota_exceeded"), strings.Contains(message, "license_invalid"):
		return "quota_blocked"
	case strings.Contains(message, "proxy"), strings.Contains(message, "econn"), strings.Contains(message, "etimedout"), strings.Contains(message, "tunnel"):
		return "proxy_failed"
	case hasOK || message != "":
		return "failed"
	default:
		return "unknown"
	}
}

func remoteImportResult(value any, action string, count int) map[string]any {
	statuses := make([]string, count)
	for i := range statuses {
		statuses[i] = "unknown"
	}
	response, err := remoteObject(value)
	if err == nil && action == "import-rt" {
		if response["duplicate"] == true {
			statuses[0] = remoteImportStatus(response)
		} else if remoteID(response["id"]) != "" && response["ok"] != false && response["success"] != false && response["error"] == nil {
			// RT returns an account; its status describes availability, not import outcome.
			response["status"] = "imported"
			statuses[0] = remoteImportStatus(response)
		} else if response["ok"] == false || response["error"] != nil {
			statuses[0] = remoteImportStatus(response)
		}
	}
	if err == nil && action == "import-sk" {
		rows, ok := response["results"].([]any)
		invalid := !ok
		if total, exists := response["total"]; exists {
			if n, valid := remoteNumber(total).(float64); !valid || n != float64(count) {
				invalid = true
			}
		}
		seen := map[int]bool{}
		for _, raw := range rows {
			row, valid := raw.(map[string]any)
			if !valid {
				invalid = true
				continue
			}
			n, valid := remoteNumber(row["index"]).(float64)
			if !valid || math.Trunc(n) != n || n < 0 || n >= float64(count) {
				invalid = true
				continue
			}
			i := int(n)
			if seen[i] {
				statuses[i] = "unknown"
			} else {
				statuses[i] = remoteImportStatus(row)
			}
			seen[i] = true
		}
		if invalid {
			for i := range statuses {
				statuses[i] = "unknown"
			}
		}
	}
	rows := make([]map[string]any, count)
	ok, duplicate, failed, unknown := 0, 0, 0, 0
	for i, status := range statuses {
		switch status {
		case "imported":
			ok++
		case "duplicate":
			duplicate++
		case "unknown":
			unknown++
		default:
			failed++
		}
		rows[i] = map[string]any{"index": i, "status": status}
	}
	return map[string]any{"total": count, "ok": ok, "duplicate": duplicate, "failed": failed, "unknown": unknown, "results": rows}
}
