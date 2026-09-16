package mailbox

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

const temporaryCVVRevealTTL = 30 * time.Minute
const temporaryCVVUnviewedTTL = 30 * 24 * time.Hour
const temporaryCVVCapacity = 10000

var cvvPattern = regexp.MustCompile(`^[0-9]{3,4}$`)
var processCVVs = newCVVStore()

type temporaryCVV struct {
	deliveryID   string
	value        []byte
	expires      time.Time
	assignmentID int64
	operatorID   int64
	authVersion  int64
	timer        *time.Timer
}

// Redis mode uses only a dedicated server whose RDB and AOF are disabled.
// CVVs have no database model and are never included in audit payloads.
type cvvStore struct {
	mu    sync.Mutex
	items map[int64]*temporaryCVV
}

func newCVVStore() *cvvStore { return &cvvStore{items: make(map[int64]*temporaryCVV)} }

func temporaryCVVEnabled() bool {
	return temporaryCVVUnavailableReason() == ""
}

func temporaryCVVUnavailableReason() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("MAILBOX_TEMP_CVV_MODE")))
	nodeType := strings.ToLower(strings.TrimSpace(os.Getenv("NODE_TYPE")))
	if !common.IsMasterNode || nodeType == "slave" {
		return "node_unsupported"
	}
	switch mode {
	case "", "single_node":
		return ""
	case "redis":
		if err := validateTemporaryCVVRedis(); err != nil {
			return temporaryCVVRedisErrorCode(err)
		}
		return ""
	case "disabled", "off", "none":
		return "not_enabled"
	default:
		return "not_enabled"
	}
}

func (s *Service) cvvStore() *cvvStore {
	if s.cvv != nil {
		return s.cvv
	}
	return processCVVs
}

func (v *cvvStore) removeLocked(id int64) {
	if item := v.items[id]; item != nil {
		clear(item.value)
		item.value = nil
		if item.timer != nil {
			item.timer.Stop()
		}
		delete(v.items, id)
	}
	if temporaryCVVRedisMode() {
		_ = deleteTemporaryCVVRedis(id)
	}
}

func (v *cvvStore) purgeLocked(now time.Time) {
	for id, item := range v.items {
		if !item.expires.IsZero() && !item.expires.After(now) {
			v.removeLocked(id)
		}
	}
}

func (v *cvvStore) putLocked(id int64, value string, assignment, operator, authVersion int64) error {
	existing, err := v.getLocked(id, time.Now())
	if err != nil {
		return err
	}
	count, err := temporaryCVVCount(v)
	if err != nil {
		return err
	}
	if existing == nil && count >= temporaryCVVCapacity {
		return errors.New("temporary cvv capacity reached")
	}
	if cached := v.items[id]; cached != nil {
		clear(cached.value)
		if cached.timer != nil {
			cached.timer.Stop()
		}
		delete(v.items, id)
	}
	item := &temporaryCVV{deliveryID: rand.Text(), value: []byte(value), assignmentID: assignment, operatorID: operator, authVersion: authVersion}
	if temporaryCVVRedisMode() {
		if err := saveTemporaryCVVRedis(id, item, temporaryCVVUnviewedTTL); err != nil {
			clear(item.value)
			return err
		}
	}
	v.items[id] = item
	return nil
}

func (v *cvvStore) getLocked(id int64, now time.Time) (*temporaryCVV, error) {
	v.purgeLocked(now)
	if !temporaryCVVRedisMode() {
		return v.items[id], nil
	}
	item, err := loadTemporaryCVVRedis(id)
	if err != nil || item == nil {
		if cached := v.items[id]; cached != nil {
			clear(cached.value)
			if cached.timer != nil {
				cached.timer.Stop()
			}
			delete(v.items, id)
		}
		return item, err
	}
	if cached := v.items[id]; cached != nil {
		clear(cached.value)
		if cached.timer != nil {
			cached.timer.Stop()
		}
	}
	if !item.expires.IsZero() && !item.expires.After(now) {
		_ = deleteTemporaryCVVRedis(id)
		return nil, nil
	}
	v.items[id] = item
	v.scheduleExpiryLocked(id, item, now)
	return item, nil
}

func (v *cvvStore) scheduleExpiryLocked(id int64, item *temporaryCVV, now time.Time) {
	if item.expires.IsZero() || !item.expires.After(now) {
		return
	}
	delay := item.expires.Sub(now)
	item.timer = time.AfterFunc(delay, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.items[id] == item {
			v.removeLocked(id)
		}
	})
}

func (v *cvvStore) startRevealLocked(id int64, item *temporaryCVV, now time.Time) error {
	if !item.expires.IsZero() {
		return nil
	}
	item.expires = now.Add(temporaryCVVRevealTTL)
	if temporaryCVVRedisMode() {
		stored, err := claimTemporaryCVVRedis(id, item, now)
		if err != nil || stored == nil {
			item.expires = time.Time{}
			if err != nil {
				return err
			}
			return errors.New("temporary cvv unavailable")
		}
		item.expires = stored.expires
	}
	v.scheduleExpiryLocked(id, item, now)
	return nil
}

func (s *Service) invalidateCVVAccount(id int64) {
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeLocked(id)
}

func (s *Service) temporaryCVVMetadata(actor Actor, view *AccountView) {
	if actor.Admin != nil || view.AccountType != AccountTypeOpening {
		return
	}
	view.TemporaryCVVStatus = "unavailable"
	if !temporaryCVVEnabled() {
		view.TemporaryCVVStatus = "disabled"
		return
	}
	if view.Status != StatusPending && view.Status != StatusRejected {
		return
	}
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purgeLocked(s.Now())
	item, err := v.getLocked(view.ID, s.Now())
	if err != nil {
		view.TemporaryCVVStatus = "disabled"
		return
	}
	if item != nil && item.assignmentID == view.AssignmentID && item.operatorID == actor.OperatorID && item.authVersion == actor.AuthVersion {
		view.TemporaryCVVStatus, view.TemporaryCVVID = "available", item.deliveryID
	}
}

func (s *Service) invalidateCVVOperator(id int64) {
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	for accountID, item := range v.items {
		if item.operatorID == id {
			v.removeLocked(accountID)
		}
	}
	if temporaryCVVRedisMode() {
		_ = deleteTemporaryCVVRedisOperator(id)
	}
}

type cvvAssignmentChange struct{ account, oldAssignment, assignment, operator, authVersion int64 }

func (s *Service) updateCVVAssignments(changes []cvvAssignmentChange) {
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, change := range changes {
		item, _ := v.getLocked(change.account, s.Now())
		if item == nil {
			continue
		}
		if change.oldAssignment != 0 || change.assignment == 0 || item.assignmentID != 0 {
			v.removeLocked(change.account)
		} else {
			item.assignmentID, item.operatorID, item.authVersion = change.assignment, change.operator, change.authVersion
			ttl := temporaryCVVUnviewedTTL
			if !item.expires.IsZero() {
				ttl = item.expires.Sub(s.Now())
			}
			if temporaryCVVRedisMode() && saveTemporaryCVVRedis(change.account, item, ttl) != nil {
				v.removeLocked(change.account)
			}
		}
	}
}

type TemporaryCVVInput struct {
	Version int64  `json:"version"`
	CVV     string `json:"cvv"`
}
type TemporaryCVVView struct {
	ExpiresAt         int64 `json:"expires_at"`
	StartsOnFirstView bool  `json:"starts_on_first_view"`
}

func (s *Service) ProvideTemporaryCVV(ctx context.Context, actor Actor, id int64, input TemporaryCVVInput, accountTypes ...string) (view *TemporaryCVVView, err error) {
	defer func() { s.auditMailboxFailure(actor, "temporary_cvv_provide", id, err) }()
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err = s.checkMailboxImportActor(actor); err != nil {
		return nil, err
	}
	if err = s.CheckActor(actor, authz.MailboxCredentials); err != nil {
		return nil, err
	}
	if !temporaryCVVEnabled() {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if s.pool() != AccountTypeOpening || !cvvPattern.MatchString(input.CVV) || input.Version <= 0 {
		return nil, fail(400, "mailbox_invalid_cvv")
	}
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purgeLocked(s.Now())
	count, countErr := temporaryCVVCount(v)
	if countErr != nil {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	item, loadErr := v.getLocked(id, s.Now())
	if loadErr != nil {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if item == nil && count >= temporaryCVVCapacity {
		return nil, fail(503, "mailbox_cvv_capacity")
	}
	var assignmentID, operatorID, authVersion int64
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.workflowActor(actor, authz.MailboxManage); err != nil {
			return err
		}
		if err := t.CheckActor(actor, authz.MailboxCredentials); err != nil {
			return err
		}
		account, err := t.workflowAccount(id)
		if err != nil {
			return err
		}
		if account.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		assignmentID = account.ActiveAssignmentID
		if assignmentID != 0 {
			var assignment model.MailboxAssignment
			if err := tx.Where("id = ? AND account_id = ? AND revoked_at = 0 AND status IN ?", assignmentID, id, []string{StatusPending, StatusRejected}).First(&assignment).Error; err != nil {
				return fail(403, "mailbox_credentials_revoked")
			}
			var operator model.MailboxOperator
			if err := tx.Where("id = ? AND enabled = ?", assignment.OperatorID, true).First(&operator).Error; err != nil {
				return fail(403, "mailbox_credentials_revoked")
			}
			operatorID, authVersion = operator.ID, operator.AuthVersion
		}
		return t.Audit(actor, "temporary_cvv_provide", id, assignmentID, 200, "")
	})
	if err != nil {
		return nil, err
	}
	if err := v.putLocked(id, input.CVV, assignmentID, operatorID, authVersion); err != nil {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	return &TemporaryCVVView{StartsOnFirstView: true}, nil
}

func (s *Service) claimTemporaryCVV(actor Actor, account *model.MailboxAccount) (*CredentialView, error) {
	if account.AccountType != AccountTypeOpening {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if !temporaryCVVEnabled() {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if actor.Admin != nil {
		return s.claimTemporaryCVVAdmin(actor, account)
	}
	var assignment model.MailboxAssignment
	if err := s.DB.Where("id = ? AND account_id = ? AND operator_id = ? AND revoked_at = 0 AND status IN ?", account.ActiveAssignmentID, account.ID, actor.OperatorID, []string{StatusPending, StatusRejected}).First(&assignment).Error; err != nil {
		return nil, fail(403, "mailbox_credentials_revoked")
	}
	v := s.cvvStore()
	v.mu.Lock()
	now := s.Now()
	v.purgeLocked(now)
	item, loadErr := v.getLocked(account.ID, now)
	if loadErr != nil {
		v.mu.Unlock()
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if item == nil || item.assignmentID != assignment.ID || item.operatorID != actor.OperatorID || item.authVersion != actor.AuthVersion {
		v.mu.Unlock()
		return nil, fail(404, "mailbox_cvv_unavailable")
	}
	if err := v.startRevealLocked(account.ID, item, now); err != nil {
		v.mu.Unlock()
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	value, expires := string(item.value), item.expires.Unix()
	v.mu.Unlock()
	if err := s.Audit(actor, "credentials_cvv", account.ID, assignment.ID, 200, ""); err != nil {
		return nil, fail(500, "mailbox_service_unavailable")
	}
	current, err := s.GetAccount(s.DB.Statement.Context, actor, account.ID, AccountTypeOpening)
	if err != nil {
		return nil, err
	}
	if current.AssignmentID != assignment.ID || current.AssignmentVersion != assignment.Version || (current.Status != StatusPending && current.Status != StatusRejected) {
		return nil, fail(403, "mailbox_credentials_revoked")
	}
	if s.Now().Unix() >= expires {
		return nil, fail(404, "mailbox_cvv_unavailable")
	}
	return &CredentialView{Available: true, CVV: value, ServerTime: s.Now().Unix(), ExpiresAt: expires}, nil
}

func (s *Service) claimTemporaryCVVAdmin(actor Actor, account *model.MailboxAccount) (*CredentialView, error) {
	if err := s.CheckActor(actor, authz.MailboxCredentials); err != nil {
		return nil, err
	}
	v := s.cvvStore()
	v.mu.Lock()
	item, err := v.getLocked(account.ID, s.Now())
	if err != nil {
		v.mu.Unlock()
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if item == nil {
		v.mu.Unlock()
		return nil, fail(404, "mailbox_cvv_unavailable")
	}
	deliveryID := item.deliveryID
	v.mu.Unlock()
	if err := s.Audit(actor, "credentials_cvv", account.ID, account.ActiveAssignmentID, 200, ""); err != nil {
		return nil, fail(500, "mailbox_service_unavailable")
	}
	current, err := s.credentialAccount(actor, account.ID)
	if err != nil {
		return nil, err
	}
	if current.Version != account.Version || current.ActiveAssignmentID != account.ActiveAssignmentID {
		return nil, fail(409, "mailbox_version_conflict")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	item, err = v.getLocked(account.ID, s.Now())
	if err != nil {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if item == nil || item.deliveryID != deliveryID {
		return nil, fail(404, "mailbox_cvv_unavailable")
	}
	expires := int64(0)
	if !item.expires.IsZero() {
		expires = item.expires.Unix()
	}
	return &CredentialView{Available: true, CVV: string(item.value), ServerTime: s.Now().Unix(), ExpiresAt: expires}, nil
}
