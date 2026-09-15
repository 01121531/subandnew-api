package mailbox

import (
	"context"
	"crypto/rand"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

const temporaryCVVTTL = 10 * time.Minute
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

// This store must never be replaced with a persistent/distributed cache.
// CVVs have no model representation and are never included in audit payloads.
type cvvStore struct {
	mu    sync.Mutex
	items map[int64]*temporaryCVV
}

func newCVVStore() *cvvStore { return &cvvStore{items: make(map[int64]*temporaryCVV)} }

func temporaryCVVEnabled() bool {
	return temporaryCVVUnavailableReason() == ""
}

func temporaryCVVUnavailableReason() string {
	if os.Getenv("MAILBOX_TEMP_CVV_MODE") != "single_node" {
		return "not_enabled"
	}
	if !common.IsMasterNode || os.Getenv("NODE_TYPE") == "slave" {
		return "node_unsupported"
	}
	return ""
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
}

func (v *cvvStore) purgeLocked(now time.Time) {
	for id, item := range v.items {
		if !item.expires.After(now) {
			v.removeLocked(id)
		}
	}
}

func (v *cvvStore) putLocked(id int64, value string, now time.Time, assignment, operator, authVersion int64) {
	v.removeLocked(id)
	item := &temporaryCVV{deliveryID: rand.Text(), value: []byte(value), expires: now.Add(temporaryCVVTTL), assignmentID: assignment, operatorID: operator, authVersion: authVersion}
	v.items[id] = item
	item.timer = time.AfterFunc(temporaryCVVTTL, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.items[id] == item {
			v.removeLocked(id)
		}
	})
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
	item := v.items[view.ID]
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
}

type cvvAssignmentChange struct{ account, oldAssignment, assignment, operator, authVersion int64 }

func (s *Service) updateCVVAssignments(changes []cvvAssignmentChange) {
	v := s.cvvStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, change := range changes {
		item := v.items[change.account]
		if item == nil {
			continue
		}
		if change.oldAssignment != 0 || change.assignment == 0 || item.assignmentID != 0 {
			v.removeLocked(change.account)
		} else {
			item.assignmentID, item.operatorID, item.authVersion = change.assignment, change.operator, change.authVersion
		}
	}
}

type TemporaryCVVInput struct {
	Version int64  `json:"version"`
	CVV     string `json:"cvv"`
}
type TemporaryCVVView struct {
	ExpiresAt int64 `json:"expires_at"`
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
	if v.items[id] == nil && len(v.items) >= temporaryCVVCapacity {
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
	now := s.Now()
	v.putLocked(id, input.CVV, now, assignmentID, operatorID, authVersion)
	return &TemporaryCVVView{ExpiresAt: now.Add(temporaryCVVTTL).Unix()}, nil
}

func (s *Service) claimTemporaryCVV(actor Actor, account *model.MailboxAccount) (*CredentialView, error) {
	if actor.Admin != nil || account.AccountType != AccountTypeOpening {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if !temporaryCVVEnabled() {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	var assignment model.MailboxAssignment
	if err := s.DB.Where("id = ? AND account_id = ? AND operator_id = ? AND revoked_at = 0 AND status IN ?", account.ActiveAssignmentID, account.ID, actor.OperatorID, []string{StatusPending, StatusRejected}).First(&assignment).Error; err != nil {
		return nil, fail(403, "mailbox_credentials_revoked")
	}
	v := s.cvvStore()
	v.mu.Lock()
	v.purgeLocked(s.Now())
	item := v.items[account.ID]
	if item == nil || item.assignmentID != assignment.ID || item.operatorID != actor.OperatorID || item.authVersion != actor.AuthVersion {
		v.mu.Unlock()
		return nil, fail(404, "mailbox_cvv_unavailable")
	}
	value, expires := string(item.value), item.expires.Unix()
	v.removeLocked(account.ID)
	v.mu.Unlock()
	// Take before audit/check: any uncertainty consumes the handoff, never replays it.
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
	return &CredentialView{CVV: value, ServerTime: s.Now().Unix(), ExpiresAt: expires}, nil
}
