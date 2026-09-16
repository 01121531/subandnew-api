package mailbox

import (
	"context"
	"errors"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Principal struct {
	Operator *model.MailboxOperator
	Session  *model.MailboxSession
}

func PortalActor(p *Principal, ip string) Actor {
	if p == nil || p.Operator == nil || p.Session == nil {
		return Actor{IP: ip}
	}
	return Actor{OperatorID: p.Operator.ID, AuthVersion: p.Session.AuthVersion, SessionHash: p.Session.TokenHash, IP: ip}
}

const operatorLoginWindow = 15 * time.Minute
const operatorDummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Reserve before bcrypt, including in-flight attempts, to bound concurrent guesses.
// Successful attempts release only their own slot, never another request's failures.
func (s *Service) reserveOperatorLogin(key string) (int64, error) {
	var start int64
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		now := s.Now().Unix()
		row := model.MailboxLoginAttempt{Key: key, WindowStart: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		q := tx.Model(&model.MailboxLoginAttempt{}).Where(clause.Eq{Column: "key", Value: key})
		if err := q.Where("window_start <= ? OR failures = 0", now-int64(operatorLoginWindow/time.Second)).Updates(map[string]any{"window_start": now, "failures": 0}).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.MailboxLoginAttempt{}).Where(clause.Eq{Column: "key", Value: key}).Where("failures < ?", 5).UpdateColumn("failures", gorm.Expr("failures + 1"))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fail(429, "mailbox_login_rate_limited")
		}
		if err := tx.Where(clause.Eq{Column: "key", Value: key}).First(&row).Error; err != nil {
			return err
		}
		start = row.WindowStart
		return nil
	})
	return start, err
}

func (s *Service) Login(ctx context.Context, username, password, ip string) (*Principal, string, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	username = operatorUsername(username)
	key := digest("mailbox-login:v1:" + username + "\x00" + ip)
	window, err := s.reserveOperatorLogin(key)
	if err != nil {
		return nil, "", err
	}
	var item model.MailboxOperator
	err = s.DB.Where("username = ?", username).First(&item).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}
	hash := item.PasswordHash
	if hash == "" {
		hash = operatorDummyHash
	}
	valid := common.ValidatePasswordAndHash(password, hash)
	if err != nil || !operatorUsernamePattern.MatchString(username) || len(password) < 8 || len(password) > 72 || !item.Enabled || !valid {
		if auditErr := s.Audit(Actor{IP: ip}, "operator_login", 0, 0, 401, "mailbox_invalid_credentials"); auditErr != nil {
			return nil, "", auditErr
		}
		return nil, "", fail(401, "mailbox_invalid_credentials")
	}
	raw, err := token()
	if err != nil {
		return nil, "", err
	}
	now := s.Now().Unix()
	session := model.MailboxSession{TokenHash: digest(raw), OperatorID: item.ID, AuthVersion: item.AuthVersion, CreatedAt: now, ExpiresAt: now + int64(SessionTTL/time.Second)}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// A write lock also serializes with disable/reset on SQLite, where FOR UPDATE is ignored.
		if err := tx.Model(&model.MailboxOperator{}).Where("id = ?", item.ID).UpdateColumn("auth_version", gorm.Expr("auth_version")).Error; err != nil {
			return err
		}
		var current model.MailboxOperator
		if err := tx.Where("id = ? AND enabled = ? AND auth_version = ? AND password_hash = ?", item.ID, true, item.AuthVersion, item.PasswordHash).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fail(401, "mailbox_invalid_credentials")
			}
			return err
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.MailboxLoginAttempt{}).Where(clause.Eq{Column: "key", Value: key}).Where("window_start = ? AND failures > 0", window).UpdateColumn("failures", gorm.Expr("failures - 1")).Error; err != nil {
			return err
		}
		item = current
		return s.WithDB(tx).Audit(Actor{OperatorID: item.ID, IP: ip}, "operator_login", 0, 0, 200, "")
	})
	if err != nil {
		return nil, "", err
	}
	item.PasswordHash = ""
	return &Principal{Operator: &item, Session: &session}, raw, nil
}

func (s *Service) Authenticate(raw string) (*Principal, error) {
	if len(raw) != 43 {
		return nil, fail(401, "mailbox_session_expired")
	}
	var session model.MailboxSession
	if err := s.DB.Where("token_hash = ? AND expires_at > ?", digest(raw), s.Now().Unix()).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(401, "mailbox_session_expired")
		}
		return nil, err
	}
	var item model.MailboxOperator
	if err := s.DB.Where("id = ? AND enabled = ? AND auth_version = ?", session.OperatorID, true, session.AuthVersion).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(401, "mailbox_session_expired")
		}
		return nil, err
	}
	p := &Principal{Operator: &item, Session: &session}
	if err := s.CheckActor(PortalActor(p, ""), authz.MailboxView); err != nil {
		return nil, err
	}
	item.PasswordHash = ""
	return p, nil
}

func (s *Service) checkPortalOperator(actor Actor) error {
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return err
	}
	if actor.Admin != nil {
		return fail(403, "mailbox_permission_denied")
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, actor Actor) error {
	s = s.WithDB(s.DB.WithContext(ctx))
	return s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.checkPortalOperator(actor); err != nil {
			return err
		}
		if err := tx.Where("token_hash = ? AND operator_id = ? AND auth_version = ?", actor.SessionHash, actor.OperatorID, actor.AuthVersion).Delete(&model.MailboxSession{}).Error; err != nil {
			return err
		}
		return t.Audit(actor, "operator_logout", 0, 0, 200, "")
	})
}

func (s *Service) ChangePassword(ctx context.Context, actor Actor, current, password string) error {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkPortalOperator(actor); err != nil {
		return err
	}
	item, err := s.operatorByID(actor.OperatorID)
	if err != nil {
		return err
	}
	if len(current) > 72 || !common.ValidatePasswordAndHash(current, item.PasswordHash) {
		return fail(400, "mailbox_current_password_invalid")
	}
	hash, err := operatorPasswordHash(password)
	if err != nil {
		return err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.checkPortalOperator(actor); err != nil {
			return err
		}
		if err := t.invalidateOperator(item, hash); err != nil {
			return err
		}
		return t.Audit(actor, "operator_password_change", 0, 0, 200, "")
	})
	if err == nil {
		s.invalidateCVVOperator(actor.OperatorID)
	}
	return err
}
