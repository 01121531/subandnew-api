package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/accountdataapi"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const portalCookieName = "account_portal_session"

type portalLoginRequest struct {
	Password string `json:"password"`
}

func LoginAccountDataPortal(c *gin.Context) {
	started := time.Now()
	requestID := uuid.NewString()
	slug := c.Param("slug")
	var request portalLoginRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Password == "" {
		portalError(c, requestID, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.LoginPortal(slug, request.Password, c.ClientIP())
	if err != nil {
		portalAccessLog(accountdataapi.PortalAPIID(slug), 0, "login", requestID, c.ClientIP(), portalStatus(err), 0, portalCode(err), started)
		portalError(c, requestID, err)
		return
	}
	setPortalCookie(c, slug, result.Token, int(accountdataapi.PortalSessionLifetime/time.Second))
	portalAccessLog(accountdataapi.PortalAPIID(slug), result.SessionID, "login", requestID, c.ClientIP(), http.StatusOK, 0, "", started)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result.Session, "csrf_token": result.CSRFToken})
}

func GetAccountDataPortalSession(c *gin.Context) {
	requestID := uuid.NewString()
	cookie, err := c.Cookie(portalCookieName)
	if err != nil || strings.TrimSpace(cookie) == "" {
		portalUnauthenticatedSession(c)
		return
	}
	auth, err := accountdataapi.AuthenticatePortal(c.Param("slug"), cookie, "", c.ClientIP(), false)
	if err != nil {
		if errors.Is(err, accountdataapi.ErrPortalUnauthorized) {
			clearPortalCookie(c, c.Param("slug"))
			portalUnauthenticatedSession(c)
			return
		}
		portalError(c, requestID, err)
		return
	}
	csrf, err := accountdataapi.RefreshPortalCSRF(auth)
	if err != nil {
		portalError(c, requestID, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": accountdataapi.PortalSessionView(auth, csrf)})
}

func portalUnauthenticatedSession(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"authenticated": false}})
}

func QueryAccountDataPortal(c *gin.Context) {
	started := time.Now()
	requestID := uuid.NewString()
	auth, err := authenticatePortalRequest(c, true)
	if err != nil {
		portalError(c, requestID, err)
		return
	}
	allowed, retryAfter := accountdataapi.AllowRequestKey(c.Request.Context(), accountdataapi.PortalRateLimitIdentity(auth.API.ID, c.ClientIP()), auth.API.RateLimitPerMinute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		portalAccessLog(auth.API.ID, auth.Session.ID, "query", requestID, c.ClientIP(), http.StatusTooManyRequests, 0, "rate_limited", started)
		portalError(c, requestID, accountdataapi.ErrRateLimited)
		return
	}
	var request accountdataapi.PortalQueryInput
	if err := c.ShouldBindJSON(&request); err != nil {
		portalAccessLog(auth.API.ID, auth.Session.ID, "query", requestID, c.ClientIP(), http.StatusBadRequest, 0, "invalid_request", started)
		portalError(c, requestID, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.QueryPortal(c.Request.Context(), auth, request)
	if err != nil {
		portalAccessLog(auth.API.ID, auth.Session.ID, "query", requestID, c.ClientIP(), portalStatus(err), 0, portalCode(err), started)
		portalError(c, requestID, err)
		return
	}
	fields := append([]string{"instance_id", "account_id"}, auth.View.Fields...)
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, accountdataapi.Project(item, fields))
	}
	portalAccessLog(auth.API.ID, auth.Session.ID, "query", requestID, c.ClientIP(), http.StatusOK, len(items), "", started)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"items": items, "pagination": gin.H{"page": result.Page, "page_size": result.PageSize, "total": result.Total, "has_more": result.HasMore},
		"summary": portalSummary(result, auth.View.Fields), "observed_at": accountDataTime(result.ObservedAt), "stale": result.Stale, "partial": result.Partial,
		"filter_options": accountdataapi.PortalFilterOptions(result.FilterOptions, accountdataapi.PortalFilterFields(auth.View.Fields)),
	}})
}

func portalSummary(result *managedaccount.Result, fields []string) gin.H {
	summary := gin.H{"total": result.Summary.Total}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	if allowed["available"] {
		summary["available"] = result.Summary.Available
		summary["unavailable"] = result.Summary.Unavailable
		summary["unknown"] = result.Summary.Unknown
	}
	if allowed["requests"] {
		summary["requests"] = result.Summary.Requests
	}
	if allowed["tokens"] {
		summary["tokens"] = result.Summary.Tokens
	}
	if allowed["amount"] {
		summary["amounts"] = result.Summary.Amounts
	}
	if allowed["cost_excluding_today"] {
		summary["costs_excluding_today"] = result.Summary.CostsExcludingToday
		summary["cost_excluding_today_eligible"] = result.Summary.CostExcludingTodayEligible
		summary["cost_excluding_today_samples"] = result.Summary.CostExcludingTodaySamples
		summary["cost_excluding_today_partial"] = result.Summary.CostExcludingTodayPartial
	}
	return summary
}

func ExportAccountDataPortal(c *gin.Context) {
	started := time.Now()
	requestID := uuid.NewString()
	auth, err := authenticatePortalRequest(c, true)
	if err != nil {
		portalError(c, requestID, err)
		return
	}
	allowed, retryAfter := accountdataapi.AllowRequestKey(c.Request.Context(), accountdataapi.PortalRateLimitIdentity(auth.API.ID, c.ClientIP()), auth.API.RateLimitPerMinute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		portalAccessLog(auth.API.ID, auth.Session.ID, "export", requestID, c.ClientIP(), http.StatusTooManyRequests, 0, "rate_limited", started)
		portalError(c, requestID, accountdataapi.ErrRateLimited)
		return
	}
	var request accountdataapi.PortalExportInput
	if err := c.ShouldBindJSON(&request); err != nil {
		portalAccessLog(auth.API.ID, auth.Session.ID, "export", requestID, c.ClientIP(), http.StatusBadRequest, 0, "invalid_request", started)
		portalError(c, requestID, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.ExportPortal(c.Request.Context(), auth, request)
	if err != nil {
		portalAccessLog(auth.API.ID, auth.Session.ID, "export", requestID, c.ClientIP(), portalStatus(err), 0, portalCode(err), started)
		portalError(c, requestID, err)
		return
	}
	portalAccessLog(auth.API.ID, auth.Session.ID, "export", requestID, c.ClientIP(), http.StatusOK, result.Count, "", started)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, result.FileName))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", result.Data)
}

func LogoutAccountDataPortal(c *gin.Context) {
	started := time.Now()
	requestID := uuid.NewString()
	auth, err := authenticatePortalRequest(c, true)
	if err != nil {
		clearPortalCookie(c, c.Param("slug"))
		portalError(c, requestID, err)
		return
	}
	_ = accountdataapi.LogoutPortal(auth)
	clearPortalCookie(c, c.Param("slug"))
	portalAccessLog(auth.API.ID, auth.Session.ID, "logout", requestID, c.ClientIP(), http.StatusOK, 0, "", started)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{}})
}

func authenticatePortalRequest(c *gin.Context, requireCSRF bool) (*accountdataapi.PortalAuthenticated, error) {
	cookie, err := c.Cookie(portalCookieName)
	if err != nil {
		return nil, accountdataapi.ErrPortalUnauthorized
	}
	csrf := ""
	if requireCSRF {
		csrf = c.GetHeader("X-Portal-CSRF")
	}
	return accountdataapi.AuthenticatePortal(c.Param("slug"), cookie, csrf, c.ClientIP(), requireCSRF)
}

func setPortalCookie(c *gin.Context, slug, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: portalCookieName, Value: token, Path: portalCookiePath(slug), MaxAge: maxAge,
		HttpOnly: true, Secure: common.SessionCookieSecure, SameSite: http.SameSiteStrictMode})
}

func clearPortalCookie(c *gin.Context, slug string) {
	http.SetCookie(c.Writer, &http.Cookie{Name: portalCookieName, Value: "", Path: portalCookiePath(slug), MaxAge: -1,
		HttpOnly: true, Secure: common.SessionCookieSecure, SameSite: http.SameSiteStrictMode})
}

func portalCookiePath(slug string) string {
	return "/open-portal/v1/account-data/" + strings.TrimSpace(slug)
}

func portalError(c *gin.Context, requestID string, err error) {
	status, code := portalStatus(err), portalCode(err)
	if errors.Is(err, accountdataapi.ErrPortalLoginLimited) {
		c.Header("Retry-After", "900")
	}
	c.JSON(status, gin.H{"success": false, "message": portalMessage(code), "error": gin.H{"code": code, "request_id": requestID}})
}

func portalStatus(err error) int {
	switch {
	case errors.Is(err, accountdataapi.ErrPortalUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, accountdataapi.ErrPortalLoginLimited), errors.Is(err, accountdataapi.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, accountdataapi.ErrInvalid), errors.Is(err, accountdataapi.ErrPortalExportLarge):
		return http.StatusUnprocessableEntity
	case errors.Is(err, accountdataapi.ErrSnapshotEmpty):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func portalCode(err error) string {
	switch {
	case errors.Is(err, accountdataapi.ErrPortalUnauthorized):
		return "portal_auth_invalid"
	case errors.Is(err, accountdataapi.ErrPortalLoginLimited):
		return "portal_login_rate_limited"
	case errors.Is(err, accountdataapi.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, accountdataapi.ErrPortalExportLarge):
		return "export_too_large"
	case errors.Is(err, accountdataapi.ErrInvalid):
		return "invalid_request"
	case errors.Is(err, accountdataapi.ErrSnapshotEmpty):
		return "snapshot_unavailable"
	default:
		return "internal_error"
	}
}

func portalMessage(code string) string {
	switch code {
	case "portal_auth_invalid":
		return "访问密码错误、会话已过期或门户不可用。"
	case "portal_login_rate_limited":
		return "登录失败次数过多，请稍后再试。"
	case "rate_limited":
		return "请求过于频繁，请稍后再试。"
	case "export_too_large":
		return "导出结果超过 10,000 条，请缩小筛选范围。"
	case "invalid_request":
		return "筛选或导出参数无效。"
	case "snapshot_unavailable":
		return "当前没有可用的账号快照。"
	default:
		return "请求暂时无法完成。"
	}
}

func portalAccessLog(apiID, sessionID int64, action, requestID, ip string, status, count int, code string, started time.Time) {
	if apiID <= 0 {
		return
	}
	accountdataapi.RecordAccess(&model.ManagedAccountAPIAccessLog{APIID: apiID, AuthType: "portal", Action: action,
		SessionID: sessionID, RequestID: requestID, IPAddress: ip, StatusCode: status, DurationMS: time.Since(started).Milliseconds(),
		ResultCount: count, ErrorCode: code})
}
