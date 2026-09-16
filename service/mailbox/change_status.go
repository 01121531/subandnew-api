package mailbox

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChangeStatusItem struct {
	ID                int64 `json:"id"`
	Version           int64 `json:"version"`
	AssignmentID      int64 `json:"assignment_id"`
	AssignmentVersion int64 `json:"assignment_version"`
}

type ChangeStatusInput struct {
	AccountType string             `json:"account_type"`
	Items       []ChangeStatusItem `json:"items"`
	Status      string             `json:"status"`
	Reason      string             `json:"reason"`
}

func validStatusChange(from, to string) bool {
	switch from {
	case StatusPending:
		return to == StatusRejected
	case StatusRejected:
		return to == StatusPending
	case StatusSubmitted:
		return to == StatusApproved || to == StatusRejected
	case StatusApproved:
		return to == StatusPending || to == StatusRejected
	}
	return false
}

// Both review entry points use the same submission/assignment CAS updates.
func (s *Service) applyReview(actor Actor, assignment *model.MailboxAssignment, submission *model.MailboxSubmission, input ReviewInput) error {
	now := s.Now().Unix()
	if err := workflowCAS(s.DB.Model(&model.MailboxSubmission{}).Where("id = ? AND version = ? AND status = ?", submission.ID, input.Version, StatusPending).Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1"), "review_reason": input.Reason, "reviewed_by": actor.Admin.UserID, "reviewed_at": now})); err != nil {
		return err
	}
	return workflowCAS(s.DB.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0 AND status = ?", assignment.ID, assignment.Version, StatusSubmitted).Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1"), "updated_at": now}))
}

func (s *Service) ChangeStatus(ctx context.Context, actor Actor, input ChangeStatusInput) error {
	var err error
	s, err = s.withAccountType(input.AccountType)
	if err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return err
	}
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Items) == 0 || len(input.Items) > 1000 || input.Reason == "" || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 2000 || strings.IndexFunc(input.Reason, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return fail(400, "mailbox_invalid_review")
	}
	items := append([]ChangeStatusItem(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if item.ID <= 0 || item.Version <= 0 || item.AssignmentID <= 0 || item.AssignmentVersion <= 0 || (i > 0 && items[i-1].ID == item.ID) {
			return fail(400, "mailbox_invalid_assignment")
		}
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxReview); err != nil {
			return err
		}
		if err := s.CheckActor(actor, authz.MailboxView); err != nil {
			return err
		}
		for _, item := range items {
			assignment, account, err := s.workflowAssignment(actor, item.AssignmentID)
			if err != nil {
				return err
			}
			if account.ID != item.ID || account.Version != item.Version || assignment.Version != item.AssignmentVersion {
				return fail(409, "mailbox_version_conflict")
			}
			if !validStatusChange(assignment.Status, input.Status) {
				return fail(409, "mailbox_invalid_state")
			}
			if assignment.Status == StatusSubmitted {
				var latest model.MailboxSubmission
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("assignment_id = ?", assignment.ID).Order("id DESC").First(&latest).Error; err != nil {
					return workflowNotFound(err)
				}
				if latest.Status != StatusPending || latest.OperatorID != assignment.OperatorID {
					return fail(409, "mailbox_invalid_state")
				}
				if err := s.applyReview(actor, assignment, &latest, ReviewInput{Version: latest.Version, Status: input.Status, Reason: input.Reason}); err != nil {
					return err
				}
			} else {
				if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0 AND status = ?", assignment.ID, assignment.Version, assignment.Status).Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1"), "updated_at": s.Now().Unix()})); err != nil {
					return err
				}
			}
			if err := tx.Create(&model.MailboxAudit{AccountType: s.pool(), AdminID: actor.Admin.UserID, AccountID: account.ID, AssignmentID: assignment.ID, Action: "change_status", FromStatus: assignment.Status, ToStatus: input.Status, Reason: input.Reason, StatusCode: 200, IPAddress: actor.IP, CreatedAt: s.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		for _, item := range items {
			s.invalidateCVVAccount(item.ID)
		}
	}
	return err
}
