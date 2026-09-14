package access

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

// IdentityDataAccess intersects live subject and responsible-admin grants.
func IdentityDataAccess(db *gorm.DB, identity *model.AssistantIdentity) (*authz.DataAccess, error) {
	if identity == nil || identity.Status != model.AssistantIdentityStatusActive {
		return nil, ErrIdentityDenied
	}
	a, err := authz.LoadDataAccess(db, identity.UserID)
	if err != nil || !a.Can(authz.AssistantAccess) {
		return nil, ErrUserDisabled
	}
	ownerID := identity.BoundBy
	if ownerID == 0 {
		var root model.User
		if db.Where("role >= ? AND status = ?", common.RoleRootUser, common.UserStatusEnabled).Order("id ASC").First(&root).Error != nil {
			return nil, ErrIdentityDenied
		}
		ownerID = root.Id
	}
	owner, err := authz.LoadDataAccess(db, ownerID)
	if err != nil || !owner.Can(authz.AssistantManage) {
		return nil, ErrIdentityDenied
	}
	if a.Role >= common.RoleRootUser && owner.Role < common.RoleRootUser {
		return nil, ErrIdentityDenied
	}
	for _, field := range model.AdminDataFields {
		a.Policy.Fields[field] = a.HasField(field) && owner.HasField(field)
	}
	if owner.Policy.InstanceScope != "all" {
		ids := []int64{}
		for _, id := range owner.Policy.InstanceIDs {
			if a.HasInstance(id) {
				ids = append(ids, id)
			}
		}
		a.Policy.InstanceScope, a.Policy.InstanceIDs = "selected", ids
	}
	return a, nil
}

func checkArguments(a *authz.DataAccess, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return err
	}
	var visit func(map[string]any) error
	visit = func(values map[string]any) error {
		for key, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				if key == "field" || key == "sort_by" || key == "sort" {
					if text == "instance" {
						text = "instance_name"
					}
					if text == "source" {
						text = "source_name"
					}
					if err := a.CheckDataField(text); err != nil {
						return err
					}
				} else if authz.DataFieldGroup(key) != "" {
					if err := a.CheckDataField(key); err != nil {
						return err
					}
				}
				if (key == "search" || key == "keyword") && !a.AllFields() {
					return authz.ErrDataForbidden
				}
			}
			if list, ok := value.([]any); ok {
				for _, item := range list {
					if rule, ok := item.(map[string]any); ok {
						if err := visit(rule); err != nil {
							return err
						}
					}
					if key == "metrics" {
						metric, _ := item.(string)
						if err := a.CheckDataField(strings.ToLower(strings.TrimSpace(metric))); err != nil {
							return err
						}
					}
				}
				if (key == "include_terms" || key == "exclude_terms") && len(list) > 0 && !a.AllFields() {
					return authz.ErrDataForbidden
				}
			}
		}
		return nil
	}
	return visit(args)
}

func Boundary(db *gorm.DB) tool.BoundaryFunc {
	return func(ctx context.Context, execution tool.ExecutionContext) (context.Context, func(tool.Result) (tool.Result, error), error) {
		_, identity, err := activeSubject(ctx, db, execution)
		if err != nil {
			return ctx, nil, err
		}
		a, err := IdentityDataAccess(db, identity)
		if err != nil {
			return ctx, nil, err
		}
		before, err := Fingerprint(db, identity)
		if err != nil {
			return ctx, nil, err
		}
		return authz.WithDataAccess(ctx, a), func(result tool.Result) (tool.Result, error) {
			var current model.AssistantIdentity
			if db.First(&current, identity.ID).Error != nil {
				return tool.Result{}, ErrIdentityDenied
			}
			after, err := Fingerprint(db, &current)
			if err != nil || after != before {
				return tool.Result{}, authz.ErrAuthorizationChanged
			}
			projected, err := a.Project(result.Data)
			if err != nil {
				return tool.Result{}, err
			}
			result.Data, err = json.Marshal(projected)
			if !a.HasField("time") {
				result.Freshness = tool.Freshness{}
				for i := range result.Provenance {
					result.Provenance[i] = tool.Provenance{Source: result.Provenance[i].Source}
				}
			}
			return result, err
		}, nil
	}
}

// Fingerprint isolates history and queued replies across all authorization changes.
func Fingerprint(db *gorm.DB, identity *model.AssistantIdentity) (string, error) {
	a, err := IdentityDataAccess(db, identity)
	if err != nil {
		return "", err
	}
	var ids []int64
	if err := db.Model(&model.AssistantIdentityInstanceScope{}).Where("identity_id = ?", identity.ID).Order("instance_id ASC").Pluck("instance_id", &ids).Error; err != nil {
		return "", err
	}
	var owner model.User
	if identity.BoundBy > 0 {
		if err := db.First(&owner, identity.BoundBy).Error; err != nil {
			return "", err
		}
	}
	encoded, err := json.Marshal([]any{identity, ids, a, authz.Capabilities(a.UserID, a.Role), owner.AuthorizationVersion, authz.Capabilities(owner.Id, owner.Role)})
	return string(encoded), err
}
