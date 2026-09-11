package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
)

var supplierServiceState struct {
	sync.Mutex
	service *supplier.Service
}

func supplierService() *supplier.Service {
	supplierServiceState.Lock()
	defer supplierServiceState.Unlock()
	if supplierServiceState.service == nil || supplierServiceState.service.DB != model.DB {
		supplierServiceState.service = supplier.New(model.DB)
	}
	return supplierServiceState.service
}
func supplierSuccess(c *gin.Context, data any) { c.JSON(200, gin.H{"success": true, "data": data}) }
func supplierFailure(c *gin.Context, err error) {
	status, code := supplier.HTTPError(err)
	var remote *supplier.RemoteError
	if errors.As(err, &remote) {
		code = "supplier_upstream_" + remote.Code
		// A vendor credential failure is not a failure of the local portal session.
		if status == http.StatusUnauthorized {
			status = http.StatusBadGateway
		}
	}
	c.Set("supplier_error", code)
	if status == 429 {
		c.Header("Retry-After", "900")
		if remote != nil {
			c.Header("Retry-After", "30")
		}
	}
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": code})
}
func supplierBadRequest(c *gin.Context, code string) {
	supplierFailure(c, &supplier.Error{Status: 400, Code: code})
}
func supplierDecode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		supplierBadRequest(c, "supplier_invalid_request")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		supplierBadRequest(c, "supplier_invalid_request")
		return false
	}
	return true
}
func supplierID(c *gin.Context, key string) int64 {
	id, _ := strconv.ParseInt(c.Param(key), 10, 64)
	if id <= 0 {
		supplierBadRequest(c, "supplier_invalid_id")
		return 0
	}
	return id
}
func supplierPage(c *gin.Context) (int, int, bool) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if e != nil || e2 != nil || page < 1 || page > 100000 || size < 1 || size > 100 {
		supplierBadRequest(c, "supplier_invalid_pagination")
		return 0, 0, false
	}
	return page, size, true
}

func SupplierAuditTrail() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Header("Cache-Control", "no-store")
		c.Next()
		if model.DB == nil {
			return
		}
		supplierID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		if v := c.GetInt64("supplier_id"); v != 0 {
			supplierID = v
		}
		if strings.HasPrefix(c.FullPath(), "/supplier-api/") && c.GetInt64("supplier_id") == 0 {
			supplierID = 0
		}
		action := c.Request.Method + " " + c.FullPath()
		entry := model.SupplierAudit{SupplierID: supplierID, BindingID: c.GetInt64("supplier_binding_id"), AdminID: c.GetInt("id"), Action: action, IPAddress: c.ClientIP(), StatusCode: c.Writer.Status(), DurationMS: time.Since(start).Milliseconds(), ErrorCode: c.GetString("supplier_error")}
		entry.PolicyChanges = c.GetString("supplier_policy_changes")
		entry.NamingChanges = c.GetString("supplier_naming_changes")
		model.DB.Create(&entry)
		supplierService().MaybeCleanup()
	}
}
func supplierSameOrigin(c *gin.Context) bool {
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, c.Request.Host) && u.User == nil
}

const supplierCookieName = "supplier_session"

func supplierCookie(c *gin.Context, raw string, age int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(supplierCookieName, raw, age, "/supplier-api/v1", "", true, true)
}
func supplierPrincipal(c *gin.Context, mutation bool) (*supplier.Principal, bool) {
	raw, _ := c.Cookie(supplierCookieName)
	p, err := supplierService().Authenticate(raw)
	if err != nil {
		status, _ := supplier.HTTPError(err)
		if status == 401 {
			supplierCookie(c, "", -1)
		}
		supplierFailure(c, err)
		return nil, false
	}
	c.Set("supplier_id", p.Supplier.ID)
	if mutation && (!supplierSameOrigin(c) || !supplier.CheckCSRF(raw, c.GetHeader("X-CSRF-Token"))) {
		supplierFailure(c, &supplier.Error{Status: 403, Code: "supplier_csrf_invalid"})
		return nil, false
	}
	return p, true
}
