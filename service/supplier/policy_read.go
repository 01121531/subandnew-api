package supplier

import (
	"github.com/01121531/subandnew-api/model"
	"net/url"
	"strconv"
	"strings"
)

func cloneQuery(q url.Values) url.Values {
	result := url.Values{}
	for key, values := range q {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func validatePolicyQuery(policy *model.SupplierEffectivePolicy, resource string, q url.Values) error {
	if resource != "accounts" {
		return nil
	}
	values := policy.Values
	if !values["account.email"] && q.Get("search") != "" {
		return fail(403, "supplier_field_forbidden")
	}
	if !values["account.status"] {
		for _, key := range []string{"status", "recovery_window", "fable_recovery_window"} {
			if v := q.Get(key); v != "" && v != "all" {
				return fail(403, "supplier_field_forbidden")
			}
		}
	}
	sort := q.Get("sort")
	if sort == "" {
		q.Set("sort", "name")
		return nil
	}
	if sort == "name" {
		return nil
	}
	key := "account." + sort
	if !values[key] {
		return fail(403, "supplier_field_forbidden")
	}
	return nil
}

func redactPolicy(result map[string]any, resource string, policy *model.SupplierEffectivePolicy, q url.Values) {
	remove := func(row map[string]any, prefix string) {
		for _, key := range model.SupplierPolicyKeys {
			if strings.HasPrefix(key, prefix) {
				field := strings.TrimPrefix(key, prefix)
				if !policy.Values[key] {
					delete(row, field)
				} else if _, present := row[field]; !present {
					row[field] = nil
				}
			}
		}
	}
	switch resource {
	case "accounts":
		items, _ := result["items"].([]any)
		for _, item := range items {
			if row, ok := item.(map[string]any); ok {
				remove(row, "account.")
				for field, limit := range map[string]string{"rpm": "max_rpm", "tpm": "max_tpm", "concurrent": "max_concurrent", "active_sessions": "max_sessions"} {
					if !policy.Values["account."+field] {
						delete(row, limit)
					}
				}
				if !policy.Values["account.status"] {
					for _, key := range accountDiagnosticFields {
						delete(row, key)
					}
				}
			}
		}
		page, _ := strconv.Atoi(q.Get("page"))
		if page < 1 {
			page = 1
		}
		size, _ := strconv.Atoi(q.Get("page_size"))
		if size < 1 {
			size = 50
		}
		more := len(items) >= size
		if total, ok := remoteNumber(result["total"]).(float64); ok {
			more = float64(page*size) < total
		}
		result["has_more"] = more
		if !policy.Values["summary.total_accounts"] {
			delete(result, "total")
		}
	case "account-summary":
		remove(result, "summary.")
	case "usage":
		for _, key := range []string{"days", "accounts"} {
			rows, _ := result[key].([]any)
			for _, item := range rows {
				if row, ok := item.(map[string]any); ok {
					remove(row, "usage.")
				}
			}
		}
	}
}
