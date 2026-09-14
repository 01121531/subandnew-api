package accountdataapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ownerAccess(userID int) (*authz.DataAccess, error) {
	if userID == 0 {
		var root model.User
		if model.DB.Where("role >= ? AND status = ?", common.RoleRootUser, common.UserStatusEnabled).Order("id ASC").First(&root).Error != nil {
			return nil, ErrDisabled
		}
		userID = root.Id
	}
	a, err := authz.LoadDataAccess(model.DB, userID)
	if err != nil || !a.Can(authz.ManagedAccountAPIManage) {
		return nil, ErrDisabled
	}
	return a, nil
}

// CheckOwner never changes CreatedBy. Recovery must explicitly revoke old grants.
func CheckOwner(id int64, actorID int) error {
	a, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil {
		return ErrDisabled
	}
	var entry model.ManagedAccountAPI
	if err := model.DB.First(&entry, id).Error; err != nil {
		return mapNotFound(err)
	}
	if a.Role < common.RoleRootUser && entry.CreatedBy != actorID {
		return ErrDisabled
	}
	return nil
}

func fieldAllowed(a *authz.DataAccess, field string) bool {
	if field == "note" {
		return a.AllFields()
	}
	if field == "vendor_email" && !a.HasField("email") {
		return false
	}
	return authz.KnownDataField(field) && a.CheckDataField(field) == nil
}

func checkQuery(a *authz.DataAccess, q managedaccount.Query) error {
	if len(q.InstanceIDs) == 0 || a.CheckInstances(q.InstanceIDs) != nil {
		return ErrDisabled
	}
	for _, rule := range append(q.Rules, q.NarrowRules...) {
		field := rule.Field
		if field == "instance" {
			field = "instance_name"
		}
		if field == "source" {
			field = "source_name"
		}
		if !fieldAllowed(a, field) {
			return ErrDisabled
		}
	}
	if q.SortBy != "" && !fieldAllowed(a, q.SortBy) {
		return ErrDisabled
	}
	// Broad text matching searches several fields, including notes and ownership.
	if !a.AllFields() && (strings.TrimSpace(q.Search) != "" || len(q.IncludeTerms) > 0 || len(q.ExcludeTerms) > 0) {
		return ErrDisabled
	}
	return nil
}

func effectiveView(entry *model.ManagedAccountAPI, view *View) (*authz.DataAccess, error) {
	a, err := ownerAccess(entry.CreatedBy)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(view.InstanceIDs))
	for _, id := range view.InstanceIDs {
		if a.HasInstance(id) {
			ids = append(ids, id)
		}
	}
	view.InstanceIDs = ids
	q := portalManagedQuery(view, PortalQueryInput{})
	if err := checkQuery(a, q); err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(view.Fields))
	for _, field := range view.Fields {
		if fieldAllowed(a, field) {
			fields = append(fields, field)
		}
	}
	view.Fields = fields
	view.Access = a
	return a, nil
}

func executeDelegated(ctx context.Context, entry *model.ManagedAccountAPI, view *View, q managedaccount.Query) (*managedaccount.Result, error) {
	a := view.Access
	if a == nil {
		return nil, ErrDisabled
	}
	if err := checkQuery(a, q); err != nil {
		return nil, err
	}
	result, err := managedaccount.Execute(authz.WithDataAccess(ctx, a), q)
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(view); err != nil {
		return nil, err
	}
	// Keep typed data internal. HTTP/AI boundaries must project the final envelope;
	// decoding a projection into a DTO would resurrect hidden zero-valued fields.
	return result, nil
}

func ProjectResponse(view *View, value any) (any, error) {
	if err := CheckResponse(view); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var node any
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	if err := decoder.Decode(&node); err != nil {
		return nil, err
	}
	return projectEnvelope(view.Access, node)
}

func configRevision(entry *model.ManagedAccountAPI, ids []int64) string {
	encoded, _ := json.Marshal([]any{entry.CreatedBy, entry.Status, entry.Dataset, entry.PresetDays, entry.Timezone, entry.IncludeTerms, entry.ExcludeTerms, entry.MatchMode, entry.Rules, entry.Fields, entry.SortBy, entry.SortOrder, entry.PageSize, entry.AllowedCIDRs, entry.PortalEnabled, entry.PortalSlug, entry.PortalPasswordHash, ids})
	hash := sha256.Sum256(encoded)
	return string(hash[:])
}

func CheckResponse(view *View) error {
	if view == nil || view.Access == nil || view.Access.Current(model.DB) != nil {
		return ErrDisabled
	}
	if _, err := ownerAccess(view.CreatedBy); err != nil {
		return err
	}
	var entry model.ManagedAccountAPI
	if model.DB.First(&entry, view.ID).Error != nil || entry.Status != model.ManagedAccountAPIEnabled {
		return ErrDisabled
	}
	var ids []int64
	if model.DB.Model(&model.ManagedAccountAPIInstance{}).Where("api_id = ?", view.ID).Order("instance_id ASC").Pluck("instance_id", &ids).Error != nil {
		return ErrDisabled
	}
	if configRevision(&entry, ids) != view.configRevision {
		return ErrDisabled
	}
	if view.keyID > 0 {
		var key model.ManagedAccountAPIKey
		if model.DB.First(&key, view.keyID).Error != nil || key.APIID != view.ID || key.RevokedAt > 0 || key.ExpiresAt <= common.GetTimestamp() {
			return ErrUnauthorized
		}
	}
	if view.sessionID > 0 {
		var session model.ManagedAccountAPIPortalSession
		if model.DB.First(&session, view.sessionID).Error != nil || session.APIID != view.ID || session.RevokedAt > 0 || session.ExpiresAt <= common.GetTimestamp() {
			return ErrPortalUnauthorized
		}
	}
	return nil
}

// BackfillLegacyOwners is called after user and account API migrations.
// Only genuinely ownerless rows qualify; a revoked owner must never fall back.
func BackfillLegacyOwners(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.ManagedAccountAPI{}).Where("created_by = 0").Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var root model.User
	if err := db.Where("role >= ? AND status = ?", common.RoleRootUser, common.UserStatusEnabled).Order("id ASC").First(&root).Error; err != nil {
		return err
	}
	return db.Model(&model.ManagedAccountAPI{}).Where("created_by = 0").UpdateColumn("created_by", root.Id).Error
}

// Takeover changes only responsibility and revokes credentials. Recovery must
// not depend on a usable snapshot or overwrite a stale editor's configuration.
func Takeover(ctx context.Context, id int64, actorID int) (*View, int, error) {
	previous := 0
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := authz.LoadDataAccess(tx, actorID)
		if err != nil || a.Role < common.RoleRootUser {
			return ErrDisabled
		}
		var entry model.ManagedAccountAPI
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entry, id).Error; err != nil {
			return mapNotFound(err)
		}
		previous = entry.CreatedBy
		if err := tx.Model(&entry).Updates(map[string]any{"created_by": actorID, "updated_by": actorID}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedAccountAPIKey{}).Where("api_id = ? AND revoked_at = 0", id).Update("revoked_at", common.GetTimestamp()).Error; err != nil {
			return err
		}
		return tx.Where("api_id = ?", id).Delete(&model.ManagedAccountAPIPortalSession{}).Error
	})
	if err != nil {
		return nil, previous, err
	}
	view, err := Get(id)
	return view, previous, err
}

func projectEnvelope(a *authz.DataAccess, node any) (any, error) {
	if m, ok := node.(map[string]any); ok {
		result := map[string]any{}
		for key, value := range m {
			switch key {
			case "pagination", "authorization", "snapshot", "data", "summary":
				projected, err := projectEnvelope(a, value)
				if err != nil {
					return nil, err
				}
				result[key] = projected
			default:
				projected, err := a.Project(map[string]any{key: value})
				if err != nil {
					return nil, err
				}
				encoded, _ := json.Marshal(projected)
				var fields map[string]any
				decoder := json.NewDecoder(strings.NewReader(string(encoded)))
				decoder.UseNumber()
				if err := decoder.Decode(&fields); err != nil {
					return nil, err
				}
				for k, v := range fields {
					result[k] = v
				}
			}
		}
		return result, nil
	}
	return a.Project(node)
}

func allowedFilterOptions(a *authz.DataAccess, options map[string][]string) map[string][]string {
	result := map[string][]string{}
	for key, values := range options {
		if fieldAllowed(a, key) {
			result[key] = values
		}
	}
	return result
}
