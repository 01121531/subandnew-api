package controller

import (
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func ListMailboxAssignmentRepairs(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	result, err := mailboxService().ListAssignmentRepairs(c.Request.Context(), mailboxActor(c), q)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, result)
}

func RepairMailboxAssignments(c *gin.Context) {
	var input mailbox.RepairInput
	if !mailboxDecode(c, &input) {
		return
	}
	if err := mailboxService().RepairAssignments(c.Request.Context(), mailboxActor(c), input); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{"updated": len(input.Items)})
}
