package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AssignmentConflict struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type AssignmentConflictError struct{ Conflicts []AssignmentConflict }

func (e *AssignmentConflictError) Error() string { return "mailbox_already_submitted" }
func (e *AssignmentConflictError) Unwrap() error { return fail(409, e.Error()) }

// Run after acquiring all account locks. A protected submission survives recall,
// archival and status reopening; none of those operations permit reassignment.
func (s *Service) assignmentConflicts(ids []int64) error {
	var items []AssignmentConflict
	err := s.DB.Table("mailbox_accounts a").Select("a.id, a.email").Where("a.id IN ? AND a.account_type = ?", ids, s.pool()).Where("EXISTS (SELECT 1 FROM mailbox_assignments t JOIN mailbox_submissions u ON u.assignment_id=t.id WHERE t.account_id=a.id AND u.status IN ?)", []string{StatusPending, StatusApproved}).Order("a.id").Scan(&items).Error
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return &AssignmentConflictError{Conflicts: items}
	}
	return nil
}

type RepairItem struct {
	AccountID                 int64 `json:"account_id"`
	AccountVersion            int64 `json:"account_version"`
	OriginalAssignmentID      int64 `json:"original_assignment_id"`
	OriginalAssignmentVersion int64 `json:"original_assignment_version"`
	CurrentAssignmentID       int64 `json:"current_assignment_id"`
	CurrentAssignmentVersion  int64 `json:"current_assignment_version"`
	SubmissionID              int64 `json:"submission_id"`
	SubmissionVersion         int64 `json:"submission_version"`
}

type RepairCandidate struct {
	RepairItem
	LaterAssignments     []RepairAssignment `json:"later_assignments"`
	Email                string             `json:"email"`
	AccountType          string             `json:"account_type"`
	OriginalOperatorID   int64              `json:"original_operator_id"`
	OriginalOperatorName string             `json:"original_operator_name"`
	OriginalRevokedAt    int64              `json:"original_revoked_at"`
	SubmissionStatus     string             `json:"submission_status"`
	SubmittedAt          int64              `json:"submitted_at"`
	ReviewReason         string             `json:"review_reason"`
	ReviewedAt           int64              `json:"reviewed_at"`
	CurrentOperatorName  string             `json:"current_operator_name"`
	CurrentStatus        string             `json:"current_status"`
	CurrentAssignedAt    int64              `json:"current_assigned_at"`
	CanRepair            bool               `json:"can_repair"`
	ConflictCode         string             `json:"conflict_code"`
}

type RepairAssignment struct {
	ID           int64  `json:"id"`
	OperatorID   int64  `json:"operator_id"`
	OperatorName string `json:"operator_name"`
	Status       string `json:"status"`
	AssignedAt   int64  `json:"assigned_at"`
	RevokedAt    int64  `json:"revoked_at"`
}

type RepairInput struct {
	AccountType string       `json:"account_type"`
	Reason      string       `json:"reason"`
	Items       []RepairItem `json:"items"`
}

func (s *Service) checkRepairActor(actor Actor) error {
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	for _, p := range []authz.Permission{authz.MailboxView, authz.MailboxAssign, authz.MailboxReview} {
		if err := s.CheckActor(actor, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) repairQuery() *gorm.DB {
	return s.DB.Table("mailbox_accounts a").Joins("JOIN mailbox_assignments t ON t.account_id=a.id").Joins("JOIN mailbox_submissions u ON u.assignment_id=t.id").Where("a.account_type=? AND t.revoked_at>0 AND u.status IN ?", s.pool(), []string{StatusPending, StatusApproved}).Where("u.id=(SELECT MAX(p.id) FROM mailbox_submissions p JOIN mailbox_assignments pt ON pt.id=p.assignment_id WHERE pt.account_id=a.id AND pt.revoked_at>0 AND p.status IN ?)", []string{StatusPending, StatusApproved})
}

// Only metadata is loaded here. A preview is advisory; repair rebuilds it inside
// the account transaction and compares every submitted version.
func (s *Service) repairCandidate(account *model.MailboxAccount, submissionID int64) (*RepairCandidate, error) {
	var sub model.MailboxSubmission
	if err := s.DB.First(&sub, submissionID).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	var original model.MailboxAssignment
	if err := s.DB.First(&original, sub.AssignmentID).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	if original.AccountID != account.ID {
		return nil, fail(409, "mailbox_repair_conflict")
	}
	r := &RepairCandidate{RepairItem: RepairItem{AccountID: account.ID, AccountVersion: account.Version, OriginalAssignmentID: original.ID, OriginalAssignmentVersion: original.Version, CurrentAssignmentID: account.ActiveAssignmentID, SubmissionID: sub.ID, SubmissionVersion: sub.Version}, Email: account.Email, AccountType: account.AccountType, OriginalOperatorID: original.OperatorID, OriginalRevokedAt: original.RevokedAt, SubmissionStatus: sub.Status, SubmittedAt: sub.CreatedAt, ReviewReason: sub.ReviewReason, ReviewedAt: sub.ReviewedAt, CurrentStatus: "unassigned"}
	conflict := func(code string) {
		if r.ConflictCode == "" {
			r.ConflictCode = code
		}
	}
	var op model.MailboxOperator
	err := s.DB.First(&op, original.OperatorID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	r.OriginalOperatorName = op.DisplayName
	if err != nil || !op.Enabled {
		conflict("mailbox_repair_operator_disabled")
	}
	if account.ArchivedAt != 0 {
		conflict("mailbox_account_archived")
	}
	expected := StatusSubmitted
	if sub.Status == StatusApproved {
		expected = StatusApproved
	}
	if (sub.Status != StatusPending && sub.Status != StatusApproved) || sub.OperatorID != original.OperatorID || original.RevokedAt == 0 || original.Status != expected || original.RevokedAt < sub.CreatedAt || original.RevokedAt < sub.ReviewedAt {
		conflict("mailbox_repair_ambiguous")
	}
	var later []model.MailboxAssignment
	if err := s.DB.Where("account_id=? AND id>?", account.ID, original.ID).Order("id").Find(&later).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(later))
	r.LaterAssignments = []RepairAssignment{}
	operatorIDs := make([]int64, 0, len(later))
	for _, a := range later {
		operatorIDs = append(operatorIDs, a.OperatorID)
	}
	names := map[int64]string{}
	if len(operatorIDs) > 0 {
		var operators []model.MailboxOperator
		if err := s.DB.Select("id, display_name").Where("id IN ?", operatorIDs).Find(&operators).Error; err != nil {
			return nil, err
		}
		for _, o := range operators {
			names[o.ID] = o.DisplayName
		}
	}
	foundActive := account.ActiveAssignmentID == 0
	for _, a := range later {
		ids = append(ids, a.ID)
		r.LaterAssignments = append(r.LaterAssignments, RepairAssignment{ID: a.ID, OperatorID: a.OperatorID, OperatorName: names[a.OperatorID], Status: a.Status, AssignedAt: a.CreatedAt, RevokedAt: a.RevokedAt})
		if a.ID == account.ActiveAssignmentID {
			foundActive = true
			r.CurrentAssignmentVersion = a.Version
			r.CurrentStatus = a.Status
			r.CurrentAssignedAt = a.CreatedAt
			var current model.MailboxOperator
			if err := s.DB.First(&current, a.OperatorID).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, err
				}
				conflict("mailbox_repair_ambiguous")
			}
			r.CurrentOperatorName = current.DisplayName
			if a.RevokedAt != 0 {
				conflict("mailbox_repair_ambiguous")
			}
		} else if a.RevokedAt == 0 {
			conflict("mailbox_repair_ambiguous")
		}
		if a.Status != StatusPending {
			conflict("mailbox_repair_ambiguous")
		}
	}
	if !foundActive {
		conflict("mailbox_repair_ambiguous")
	}
	var unexpectedActive int64
	if err := s.DB.Model(&model.MailboxAssignment{}).Where("account_id=? AND revoked_at=0 AND id<>?", account.ID, account.ActiveAssignmentID).Count(&unexpectedActive).Error; err != nil {
		return nil, err
	}
	if unexpectedActive > 0 {
		conflict("mailbox_repair_ambiguous")
	}
	var newer int64
	if err := s.DB.Table("mailbox_submissions u").Joins("JOIN mailbox_assignments t ON t.id=u.assignment_id").Where("t.account_id=? AND u.id>?", account.ID, sub.ID).Count(&newer).Error; err != nil {
		return nil, err
	}
	if newer > 0 {
		conflict("mailbox_repair_later_activity")
	}
	if len(ids) > 0 {
		for _, table := range []string{"mailbox_submissions", "mailbox_issues", "mailbox_attachments"} {
			var n int64
			if err := s.DB.Table(table).Where("assignment_id IN ?", ids).Count(&n).Error; err != nil {
				return nil, err
			}
			if n > 0 {
				conflict("mailbox_repair_later_activity")
			}
		}
	}
	var mutations int64
	if err := s.DB.Model(&model.MailboxAudit{}).Where("account_id=? AND action='change_status' AND created_at>=?", account.ID, sub.CreatedAt).Where("NOT (assignment_id=? AND from_status=? AND to_status=?)", original.ID, StatusSubmitted, StatusApproved).Count(&mutations).Error; err != nil {
		return nil, err
	}
	if mutations > 0 {
		conflict("mailbox_repair_ambiguous")
	}
	r.CanRepair = r.ConflictCode == ""
	return r, nil
}

func (s *Service) ListAssignmentRepairs(ctx context.Context, actor Actor, query ListQuery) (*Page[RepairCandidate], error) {
	var err error
	s, err = s.withAccountType(query.AccountType)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err = s.checkRepairActor(actor); err != nil {
		return nil, err
	}
	query = normalizePage(query)
	q := s.repairQuery()
	if query.Search != "" {
		q = q.Where("LOWER(a.email) LIKE ?", "%"+strings.ToLower(strings.TrimSpace(query.Search))+"%")
	}
	if query.OperatorID > 0 {
		q = q.Where("t.operator_id=? OR EXISTS(SELECT 1 FROM mailbox_assignments later WHERE later.account_id=a.id AND later.id>t.id AND later.operator_id=?)", query.OperatorID, query.OperatorID)
	}
	p := &Page[RepairCandidate]{Items: []RepairCandidate{}, Page: query.Page, PageSize: query.PageSize}
	if err = q.Count(&p.Total).Error; err != nil {
		return nil, err
	}
	var refs []struct{ AccountID, SubmissionID int64 }
	if err = q.Select("a.id AS account_id,u.id AS submission_id").Order("a.id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&refs).Error; err != nil {
		return nil, err
	}
	for _, ref := range refs {
		var a model.MailboxAccount
		if err = s.DB.First(&a, ref.AccountID).Error; err != nil {
			return nil, err
		}
		candidate, e := s.repairCandidate(&a, ref.SubmissionID)
		if e != nil {
			return nil, e
		}
		p.Items = append(p.Items, *candidate)
	}
	p.HasMore = int64(query.Page*query.PageSize) < p.Total
	if err = s.checkRepairActor(actor); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) RepairAssignments(ctx context.Context, actor Actor, input RepairInput) error {
	var err error
	s, err = s.withAccountType(input.AccountType)
	if err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err = s.checkRepairActor(actor); err != nil {
		return err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Items) == 0 || len(input.Items) > 1000 || input.Reason == "" || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 2000 || strings.IndexFunc(input.Reason, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return fail(400, "mailbox_invalid_review")
	}
	items := append([]RepairItem(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].AccountID < items[j].AccountID })
	for i, x := range items {
		if x.AccountID <= 0 || x.AccountVersion <= 0 || x.OriginalAssignmentID <= 0 || x.OriginalAssignmentVersion <= 0 || x.SubmissionID <= 0 || x.SubmissionVersion <= 0 || x.CurrentAssignmentID < 0 || x.CurrentAssignmentVersion < 0 || (x.CurrentAssignmentID == 0) != (x.CurrentAssignmentVersion == 0) || (i > 0 && items[i-1].AccountID == x.AccountID) {
			return fail(400, "mailbox_invalid_assignment")
		}
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxAssign); err != nil {
			return err
		}
		if err := s.checkRepairActor(actor); err != nil {
			return err
		}
		// Operator locks precede account locks, matching ordinary workflow writes.
		originalIDs := make([]int64, len(items))
		for i, x := range items {
			originalIDs[i] = x.OriginalAssignmentID
		}
		var operators []model.MailboxOperator
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN (?)", tx.Model(&model.MailboxAssignment{}).Select("operator_id").Where("id IN ?", originalIDs)).Order("id").Find(&operators).Error; err != nil {
			return err
		}
		// Use the locking read, not an older REPEATABLE READ snapshot, for
		// operator eligibility. Disabling an operator does not bump accounts.
		eligibleOperators := make(map[int64]bool, len(operators))
		for _, operator := range operators {
			eligibleOperators[operator.ID] = operator.Enabled
		}
		for _, x := range items {
			a, err := s.workflowAccount(x.AccountID)
			if err != nil {
				return err
			}
			if a.Version != x.AccountVersion {
				return fail(409, "mailbox_version_conflict")
			}
			var locked []model.MailboxAssignment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id=?", a.ID).Order("id").Find(&locked).Error; err != nil {
				return err
			}
			r, err := s.repairCandidate(a, x.SubmissionID)
			if err != nil {
				return err
			}
			if !eligibleOperators[r.OriginalOperatorID] {
				return fail(409, "mailbox_repair_operator_disabled")
			}
			if r.RepairItem != x {
				return fail(409, "mailbox_version_conflict")
			}
			if !r.CanRepair {
				return fail(409, r.ConflictCode)
			}
			now := s.Now().Unix()
			if x.CurrentAssignmentID != 0 {
				if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id=? AND version=? AND revoked_at=0", x.CurrentAssignmentID, x.CurrentAssignmentVersion).Updates(map[string]any{"revoked_at": now, "version": gorm.Expr("version+1"), "updated_at": now})); err != nil {
					return err
				}
			}
			status := StatusSubmitted
			if r.SubmissionStatus == StatusApproved {
				status = StatusApproved
			}
			if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id=? AND version=? AND revoked_at=?", x.OriginalAssignmentID, x.OriginalAssignmentVersion, r.OriginalRevokedAt).Updates(map[string]any{"revoked_at": 0, "status": status, "version": gorm.Expr("version+1"), "updated_at": now})); err != nil {
				return err
			}
			if err := workflowCAS(tx.Model(&model.MailboxAccount{}).Where("id=? AND version=?", a.ID, a.Version+1).Update("active_assignment_id", x.OriginalAssignmentID)); err != nil {
				return err
			}
			metadata, _ := json.Marshal(struct{ OriginalAssignmentID, RevokedAssignmentID, SubmissionID, OriginalRevokedAt int64 }{x.OriginalAssignmentID, x.CurrentAssignmentID, x.SubmissionID, r.OriginalRevokedAt})
			for _, audit := range []model.MailboxAudit{
				{Action: "repair_assignment", Reason: input.Reason, FromStatus: r.CurrentStatus, ToStatus: status},
				{Action: "repair_assignment_links", Reason: string(metadata)},
			} {
				audit.AccountType = s.pool()
				audit.AdminID = actor.Admin.UserID
				audit.AccountID = a.ID
				audit.AssignmentID = x.OriginalAssignmentID
				audit.TargetOperatorID = r.OriginalOperatorID
				audit.StatusCode = 200
				audit.IPAddress = actor.IP
				audit.CreatedAt = now
				if err := tx.Create(&audit).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
