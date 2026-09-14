package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

type cacheEntry struct {
	Data []byte
	At   time.Time
}

func capability(resource string) string {
	switch resource {
	case "accounts", "account-summary":
		return "accounts"
	case "usage":
		return "usage"
	case "proxies":
		return "proxies"
	case "account-upload/options":
		return "upload"
	}
	return ""
}
func (s *Service) Read(ctx context.Context, p *Principal, bindingID int64, resource string, q url.Values, force bool) (map[string]any, error) {
	b, err := s.authorize(p, bindingID, capability(resource))
	if err != nil {
		return nil, err
	}
	if err = validateQuery(resource, q); err != nil {
		return nil, err
	}
	q = cloneQuery(q)
	if err = validatePolicyQuery(b.EffectivePolicy, resource, q); err != nil {
		return nil, err
	}
	remote, err := s.remoteFor(b)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%d:%s:%d:%d:%s:%s:%s", b.ID, b.Revision, b.CacheVersion, p.Supplier.AuthVersion, b.EffectivePolicy.Version, resource, q.Encode())
	value, err, _ := s.reads.Do(key, func() (any, error) {
		s.cacheMu.Lock()
		old, ok := s.cache[key]
		s.cacheMu.Unlock()
		if ok && !force && s.Now().Sub(old.At) < 30*time.Second {
			return cached(old, false)
		}
		data, fetchErr := remote.Read(ctx, resource, q)
		if fetchErr != nil {
			status, _ := HTTPError(fetchErr)
			if ok && s.Now().Sub(old.At) < 15*time.Minute && (status == 429 || status == 408 || status >= 500) {
				return cached(old, true)
			}
			s.cacheMu.Lock()
			delete(s.cache, key)
			s.cacheMu.Unlock()
			return nil, fetchErr
		}
		payload, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		entry := cacheEntry{Data: payload, At: s.Now()}
		s.cacheMu.Lock()
		delete(s.cache, key)
		bytes := len(payload)
		for k, v := range s.cache {
			if s.Now().Sub(v.At) >= 15*time.Minute {
				delete(s.cache, k)
			} else {
				bytes += len(v.Data)
			}
		}
		for len(s.cache) > 0 && (len(s.cache) >= 256 || bytes > 64*1024*1024) {
			oldest := ""
			var at time.Time
			for k, v := range s.cache {
				if oldest == "" || v.At.Before(at) {
					oldest = k
					at = v.At
				}
			}
			bytes -= len(s.cache[oldest].Data)
			delete(s.cache, oldest)
		}
		if len(payload) <= 64*1024*1024 {
			s.cache[key] = entry
		}
		s.cacheMu.Unlock()
		return cached(entry, false)
	})
	if err != nil {
		return nil, err
	}
	// Recheck after upstream I/O so revoked permissions cannot read cached data.
	current, err := s.authorize(p, bindingID, capability(resource))
	if err != nil {
		return nil, err
	}
	if current.Revision != b.Revision {
		return nil, fail(409, "supplier_binding_changed")
	}
	if current.EffectivePolicy.Version != b.EffectivePolicy.Version {
		return nil, fail(409, "supplier_policy_changed")
	}
	// singleflight may share results: never redact another caller's map in place.
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result, err := cached(cacheEntry{Data: encoded, At: s.Now()}, false)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"observed_at", "stale"} {
		result[key] = value.(map[string]any)[key]
	}
	redactPolicy(result, resource, current.EffectivePolicy, q)
	if resource == "account-upload/options" {
		result["effective_naming"] = current.EffectiveNaming
		settings, err := s.PortalSettings()
		if err != nil {
			return nil, err
		}
		result["allowed_upload_methods"] = allowedUploadMethods(settings, *current)
		result["portal_revision"] = settings.Revision
	}
	return result, nil
}
func cached(entry cacheEntry, stale bool) (map[string]any, error) {
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(entry.Data)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	result["observed_at"] = entry.At.Unix()
	result["stale"] = stale
	return result, nil
}
func validateQuery(resource string, q url.Values) error {
	allowed := map[string]bool{"page": true, "page_size": true, "search": true, "status": true, "recovery_window": true, "fable_recovery_window": true, "sort": true, "direction": true, "days": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 200 {
			return fail(400, "supplier_invalid_query")
		}
	}
	for _, key := range []string{"page", "page_size"} {
		if q.Get(key) != "" {
			n, e := strconv.Atoi(q.Get(key))
			ceiling := 100000
			if key == "page_size" {
				ceiling = 100
			}
			if e != nil || n < 1 || n > ceiling {
				return fail(400, "supplier_invalid_pagination")
			}
		}
	}
	if resource == "usage" && q.Get("days") != "1" && q.Get("days") != "7" && q.Get("days") != "30" {
		return fail(400, "supplier_invalid_days")
	}
	return nil
}
func (s *Service) invalidate(b *model.SupplierBinding) {
	s.DB.Model(&model.SupplierBinding{}).Where("id = ?", b.ID).UpdateColumn("cache_version", gorm.Expr("cache_version + 1"))
	prefix := fmt.Sprintf("%d:", b.ID)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	for key := range s.cache {
		if strings.HasPrefix(key, prefix) {
			delete(s.cache, key)
		}
	}
}
func (s *Service) ProxyWrite(ctx context.Context, p *Principal, bindingID int64, method, id, action string, body map[string]any) (map[string]any, error) {
	valid := (method == "POST" && id == "" && action == "") ||
		(method == "POST" && id != "" && action == "test") ||
		((method == "PATCH" || method == "DELETE") && id != "" && action == "")
	if !valid {
		return nil, fail(400, "supplier_invalid_proxy_operation")
	}
	b, err := s.authorize(p, bindingID, "proxies")
	if err != nil {
		return nil, err
	}
	resource := "proxies"
	if id != "" {
		if !resourceID.MatchString(id) {
			return nil, fail(400, "supplier_invalid_resource")
		}
		resource += "/" + id
		if action == "test" {
			resource += "/test"
		}
	}
	if method == "POST" && id == "" {
		text, _ := body["text"].(string)
		if text == "" || len(text) > 256*1024 || len(strings.Split(text, "\n")) > 1000 {
			return nil, fail(400, "supplier_invalid_proxy_import")
		}
		body = map[string]any{"text": text}
	}
	if method == "PATCH" {
		status, _ := body["status"].(string)
		if status != "enabled" && status != "disabled" {
			return nil, fail(400, "supplier_invalid_proxy_status")
		}
		body = map[string]any{"status": status}
	}
	if method == "DELETE" || action == "test" {
		body = map[string]any{}
	}
	remote, err := s.remoteFor(b)
	if err != nil {
		return nil, err
	}
	defer s.invalidate(b)
	return remote.Write(ctx, method, resource, body)
}
