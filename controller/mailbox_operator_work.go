package controller

import (
	"strconv"

	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func mailboxWorkPage(c *gin.Context, key string, fallback, maximum int) (int, bool) {
	value, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil || value < 1 || value > maximum || len(c.QueryArray(key)) > 1 {
		mailboxBadRequest(c, "mailbox_invalid_work_query")
		return 0, false
	}
	return value, true
}

func mailboxOperatorWorkQuery(c *gin.Context) (mailbox.OperatorWorkQuery, bool) {
	var query mailbox.OperatorWorkQuery
	page, ok := mailboxWorkPage(c, "page", 1, 100000)
	if !ok {
		return query, false
	}
	size, ok := mailboxWorkPage(c, "page_size", 20, 100)
	if !ok {
		return query, false
	}
	for _, key := range []string{"period", "start_date", "end_date", "scope", "account_type", "search", "include_stats"} {
		if len(c.QueryArray(key)) > 1 {
			mailboxBadRequest(c, "mailbox_invalid_work_query")
			return query, false
		}
	}
	query = mailbox.OperatorWorkQuery{
		Period: c.DefaultQuery("period", "all"), StartDate: c.Query("start_date"), EndDate: c.Query("end_date"),
		AccountType: c.DefaultQuery("account_type", "all"), Scope: c.DefaultQuery("scope", "submitted"),
		Search: c.Query("search"), Page: page, PageSize: size,
	}
	return query, true
}

func mailboxWorkSuccess(c *gin.Context, data any) {
	for _, permission := range []authz.Permission{authz.MailboxOperators, authz.MailboxView, authz.MailboxReview} {
		if err := mailboxService().CheckActor(mailboxActor(c), permission); err != nil {
			mailboxFailure(c, err)
			return
		}
	}
	mailboxSuccess(c, data)
}

func mailboxWorkOperatorID(c *gin.Context) int64 {
	id := mailboxID(c)
	if id != 0 {
		actor := mailboxActor(c)
		actor.TargetOperatorID = id
		c.Set("mailbox_actor", actor)
	}
	return id
}

func listMailboxOperatorsWithStats(c *gin.Context) {
	query, ok := mailboxOperatorWorkQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().ListOperatorsWithStats(c.Request.Context(), mailboxActor(c), mailbox.ListQuery{Page: query.Page, PageSize: query.PageSize, Search: query.Search}, query)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxWorkSuccess(c, data)
}

func GetMailboxOperatorWorkSummary(c *gin.Context) {
	id := mailboxWorkOperatorID(c)
	if id == 0 {
		return
	}
	query, ok := mailboxOperatorWorkQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().OperatorWorkSummary(c.Request.Context(), mailboxActor(c), id, query)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxWorkSuccess(c, data)
}

func ListMailboxOperatorWorkAccounts(c *gin.Context) {
	id := mailboxWorkOperatorID(c)
	if id == 0 {
		return
	}
	query, ok := mailboxOperatorWorkQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().OperatorWorkAccounts(c.Request.Context(), mailboxActor(c), id, query)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxWorkSuccess(c, data)
}

func GetMailboxOperatorAccountHistory(c *gin.Context) {
	id := mailboxWorkOperatorID(c)
	if id == 0 {
		return
	}
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || accountID < 1 {
		mailboxBadRequest(c, "mailbox_invalid_id")
		return
	}
	c.Set("mailbox_account_id", accountID)
	accountType := c.Query("account_type")
	if (accountType != "refund" && accountType != "opening") || len(c.QueryArray("account_type")) > 1 {
		mailboxBadRequest(c, "mailbox_invalid_work_query")
		return
	}
	c.Set("mailbox_account_type", accountType)
	assignments, ok := mailboxWorkPage(c, "assignment_page", 1, 100000)
	if !ok {
		return
	}
	submissions, ok := mailboxWorkPage(c, "submission_page", 1, 100000)
	if !ok {
		return
	}
	issues, ok := mailboxWorkPage(c, "issue_page", 1, 100000)
	if !ok {
		return
	}
	size, ok := mailboxWorkPage(c, "page_size", 20, 100)
	if !ok {
		return
	}
	query := mailbox.OperatorHistoryQuery{AccountType: accountType, AssignmentPage: assignments, SubmissionPage: submissions, IssuePage: issues, PageSize: size}
	data, err := mailboxService().OperatorAccountHistory(c.Request.Context(), mailboxActor(c), id, accountID, query)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxWorkSuccess(c, data)
}
