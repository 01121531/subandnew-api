package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/accountdataapi"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type accountDataAPIKeyRequest struct {
	Name      string `json:"name"`
	ExpiresAt int64  `json:"expires_at"`
}

func ListAccountDataAPIs(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	result, err := accountdataapi.List(c.Query("search"), c.Query("status"), page, pageSize)
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func ListAccountDataAPIInstanceOptions(c *gin.Context) {
	result, err := accountdataapi.ListInstanceOptions()
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func GetAccountDataAPI(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	result, err := accountdataapi.Get(id)
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func CreateAccountDataAPI(c *gin.Context) {
	var request accountdataapi.ConfigInput
	if err := c.ShouldBindJSON(&request); err != nil {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.Create(c.Request.Context(), request, c.GetInt("id"))
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	recordManageAudit(c, "account_data_api.create", map[string]interface{}{"id": result.API.ID, "name": result.API.Name})
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": result})
}

func UpdateAccountDataAPI(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	var request accountdataapi.ConfigInput
	if err := c.ShouldBindJSON(&request); err != nil {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.Update(c.Request.Context(), id, request, c.GetInt("id"))
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	recordManageAudit(c, "account_data_api.update", map[string]interface{}{"id": result.ID, "name": result.Name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func DeleteAccountDataAPI(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	if err := accountdataapi.Delete(id); err != nil {
		accountDataAPIError(c, err)
		return
	}
	recordManageAudit(c, "account_data_api.delete", map[string]interface{}{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id}})
}

func PreviewAccountDataAPI(c *gin.Context) {
	var request accountdataapi.ConfigInput
	if err := c.ShouldBindJSON(&request); err != nil {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return
	}
	result, err := accountdataapi.Preview(c.Request.Context(), request)
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func CreateAccountDataAPIKey(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	var request accountDataAPIKeyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return
	}
	key, secret, err := accountdataapi.CreateKey(id, request.Name, request.ExpiresAt, c.GetInt("id"))
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	recordManageAudit(c, "account_data_api.key_create", map[string]interface{}{"id": id})
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": gin.H{"key": key, "secret": secret}})
}

func RevokeAccountDataAPIKey(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	keyID, err := strconv.ParseInt(c.Param("key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return
	}
	if err := accountdataapi.RevokeKey(id, keyID); err != nil {
		accountDataAPIError(c, err)
		return
	}
	recordManageAudit(c, "account_data_api.key_revoke", map[string]interface{}{"id": id, "key_id": keyID})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": keyID}})
}

func ListAccountDataAPIAccessLogs(c *gin.Context) {
	id, ok := accountDataAPIID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	items, total, err := accountdataapi.ListAccessLogs(id, page, pageSize)
	if err != nil {
		accountDataAPIError(c, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"items": items, "total": total, "page": page, "page_size": pageSize}})
}

func GetOpenAccountData(c *gin.Context) {
	started := time.Now()
	requestID := uuid.NewString()
	c.Header("X-Request-ID", requestID)
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	auth, err := accountdataapi.Authenticate(token, c.ClientIP())
	if err != nil {
		if auth != nil {
			status, code := openAccountDataStatus(err)
			accountdataapi.RecordAccess(&model.ManagedAccountAPIAccessLog{APIID: auth.API.ID, KeyID: auth.Key.ID, KeyPrefix: auth.Key.Prefix,
				AuthType: "api_key", Action: "query", RequestID: requestID, IPAddress: c.ClientIP(), StatusCode: status,
				DurationMS: time.Since(started).Milliseconds(), ErrorCode: code})
		}
		openAccountDataError(c, requestID, err)
		return
	}
	statusCode, resultCount, errorCode := http.StatusOK, 0, ""
	defer func() {
		accountdataapi.RecordAccess(&model.ManagedAccountAPIAccessLog{APIID: auth.API.ID, KeyID: auth.Key.ID, KeyPrefix: auth.Key.Prefix,
			AuthType: "api_key", Action: "query", RequestID: requestID, IPAddress: c.ClientIP(), StatusCode: statusCode,
			DurationMS: time.Since(started).Milliseconds(), ResultCount: resultCount, ErrorCode: errorCode})
	}()
	allowed, retryAfter := accountdataapi.AllowRequest(c.Request.Context(), auth.Key.ID, auth.API.RateLimitPerMinute)
	if !allowed {
		statusCode, errorCode = http.StatusTooManyRequests, "rate_limited"
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		openAccountDataError(c, requestID, accountdataapi.ErrRateLimited)
		return
	}
	page, pageSize := intQuery(c, "page"), intQuery(c, "page_size")
	result, err := accountdataapi.QueryExternal(c.Request.Context(), auth, page, pageSize, c.Query("search"), c.Query("sort_by"), c.Query("sort_order"))
	if err != nil {
		statusCode, errorCode = openAccountDataStatus(err)
		openAccountDataError(c, requestID, err)
		return
	}
	fields := append([]string{"instance_id", "account_id"}, auth.View.Fields...)
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, accountdataapi.Project(item, fields))
	}
	resultCount = len(items)
	etag := accountDataETag(auth.View.ID, auth.View.UpdatedAt, result.ObservedAt, result.Total, c.Request.URL.RawQuery)
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, max-age=30")
	accountdataapi.MarkUsed(auth, result)
	if accountDataETagMatches(c.Request.Header.Get("If-None-Match"), etag) {
		statusCode = http.StatusNotModified
		c.Status(statusCode)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list", "data": items,
		"pagination":    gin.H{"page": result.Page, "page_size": result.PageSize, "total": result.Total, "has_more": result.HasMore},
		"authorization": gin.H{"name": auth.View.Name, "dataset": result.Dataset, "preset_days": result.PresetDays},
		"snapshot":      gin.H{"observed_at": accountDataTime(result.ObservedAt), "timezone": accountdataapiTimezone(), "stale": result.Stale, "partial": result.Partial, "sources": externalSourceStatuses(result.Sources)},
	})
}

func accountDataAPIID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		accountDataAPIError(c, accountdataapi.ErrInvalid)
		return 0, false
	}
	return id, true
}

func accountDataAPIError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, accountdataapi.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, accountdataapi.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, accountdataapi.ErrTooManyKeys):
		status = http.StatusConflict
	case errors.Is(err, accountdataapi.ErrSnapshotEmpty):
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

func openAccountDataError(c *gin.Context, requestID string, err error) {
	status, code := openAccountDataStatus(err)
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": openAccountDataMessage(code), "request_id": requestID}})
}

func openAccountDataStatus(err error) (int, string) {
	switch {
	case errors.Is(err, accountdataapi.ErrUnauthorized):
		return http.StatusUnauthorized, "invalid_api_key"
	case errors.Is(err, accountdataapi.ErrDisabled):
		return http.StatusForbidden, "authorization_disabled"
	case errors.Is(err, accountdataapi.ErrIPDenied):
		return http.StatusForbidden, "ip_not_allowed"
	case errors.Is(err, accountdataapi.ErrRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, accountdataapi.ErrInvalid):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, accountdataapi.ErrSnapshotEmpty):
		return http.StatusServiceUnavailable, "snapshot_unavailable"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func openAccountDataMessage(code string) string {
	switch code {
	case "invalid_api_key":
		return "The API key is invalid or expired."
	case "authorization_disabled":
		return "This account data authorization is disabled."
	case "ip_not_allowed":
		return "The client IP is not allowed."
	case "rate_limited":
		return "Too many requests."
	case "invalid_request":
		return "The request parameters are invalid."
	case "snapshot_unavailable":
		return "No successful account snapshot is available."
	default:
		return "The request could not be completed."
	}
}

func intQuery(c *gin.Context, key string) int { value, _ := strconv.Atoi(c.Query(key)); return value }

func accountDataETag(id int64, updatedAt, observedAt int64, total int, query string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(updatedAt, 10) + ":" + strconv.FormatInt(observedAt, 10) + ":" + strconv.Itoa(total) + ":" + query))
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func accountDataETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func accountDataTime(timestamp int64) string {
	if timestamp <= 0 {
		return ""
	}
	location, _ := time.LoadLocation(accountdataapiTimezone())
	return time.Unix(timestamp, 0).In(location).Format(time.RFC3339)
}

func accountdataapiTimezone() string { return "Asia/Shanghai" }

func externalSourceStatuses(sources []managedaccount.SourceStatus) []gin.H {
	result := make([]gin.H, 0, len(sources))
	for _, source := range sources {
		result = append(result, gin.H{"instance_id": source.InstanceID, "status": source.Status, "observed_at": accountDataTime(source.ObservedAt),
			"last_attempt_status": source.LastAttemptStatus, "error_code": source.ErrorCode, "stale": source.Stale})
	}
	return result
}
