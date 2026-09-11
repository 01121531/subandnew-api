package supplier

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
)

func normalizeNaming(rule *model.SupplierNamingRule) error {
	if rule == nil {
		return nil
	}
	if !utf8.ValidString(rule.Prefix) || !utf8.ValidString(rule.Suffix) || strings.ContainsFunc(rule.Prefix+rule.Suffix, unicode.IsControl) {
		return fail(400, "supplier_invalid_naming_rule")
	}
	rule.Prefix, rule.Suffix = strings.TrimSpace(rule.Prefix), strings.TrimSpace(rule.Suffix)
	if utf8.RuneCountInString(rule.Prefix+rule.Suffix) > 63 {
		return fail(400, "supplier_invalid_naming_rule")
	}
	return nil
}

func parseNamingOverride(raw json.RawMessage) (*model.SupplierNamingRule, error) {
	var rule *model.SupplierNamingRule
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		return nil, fail(400, "supplier_invalid_naming_rule")
	}
	return rule, normalizeNaming(rule)
}

func namingChanges(before, after *model.SupplierNamingRule) string {
	if reflect.DeepEqual(before, after) {
		return ""
	}
	encoded, _ := json.Marshal(map[string]any{"before": before, "after": after})
	return string(encoded)
}

func updateBindingNaming(b *model.SupplierBinding, owner model.Supplier, in BindingInput) error {
	if in.NamingRevision != "" && in.NamingRevision != model.ResolveSupplierNaming(owner, b).Version {
		return fail(409, "supplier_naming_changed")
	}
	if len(in.NamingOverride) == 0 {
		return nil
	}
	rule, err := parseNamingOverride(in.NamingOverride)
	if err != nil {
		return err
	}
	b.NamingChanges = namingChanges(b.NamingOverride, rule)
	b.NamingOverride = rule
	if b.NamingChanges != "" {
		b.NamingVersion++
	}
	return nil
}

func applyUploadNaming(in UploadInput, b *model.SupplierBinding, sk bool, at time.Time) (UploadInput, error) {
	rule := b.EffectiveNaming
	if rule == nil {
		return in, fail(503, "supplier_service_unavailable")
	}
	if in.NamingRevision != "" && in.NamingRevision != rule.Version {
		return in, fail(409, "supplier_naming_changed")
	}
	if sk && rule.Suffix != "" {
		return in, fail(400, "supplier_sk_suffix_not_supported")
	}
	if !utf8.ValidString(in.Name) || strings.ContainsFunc(in.Name, unicode.IsControl) {
		return in, fail(400, "supplier_invalid_account_name")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return in, fail(400, "supplier_invalid_account_name")
	}
	suffix := ""
	china := at.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	switch in.NameTimeMode {
	case "", "none":
	case "date":
		suffix = "-" + china.Format("0102")
	case "date_time":
		suffix = "-" + china.Format("0102-1504")
	default:
		return in, fail(400, "supplier_invalid_upload_parameters")
	}
	in.Name = rule.Prefix + name + suffix + rule.Suffix
	if utf8.RuneCountInString(in.Name) > 64 {
		return in, fail(400, "supplier_account_name_too_long")
	}
	in.NamingRevision = rule.Version
	return in, nil
}
