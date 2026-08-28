package access

import (
	"context"
	"errors"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"gorm.io/gorm"
)

type InstanceOption struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

func ListAllInstanceOptions(ctx context.Context, db *gorm.DB) ([]InstanceOption, error) {
	options := make([]InstanceOption, 0)
	err := db.WithContext(ctx).Model(&model.ManagedInstance{}).
		Select("id", "name", "kind", "status").Order("name ASC, id ASC").Scan(&options).Error
	return options, err
}

func ListIdentityInstanceOptions(ctx context.Context, db *gorm.DB, identity *model.AssistantIdentity) ([]InstanceOption, error) {
	if db == nil || identity == nil {
		return nil, ErrIdentityDenied
	}
	query := db.WithContext(ctx).Model(&model.ManagedInstance{}).Select("id", "name", "kind", "status")
	switch identity.AllowedInstanceScope {
	case model.AssistantInstanceScopeAll:
	case model.AssistantInstanceScopeSelected:
		var ids []int64
		if err := db.WithContext(ctx).Model(&model.AssistantIdentityInstanceScope{}).
			Where("identity_id = ?", identity.ID).Pluck("instance_id", &ids).Error; err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []InstanceOption{}, nil
		}
		query = query.Where("id IN ?", ids)
	default:
		return []InstanceOption{}, nil
	}
	options := make([]InstanceOption, 0)
	err := query.Order("name ASC, id ASC").Scan(&options).Error
	return options, err
}

func GetGlobalDefaultInstanceID(ctx context.Context, db *gorm.DB) (*int64, error) {
	var setting model.AssistantSetting
	err := db.WithContext(ctx).Where("id = ?", 1).Limit(1).Find(&setting).Error
	if err != nil {
		return nil, err
	}
	return cloneInt64(setting.GlobalDefaultInstanceID), nil
}

func UpdateGlobalDefaultInstance(ctx context.Context, db *gorm.DB, instanceID *int64, actorID int) error {
	if db == nil || actorID <= 0 {
		return ErrInstanceDenied
	}
	if err := validateExistingInstance(ctx, db, instanceID); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var setting model.AssistantSetting
		err := tx.First(&setting, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setting = model.AssistantSetting{ID: 1}
		} else if err != nil {
			return err
		}
		setting.GlobalDefaultInstanceID = cloneInt64(instanceID)
		setting.UpdatedBy = actorID
		return tx.Save(&setting).Error
	})
}

func UpdateIdentityDefaultInstance(ctx context.Context, db *gorm.DB, identityID int64, instanceID *int64) error {
	if db == nil || identityID <= 0 {
		return ErrIdentityDenied
	}
	var identity model.AssistantIdentity
	if err := db.WithContext(ctx).First(&identity, identityID).Error; err != nil {
		return err
	}
	if identity.Status != model.AssistantIdentityStatusActive {
		return ErrIdentityDenied
	}
	if instanceID != nil {
		if *instanceID <= 0 {
			return ErrInstanceDenied
		}
		options, err := ListIdentityInstanceOptions(ctx, db, &identity)
		if err != nil {
			return err
		}
		allowed := false
		for _, option := range options {
			if option.ID == *instanceID {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrInstanceDenied
		}
	}
	return db.WithContext(ctx).Model(&model.AssistantIdentity{}).Where("id = ?", identityID).
		Update("default_instance_id", cloneInt64(instanceID)).Error
}

func ResolveIdentityDefault(ctx context.Context, db *gorm.DB, identity *model.AssistantIdentity) (InstanceResolution, error) {
	if identity == nil {
		return InstanceResolution{}, ErrIdentityDenied
	}
	var user model.User
	if err := db.WithContext(ctx).First(&user, identity.UserID).Error; err != nil {
		return InstanceResolution{}, err
	}
	execution := tool.ExecutionContext{
		RunID: "default-instance-resolution", ConversationID: "default-instance-resolution",
		Channel: "control-plane", IdentityID: identity.ID, UserID: user.Id, UserRole: user.Role,
	}
	return ResolveInstanceSelection(ctx, db, execution, nil, InstanceSelectionDefault)
}

func validateExistingInstance(ctx context.Context, db *gorm.DB, instanceID *int64) error {
	if instanceID == nil {
		return nil
	}
	if *instanceID <= 0 {
		return ErrInstanceDenied
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.ManagedInstance{}).Where("id = ?", *instanceID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInstanceDenied
	}
	return nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
