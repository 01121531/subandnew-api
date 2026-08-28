package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

var (
	ErrIdentityDenied = errors.New("assistant identity is not active")
	ErrUserDisabled   = errors.New("assistant user is disabled")
	ErrInstanceDenied = errors.New("assistant instance access denied")
)

const (
	InstanceSelectionDefault = ""
	InstanceSelectionAll     = "all"

	DefaultSourcePersonal = "personal"
	DefaultSourceGlobal   = "global"
	DefaultSourceAll      = "all"
)

type InstanceResolution struct {
	IDs         []int64
	Source      string
	DefaultID   int64
	DefaultName string
	Fallback    bool
}

func Authorize(db *gorm.DB) tool.AuthorizeFunc {
	return func(ctx context.Context, request tool.AuthorizationRequest) error {
		if db == nil {
			return errors.New("assistant authorization database is nil")
		}
		user, _, err := activeSubject(ctx, db, request.Execution)
		if err != nil {
			return err
		}
		if !authz.Can(user.Id, user.Role, authz.AssistantAccess) {
			return errors.New("assistant access permission denied")
		}
		permission := authz.Permission{Resource: request.Tool.Permission.Resource, Action: request.Tool.Permission.Action}
		if !authz.Can(user.Id, user.Role, permission) {
			return fmt.Errorf("domain permission %s:%s denied", permission.Resource, permission.Action)
		}
		return nil
	}
}

// ResolveInstanceIDs enforces the bound identity's instance scope and applies
// its effective default when the caller does not make an explicit selection.
func ResolveInstanceIDs(ctx context.Context, db *gorm.DB, execution tool.ExecutionContext, requested []int64) ([]int64, error) {
	resolution, err := ResolveInstanceSelection(ctx, db, execution, requested, InstanceSelectionDefault)
	return resolution.IDs, err
}

// ResolveInstanceSelection enforces the identity allow-list and applies the
// personal/global default only for implicit selections.
func ResolveInstanceSelection(ctx context.Context, db *gorm.DB, execution tool.ExecutionContext, requested []int64, selectionScope string) (InstanceResolution, error) {
	if db == nil {
		return InstanceResolution{}, errors.New("assistant authorization database is nil")
	}
	_, identity, err := activeSubject(ctx, db, execution)
	if err != nil {
		return InstanceResolution{}, err
	}
	if identity.AllowedInstanceScope == model.AssistantInstanceScopeNone {
		return InstanceResolution{}, ErrInstanceDenied
	}
	if selectionScope != InstanceSelectionDefault && selectionScope != InstanceSelectionAll {
		return InstanceResolution{}, ErrInstanceDenied
	}
	if selectionScope == InstanceSelectionAll && len(requested) > 0 {
		return InstanceResolution{}, ErrInstanceDenied
	}

	var allowed []int64
	switch identity.AllowedInstanceScope {
	case model.AssistantInstanceScopeAll:
		err = db.WithContext(ctx).Model(&model.ManagedInstance{}).Order("id ASC").Pluck("id", &allowed).Error
	case model.AssistantInstanceScopeSelected:
		err = db.WithContext(ctx).Model(&model.AssistantIdentityInstanceScope{}).
			Where("identity_id = ?", identity.ID).Order("instance_id ASC").Pluck("instance_id", &allowed).Error
		if err != nil {
			return InstanceResolution{}, err
		}
	default:
		return InstanceResolution{}, ErrInstanceDenied
	}
	if err != nil {
		return InstanceResolution{}, err
	}
	if len(allowed) == 0 && identity.AllowedInstanceScope == model.AssistantInstanceScopeSelected {
		return InstanceResolution{}, ErrInstanceDenied
	}

	allowedSet := make(map[int64]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	if selectionScope == InstanceSelectionAll {
		return InstanceResolution{IDs: allowed, Source: DefaultSourceAll}, nil
	}
	if len(requested) == 0 {
		resolution := InstanceResolution{Source: DefaultSourceAll}
		if identity.DefaultInstanceID != nil {
			if _, ok := allowedSet[*identity.DefaultInstanceID]; ok {
				selected, selectedErr := selectedInstanceResolution(ctx, db, *identity.DefaultInstanceID, DefaultSourcePersonal, false)
				if selectedErr == nil {
					return selected, nil
				}
				if !errors.Is(selectedErr, gorm.ErrRecordNotFound) {
					return InstanceResolution{}, selectedErr
				}
			}
			resolution.Fallback = true
		}
		var setting model.AssistantSetting
		settingErr := db.WithContext(ctx).Where("id = ?", 1).Limit(1).Find(&setting).Error
		if settingErr != nil {
			return InstanceResolution{}, settingErr
		}
		if setting.GlobalDefaultInstanceID != nil {
			if _, ok := allowedSet[*setting.GlobalDefaultInstanceID]; ok {
				selected, selectedErr := selectedInstanceResolution(ctx, db, *setting.GlobalDefaultInstanceID, DefaultSourceGlobal, resolution.Fallback)
				if selectedErr == nil {
					return selected, nil
				}
				if !errors.Is(selectedErr, gorm.ErrRecordNotFound) {
					return InstanceResolution{}, selectedErr
				}
			}
			resolution.Fallback = true
		}
		resolution.IDs = allowed
		return resolution, nil
	}

	result := make([]int64, 0, len(requested))
	seen := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		if id <= 0 {
			return InstanceResolution{}, ErrInstanceDenied
		}
		if _, ok := allowedSet[id]; !ok {
			return InstanceResolution{}, ErrInstanceDenied
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return InstanceResolution{IDs: result, Source: "explicit"}, nil
}

func selectedInstanceResolution(ctx context.Context, db *gorm.DB, id int64, source string, fallback bool) (InstanceResolution, error) {
	var instance model.ManagedInstance
	if err := db.WithContext(ctx).Select("id", "name").First(&instance, id).Error; err != nil {
		return InstanceResolution{}, err
	}
	return InstanceResolution{IDs: []int64{id}, Source: source, DefaultID: id, DefaultName: instance.Name, Fallback: fallback}, nil
}

func activeSubject(ctx context.Context, db *gorm.DB, execution tool.ExecutionContext) (*model.User, *model.AssistantIdentity, error) {
	var identity model.AssistantIdentity
	if execution.IdentityID <= 0 || db.WithContext(ctx).First(&identity, execution.IdentityID).Error != nil {
		return nil, nil, ErrIdentityDenied
	}
	if identity.Status != model.AssistantIdentityStatusActive || identity.UserID != execution.UserID {
		return nil, nil, ErrIdentityDenied
	}
	var user model.User
	if err := db.WithContext(ctx).First(&user, execution.UserID).Error; err != nil {
		return nil, nil, ErrUserDisabled
	}
	if user.Status != common.UserStatusEnabled {
		return nil, nil, ErrUserDisabled
	}
	return &user, &identity, nil
}
