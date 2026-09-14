package mailbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

const SessionTTL = 12 * time.Hour
const StatusPending = "pending"
const StatusSubmitted = "submitted"
const StatusApproved = "approved"
const StatusRejected = "rejected"

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string           { return e.Code }
func fail(status int, code string) error { return &Error{status, code} }
func HTTPError(err error) (int, string) {
	var local *Error
	if errors.As(err, &local) {
		return local.Status, local.Code
	}
	if errors.Is(err, authz.ErrAuthorizationChanged) {
		return 401, "admin_authorization_changed"
	}
	if errors.Is(err, authz.ErrDataForbidden) {
		return 403, "mailbox_permission_denied"
	}
	return 500, "mailbox_service_unavailable"
}

type Actor struct {
	Admin            *authz.DataAccess
	OperatorID       int64
	AuthVersion      int64
	SessionHash      string
	IP               string
	TargetOperatorID int64
}
type Service struct {
	DB         *gorm.DB
	Now        func() time.Time
	Cipher     func() (*managedinstance.CredentialCipher, error)
	StorageDir string
}

func New(db *gorm.DB) *Service {
	return &Service{DB: db, Now: time.Now, Cipher: managedinstance.NewCredentialCipherFromEnvironment}
}
func (s *Service) WithDB(db *gorm.DB) *Service { copy := *s; copy.DB = db; return &copy }
func token() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func digest(raw string) string { sum := sha256.Sum256([]byte(raw)); return hex.EncodeToString(sum[:]) }
func CSRF(raw string) string   { return digest("mailbox-csrf:v1:" + raw) }

func (s *Service) CheckActor(actor Actor, permission authz.Permission) error {
	if actor.Admin != nil {
		if err := actor.Admin.Current(s.DB); err != nil {
			return err
		}
		if !actor.Admin.Can(permission) {
			return fail(403, "mailbox_permission_denied")
		}
		return nil
	}
	if actor.OperatorID <= 0 || actor.SessionHash == "" {
		return fail(401, "mailbox_session_expired")
	}
	var count int64
	err := s.DB.Model(&model.MailboxSession{}).Joins("JOIN mailbox_operators ON mailbox_operators.id = mailbox_sessions.operator_id").
		Where("mailbox_sessions.token_hash = ? AND mailbox_sessions.operator_id = ? AND mailbox_sessions.auth_version = ? AND mailbox_sessions.expires_at > ? AND mailbox_operators.enabled = ? AND mailbox_operators.auth_version = ?", actor.SessionHash, actor.OperatorID, actor.AuthVersion, s.Now().Unix(), true, actor.AuthVersion).Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return fail(401, "mailbox_session_expired")
	}
	return nil
}

func (s *Service) AccountForActor(actor Actor, id int64, credentials bool) (*model.MailboxAccount, error) {
	permission := authz.MailboxView
	if credentials {
		permission = authz.MailboxCredentials
	}
	if err := s.CheckActor(actor, permission); err != nil {
		return nil, err
	}
	var account model.MailboxAccount
	if err := s.DB.First(&account, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(404, "mailbox_not_found")
		}
		return nil, err
	}
	if actor.Admin != nil {
		return &account, nil
	}
	var assignment model.MailboxAssignment
	if account.ActiveAssignmentID == 0 || s.DB.First(&assignment, account.ActiveAssignmentID).Error != nil || assignment.AccountID != id || assignment.OperatorID != actor.OperatorID || assignment.RevokedAt != 0 {
		return nil, fail(404, "mailbox_not_found")
	}
	if credentials && assignment.Status == StatusApproved {
		return nil, fail(403, "mailbox_credentials_revoked")
	}
	return &account, nil
}

func (s *Service) Audit(actor Actor, action string, accountID, assignmentID int64, status int, code string) error {
	adminID := 0
	if actor.Admin != nil {
		adminID = actor.Admin.UserID
	}
	return s.DB.Create(&model.MailboxAudit{AdminID: adminID, OperatorID: actor.OperatorID, TargetOperatorID: actor.TargetOperatorID, AccountID: accountID, AssignmentID: assignmentID, Action: action, StatusCode: status, ErrorCode: code, IPAddress: actor.IP, CreatedAt: s.Now().Unix()}).Error
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}
type ListQuery struct {
	Search     string
	Status     string
	OperatorID int64
	Page       int
	PageSize   int
}

func normalizePage(q ListQuery) ListQuery {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	return q
}
