package supplier

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var uploadMethods = []string{"login", "setup_token", "rt", "sk"}

type PortalSettings struct {
	Title         string          `json:"title"`
	UploadMethods map[string]bool `json:"upload_methods"`
	Revision      int64           `json:"revision"`
}

func portalText(value string, empty bool) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", fail(400, "supplier_invalid_portal_text")
	}
	value = strings.TrimSpace(value)
	if (!empty && value == "") || utf8.RuneCountInString(value) > 64 {
		return "", fail(400, "supplier_invalid_portal_text")
	}
	return value, nil
}

func settingsFrom(defaults model.SupplierPolicyDefault) PortalSettings {
	title := defaults.PortalTitle
	if title == "" {
		title = "工作台"
	}
	revision := defaults.PortalRevision
	if revision < 1 {
		revision = 1
	}
	methods := map[string]bool{}
	for _, method := range uploadMethods {
		methods[method] = defaults.UploadMethods == nil || defaults.UploadMethods[method]
	}
	return PortalSettings{Title: title, UploadMethods: methods, Revision: revision}
}

func (s *Service) PortalSettings() (PortalSettings, error) {
	defaults, err := defaultPolicy(s.DB)
	return settingsFrom(defaults), err
}

func (s *Service) SavePortalSettings(in PortalSettings) (PortalSettings, string, error) {
	title, err := portalText(in.Title, false)
	if err != nil {
		return PortalSettings{}, "", err
	}
	if len(in.UploadMethods) != len(uploadMethods) {
		return PortalSettings{}, "", fail(400, "supplier_invalid_upload_methods")
	}
	for _, method := range uploadMethods {
		if _, ok := in.UploadMethods[method]; !ok {
			return PortalSettings{}, "", fail(400, "supplier_invalid_upload_methods")
		}
	}
	in.Title = title
	var changes string
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		old, err := lockPolicy(tx)
		if err != nil {
			return err
		}
		before := settingsFrom(old)
		if before.Revision != in.Revision {
			return fail(409, "supplier_portal_settings_changed")
		}
		if before.Title == in.Title && reflect.DeepEqual(before.UploadMethods, in.UploadMethods) {
			return nil
		}
		in.Revision++
		old.PortalTitle, old.UploadMethods, old.PortalRevision = in.Title, in.UploadMethods, in.Revision
		if !reflect.DeepEqual(before.UploadMethods, in.UploadMethods) {
			if err := tx.Where("consumed_at = 0").Delete(&model.SupplierOAuthFlow{}).Error; err != nil {
				return err
			}
		}
		encoded, _ := json.Marshal(map[string]any{"portal_settings": map[string]any{"before": before, "after": in}})
		changes = string(encoded)
		return tx.Save(&old).Error
	})
	return in, changes, err
}

func (s *Service) checkUploadMethod(method string, revision int64) (PortalSettings, error) {
	if method == "" {
		method = "login"
	}
	settings, err := s.PortalSettings()
	if err != nil {
		return settings, err
	}
	if !settings.UploadMethods[method] {
		return settings, fail(403, "supplier_upload_method_disabled")
	}
	if revision != 0 && revision != settings.Revision {
		return settings, fail(409, "supplier_portal_settings_changed")
	}
	return settings, nil
}

func allowedUploadMethods(settings PortalSettings, b model.SupplierBinding) []string {
	result := []string{}
	if b.EffectivePolicy == nil || !b.EffectivePolicy.Values["upload_accounts"] {
		return result
	}
	for _, method := range uploadMethods {
		if settings.UploadMethods[method] && (method != "sk" || b.EffectiveNaming == nil || b.EffectiveNaming.Suffix == "") {
			result = append(result, method)
		}
	}
	return result
}

type PortalSupplier struct {
	ID             int64 `json:"id"`
	ViewAccounts   bool  `json:"view_accounts"`
	ViewUsage      bool  `json:"view_usage"`
	ManageProxies  bool  `json:"manage_proxies"`
	UploadAccounts bool  `json:"upload_accounts"`
}

func PortalIdentity(item model.Supplier) PortalSupplier {
	return PortalSupplier{item.ID, item.ViewAccounts, item.ViewUsage, item.ManageProxies, item.UploadAccounts}
}

type PortalBinding struct {
	ID              int64                          `json:"id"`
	DisplayName     string                         `json:"display_name"`
	Enabled         bool                           `json:"enabled"`
	EffectivePolicy *model.SupplierEffectivePolicy `json:"effective_policy"`
	EffectiveNaming *model.SupplierEffectiveNaming `json:"effective_naming"`
	AllowedMethods  []string                       `json:"allowed_upload_methods"`
	PortalRevision  int64                          `json:"portal_revision"`
}

func (s *Service) PortalBindings(supplierID int64) ([]PortalBinding, error) {
	owner, err := s.Supplier(supplierID)
	if err != nil {
		return nil, err
	}
	a, err := s.ownerAccess(owner)
	if err != nil {
		return nil, err
	}
	items, err := s.Bindings(supplierID, true)
	if err != nil {
		return nil, err
	}
	settings, err := s.PortalSettings()
	if err != nil {
		return nil, err
	}
	result := make([]PortalBinding, 0, len(items))
	for _, item := range items {
		if !a.HasInstance(item.InstanceID) {
			continue
		}
		intersectPolicy(item.EffectivePolicy, a)
		name := fmt.Sprintf("线路 %d", item.ID)
		if item.DisplayName != nil && *item.DisplayName != "" {
			name = *item.DisplayName
		}
		result = append(result, PortalBinding{item.ID, name, item.Enabled, item.EffectivePolicy, item.EffectiveNaming, allowedUploadMethods(settings, item), settings.Revision})
	}
	return result, nil
}
