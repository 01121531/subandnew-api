package mailbox

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OperatorInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Enabled     bool   `json:"enabled"`
	Version     int64  `json:"version"`
}

type OperatorView struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Enabled           bool   `json:"enabled"`
	Version           int64  `json:"version"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	GeneratedPassword string `json:"generated_password,omitempty"`
}

var operatorUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9@._+\-]{2,95}$`)

func operatorUsername(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func operatorView(item model.MailboxOperator) OperatorView {
	return OperatorView{ID: item.ID, Username: item.Username, DisplayName: item.DisplayName, Enabled: item.Enabled,
		Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func operatorPasswordHash(password string) (string, error) {
	if len(password) < 8 || len(password) > 72 || strings.TrimSpace(password) == "" {
		return "", fail(400, "mailbox_invalid_password")
	}
	return common.Password2Hash(password)
}

func (s *Service) checkOperatorAdmin(actor Actor) error {
	if err := s.CheckActor(actor, authz.MailboxOperators); err != nil {
		return err
	}
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	return nil
}

func (s *Service) operatorByID(id int64) (*model.MailboxOperator, error) {
	if id <= 0 {
		return nil, fail(404, "mailbox_not_found")
	}
	var item model.MailboxOperator
	if err := s.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(404, "mailbox_not_found")
		}
		return nil, err
	}
	return &item, nil
}

func (s *Service) ListOperators(ctx context.Context, actor Actor, query ListQuery) (*Page[OperatorView], error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorAdmin(actor); err != nil {
		return nil, err
	}
	query = normalizePage(query)
	q := s.DB.Model(&model.MailboxOperator{})
	if search := strings.TrimSpace(query.Search); search != "" {
		q = q.Where("username LIKE ? OR display_name LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	page := &Page[OperatorView]{Items: []OperatorView{}, Page: query.Page, PageSize: query.PageSize}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	var items []model.MailboxOperator
	if err := q.Select("id", "username", "display_name", "enabled", "version", "created_at", "updated_at").Order("id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		page.Items = append(page.Items, operatorView(item))
	}
	page.HasMore = int64(query.Page)*int64(query.PageSize) < page.Total
	if err := s.checkOperatorAdmin(actor); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) SaveOperator(ctx context.Context, actor Actor, id int64, input OperatorInput) (*OperatorView, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorAdmin(actor); err != nil {
		return nil, err
	}
	input.Username = operatorUsername(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if id < 0 || !operatorUsernamePattern.MatchString(input.Username) || input.DisplayName == "" || !utf8.ValidString(input.DisplayName) || utf8.RuneCountInString(input.DisplayName) > 128 {
		return nil, fail(400, "mailbox_invalid_operator")
	}
	generated, hash := "", ""
	if id == 0 {
		if input.Password == "" {
			var err error
			generated, err = token()
			if err != nil {
				return nil, err
			}
			input.Password = generated
		}
		var err error
		hash, err = operatorPasswordHash(input.Password)
		if err != nil {
			return nil, err
		}
	} else if input.Password != "" {
		return nil, fail(400, "mailbox_password_reset_required")
	}
	var result OperatorView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.checkOperatorAdmin(actor); err != nil {
			return err
		}
		now := s.Now().Unix()
		if id == 0 {
			item := model.MailboxOperator{Username: input.Username, DisplayName: input.DisplayName, PasswordHash: hash, Enabled: input.Enabled, Version: 1, AuthVersion: 1, CreatedAt: now, UpdatedAt: now}
			created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "username"}}, DoNothing: true}).Create(&item)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected != 1 {
				return fail(409, "mailbox_operator_exists")
			}
			result = operatorView(item)
			result.GeneratedPassword = generated
			actor.TargetOperatorID = item.ID
			return t.Audit(actor, "operator_create", 0, 0, 200, "")
		}
		item, err := t.operatorByID(id)
		if err != nil {
			return err
		}
		if item.Username != input.Username {
			return fail(400, "mailbox_username_immutable")
		}
		if input.Version <= 0 || item.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		updates := map[string]any{"display_name": input.DisplayName, "enabled": input.Enabled, "version": item.Version + 1, "updated_at": now}
		if !input.Enabled {
			updates["auth_version"] = item.AuthVersion + 1
		}
		updated := tx.Model(&model.MailboxOperator{}).Where("id = ? AND version = ?", id, item.Version).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fail(409, "mailbox_version_conflict")
		}
		if !input.Enabled {
			if err := tx.Where("operator_id = ?", id).Delete(&model.MailboxSession{}).Error; err != nil {
				return err
			}
		}
		item.DisplayName = input.DisplayName
		item.Enabled = input.Enabled
		item.Version++
		item.UpdatedAt = now
		result = operatorView(*item)
		actor.TargetOperatorID = id
		return t.Audit(actor, "operator_update", 0, 0, 200, "")
	})
	if err != nil {
		return nil, err
	}
	if id > 0 && !input.Enabled {
		s.invalidateCVVOperator(id)
	}
	return &result, nil
}

// CAS and session deletion share a transaction so revocation cannot be partially applied.
func (s *Service) invalidateOperator(item *model.MailboxOperator, hash string) error {
	updates := map[string]any{"auth_version": item.AuthVersion + 1, "version": item.Version + 1, "updated_at": s.Now().Unix()}
	if hash != "" {
		updates["password_hash"] = hash
	}
	updated := s.DB.Model(&model.MailboxOperator{}).Where("id = ? AND version = ? AND auth_version = ?", item.ID, item.Version, item.AuthVersion).Updates(updates)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fail(409, "mailbox_version_conflict")
	}
	return s.DB.Where("operator_id = ?", item.ID).Delete(&model.MailboxSession{}).Error
}

func (s *Service) ResetPassword(ctx context.Context, actor Actor, id int64, password string) (string, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorAdmin(actor); err != nil {
		return "", err
	}
	if password == "" {
		var err error
		password, err = token()
		if err != nil {
			return "", err
		}
	}
	hash, err := operatorPasswordHash(password)
	if err != nil {
		return "", err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.checkOperatorAdmin(actor); err != nil {
			return err
		}
		item, err := t.operatorByID(id)
		if err != nil {
			return err
		}
		if err := t.invalidateOperator(item, hash); err != nil {
			return err
		}
		actor.TargetOperatorID = id
		return t.Audit(actor, "operator_password_reset", 0, 0, 200, "")
	})
	if err != nil {
		return "", err
	}
	s.invalidateCVVOperator(id)
	return password, nil
}

func (s *Service) RevokeSessions(ctx context.Context, actor Actor, id int64) error {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorAdmin(actor); err != nil {
		return err
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.checkOperatorAdmin(actor); err != nil {
			return err
		}
		item, err := t.operatorByID(id)
		if err != nil {
			return err
		}
		if err := t.invalidateOperator(item, ""); err != nil {
			return err
		}
		actor.TargetOperatorID = id
		return t.Audit(actor, "operator_sessions_revoke", 0, 0, 200, "")
	})
	if err == nil {
		s.invalidateCVVOperator(id)
	}
	return err
}
