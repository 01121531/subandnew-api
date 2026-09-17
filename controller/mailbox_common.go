package controller

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

const mailboxCookieName = "mailbox_session"

func mailboxService() *mailbox.Service { return mailbox.New(model.DB) }
func mailboxFailure(c *gin.Context, err error) {
	status, code := mailbox.HTTPError(err)
	c.Set("mailbox_error", code)
	if status == 429 {
		c.Header("Retry-After", "900")
	}
	if status == 401 && strings.HasPrefix(c.Request.URL.Path, "/mailbox-api/") {
		mailboxCookie(c, "", -1)
	}
	body := gin.H{"success": false, "message": code}
	var conflicts *mailbox.AssignmentConflictError
	if errors.As(err, &conflicts) {
		body["conflicts"] = conflicts.Conflicts
	}
	c.AbortWithStatusJSON(status, body)
}
func mailboxBadRequest(c *gin.Context, code string) {
	mailboxFailure(c, &mailbox.Error{Status: 400, Code: code})
}
func mailboxSameOrigin(c *gin.Context) bool {
	site := c.GetHeader("Sec-Fetch-Site")
	if site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if c.GetHeader("Origin") == "" {
		return site == "same-origin" || c.GetHeader("X-Mailbox-Request") == "1"
	}
	u, err := url.Parse(c.GetHeader("Origin"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, c.Request.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if c.Request.TLS != nil && u.Scheme != "https" {
		return false
	}
	if scheme := c.Request.URL.Scheme; scheme != "" && scheme != u.Scheme {
		return false
	}
	// A browser's same-origin fetch metadata covers TLS terminated at a proxy.
	return true
}
func MailboxAdminOriginGuard(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && (!mailboxSameOrigin(c) || c.GetHeader("X-Mailbox-Request") != "1") {
		mailboxFailure(c, &mailbox.Error{Status: 403, Code: "mailbox_csrf_invalid"})
		return
	}
	c.Next()
}
func mailboxCookie(c *gin.Context, raw string, age int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(mailboxCookieName, raw, age, "/mailbox-api/v1", "", true, true)
}
func MailboxGuard(permission authz.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := mailbox.Actor{Admin: authz.DataAccessFrom(c.Request.Context()), IP: c.ClientIP()}
		if actor.Admin == nil {
			mailboxFailure(c, &mailbox.Error{Status: 403, Code: "mailbox_permission_denied"})
			return
		}
		c.Set("mailbox_actor", actor)
		c.Set("mailbox_permission", permission)
		if err := mailboxService().CheckActor(actor, permission); err != nil {
			mailboxFailure(c, err)
			return
		}
		c.Next()
	}
}
func MailboxAttachmentGuard(c *gin.Context) {
	actor := mailbox.Actor{Admin: authz.DataAccessFrom(c.Request.Context()), IP: c.ClientIP()}
	if actor.Admin != nil {
		for _, permission := range []authz.Permission{authz.MailboxView, authz.MailboxReview} {
			if mailboxService().CheckActor(actor, permission) == nil {
				c.Set("mailbox_actor", actor)
				c.Set("mailbox_permission", permission)
				c.Next()
				return
			}
		}
	}
	mailboxFailure(c, &mailbox.Error{Status: 403, Code: "mailbox_permission_denied"})
}
func MailboxPortalGuard(c *gin.Context) {
	raw, _ := c.Cookie(mailboxCookieName)
	p, err := mailboxService().Authenticate(raw)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	actor := mailbox.PortalActor(p, c.ClientIP())
	c.Set("mailbox_actor", actor)
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		expected := mailbox.CSRF(raw)
		if !mailboxSameOrigin(c) || subtle.ConstantTimeCompare([]byte(expected), []byte(c.GetHeader("X-Mailbox-CSRF"))) != 1 {
			mailboxFailure(c, &mailbox.Error{Status: 403, Code: "mailbox_csrf_invalid"})
			return
		}
	}
	c.Next()
}
func mailboxActor(c *gin.Context) mailbox.Actor {
	value, _ := c.Get("mailbox_actor")
	actor, _ := value.(mailbox.Actor)
	return actor
}
func mailboxCurrent(c *gin.Context) bool {
	permission := authz.MailboxView
	if value, ok := c.Get("mailbox_permission"); ok {
		permission = value.(authz.Permission)
	}
	if err := mailboxService().CheckActor(mailboxActor(c), permission); err != nil {
		mailboxFailure(c, err)
		return false
	}
	return true
}
func mailboxSuccess(c *gin.Context, data any) {
	if !mailboxCurrent(c) {
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": data})
}
func mailboxDecode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		mailboxBadRequest(c, "mailbox_invalid_request")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		mailboxBadRequest(c, "mailbox_invalid_request")
		return false
	}
	return true
}
func mailboxID(c *gin.Context) int64 {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		mailboxBadRequest(c, "mailbox_invalid_id")
		return 0
	}
	return id
}
func mailboxListQuery(c *gin.Context) (mailbox.ListQuery, bool) {
	if c.Request.Method == "POST" {
		var q mailbox.ListQuery
		if !mailboxDecode(c, &q) {
			return q, false
		}
		var ok bool
		q.AccountType, ok = mailboxAccountType(c, q.AccountType)
		if !ok {
			return q, false
		}
		if q.Page < 0 || q.Page > 100000 || q.PageSize < 0 || q.PageSize > 100 || q.OperatorID < 0 || len(q.Search) > 320 {
			mailboxBadRequest(c, "mailbox_invalid_query")
			return q, false
		}
		return q, true
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	operator, e3 := strconv.ParseInt(c.DefaultQuery("operator_id", "0"), 10, 64)
	assignment, e4 := strconv.ParseInt(c.DefaultQuery("assignment_id", "0"), 10, 64)
	archived, archiveErr := strconv.ParseBool(c.DefaultQuery("archived", "false"))
	if archiveErr != nil {
		mailboxBadRequest(c, "mailbox_invalid_query")
		return mailbox.ListQuery{}, false
	}
	if e != nil || e2 != nil || e3 != nil || e4 != nil || assignment < 0 || page < 1 || page > 100000 || size < 1 || size > 100 || operator < 0 || len(c.Query("search")) > 320 || len(c.Query("kind")) > 32 {
		mailboxBadRequest(c, "mailbox_invalid_query")
		return mailbox.ListQuery{}, false
	}
	accountType, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return mailbox.ListQuery{}, false
	}
	return mailbox.ListQuery{Archived: archived, Page: page, PageSize: size, OperatorID: operator, Search: c.Query("search"), Status: c.Query("status"), AccountType: accountType, Kind: c.Query("kind"), AssignmentID: assignment}, true
}

func mailboxAccountType(c *gin.Context, value string) (string, bool) {
	if value == "" {
		value = "refund"
	}
	if value != "refund" && value != "opening" {
		mailboxBadRequest(c, "mailbox_invalid_account_type")
		return "", false
	}
	c.Set("mailbox_account_type", value)
	return value, true
}
func MailboxAuditTrail() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
		c.Header("Cache-Control", "no-store, private")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
		if model.DB == nil {
			return
		}
		actor := mailboxActor(c)
		actor.IP = c.ClientIP()
		action := c.Request.Method + " " + c.FullPath()
		if len(action) > 64 {
			action = action[:64]
		}
		if err := mailboxService().Audit(actor, action, c.GetInt64("mailbox_account_id"), c.GetInt64("mailbox_assignment_id"), c.Writer.Status(), c.GetString("mailbox_error"), c.GetString("mailbox_account_type")); err != nil {
			common.SysError("mailbox audit could not be recorded")
		}
	}
}
