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

// ResolveInstanceIDs enforces the bound identity's instance scope. An empty
// requested list means "all visible" and is expanded server-side.
func ResolveInstanceIDs(ctx context.Context, db *gorm.DB, execution tool.ExecutionContext, requested []int64) ([]int64, error) {
	if db == nil {
		return nil, errors.New("assistant authorization database is nil")
	}
	_, identity, err := activeSubject(ctx, db, execution)
	if err != nil {
		return nil, err
	}
	if identity.AllowedInstanceScope == model.AssistantInstanceScopeNone {
		return nil, ErrInstanceDenied
	}

	var allowed []int64
	switch identity.AllowedInstanceScope {
	case model.AssistantInstanceScopeAll:
		if len(requested) == 0 {
			err = db.WithContext(ctx).Model(&model.ManagedInstance{}).Order("id ASC").Pluck("id", &allowed).Error
			return allowed, err
		}
		allowed = append([]int64(nil), requested...)
	case model.AssistantInstanceScopeSelected:
		err = db.WithContext(ctx).Model(&model.AssistantIdentityInstanceScope{}).
			Where("identity_id = ?", identity.ID).Order("instance_id ASC").Pluck("instance_id", &allowed).Error
		if err != nil {
			return nil, err
		}
		if len(requested) == 0 {
			if len(allowed) == 0 {
				return nil, ErrInstanceDenied
			}
			return allowed, nil
		}
	default:
		return nil, ErrInstanceDenied
	}

	allowedSet := make(map[int64]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	result := make([]int64, 0, len(requested))
	seen := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		if id <= 0 {
			return nil, ErrInstanceDenied
		}
		if identity.AllowedInstanceScope == model.AssistantInstanceScopeSelected {
			if _, ok := allowedSet[id]; !ok {
				return nil, ErrInstanceDenied
			}
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
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
