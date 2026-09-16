package mailbox

import (
	"context"
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

type AccountView struct {
	ArchivedAt           int64  `json:"archived_at"`
	ArchivedBy           int    `json:"archived_by"`
	TemporaryCVVID       string `json:"temporary_cvv_id,omitempty"`
	TemporaryCVVStatus   string `json:"temporary_cvv_status,omitempty"`
	AccountType          string `json:"account_type"`
	CardLast4            string `json:"card_last4"`
	ID                   int64  `json:"id"`
	Email                string `json:"email"`
	Version              int64  `json:"version"`
	AssignmentID         int64  `json:"assignment_id"`
	AssignmentVersion    int64  `json:"assignment_version"`
	OperatorID           int64  `json:"operator_id"`
	OperatorName         string `json:"operator_name"`
	Status               string `json:"status"`
	AssignedAt           int64  `json:"assigned_at"`
	CredentialsAvailable bool   `json:"credentials_available"`
}

type AssignItem struct {
	ID      int64 `json:"id"`
	Version int64 `json:"version"`
}

type AssignInput struct {
	AccountType string       `json:"account_type"`
	Items       []AssignItem `json:"items"`
	OperatorID  int64        `json:"operator_id"`
}

type SubmissionView struct {
	Remark       string           `json:"remark"`
	AccountType  string           `json:"account_type"`
	CardLast4    string           `json:"card_last4"`
	ID           int64            `json:"id"`
	AssignmentID int64            `json:"assignment_id"`
	AccountID    int64            `json:"account_id"`
	Email        string           `json:"email"`
	OperatorID   int64            `json:"operator_id"`
	OperatorName string           `json:"operator_name"`
	Status       string           `json:"status"`
	Version      int64            `json:"version"`
	ReviewReason string           `json:"review_reason"`
	ReviewedBy   int              `json:"reviewed_by"`
	ReviewedAt   int64            `json:"reviewed_at"`
	CreatedAt    int64            `json:"created_at"`
	Attachments  []AttachmentView `json:"attachments" gorm:"-"`
}

type ReviewInput struct {
	Version int64  `json:"version"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
}

func workflowNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fail(404, "mailbox_not_found")
	}
	return err
}

func workflowCAS(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fail(409, "mailbox_version_conflict")
	}
	return nil
}

// Called inside write transactions before account locks. Revocation and password
// changes must finish before validation, or wait until this transaction commits.
func (s *Service) workflowActor(actor Actor, permission authz.Permission) error {
	if actor.Admin != nil {
		var user model.User
		if err := s.DB.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, actor.Admin.UserID).Error; err != nil {
			return fail(401, "mailbox_session_expired")
		}
	} else {
		if actor.OperatorID <= 0 || actor.SessionHash == "" {
			return fail(401, "mailbox_session_expired")
		}
		var operator model.MailboxOperator
		if err := s.DB.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operator, actor.OperatorID).Error; err != nil {
			return fail(401, "mailbox_session_expired")
		}
		var session model.MailboxSession
		if err := s.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND operator_id = ?", actor.SessionHash, actor.OperatorID).First(&session).Error; err != nil {
			return fail(401, "mailbox_session_expired")
		}
	}
	return s.CheckActor(actor, permission)
}

// All assignment mutations acquire the account before the assignment. The CAS
// also serializes SQLite, where SELECT FOR UPDATE is not available.
func (s *Service) workflowAccount(id int64) (*model.MailboxAccount, error) {
	var account model.MailboxAccount
	if err := s.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_type = ?", s.pool()).First(&account, id).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	if account.ArchivedAt != 0 {
		return nil, fail(409, "mailbox_account_archived")
	}
	if err := workflowCAS(s.DB.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", id, account.Version).Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": s.Now().Unix()})); err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *Service) workflowAssignment(actor Actor, id int64) (*model.MailboxAssignment, *model.MailboxAccount, error) {
	var hint model.MailboxAssignment
	if err := s.DB.First(&hint, id).Error; err != nil {
		return nil, nil, workflowNotFound(err)
	}
	account, err := s.workflowAccount(hint.AccountID)
	if err != nil {
		return nil, nil, err
	}
	var assignment model.MailboxAssignment
	if err := s.DB.Clauses(clause.Locking{Strength: "UPDATE"}).First(&assignment, id).Error; err != nil {
		return nil, nil, workflowNotFound(err)
	}
	if account.ActiveAssignmentID != id || assignment.RevokedAt != 0 || assignment.AccountID != account.ID || (actor.Admin == nil && assignment.OperatorID != actor.OperatorID) {
		return nil, nil, fail(404, "mailbox_not_found")
	}
	return &assignment, account, nil
}

func (s *Service) accountQuery(actor Actor) *gorm.DB {
	q := s.DB.Table("mailbox_accounts AS a").Where("a.account_type = ?", s.pool()).
		Joins("LEFT JOIN mailbox_assignments AS t ON t.id = a.active_assignment_id AND t.account_id = a.id AND t.revoked_at = 0").
		Joins("LEFT JOIN mailbox_operators AS o ON o.id = t.operator_id")
	if actor.Admin == nil {
		q = q.Where("t.operator_id = ?", actor.OperatorID)
	}
	return q
}

const accountViewSelect = "a.id, a.account_type, a.card_last4, a.email, a.version, a.archived_at, a.archived_by, COALESCE(t.id, 0) AS assignment_id, COALESCE(t.version, 0) AS assignment_version, COALESCE(t.operator_id, 0) AS operator_id, COALESCE(o.display_name, '') AS operator_name, COALESCE(t.status, 'unassigned') AS status, COALESCE(t.created_at, 0) AS assigned_at"

func accountCredentials(actor Actor, view *AccountView) {
	if view.ArchivedAt != 0 {
		view.CredentialsAvailable = false
		return
	}
	if actor.Admin != nil {
		view.CredentialsAvailable = actor.Admin.Can(authz.MailboxCredentials)
		return
	}
	view.CredentialsAvailable = view.Status == StatusPending || view.Status == StatusSubmitted || view.Status == StatusRejected
}

func (s *Service) ListAccounts(ctx context.Context, actor Actor, query ListQuery, accountTypes ...string) (*Page[AccountView], error) {
	var err error
	s, err = s.withAccountType(query.AccountType, accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	query = normalizePage(query)
	q := s.accountQuery(actor)
	if query.Archived {
		if actor.Admin == nil {
			return nil, fail(403, "mailbox_permission_denied")
		}
		q = q.Where("a.archived_at > 0")
	} else {
		q = q.Where("a.archived_at = 0")
	}
	if query.Search != "" {
		q = q.Where("LOWER(a.email) LIKE ?", "%"+strings.ToLower(query.Search)+"%")
	}
	if query.Status == "actionable" {
		q = q.Where("t.status IN ?", []string{StatusPending, StatusRejected})
	} else if query.Status != "" {
		q = q.Where("COALESCE(t.status, 'unassigned') = ?", query.Status)
	}
	if query.OperatorID > 0 {
		q = q.Where("t.operator_id = ?", query.OperatorID)
	}
	page := &Page[AccountView]{Items: []AccountView{}, Page: query.Page, PageSize: query.PageSize}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	if err := q.Select(accountViewSelect).Order("a.id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&page.Items).Error; err != nil {
		return nil, err
	}
	for i := range page.Items {
		accountCredentials(actor, &page.Items[i])
	}
	page.HasMore = int64(query.Page)*int64(query.PageSize) < page.Total
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) GetAccount(ctx context.Context, actor Actor, id int64, accountTypes ...string) (*AccountView, error) {
	var err error
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	var view AccountView
	if err := s.accountQuery(actor).Select(accountViewSelect).Where("a.id = ?", id).Take(&view).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	accountCredentials(actor, &view)
	current, err := s.AccountForActor(actor, id, false)
	if err != nil {
		return nil, err
	}
	if current.Version != view.Version || current.ActiveAssignmentID != view.AssignmentID || current.AccountType != view.AccountType || current.CardLast4 != view.CardLast4 {
		return nil, fail(409, "mailbox_version_conflict")
	}
	if current.ActiveAssignmentID != 0 {
		var assignment model.MailboxAssignment
		if err := s.DB.First(&assignment, current.ActiveAssignmentID).Error; err != nil {
			return nil, workflowNotFound(err)
		}
		if assignment.AccountID != id || assignment.RevokedAt != 0 || assignment.Version != view.AssignmentVersion || assignment.Status != view.Status || assignment.OperatorID != view.OperatorID {
			return nil, fail(409, "mailbox_version_conflict")
		}
	}
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	s.temporaryCVVMetadata(actor, &view)
	return &view, nil
}

func (s *Service) Assign(ctx context.Context, actor Actor, input AssignInput, accountTypes ...string) error {
	var err error
	s, err = s.withAccountType(input.AccountType, accountTypes...)
	if err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxAssign); err != nil {
		return err
	}
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	if len(input.Items) < 1 || len(input.Items) > 1000 || input.OperatorID < 0 {
		return fail(400, "mailbox_invalid_assignment")
	}
	items := append([]AssignItem(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if item.ID <= 0 || item.Version <= 0 || (i > 0 && item.ID == items[i-1].ID) {
			return fail(400, "mailbox_invalid_assignment")
		}
	}
	changes := []cvvAssignmentChange{}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxAssign); err != nil {
			return err
		}
		var operator model.MailboxOperator
		if input.OperatorID != 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND enabled = ?", input.OperatorID, true).First(&operator).Error; err != nil {
				return workflowNotFound(err)
			}
		}
		for _, item := range items {
			account, err := s.workflowAccount(item.ID)
			if err != nil {
				return err
			}
			if account.Version != item.Version {
				return fail(409, "mailbox_version_conflict")
			}
			now := s.Now().Unix()
			if account.ActiveAssignmentID != 0 {
				if err := s.invalidateIssues("assignment_id = ?", account.ActiveAssignmentID); err != nil {
					return err
				}
				if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND account_id = ? AND revoked_at = 0", account.ActiveAssignmentID, account.ID).Updates(map[string]any{"revoked_at": now, "version": gorm.Expr("version + 1"), "updated_at": now})); err != nil {
					return err
				}
			}
			assignmentID := int64(0)
			if input.OperatorID != 0 {
				assignment := model.MailboxAssignment{AccountID: account.ID, OperatorID: input.OperatorID, Status: StatusPending, Version: 1, AssignedBy: actor.Admin.UserID, CreatedAt: now, UpdatedAt: now}
				if err := tx.Create(&assignment).Error; err != nil {
					return err
				}
				assignmentID = assignment.ID
			}
			// workflowAccount already acquired the account CAS. Recalling an
			// unassigned account may report zero changed rows on MySQL.
			if err := tx.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", account.ID, account.Version+1).Update("active_assignment_id", assignmentID).Error; err != nil {
				return err
			}
			action := "assign"
			if input.OperatorID == 0 {
				action = "recall"
			}
			if err := s.Audit(actor, action, account.ID, assignmentID, 200, ""); err != nil {
				return err
			}
			changes = append(changes, cvvAssignmentChange{account.ID, account.ActiveAssignmentID, assignmentID, input.OperatorID, operator.AuthVersion})
		}
		return nil
	})
	if err == nil {
		s.updateCVVAssignments(changes)
	}
	return err
}

func (s *Service) submissionQuery(actor Actor) *gorm.DB {
	q := s.DB.Table("mailbox_submissions AS u").Joins("JOIN mailbox_assignments AS t ON t.id = u.assignment_id AND t.operator_id = u.operator_id").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Joins("JOIN mailbox_operators AS o ON o.id = u.operator_id").Where("a.account_type = ?", s.pool())
	if actor.Admin == nil {
		q = q.Where("u.operator_id = ?", actor.OperatorID)
	}
	return q
}

const submissionViewSelect = "u.id, u.assignment_id, t.account_id, a.account_type, a.card_last4, a.email, u.operator_id, o.display_name AS operator_name, u.status, u.version, u.remark, u.review_reason, u.reviewed_by, u.reviewed_at, u.created_at"

func (s *Service) submissionAttachments(views []SubmissionView) error {
	if len(views) == 0 {
		return nil
	}
	ids := make([]int64, len(views))
	indexes := make(map[int64]int, len(views))
	for i := range views {
		ids[i] = views[i].ID
		indexes[views[i].ID] = i
		views[i].Attachments = []AttachmentView{}
	}
	var attachments []model.MailboxAttachment
	if err := s.DB.Where("submission_id IN ?", ids).Order("created_at, id").Find(&attachments).Error; err != nil {
		return err
	}
	for _, attachment := range attachments {
		i := indexes[attachment.SubmissionID]
		if attachment.AssignmentID == views[i].AssignmentID && attachment.OperatorID == views[i].OperatorID {
			views[i].Attachments = append(views[i].Attachments, attachmentView(attachment))
		}
	}
	return nil
}

func (s *Service) ListSubmissions(ctx context.Context, actor Actor, query ListQuery, accountTypes ...string) (*Page[SubmissionView], error) {
	var err error
	s, err = s.withAccountType(query.AccountType, accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	query = normalizePage(query)
	q := s.submissionQuery(actor)
	if query.Search != "" {
		q = q.Where("LOWER(a.email) LIKE ?", "%"+strings.ToLower(query.Search)+"%")
	}
	if query.Status != "" {
		q = q.Where("u.status = ?", query.Status)
	}
	if query.OperatorID > 0 {
		q = q.Where("u.operator_id = ?", query.OperatorID)
	}
	page := &Page[SubmissionView]{Items: []SubmissionView{}, Page: query.Page, PageSize: query.PageSize}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	if err := q.Select(submissionViewSelect).Order("u.id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&page.Items).Error; err != nil {
		return nil, err
	}
	if err := s.submissionAttachments(page.Items); err != nil {
		return nil, err
	}
	page.HasMore = int64(query.Page)*int64(query.PageSize) < page.Total
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) Submit(ctx context.Context, actor Actor, assignmentID, version int64, attachmentIDs []string, accountTypes ...string) (*SubmissionView, error) {
	return s.SubmitWithRemark(ctx, actor, assignmentID, version, attachmentIDs, "", accountTypes...)
}

// SubmitWithRemark freezes the operator's note alongside the existing review
// submission. A note or at least one screenshot is required; neither is approval.
func (s *Service) SubmitWithRemark(ctx context.Context, actor Actor, assignmentID, version int64, attachmentIDs []string, remark string, accountTypes ...string) (*SubmissionView, error) {
	var err error
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	if actor.Admin != nil {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if !utf8.ValidString(remark) || strings.IndexFunc(remark, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return nil, fail(400, "mailbox_invalid_submission")
	}
	remark = strings.TrimSpace(remark)
	if assignmentID <= 0 || version <= 0 || (len(attachmentIDs) == 0 && remark == "") || len(attachmentIDs) > 5 || utf8.RuneCountInString(remark) > 2000 {
		return nil, fail(400, "mailbox_invalid_submission")
	}
	seen := make(map[string]bool, len(attachmentIDs))
	for _, id := range attachmentIDs {
		if !validAttachmentID(id) || seen[id] {
			return nil, fail(400, "mailbox_invalid_submission")
		}
		seen[id] = true
	}
	var view SubmissionView
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxView); err != nil {
			return err
		}
		assignment, account, err := s.workflowAssignment(actor, assignmentID)
		if err != nil {
			return err
		}
		if assignment.Version != version {
			return fail(409, "mailbox_version_conflict")
		}
		if assignment.Status != StatusPending && assignment.Status != StatusRejected {
			return fail(409, "mailbox_invalid_state")
		}
		now := s.Now().Unix()
		var attachments []model.MailboxAttachment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ? AND assignment_id = ? AND operator_id = ? AND submission_id = 0 AND issue_id = 0 AND deleted_at = 0 AND expires_at > ?", attachmentIDs, assignmentID, actor.OperatorID, now).Find(&attachments).Error; err != nil {
			return err
		}
		if len(attachments) != len(attachmentIDs) {
			return fail(400, "mailbox_invalid_attachments")
		}
		submission := model.MailboxSubmission{AssignmentID: assignmentID, OperatorID: actor.OperatorID, Status: StatusPending, Version: 1, CreatedAt: now, Remark: remark}
		if err := tx.Create(&submission).Error; err != nil {
			return err
		}
		result := tx.Model(&model.MailboxAttachment{}).Where("id IN ? AND assignment_id = ? AND operator_id = ? AND submission_id = 0 AND issue_id = 0 AND deleted_at = 0 AND expires_at > ?", attachmentIDs, assignmentID, actor.OperatorID, now).Updates(map[string]any{"submission_id": submission.ID, "expires_at": now + 365*24*60*60})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(attachmentIDs)) {
			return fail(409, "mailbox_version_conflict")
		}
		if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0 AND status = ?", assignmentID, version, assignment.Status).Updates(map[string]any{"status": StatusSubmitted, "version": gorm.Expr("version + 1"), "updated_at": now})); err != nil {
			return err
		}
		if err := s.submissionQuery(actor).Select(submissionViewSelect).Where("u.id = ?", submission.ID).Take(&view).Error; err != nil {
			return err
		}
		views := []SubmissionView{view}
		if err := s.submissionAttachments(views); err != nil {
			return err
		}
		view = views[0]
		return s.Audit(actor, "submit", account.ID, assignmentID, 200, "")
	})
	if err != nil {
		return nil, err
	}
	s.invalidateCVVAccount(view.AccountID)
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Service) Review(ctx context.Context, actor Actor, submissionID int64, input ReviewInput, accountTypes ...string) error {
	var err error
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return err
	}
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	if submissionID <= 0 || input.Version <= 0 || (input.Status != StatusApproved && input.Status != StatusRejected) || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 2000 || (input.Status == StatusRejected && strings.TrimSpace(input.Reason) == "") {
		return fail(400, "mailbox_invalid_review")
	}
	var accountID int64
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxReview); err != nil {
			return err
		}
		var hint model.MailboxSubmission
		if err := tx.First(&hint, submissionID).Error; err != nil {
			return workflowNotFound(err)
		}
		assignment, account, err := s.workflowAssignment(actor, hint.AssignmentID)
		if err != nil {
			return err
		}
		accountID = account.ID
		var submission model.MailboxSubmission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&submission, submissionID).Error; err != nil {
			return workflowNotFound(err)
		}
		if submission.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		if submission.AssignmentID != assignment.ID || submission.OperatorID != assignment.OperatorID || submission.Status != StatusPending || assignment.Status != StatusSubmitted {
			return fail(409, "mailbox_invalid_state")
		}
		var newest model.MailboxSubmission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("assignment_id = ?", assignment.ID).Order("id DESC").First(&newest).Error; err != nil {
			return err
		}
		if newest.ID != submission.ID {
			return fail(409, "mailbox_invalid_state")
		}
		now := s.Now().Unix()
		if err := workflowCAS(tx.Model(&model.MailboxSubmission{}).Where("id = ? AND version = ? AND status = ?", submissionID, input.Version, StatusPending).Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1"), "review_reason": input.Reason, "reviewed_by": actor.Admin.UserID, "reviewed_at": now})); err != nil {
			return err
		}
		if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0 AND status = ?", assignment.ID, assignment.Version, StatusSubmitted).Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1"), "updated_at": now})); err != nil {
			return err
		}
		return s.Audit(actor, "review", account.ID, assignment.ID, 200, "")
	})
	if err == nil {
		s.invalidateCVVAccount(accountID)
	}
	return err
}
