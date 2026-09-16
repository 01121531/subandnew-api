package mailbox

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IssueInput struct {
	Version       int64    `json:"version"`
	Kind          string   `json:"kind"`
	Description   string   `json:"description"`
	AttachmentIDs []string `json:"attachment_ids"`
}

func (s *Service) ImportOptions(actor Actor) (map[string]any, error) {
	if actor.Admin == nil {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if err := s.CheckActor(actor, authz.MailboxManage); err != nil {
		return nil, err
	}
	reason := temporaryCVVUnavailableReason()
	if !actor.Admin.Can(authz.MailboxCredentials) {
		reason = "permission_denied"
	}
	return map[string]any{"temporary_cvv_enabled": reason == "", "temporary_cvv_unavailable_reason": reason}, nil
}

type IssueCredentials struct {
	Password   *string `json:"password"`
	OTP        *string `json:"otp"`
	CardNumber *string `json:"card_number"`
	CardExpiry *string `json:"card_expiry"`
}

type ResolveIssueInput struct {
	Version           int64             `json:"version"`
	AccountVersion    int64             `json:"account_version"`
	AssignmentVersion int64             `json:"assignment_version"`
	Resolution        string            `json:"resolution"`
	Reply             string            `json:"reply"`
	Credentials       *IssueCredentials `json:"credentials"`
}

type IssueView struct {
	SubmittedVersion  int64            `json:"submitted_version"`
	ID                int64            `json:"id"`
	AccountID         int64            `json:"account_id"`
	AccountType       string           `json:"account_type"`
	Email             string           `json:"email"`
	CardLast4         string           `json:"card_last4"`
	AccountVersion    int64            `json:"account_version"`
	AssignmentID      int64            `json:"assignment_id"`
	AssignmentVersion int64            `json:"assignment_version"`
	AssignmentActive  bool             `json:"assignment_active"`
	OperatorID        int64            `json:"operator_id"`
	OperatorName      string           `json:"operator_name"`
	Kind              string           `json:"kind"`
	Description       string           `json:"description"`
	Status            string           `json:"status"`
	Version           int64            `json:"version"`
	Resolution        string           `json:"resolution"`
	Reply             string           `json:"reply"`
	ResolvedAt        int64            `json:"resolved_at"`
	CreatedAt         int64            `json:"created_at"`
	Attachments       []AttachmentView `json:"attachments" gorm:"-"`
}

func validIssueKind(kind, pool string) bool {
	return kind == "email_login" || kind == "otp" || kind == "other" || kind == "card" && pool == AccountTypeOpening
}

func validIssueText(value string) bool {
	return validMailboxText(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 2000
}

func (s *Service) issueQuery(actor Actor) *gorm.DB {
	return s.issueBaseQuery(actor).Where("a.account_type = ?", s.pool())
}

func (s *Service) issueBaseQuery(actor Actor) *gorm.DB {
	q := s.DB.Table("mailbox_issues AS i").
		Joins("JOIN mailbox_accounts AS a ON a.id = i.account_id").
		Joins("JOIN mailbox_assignments AS t ON t.id = i.assignment_id AND t.account_id = i.account_id AND t.operator_id = i.operator_id").
		Joins("JOIN mailbox_operators AS o ON o.id = i.operator_id")
	if actor.Admin == nil {
		q = q.Where("i.operator_id = ?", actor.OperatorID)
	}
	return q
}

func (s *Service) filteredIssueQuery(actor Actor, query ListQuery) (*gorm.DB, error) {
	if query.Kind != "" && !validIssueKind(query.Kind, s.pool()) || query.Status != "" && query.Status != "pending" && query.Status != "processed" && query.Status != "resolved" && query.Status != "invalidated" || query.OperatorID < 0 {
		return nil, fail(400, "mailbox_invalid_query")
	}
	q := s.issueQuery(actor)
	if query.Search != "" {
		q = q.Where("LOWER(a.email) LIKE ?", "%"+strings.ToLower(query.Search)+"%")
	}
	if query.Kind != "" {
		q = q.Where("i.kind = ?", query.Kind)
	}
	if query.Status == "processed" {
		q = q.Where("i.status <> 'pending'")
	} else if query.Status != "" {
		q = q.Where("i.status = ?", query.Status)
	}
	if query.OperatorID > 0 {
		q = q.Where("i.operator_id = ?", query.OperatorID)
	}
	if query.AssignmentID > 0 {
		q = q.Where("i.assignment_id = ?", query.AssignmentID)
	}
	return q, nil
}

const issueViewSelect = "i.submitted_version, i.id, i.account_id, a.account_type, a.email, a.card_last4, a.version AS account_version, i.assignment_id, t.version AS assignment_version, CASE WHEN a.active_assignment_id = t.id AND t.revoked_at = 0 AND o.enabled = true AND i.status = 'pending' AND t.status = 'issue_pending' THEN true ELSE false END AS assignment_active, i.operator_id, o.display_name AS operator_name, i.kind, i.description, i.status, i.version, i.resolution, i.reply, i.resolved_at, i.created_at"

func (s *Service) issueAttachments(views []IssueView) error {
	if len(views) == 0 {
		return nil
	}
	ids := make([]int64, len(views))
	indexes := map[int64]int{}
	for i := range views {
		ids[i] = views[i].ID
		indexes[views[i].ID] = i
		views[i].Attachments = []AttachmentView{}
	}
	var files []model.MailboxAttachment
	if err := s.DB.Where("issue_id IN ? AND submission_id = 0", ids).Order("created_at, id").Find(&files).Error; err != nil {
		return err
	}
	for _, file := range files {
		i := indexes[file.IssueID]
		if file.AssignmentID == views[i].AssignmentID && file.OperatorID == views[i].OperatorID {
			views[i].Attachments = append(views[i].Attachments, attachmentView(file))
		}
	}
	return nil
}

func (s *Service) ListIssues(ctx context.Context, actor Actor, query ListQuery) (*Page[IssueView], error) {
	var err error
	s, err = s.withAccountType(query.AccountType)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	query = normalizePage(query)
	q, err := s.filteredIssueQuery(actor, query)
	if err != nil {
		return nil, err
	}
	page := &Page[IssueView]{Items: []IssueView{}, Page: query.Page, PageSize: query.PageSize}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	if err := q.Select(issueViewSelect).Order("i.id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&page.Items).Error; err != nil {
		return nil, err
	}
	if err := s.issueAttachments(page.Items); err != nil {
		return nil, err
	}
	page.HasMore = int64(query.Page)*int64(query.PageSize) < page.Total
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) GetIssue(ctx context.Context, actor Actor, id int64, accountType string) (*IssueView, error) {
	var err error
	s, err = s.withAccountType(accountType)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	var view IssueView
	if err := s.issueQuery(actor).Select(issueViewSelect).Where("i.id = ?", id).Take(&view).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	views := []IssueView{view}
	if err := s.issueAttachments(views); err != nil {
		return nil, err
	}
	if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (s *Service) SubmitIssue(ctx context.Context, actor Actor, assignmentID int64, input IssueInput, accountType string) (*IssueView, error) {
	var err error
	s, err = s.withAccountType(accountType)
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
	if assignmentID <= 0 || input.Version <= 0 || !validIssueKind(input.Kind, s.pool()) || !validIssueText(input.Description) || len(input.AttachmentIDs) > 5 {
		return nil, fail(400, "mailbox_invalid_issue")
	}
	seen := map[string]bool{}
	for _, id := range input.AttachmentIDs {
		if !validAttachmentID(id) || seen[id] {
			return nil, fail(400, "mailbox_invalid_attachments")
		}
		seen[id] = true
	}
	var issue model.MailboxIssue
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxView); err != nil {
			return err
		}
		assignment, account, err := s.workflowAssignment(actor, assignmentID)
		if err != nil {
			return err
		}
		if assignment.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		if assignment.Status != StatusPending && assignment.Status != StatusRejected {
			return fail(409, "mailbox_invalid_state")
		}
		now := s.Now().Unix()
		var files []model.MailboxAttachment
		if len(input.AttachmentIDs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ? AND assignment_id = ? AND operator_id = ? AND submission_id = 0 AND issue_id = 0 AND deleted_at = 0 AND expires_at > ?", input.AttachmentIDs, assignmentID, actor.OperatorID, now).Find(&files).Error; err != nil {
				return err
			}
			if len(files) != len(input.AttachmentIDs) {
				return fail(400, "mailbox_invalid_attachments")
			}
		}
		issue = model.MailboxIssue{AccountID: account.ID, AssignmentID: assignmentID, OperatorID: actor.OperatorID, OpenAssignmentID: &assignmentID, SubmittedVersion: input.Version, Kind: input.Kind, Description: strings.TrimSpace(input.Description), Status: "pending", Version: 1, CreatedAt: now}
		if err := tx.Create(&issue).Error; err != nil {
			return err
		}
		if len(files) > 0 {
			r := tx.Model(&model.MailboxAttachment{}).Where("id IN ? AND submission_id = 0 AND issue_id = 0", input.AttachmentIDs).Updates(map[string]any{"issue_id": issue.ID, "expires_at": now + 365*24*60*60})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != int64(len(files)) {
				return fail(409, "mailbox_version_conflict")
			}
		}
		if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0", assignmentID, input.Version).Updates(map[string]any{"status": StatusIssuePending, "version": gorm.Expr("version + 1"), "updated_at": now})); err != nil {
			return err
		}
		return s.Audit(actor, "issue_submit", account.ID, assignmentID, 200, "")
	})
	if err != nil {
		return nil, err
	}
	s.invalidateCVVAccount(issue.AccountID)
	return s.GetIssue(ctx, actor, issue.ID, accountType)
}

// Invalidated feedback remains readable by its original author, never restorable.
func (s *Service) invalidateIssues(where string, value int64) error {
	return s.DB.Model(&model.MailboxIssue{}).Where(where, value).Where("status = 'pending'").Updates(map[string]any{"status": "invalidated", "open_assignment_id": nil, "version": gorm.Expr("version + 1"), "resolved_at": s.Now().Unix()}).Error
}

func (s *Service) replaceIssueCredentials(actor Actor, account *model.MailboxAccount, input *IssueCredentials) error {
	if input == nil {
		return nil
	}
	if err := s.CheckActor(actor, authz.MailboxManage); err != nil {
		return err
	}
	if err := s.CheckActor(actor, authz.MailboxCredentials); err != nil {
		return err
	}
	cipher, err := s.Cipher()
	if err != nil {
		return fail(503, "mailbox_credentials_unavailable")
	}
	updates := map[string]any{}
	if input.Password != nil || input.OTP != nil {
		payload, err := cipher.Decrypt(account.ID, mailboxCredentialKind, account.KeyVersion, account.Ciphertext)
		if err != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		var secret mailboxCredentialSecret
		if json.Unmarshal([]byte(payload.Secret), &secret) != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		if input.Password != nil {
			if *input.Password == "" || !validMailboxText(*input.Password) || len(*input.Password) > 4096 {
				return fail(400, "mailbox_import_invalid_password")
			}
			secret.Password = *input.Password
		}
		if input.OTP != nil {
			if mailboxOTPAbsent(*input.OTP) {
				secret.OTP = mailboxOTPConfig{}
				secret.OTPAbsent = true
			} else {
				secret.OTP, err = parseMailboxOTP(*input.OTP)
				if err != nil {
					return err
				}
				secret.OTPAbsent = false
			}
		}
		encoded, err := json.Marshal(secret)
		if err != nil {
			return err
		}
		ciphertext, version, _, err := cipher.Encrypt(account.ID, mailboxCredentialKind, managedinstance.CredentialPayload{Secret: string(encoded)})
		if err != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		updates["ciphertext"], updates["key_version"] = ciphertext, version
	}
	if input.CardNumber != nil || input.CardExpiry != nil {
		if account.AccountType != AccountTypeOpening || input.CardNumber == nil || input.CardExpiry == nil {
			return fail(400, "mailbox_invalid_issue_credentials")
		}
		number, err := normalizeMailboxPAN(*input.CardNumber)
		if err != nil {
			return err
		}
		expiry, err := normalizeMailboxExpiry(*input.CardExpiry, s.Now())
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(mailboxCardSecret{Number: number, Expiry: expiry})
		if err != nil {
			return err
		}
		ciphertext, version, _, err := cipher.Encrypt(account.ID, mailboxCardCredentialKind, managedinstance.CredentialPayload{Secret: string(encoded)})
		if err != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		updates["card_ciphertext"], updates["card_key_version"], updates["card_last4"] = ciphertext, version, number[len(number)-4:]
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.DB.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", account.ID, account.Version+1).Updates(updates).Error; err != nil {
		return err
	}
	return s.Audit(actor, "issue_credentials_replace", account.ID, account.ActiveAssignmentID, 200, "")
}

func (s *Service) ResolveIssue(ctx context.Context, actor Actor, id int64, input ResolveIssueInput, accountType string) error {
	var err error
	s, err = s.withAccountType(accountType)
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
	if id <= 0 || input.Version <= 0 || input.AccountVersion <= 0 || input.AssignmentVersion <= 0 || !validIssueText(input.Reply) || (input.Resolution != "resume" && input.Resolution != "recall") {
		return fail(400, "mailbox_invalid_issue")
	}
	var accountID int64
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxReview); err != nil {
			return err
		}
		if input.Resolution == "recall" {
			if err := s.CheckActor(actor, authz.MailboxAssign); err != nil {
				return err
			}
		}
		var hint model.MailboxIssue
		if err := tx.First(&hint, id).Error; err != nil {
			return workflowNotFound(err)
		}
		// Match assignment mutation lock order: operator, account, assignment, issue.
		var operator model.MailboxOperator
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operator, hint.OperatorID).Error; err != nil {
			return workflowNotFound(err)
		}
		if !operator.Enabled {
			return fail(409, "mailbox_issue_inactive")
		}
		assignment, account, err := s.workflowAssignment(actor, hint.AssignmentID)
		if err != nil {
			return err
		}
		accountID = account.ID
		var issue model.MailboxIssue
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&issue, id).Error; err != nil {
			return workflowNotFound(err)
		}
		if issue.Version != input.Version || account.Version != input.AccountVersion || assignment.Version != input.AssignmentVersion {
			return fail(409, "mailbox_version_conflict")
		}
		if issue.Status != "pending" || issue.AccountID != account.ID || issue.OperatorID != assignment.OperatorID || assignment.Status != StatusIssuePending {
			return fail(409, "mailbox_issue_inactive")
		}
		if err := s.replaceIssueCredentials(actor, account, input.Credentials); err != nil {
			return err
		}
		now := s.Now().Unix()
		updates := map[string]any{"status": StatusPending, "version": gorm.Expr("version + 1"), "updated_at": now}
		if input.Resolution == "recall" {
			updates["revoked_at"] = now
			if err := tx.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("active_assignment_id", 0).Error; err != nil {
				return err
			}
		}
		if err := workflowCAS(tx.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0", assignment.ID, assignment.Version).Updates(updates)); err != nil {
			return err
		}
		if err := workflowCAS(tx.Model(&model.MailboxIssue{}).Where("id = ? AND version = ? AND status = 'pending'", id, input.Version).Updates(map[string]any{"status": "resolved", "open_assignment_id": nil, "version": gorm.Expr("version + 1"), "resolution": input.Resolution, "reply": strings.TrimSpace(input.Reply), "resolved_by": actor.Admin.UserID, "resolved_at": now})); err != nil {
			return err
		}
		return s.Audit(actor, "issue_"+input.Resolution, account.ID, assignment.ID, 200, "")
	})
	if err == nil {
		s.invalidateCVVAccount(accountID)
	}
	return err
}
