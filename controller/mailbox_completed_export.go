package controller

import (
	"github.com/gin-gonic/gin"
)

func ExportCompletedMailboxAccounts(c *gin.Context) {
	c.Set("mailbox_account_type", "opening")
	s := mailboxService()
	s = s.WithDB(s.DB.WithContext(c.Request.Context()))
	actor := mailboxActor(c)
	data, count, err := s.ExportCompletedOpening(c.Request.Context(), actor)
	if err == nil {
		err = s.CheckCompletedExport(actor)
	}
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	recordManageAudit(c, "mailbox_export_completed", map[string]interface{}{"count": count, "result": "success", "account_type": "opening"})
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="opening-completed.xlsx"`)
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}
