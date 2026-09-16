package controller

import (
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func ExportMailboxData(issues bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := mailboxService()
		s = s.WithDB(s.DB.WithContext(c.Request.Context()))
		actor := mailboxActor(c)
		var data []byte
		var counts map[string]int
		var err error
		scope, filename, action := "all", "mailbox-all.xlsx", "mailbox_export_all"
		if issues {
			var input mailbox.IssueExportInput
			if !mailboxDecode(c, &input) {
				return
			}
			scope, filename, action = input.Scope, "mailbox-issues.xlsx", "mailbox_export_issues"
			data, counts, err = s.ExportIssues(c.Request.Context(), actor, input)
		} else {
			var input mailbox.AccountExportInput
			if c.Request.ContentLength != 0 && !mailboxDecode(c, &input) {
				return
			}
			scope = input.Scope
			data, counts, err = s.ExportAllData(c.Request.Context(), actor, input)
		}
		if err == nil {
			err = s.CheckCompletedExport(actor)
		}
		if err != nil {
			mailboxFailure(c, err)
			return
		}
		recordManageAudit(c, action, map[string]interface{}{"counts": counts, "scope": scope, "result": "success"})
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
		c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	}
}
