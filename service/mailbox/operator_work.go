package mailbox

import (
	"context"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

type OperatorWorkQuery struct {
	Period, StartDate, EndDate, AccountType, Scope, Search string
	Page, PageSize                                         int
}

type OperatorHistoryQuery struct {
	AccountType                                         string
	AssignmentPage, SubmissionPage, IssuePage, PageSize int
}

type WorkRange struct {
	Period   string `json:"period"`
	StartAt  int64  `json:"start_at"`
	EndAt    int64  `json:"end_at"`
	Timezone string `json:"timezone"`
}

type WorkCurrent struct {
	Pending      int64 `json:"pending"`
	Submitted    int64 `json:"submitted"`
	Approved     int64 `json:"approved"`
	Rejected     int64 `json:"rejected"`
	IssuePending int64 `json:"issue_pending"`
}

type WorkSummary struct {
	SubmittedAccounts int64       `json:"submitted_accounts"`
	RefundSubmitted   int64       `json:"refund_submitted"`
	OpeningSubmitted  int64       `json:"opening_submitted"`
	SubmissionCount   int64       `json:"submission_count"`
	IssueAccounts     int64       `json:"issue_accounts"`
	Current           WorkCurrent `json:"current"`
}

type OperatorWorkOverview struct {
	Operator OperatorView `json:"operator"`
	Summary  WorkSummary  `json:"summary"`
	Range    WorkRange    `json:"range"`
}

type OperatorWorkAccount struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	AccountType       string `json:"account_type"`
	CardLast4         string `json:"card_last4"`
	ArchivedAt        int64  `json:"archived_at"`
	AssignmentID      int64  `json:"assignment_id"`
	AssignmentVersion int64  `json:"assignment_version"`
	Status            string `json:"status"`
	AssignmentActive  bool   `json:"assignment_active"`
	AssignedAt        int64  `json:"assigned_at"`
	RevokedAt         int64  `json:"revoked_at"`
	SubmissionCount   int64  `json:"submission_count"`
	LastSubmittedAt   int64  `json:"last_submitted_at"`
	LastIssueAt       int64  `json:"last_issue_at"`
}

type WorkAssignment struct {
	ID               int64  `json:"id"`
	AccountID        int64  `json:"account_id"`
	OperatorID       int64  `json:"operator_id"`
	Status           string `json:"status"`
	Version          int64  `json:"version"`
	AssignedBy       int    `json:"assigned_by"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	RevokedAt        int64  `json:"revoked_at"`
	AssignmentActive bool   `json:"assignment_active"`
}

type OperatorWorkHistory struct {
	Account     OperatorWorkAccount  `json:"account"`
	Assignments Page[WorkAssignment] `json:"assignments"`
	Submissions Page[SubmissionView] `json:"submissions"`
	Issues      Page[IssueView]      `json:"issues"`
}

func (s *Service) checkOperatorWorkAdmin(actor Actor) error {
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	for _, permission := range []authz.Permission{authz.MailboxOperators, authz.MailboxView, authz.MailboxReview} {
		if err := s.CheckActor(actor, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) normalizeWorkQuery(q OperatorWorkQuery) (OperatorWorkQuery, WorkRange, error) {
	r := WorkRange{Period: q.Period, Timezone: "Asia/Shanghai"}
	invalid := func() (OperatorWorkQuery, WorkRange, error) {
		return q, r, fail(400, "mailbox_invalid_work_query")
	}
	if q.Period == "" {
		q.Period, r.Period = "all", "all"
	}
	if q.AccountType == "" {
		q.AccountType = "all"
	}
	if q.AccountType != "all" && q.AccountType != AccountTypeRefund && q.AccountType != AccountTypeOpening {
		return invalid()
	}
	if q.Scope == "" {
		q.Scope = "submitted"
	}
	switch q.Scope {
	case "assigned", "submitted", "issues", "current_pending", "current_submitted", "current_approved", "current_rejected", "current_issue_pending":
	default:
		return invalid()
	}
	if q.Period != "custom" && (q.StartDate != "" || q.EndDate != "") {
		return invalid()
	}
	location, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return q, r, err
	}
	now := s.Now().In(location)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	switch q.Period {
	case "all":
	case "today", "last_7_days", "last_30_days":
		days := 1
		if q.Period == "last_7_days" {
			days = 7
		} else if q.Period == "last_30_days" {
			days = 30
		}
		r.StartAt, r.EndAt = midnight.AddDate(0, 0, 1-days).Unix(), midnight.AddDate(0, 0, 1).Unix()
	case "custom":
		start, startErr := time.ParseInLocation("2006-01-02", q.StartDate, location)
		end, endErr := time.ParseInLocation("2006-01-02", q.EndDate, location)
		if startErr != nil || endErr != nil || start.Format("2006-01-02") != q.StartDate || end.Format("2006-01-02") != q.EndDate || start.Year() < 1 || end.Year() < 1 || end.Before(start) {
			return invalid()
		}
		r.StartAt, r.EndAt = start.Unix(), end.AddDate(0, 0, 1).Unix()
	default:
		return invalid()
	}
	page := normalizePage(ListQuery{Page: q.Page, PageSize: q.PageSize})
	q.Page, q.PageSize, q.Search = page.Page, page.PageSize, strings.TrimSpace(q.Search)
	return q, r, nil
}

func workEventRange(q *gorm.DB, column string, r WorkRange) *gorm.DB {
	if r.Period != "all" {
		return q.Where(column+" >= ? AND "+column+" < ?", r.StartAt, r.EndAt)
	}
	return q
}

func workPool(q *gorm.DB, kind string) *gorm.DB {
	if kind != "all" {
		return q.Where("a.account_type = ?", kind)
	}
	return q
}

func (s *Service) workSubmissions(ids []int64, kind string, r WorkRange) *gorm.DB {
	q := s.DB.Table("mailbox_submissions AS u").
		Joins("JOIN mailbox_assignments AS st ON st.id = u.assignment_id AND st.operator_id = u.operator_id").
		Joins("JOIN mailbox_accounts AS a ON a.id = st.account_id").Where("u.operator_id IN ?", ids)
	return workEventRange(workPool(q, kind), "u.created_at", r)
}

func (s *Service) workIssues(ids []int64, kind string, r WorkRange) *gorm.DB {
	q := s.DB.Table("mailbox_issues AS i").
		Joins("JOIN mailbox_assignments AS it ON it.id = i.assignment_id AND it.account_id = i.account_id AND it.operator_id = i.operator_id").
		Joins("JOIN mailbox_accounts AS a ON a.id = i.account_id").Where("i.operator_id IN ?", ids)
	return workEventRange(workPool(q, kind), "i.created_at", r)
}

const workActive = "a.active_assignment_id = t.id AND t.revoked_at = 0 AND a.archived_at = 0"
const workPendingIssue = "EXISTS (SELECT 1 FROM mailbox_operators wo WHERE wo.id = t.operator_id AND wo.enabled = ?) AND EXISTS (SELECT 1 FROM mailbox_issues wi WHERE wi.assignment_id = t.id AND wi.operator_id = t.operator_id AND wi.account_id = t.account_id AND wi.status = 'pending')"

func (s *Service) workCurrent(ids []int64, kind string) *gorm.DB {
	return workPool(s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").
		Where("t.operator_id IN ?", ids).Where(workActive), kind)
}

// Aggregate each event stream independently: submissions and issues must never
// multiply one another when an account has several assignment generations.
func (s *Service) workSummaries(ids []int64, kind string, r WorkRange) (map[int64]WorkSummary, error) {
	result := make(map[int64]WorkSummary, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var submissions []struct {
		OperatorID                                                            int64
		SubmittedAccounts, RefundSubmitted, OpeningSubmitted, SubmissionCount int64
	}
	err := s.workSubmissions(ids, kind, r).Select("u.operator_id, COUNT(DISTINCT a.id) AS submitted_accounts, COUNT(DISTINCT CASE WHEN a.account_type = 'refund' THEN a.id END) AS refund_submitted, COUNT(DISTINCT CASE WHEN a.account_type = 'opening' THEN a.id END) AS opening_submitted, COUNT(*) AS submission_count").Group("u.operator_id").Scan(&submissions).Error
	if err != nil {
		return nil, err
	}
	for _, row := range submissions {
		result[row.OperatorID] = WorkSummary{SubmittedAccounts: row.SubmittedAccounts, RefundSubmitted: row.RefundSubmitted, OpeningSubmitted: row.OpeningSubmitted, SubmissionCount: row.SubmissionCount}
	}
	var issues []struct{ OperatorID, IssueAccounts int64 }
	if err := s.workIssues(ids, kind, r).Select("i.operator_id, COUNT(DISTINCT a.id) AS issue_accounts").Group("i.operator_id").Scan(&issues).Error; err != nil {
		return nil, err
	}
	for _, row := range issues {
		summary := result[row.OperatorID]
		summary.IssueAccounts = row.IssueAccounts
		result[row.OperatorID] = summary
	}
	var current []struct {
		OperatorID                                           int64
		Pending, Submitted, Approved, Rejected, IssuePending int64
	}
	selection := "t.operator_id, SUM(CASE WHEN t.status = 'pending' THEN 1 ELSE 0 END) AS pending, SUM(CASE WHEN t.status = 'submitted' THEN 1 ELSE 0 END) AS submitted, SUM(CASE WHEN t.status = 'approved' THEN 1 ELSE 0 END) AS approved, SUM(CASE WHEN t.status = 'rejected' THEN 1 ELSE 0 END) AS rejected, SUM(CASE WHEN t.status = 'issue_pending' AND " + workPendingIssue + " THEN 1 ELSE 0 END) AS issue_pending"
	if err := s.workCurrent(ids, kind).Select(selection, true).Group("t.operator_id").Scan(&current).Error; err != nil {
		return nil, err
	}
	for _, row := range current {
		summary := result[row.OperatorID]
		summary.Current = WorkCurrent{Pending: row.Pending, Submitted: row.Submitted, Approved: row.Approved, Rejected: row.Rejected, IssuePending: row.IssuePending}
		result[row.OperatorID] = summary
	}
	return result, nil
}

func (s *Service) ListOperatorsWithStats(ctx context.Context, actor Actor, query ListQuery, workQuery OperatorWorkQuery) (*Page[OperatorView], error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	workQuery, r, err := s.normalizeWorkQuery(workQuery)
	if err != nil {
		return nil, err
	}
	page, err := s.ListOperators(ctx, actor, query)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(page.Items))
	for i := range page.Items {
		ids[i] = page.Items[i].ID
	}
	summaries, err := s.workSummaries(ids, workQuery.AccountType, r)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		summary := summaries[page.Items[i].ID]
		page.Items[i].WorkSummary = &summary
	}
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) OperatorWorkSummary(ctx context.Context, actor Actor, id int64, query OperatorWorkQuery) (*OperatorWorkOverview, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	query, r, err := s.normalizeWorkQuery(query)
	if err != nil {
		return nil, err
	}
	operator, err := s.operatorByID(id)
	if err != nil {
		return nil, err
	}
	summaries, err := s.workSummaries([]int64{id}, query.AccountType, r)
	if err != nil {
		return nil, err
	}
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	return &OperatorWorkOverview{Operator: operatorView(*operator), Summary: summaries[id], Range: r}, nil
}

func (s *Service) workAccountQuery(id int64, kind string) *gorm.DB {
	return workPool(s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").
		Where("t.operator_id = ?", id).
		Where("NOT EXISTS (SELECT 1 FROM mailbox_assignments newer WHERE newer.operator_id = t.operator_id AND newer.account_id = t.account_id AND newer.id > t.id)"), kind)
}

// Correlated aggregates are lifetime values for this operator and account only.
// They run in SQL on the paginated account selection, not one query per item.
const workAccountSelect = "a.id, a.email, a.account_type, a.card_last4, a.archived_at, t.id AS assignment_id, t.version AS assignment_version, t.status, CASE WHEN " + workActive + " THEN true ELSE false END AS assignment_active, t.created_at AS assigned_at, t.revoked_at, " +
	"(SELECT COUNT(*) FROM mailbox_submissions ws JOIN mailbox_assignments wt ON wt.id = ws.assignment_id AND wt.operator_id = ws.operator_id WHERE wt.account_id = a.id AND ws.operator_id = t.operator_id) AS submission_count, " +
	"COALESCE((SELECT MAX(ws.created_at) FROM mailbox_submissions ws JOIN mailbox_assignments wt ON wt.id = ws.assignment_id AND wt.operator_id = ws.operator_id WHERE wt.account_id = a.id AND ws.operator_id = t.operator_id), 0) AS last_submitted_at, " +
	"COALESCE((SELECT MAX(wi.created_at) FROM mailbox_issues wi JOIN mailbox_assignments wt ON wt.id = wi.assignment_id AND wt.account_id = wi.account_id AND wt.operator_id = wi.operator_id WHERE wi.account_id = a.id AND wi.operator_id = t.operator_id), 0) AS last_issue_at"

func (s *Service) filterWorkAccounts(q *gorm.DB, id int64, query OperatorWorkQuery, r WorkRange) *gorm.DB {
	if query.Search != "" {
		q = q.Where("LOWER(a.email) LIKE ?", "%"+strings.ToLower(query.Search)+"%")
	}
	switch query.Scope {
	case "assigned":
		events := workEventRange(s.DB.Table("mailbox_assignments AS wa").Where("wa.operator_id = ?", id), "wa.created_at", r).Select("wa.account_id")
		return q.Where("a.id IN (?)", events)
	case "submitted":
		return q.Where("a.id IN (?)", s.workSubmissions([]int64{id}, query.AccountType, r).Select("a.id"))
	case "issues":
		return q.Where("a.id IN (?)", s.workIssues([]int64{id}, query.AccountType, r).Select("a.id"))
	default:
		q = q.Where(workActive).Where("t.status = ?", strings.TrimPrefix(query.Scope, "current_"))
		if query.Scope == "current_issue_pending" {
			q = q.Where(workPendingIssue, true)
		}
		return q
	}
}

func workPage[T any](q *gorm.DB, selection, order string, page, size int) (*Page[T], error) {
	p := normalizePage(ListQuery{Page: page, PageSize: size})
	result := &Page[T]{Items: []T{}, Page: p.Page, PageSize: p.PageSize}
	if err := q.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := q.Select(selection).Order(order).Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	result.HasMore = int64(p.Page)*int64(p.PageSize) < result.Total
	return result, nil
}

func (s *Service) OperatorWorkAccounts(ctx context.Context, actor Actor, id int64, query OperatorWorkQuery) (*Page[OperatorWorkAccount], error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	query, r, err := s.normalizeWorkQuery(query)
	if err != nil {
		return nil, err
	}
	if _, err := s.operatorByID(id); err != nil {
		return nil, err
	}
	q := s.filterWorkAccounts(s.workAccountQuery(id, query.AccountType), id, query, r)
	page, err := workPage[OperatorWorkAccount](q, workAccountSelect, "t.id DESC", query.Page, query.PageSize)
	if err != nil {
		return nil, err
	}
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) OperatorAccountHistory(ctx context.Context, actor Actor, id, accountID int64, query OperatorHistoryQuery) (*OperatorWorkHistory, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	if query.AccountType != AccountTypeRefund && query.AccountType != AccountTypeOpening {
		return nil, fail(400, "mailbox_invalid_work_query")
	}
	if _, err := s.operatorByID(id); err != nil {
		return nil, err
	}
	s, _ = s.withAccountType(query.AccountType)
	result := &OperatorWorkHistory{}
	if err := s.workAccountQuery(id, query.AccountType).Where("a.id = ?", accountID).Select(workAccountSelect).Take(&result.Account).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	assignments := s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Where("t.operator_id = ? AND t.account_id = ? AND a.account_type = ?", id, accountID, query.AccountType)
	ap, err := workPage[WorkAssignment](assignments, "t.id, t.account_id, t.operator_id, t.status, t.version, t.assigned_by, t.created_at, t.updated_at, t.revoked_at, CASE WHEN "+workActive+" THEN true ELSE false END AS assignment_active", "t.id DESC", query.AssignmentPage, query.PageSize)
	if err != nil {
		return nil, err
	}
	sp, err := workPage[SubmissionView](s.submissionQuery(actor).Where("u.operator_id = ? AND t.account_id = ?", id, accountID), submissionViewSelect, "u.id DESC", query.SubmissionPage, query.PageSize)
	if err != nil {
		return nil, err
	}
	if err := s.submissionAttachments(sp.Items); err != nil {
		return nil, err
	}
	ip, err := workPage[IssueView](s.issueQuery(actor).Where("i.operator_id = ? AND i.account_id = ?", id, accountID), issueViewSelect, "i.id DESC", query.IssuePage, query.PageSize)
	if err != nil {
		return nil, err
	}
	if err := s.issueAttachments(ip.Items); err != nil {
		return nil, err
	}
	// Archived history is readable, but never actionable, even for stale issues.
	if result.Account.ArchivedAt != 0 {
		for i := range ip.Items {
			ip.Items[i].AssignmentActive = false
		}
	}
	result.Assignments, result.Submissions, result.Issues = *ap, *sp, *ip
	if err := s.checkOperatorWorkAdmin(actor); err != nil {
		return nil, err
	}
	return result, nil
}
