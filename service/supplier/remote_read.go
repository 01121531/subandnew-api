package supplier

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var remoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validRemoteID(id string) bool { return remoteIDPattern.MatchString(id) }

func remoteID(value any) string {
	var id string
	switch v := value.(type) {
	case string:
		id = v
	case json.Number:
		id = v.String()
	}
	if !validRemoteID(id) {
		return ""
	}
	return id
}

func remoteNumber(value any) any {
	var number float64
	var err error
	switch v := value.(type) {
	case json.Number:
		number, err = v.Float64()
	case string:
		number, err = strconv.ParseFloat(strings.TrimSpace(v), 64)
	case float64:
		number = v
	case int:
		number = float64(v)
	default:
		return nil
	}
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return number
}

func remoteFields(m map[string]any, textFields, numericFields string) map[string]any {
	out := map[string]any{}
	for _, key := range strings.Fields(textFields) {
		out[key] = nil
		if key == "id" {
			if id := remoteID(m[key]); id != "" {
				out[key] = id
			}
			continue
		}
		if text, ok := m[key].(string); ok && len(text) <= 4096 {
			out[key] = text
		}
	}
	for _, key := range strings.Fields(numericFields) {
		out[key] = remoteNumber(m[key])
	}
	return out
}

func remoteObject(value any) (map[string]any, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, remoteErr(502, "invalid_response")
	}
	return m, nil
}

func remoteList(value any, keys ...string) ([]map[string]any, map[string]any, error) {
	meta := map[string]any{}
	if m, ok := value.(map[string]any); ok {
		meta = m
		value = nil
		for _, key := range append(keys, "items") {
			if v, exists := m[key]; exists {
				value = v
				break
			}
		}
	}
	list, ok := value.([]any)
	if !ok {
		return nil, nil, remoteErr(502, "invalid_response")
	}
	result := make([]map[string]any, 0, len(list))
	for _, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil, remoteErr(502, "invalid_response")
		}
		result = append(result, m)
	}
	return result, meta, nil
}

func remoteTotal(meta map[string]any) any {
	for _, key := range []string{"total", "total_rows"} {
		if n := remoteNumber(meta[key]); n != nil {
			return n
		}
	}
	if pagination, ok := meta["pagination"].(map[string]any); ok {
		for _, key := range []string{"total", "total_rows"} {
			if n := remoteNumber(pagination[key]); n != nil {
				return n
			}
		}
	}
	if summary, ok := meta["summary"].(map[string]any); ok {
		return remoteNumber(summary["total_rows"])
	}
	return nil
}

const remoteAccountsPath = "/api/admin/oauth-accounts"
const remotePoolSummaryPath = remoteAccountsPath + "/pool-summary"
const remoteProxiesPath = "/api/admin/outbound-proxies"

var remoteOptionPaths = map[string]string{
	"groups":    "/api/admin/groups",
	"policies":  "/api/admin/account-policy-templates",
	"templates": "/api/admin/cc-disguise-templates",
	"proxies":   remoteProxiesPath,
}

func remoteQuery(resource string, input url.Values) (url.Values, error) {
	out := url.Values{}
	if resource == "accounts" || resource == "proxies" {
		out.Set("page", "1")
		out.Set("limit", "50")
	}
	if resource == "accounts" {
		out.Del("limit")
		out.Set("page_size", "50")
		out.Set("page_mode", "1")
	}
	for key, values := range input {
		if len(values) != 1 || len(values[0]) > 256 {
			return nil, remoteErr(400, "invalid_query")
		}
		v := values[0]
		switch {
		case (resource == "accounts" || resource == "proxies") && (key == "page" || key == "page_size"):
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || (key == "page_size" && n > 100) || n > 100000 {
				return nil, remoteErr(400, "invalid_query")
			}
			if key == "page_size" && resource == "proxies" {
				key = "limit"
			}
			out.Set(key, strconv.Itoa(n))
		case (resource == "accounts" || resource == "proxies") && (key == "q" || key == "search"):
			key = "q"
			if resource == "accounts" {
				key = "query"
			}
			out.Set(key, v)
		case (resource == "accounts" || resource == "proxies") && key == "status":
			if v != "" && !regexp.MustCompile(`^[a-z_]{1,40}$`).MatchString(v) {
				return nil, remoteErr(400, "invalid_query")
			}
			if v == "enabled" {
				v = "active"
			}
			out.Set(key, v)
		case resource == "accounts" && (key == "recovery_window" || key == "fable_recovery_window"):
			if v != "" && !regexp.MustCompile(`^[a-z0-9_-]{1,40}$`).MatchString(v) {
				return nil, remoteErr(400, "invalid_query")
			}
			out.Set(key, v)
		case resource == "accounts" && key == "sort":
			if !strings.Contains("|name|email|status|created_at|group_name|today_cost|total_cost|total_requests|total_tokens|rpm|", "|"+v+"|") {
				return nil, remoteErr(400, "invalid_query")
			}
			out.Set(key, v)
		case resource == "accounts" && key == "direction":
			if v != "asc" && v != "desc" {
				return nil, remoteErr(400, "invalid_query")
			}
			out.Set(key, v)
		case resource == "usage" && key == "days":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 365 {
				return nil, remoteErr(400, "invalid_query")
			}
			out.Set(key, v)
		default:
			return nil, remoteErr(400, "invalid_query")
		}
	}
	return out, nil
}

func (r *remoteClient) Read(ctx context.Context, resource string, query url.Values) (map[string]any, error) {
	if resource != "accounts" && resource != "account-summary" && resource != "usage" && resource != "proxies" && resource != "account-upload/options" {
		return nil, remoteErr(400, "invalid_resource")
	}
	q, err := remoteQuery(resource, query)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := r.session()
	if err != nil {
		return nil, err
	}
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	if resource == "account-upload/options" {
		return r.options(ctx, s)
	}
	path := remoteAccountsPath
	switch resource {
	case "proxies":
		path = remoteProxiesPath
	case "usage":
		path = "/api/admin/vendor-usage"
	case "account-summary":
		// Own-account counters come only from the scoped list summary. Pool
		// metrics are fetched separately below and always explicitly prefixed.
		q.Set("page", "1")
		q.Set("page_size", "1")
		q.Set("page_mode", "1")
	}
	value, err := r.request(ctx, s, http.MethodGet, path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	switch resource {
	case "accounts", "proxies":
		list, meta, err := remoteList(value, resource)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if resource == "proxies" {
				items = append(items, remoteProxy(item))
			} else {
				n := remoteFields(item, "id name email status created_at group_name", "total_cost today_cost total_requests total_tokens")
				if n["today_cost"] == nil {
					if stats, ok := item["stats"].(map[string]any); ok {
						n["today_cost"] = remoteNumber(stats["daily_cost"])
					}
				}
				items = append(items, n)
			}
		}
		result = map[string]any{"items": items, "total": remoteTotal(meta)}
		if resource == "accounts" {
			page, _ := strconv.Atoi(q.Get("page"))
			limit, _ := strconv.Atoi(q.Get("page_size"))
			result["page"], result["page_size"] = page, limit
		}
	case "account-summary":
		m, err := remoteObject(value)
		if err != nil {
			return nil, err
		}
		if summary, ok := m["summary"].(map[string]any); ok {
			m = summary
		}
		result = remoteFields(m, "", "total_accounts available_accounts rpm")
		if result["total_accounts"] == nil {
			result["total_accounts"] = remoteTotal(m)
		}
		pool, poolErr := r.request(ctx, s, http.MethodGet, remotePoolSummaryPath, nil)
		if poolErr != nil {
			if e, ok := poolErr.(*RemoteError); !ok || e.Status != http.StatusNotFound || e.Code != "pool_summary_unsupported" {
				return nil, poolErr
			}
		} else {
			m, err := remoteObject(pool)
			if err != nil {
				return nil, err
			}
			for _, field := range []struct{ section, source, target string }{
				{"global", "rpm", "pool_rpm"},
				{"global", "concurrent", "pool_concurrent"},
				{"pool", "available_accounts", "pool_available_accounts"},
			} {
				if m[field.section] == nil {
					continue
				}
				section, err := remoteObject(m[field.section])
				if err != nil {
					return nil, err
				}
				if value, exists := section[field.source]; exists {
					result[field.target] = remoteNumber(value)
				}
			}
		}
	case "usage":
		m, err := remoteObject(value)
		if err != nil {
			return nil, err
		}
		result = map[string]any{}
		for _, key := range []string{"days", "accounts"} {
			list, _, err := remoteList(m[key])
			if err != nil {
				return nil, err
			}
			items := make([]map[string]any, 0, len(list))
			for _, item := range list {
				fields := "date"
				if key == "accounts" {
					fields = "id name"
					if item["id"] == nil {
						item["id"] = item["account_id"]
					}
				} else if item["date"] == nil {
					item["date"] = item["day"]
				}
				items = append(items, remoteFields(item, fields, "requests tokens cost"))
			}
			result[key] = items
		}
	}
	r.redact(s, result)
	return result, nil
}

func remoteProxy(m map[string]any) map[string]any {
	out := remoteFields(m, "id name scheme host status health_status", "port latency_ms")
	if out["latency_ms"] == nil {
		out["latency_ms"] = remoteNumber(m["last_health_latency_ms"])
	}
	if out["status"] == "active" {
		out["status"] = "enabled"
	}
	if out["scheme"] != "http" && out["scheme"] != "https" && out["scheme"] != "socks5" && out["scheme"] != "socks5h" {
		out["scheme"] = nil
	}
	if host, ok := out["host"].(string); ok && !validRemoteHost(host) {
		out["host"] = nil
	}
	if port, ok := out["port"].(float64); ok && (port < 1 || port > 65535 || math.Trunc(port) != port) {
		out["port"] = nil
	}
	out["is_owner"] = nil
	if owner, ok := m["is_owner"].(bool); ok {
		out["is_owner"] = owner
	}
	return out
}

func validRemoteHost(host string) bool {
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Authorization checks exhaust supplier-visible lists, never unrestricted ID
// endpoints. Duplicate pages and inconsistent totals fail closed.
func (r *remoteClient) all(ctx context.Context, s *remoteSession, path, key string, filters url.Values) ([]map[string]any, error) {
	result := []map[string]any{}
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		q := make(url.Values, len(filters)+2)
		for key, values := range filters {
			q[key] = append([]string(nil), values...)
		}
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", "100")
		value, err := r.request(ctx, s, http.MethodGet, path+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		items, meta, err := remoteList(value, key)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			id := remoteID(item["id"])
			if id == "" || seen[id] {
				return nil, remoteErr(502, "invalid_response")
			}
			seen[id] = true
			result = append(result, item)
			if len(result) > 10000 {
				return nil, remoteErr(502, "pagination_limit")
			}
		}
		if total, ok := remoteTotal(meta).(float64); ok {
			if total < 0 || math.Trunc(total) != total || total > 10000 || float64(len(result)) > total {
				return nil, remoteErr(502, "pagination_limit")
			}
			if float64(len(result)) == total {
				return result, nil
			}
			if len(items) == 0 {
				return nil, remoteErr(502, "invalid_response")
			}
		} else if len(items) < 100 {
			return result, nil
		}
	}
	return nil, remoteErr(502, "pagination_limit")
}

func remoteOptionQuery(key string) url.Values {
	if key == "proxies" {
		return url.Values{"available_for_binding": {"1"}, "inference_backend": {"native"}}
	}
	return nil
}

func (r *remoteClient) options(ctx context.Context, s *remoteSession) (map[string]any, error) {
	result := map[string]any{}
	for _, key := range []string{"groups", "policies", "templates", "proxies"} {
		items, err := r.all(ctx, s, remoteOptionPaths[key], key, remoteOptionQuery(key))
		if err != nil {
			return nil, err
		}
		list := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if key == "proxies" {
				list = append(list, remoteProxy(item))
			} else {
				list = append(list, remoteFields(item, "id name", ""))
			}
		}
		result[key] = list
	}
	r.redact(s, result)
	return result, nil
}

func (r *remoteClient) redact(s *remoteSession, value any) {
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case string:
			for _, secret := range []string{r.password, s.token} {
				if secret != "" && strings.Contains(x, secret) {
					return nil
				}
			}
			if strings.Contains(x, "://") {
				if u, err := url.Parse(x); err == nil && u.User != nil {
					return nil
				}
			}
		case map[string]any:
			for k, v := range x {
				x[k] = scrub(v)
			}
		case []map[string]any:
			for _, v := range x {
				scrub(v)
			}
		}
		return v
	}
	scrub(value)
}
