package binding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const bindingCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var (
	ErrInvalidBinding = errors.New("invalid assistant binding request")
	ErrCodeInvalid    = errors.New("assistant binding code is invalid or expired")
	ErrIdentityBound  = errors.New("assistant external identity is already bound")
	ErrUserDenied     = errors.New("assistant user is not allowed")
)

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

type GeneratedCode struct {
	Code      string `json:"code"`
	Command   string `json:"command"`
	ExpiresAt int64  `json:"expires_at"`
}

type CreateInput struct {
	UserID      int
	CreatedBy   int
	Scope       string
	InstanceIDs []int64
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, errors.New("assistant binding database is nil")
	}
	return &Service{db: db, now: time.Now}, nil
}

func (s *Service) CreateCode(ctx context.Context, input CreateInput) (*GeneratedCode, error) {
	input.Scope = strings.TrimSpace(input.Scope)
	if input.UserID <= 0 || input.CreatedBy <= 0 || !validScope(input.Scope) {
		return nil, ErrInvalidBinding
	}
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, input.UserID).Error; err != nil || user.Status != common.UserStatusEnabled || !authz.Can(user.Id, user.Role, authz.AssistantAccess) {
		return nil, ErrUserDenied
	}
	instanceIDs, err := normalizeInstanceIDs(input.Scope, input.InstanceIDs)
	if err != nil {
		return nil, err
	}
	if len(instanceIDs) > 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.ManagedInstance{}).Where("id IN ?", instanceIDs).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(instanceIDs)) {
			return nil, ErrInvalidBinding
		}
	}
	encodedIDs, _ := json.Marshal(instanceIDs)
	expiresAt := s.now().Add(5 * time.Minute).Unix()
	for attempt := 0; attempt < 3; attempt++ {
		raw, err := generateCode()
		if err != nil {
			return nil, err
		}
		row := model.AssistantBindingCode{
			CodeHash: hashCode(raw), UserID: input.UserID, CreatedBy: input.CreatedBy,
			AllowedInstanceScope: input.Scope, InstanceIDs: string(encodedIDs), ExpiresAt: expiresAt,
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			continue
		}
		display := raw[:4] + "-" + raw[4:]
		return &GeneratedCode{Code: display, Command: "/绑定 " + display, ExpiresAt: expiresAt}, nil
	}
	return nil, errors.New("generate unique assistant binding code")
}

func (s *Service) Consume(ctx context.Context, channelID int64, externalUserID string, rawCode string) (*model.AssistantIdentity, error) {
	externalUserID = strings.TrimSpace(externalUserID)
	codeHash := hashCode(rawCode)
	if channelID <= 0 || externalUserID == "" || codeHash == "" {
		return nil, ErrInvalidBinding
	}
	var identity model.AssistantIdentity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var code model.AssistantBindingCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ? AND consumed_at = 0", codeHash).First(&code).Error; err != nil {
			return ErrCodeInvalid
		}
		if code.ExpiresAt <= s.now().Unix() {
			return ErrCodeInvalid
		}
		var user model.User
		if err := tx.First(&user, code.UserID).Error; err != nil || user.Status != common.UserStatusEnabled || !authz.Can(user.Id, user.Role, authz.AssistantAccess) {
			return ErrUserDenied
		}
		findErr := tx.Where("channel_id = ? AND external_user_id = ?", channelID, externalUserID).First(&identity).Error
		switch {
		case findErr == nil && identity.UserID != code.UserID:
			return ErrIdentityBound
		case findErr == nil:
			identity.Status = model.AssistantIdentityStatusActive
			identity.AllowedInstanceScope = code.AllowedInstanceScope
			identity.BoundBy = code.CreatedBy
			identity.BoundAt = s.now().Unix()
			if err := tx.Save(&identity).Error; err != nil {
				return err
			}
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			identity = model.AssistantIdentity{
				ChannelID: channelID, ExternalUserID: externalUserID, UserID: code.UserID,
				Status: model.AssistantIdentityStatusActive, AllowedInstanceScope: code.AllowedInstanceScope,
				BoundBy: code.CreatedBy, BoundAt: s.now().Unix(),
			}
			if err := tx.Create(&identity).Error; err != nil {
				return err
			}
		default:
			return findErr
		}

		if err := tx.Where("identity_id = ?", identity.ID).Delete(&model.AssistantIdentityInstanceScope{}).Error; err != nil {
			return err
		}
		if code.AllowedInstanceScope == model.AssistantInstanceScopeSelected {
			var instanceIDs []int64
			if err := json.Unmarshal([]byte(code.InstanceIDs), &instanceIDs); err != nil || len(instanceIDs) == 0 {
				return ErrInvalidBinding
			}
			for _, instanceID := range instanceIDs {
				if err := tx.Create(&model.AssistantIdentityInstanceScope{IdentityID: identity.ID, InstanceID: instanceID, CreatedBy: code.CreatedBy}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&code).Updates(map[string]any{
			"consumed_at": s.now().Unix(), "consumed_by_identity_id": identity.ID,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func generateCode() (string, error) {
	buffer := make([]byte, 8)
	random := make([]byte, len(buffer))
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for index, value := range random {
		buffer[index] = bindingCodeAlphabet[int(value)%len(bindingCodeAlphabet)]
	}
	return string(buffer), nil
}

func hashCode(raw string) string {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "", "\r", "", "\n", "").Replace(strings.TrimSpace(raw)))
	if len(normalized) != 8 {
		return ""
	}
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}

func validScope(scope string) bool {
	return scope == model.AssistantInstanceScopeAll || scope == model.AssistantInstanceScopeSelected
}

func normalizeInstanceIDs(scope string, values []int64) ([]int64, error) {
	if len(values) > 100 {
		return nil, ErrInvalidBinding
	}
	if scope == model.AssistantInstanceScopeAll {
		return []int64{}, nil
	}
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, ErrInvalidBinding
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, ErrInvalidBinding
	}
	return result, nil
}
