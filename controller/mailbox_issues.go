package controller

import (
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func GetMailboxImportOptions(c *gin.Context) {
	data, err := mailboxService().ImportOptions(mailboxActor(c))
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}

func ListMailboxIssues(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().ListIssues(c.Request.Context(), mailboxActor(c), q)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}

func GetMailboxIssue(c *gin.Context) {
	pool, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	data, err := mailboxService().GetIssue(c.Request.Context(), mailboxActor(c), id, pool)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}

func SubmitMailboxIssue(c *gin.Context) {
	pool, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_assignment_id", id)
	var input mailbox.IssueInput
	if !mailboxDecode(c, &input) {
		return
	}
	data, err := mailboxService().SubmitIssue(c.Request.Context(), mailboxActor(c), id, input, pool)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}

func ResolveMailboxIssue(c *gin.Context) {
	pool, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	var input mailbox.ResolveIssueInput
	if !mailboxDecode(c, &input) {
		return
	}
	err := mailboxService().ResolveIssue(c.Request.Context(), mailboxActor(c), id, input, pool)
	input.Credentials = nil
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{})
}
