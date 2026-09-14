package controller

import (
	"net/http"

	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func mailboxSessionResponse(c *gin.Context, p *mailbox.Principal, raw string) {
	if p == nil {
		c.JSON(200, gin.H{"success": true, "data": gin.H{"authenticated": false}})
		return
	}
	actor := mailbox.PortalActor(p, c.ClientIP())
	if err := mailboxService().CheckActor(actor, authz.MailboxView); err != nil {
		mailboxFailure(c, err)
		return
	}
	c.Set("mailbox_actor", actor)
	c.JSON(200, gin.H{"success": true, "data": gin.H{"authenticated": true, "operator": gin.H{"id": p.Operator.ID, "username": p.Operator.Username, "display_name": p.Operator.DisplayName}, "csrf_token": mailbox.CSRF(raw), "expires_at": p.Session.ExpiresAt}})
}
func LoginMailboxPortal(c *gin.Context) {
	if !mailboxSameOrigin(c) {
		mailboxFailure(c, &mailbox.Error{Status: 403, Code: "mailbox_csrf_invalid"})
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !mailboxDecode(c, &input) {
		return
	}
	p, raw, err := mailboxService().Login(c.Request.Context(), input.Username, input.Password, c.ClientIP())
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	if old, _ := c.Cookie(mailboxCookieName); old != "" {
		if principal, e := mailboxService().Authenticate(old); e == nil {
			_ = mailboxService().Logout(c.Request.Context(), mailbox.PortalActor(principal, c.ClientIP()))
		}
	}
	mailboxCookie(c, raw, int(mailbox.SessionTTL.Seconds()))
	mailboxSessionResponse(c, p, raw)
}
func GetMailboxPortalSession(c *gin.Context) {
	raw, _ := c.Cookie(mailboxCookieName)
	p, err := mailboxService().Authenticate(raw)
	if err != nil {
		status, _ := mailbox.HTTPError(err)
		if status != 401 {
			mailboxFailure(c, err)
			return
		}
		if raw != "" {
			mailboxCookie(c, "", -1)
		}
		mailboxSessionResponse(c, nil, "")
		return
	}
	mailboxSessionResponse(c, p, raw)
}
func LogoutMailboxPortal(c *gin.Context) {
	if err := mailboxService().Logout(c.Request.Context(), mailboxActor(c)); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxCookie(c, "", -1)
	c.JSON(200, gin.H{"success": true, "data": gin.H{"authenticated": false}})
}
func ChangeMailboxPortalPassword(c *gin.Context) {
	var input struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !mailboxDecode(c, &input) {
		return
	}
	if err := mailboxService().ChangePassword(c.Request.Context(), mailboxActor(c), input.Current, input.Password); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxCookie(c, "", -1)
	c.JSON(200, gin.H{"success": true, "data": gin.H{"authenticated": false}})
}
func UploadMailboxAttachment(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_assignment_id", id)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 11<<20)
	if err := c.Request.ParseMultipartForm(11 << 20); err != nil {
		mailboxBadRequest(c, "mailbox_invalid_image")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 {
		mailboxBadRequest(c, "mailbox_invalid_image")
		return
	}
	file, err := files[0].Open()
	if err != nil {
		mailboxBadRequest(c, "mailbox_invalid_image")
		return
	}
	defer file.Close()
	data, err := mailboxService().UploadAttachment(c.Request.Context(), mailboxActor(c), id, file)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func SubmitMailboxScreenshots(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_assignment_id", id)
	var input struct {
		Version       int64    `json:"version"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if !mailboxDecode(c, &input) {
		return
	}
	data, err := mailboxService().Submit(c.Request.Context(), mailboxActor(c), id, input.Version, input.AttachmentIDs)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func ReadMailboxAttachment(c *gin.Context) {
	data, contentType, err := mailboxService().ReadAttachment(c.Request.Context(), mailboxActor(c), c.Param("id"))
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	if !mailboxCurrent(c) {
		return
	}
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Disposition", "inline; filename=\"mailbox-screenshot\"")
	c.Data(200, contentType, data)
}
