package mailbox

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RemarkInput struct {
	AccountType string `json:"account_type"`
	Version     int64  `json:"version"`
	Remark      string `json:"remark"`
}

func (s *Service) remarkActor(actor Actor) error {
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return err
	}
	if actor.Admin != nil {
		return s.CheckActor(actor, authz.MailboxReview)
	}
	return nil
}

func (s *Service) EditRemark(ctx context.Context, actor Actor, id int64, input RemarkInput) (*SubmissionView, error) {
	var err error
	s, err = s.withAccountType(input.AccountType)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.remarkActor(actor); err != nil {
		return nil, err
	}
	if id <= 0 || input.Version <= 0 || !utf8.ValidString(input.Remark) || utf8.RuneCountInString(input.Remark) > 2000 || strings.IndexFunc(input.Remark, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return nil, fail(400, "mailbox_invalid_submission")
	}
	input.Remark = strings.TrimSpace(input.Remark)
	var result SubmissionView
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxView); err != nil {
			return err
		}
		if err := s.remarkActor(actor); err != nil {
			return err
		}
		var hint SubmissionView
		if err := s.submissionQuery(actor).Select(submissionViewSelect).Where("u.id = ?", id).Take(&hint).Error; err != nil {
			return workflowNotFound(err)
		}
		// Historical admin edits may target archived accounts or revoked assignments.
		var account model.MailboxAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_type = ?", hint.AccountID, s.pool()).First(&account).Error; err != nil {
			return workflowNotFound(err)
		}
		if err := workflowCAS(tx.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", account.ID, account.Version).Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": s.Now().Unix()})); err != nil {
			return err
		}
		var assignment model.MailboxAssignment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&assignment, hint.AssignmentID).Error; err != nil {
			return workflowNotFound(err)
		}
		var submission model.MailboxSubmission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&submission, id).Error; err != nil {
			return workflowNotFound(err)
		}
		if submission.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		if assignment.AccountID != account.ID || submission.AssignmentID != assignment.ID || submission.OperatorID != assignment.OperatorID {
			return fail(409, "mailbox_invalid_state")
		}
		if actor.Admin == nil {
			var newest model.MailboxSubmission
			if err := tx.Where("assignment_id = ?", assignment.ID).Order("id DESC").First(&newest).Error; err != nil {
				return err
			}
			if account.ArchivedAt != 0 || account.ActiveAssignmentID != assignment.ID || assignment.RevokedAt != 0 || assignment.OperatorID != actor.OperatorID || newest.ID != id || !((assignment.Status == StatusSubmitted && submission.Status == StatusPending) || (assignment.Status == StatusRejected && submission.Status == StatusRejected)) {
				return fail(403, "mailbox_permission_denied")
			}
		}
		if input.Remark == "" {
			var count int64
			if err := tx.Model(&model.MailboxAttachment{}).Where("submission_id = ? AND assignment_id = ? AND operator_id = ?", id, assignment.ID, submission.OperatorID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fail(400, "mailbox_invalid_submission")
			}
		}
		revision := model.MailboxRemarkRevision{SubmissionID: id, OperatorID: actor.OperatorID, OldRemark: submission.Remark, NewRemark: input.Remark, Version: submission.Version + 1, CreatedAt: s.Now().Unix()}
		if actor.Admin != nil {
			revision.AdminID = actor.Admin.UserID
		}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		if err := workflowCAS(tx.Model(&model.MailboxSubmission{}).Where("id = ? AND version = ?", id, input.Version).Updates(map[string]any{"remark": input.Remark, "version": gorm.Expr("version + 1")})); err != nil {
			return err
		}
		if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ?", assignment.ID, assignment.Version).Update("version", gorm.Expr("version + 1"))); err != nil {
			return err
		}
		if err := s.submissionQuery(actor).Select(submissionViewSelect).Where("u.id = ?", id).Take(&result).Error; err != nil {
			return err
		}
		views := []SubmissionView{result}
		if err := s.submissionAttachments(views); err != nil {
			return err
		}
		result = views[0]
		return s.Audit(actor, "edit_remark", account.ID, assignment.ID, 200, "")
	})
	if err != nil {
		return nil, err
	}
	if err := s.remarkActor(actor); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) RemarkHistory(ctx context.Context, actor Actor, id int64, query ListQuery) (*Page[model.MailboxRemarkRevision], error) {
	var err error
	s, err = s.withAccountType(query.AccountType)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.remarkActor(actor); err != nil {
		return nil, err
	}
	var view SubmissionView
	if err := s.submissionQuery(actor).Select(submissionViewSelect).Where("u.id = ?", id).Take(&view).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	query = normalizePage(query)
	result := &Page[model.MailboxRemarkRevision]{Items: []model.MailboxRemarkRevision{}, Page: query.Page, PageSize: query.PageSize}
	q := s.DB.Model(&model.MailboxRemarkRevision{}).Where("submission_id = ?", id)
	if err := q.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := q.Order("id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	result.HasMore = int64(query.Page*query.PageSize) < result.Total
	if err := s.remarkActor(actor); err != nil {
		return nil, err
	}
	return result, nil
}
