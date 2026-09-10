package controller

import (
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
)

func LoginSupplierPortal(c *gin.Context) {
	if !supplierSameOrigin(c) {
		supplierFailure(c, &supplier.Error{Status: 403, Code: "supplier_csrf_invalid"})
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !supplierDecode(c, &in) {
		return
	}
	var identity model.Supplier
	if model.DB.Select("id").Where("username = ?", strings.ToLower(strings.TrimSpace(in.Username))).First(&identity).Error == nil {
		c.Set("supplier_id", identity.ID)
	}
	p, raw, err := supplierService().Login(c.Request.Context(), in.Username, in.Password, c.ClientIP())
	if err != nil {
		supplierFailure(c, err)
		return
	}
	if old, _ := c.Cookie(supplierCookieName); old != "" {
		if previous, e := supplierService().Authenticate(old); e == nil {
			supplierService().Logout(previous.Session.ID)
		}
	}
	c.Set("supplier_id", p.Supplier.ID)
	supplierCookie(c, raw, int(supplier.SessionTTL.Seconds()))
	supplierSuccess(c, gin.H{"authenticated": true, "supplier": p.Supplier, "csrf_token": supplier.CSRF(raw)})
}
func GetSupplierPortalSession(c *gin.Context) {
	raw, _ := c.Cookie(supplierCookieName)
	p, err := supplierService().Authenticate(raw)
	if err != nil {
		status, _ := supplier.HTTPError(err)
		if status != 401 {
			supplierFailure(c, err)
			return
		}
		if raw != "" {
			supplierCookie(c, "", -1)
		}
		supplierSuccess(c, gin.H{"authenticated": false})
		return
	}
	c.Set("supplier_id", p.Supplier.ID)
	supplierSuccess(c, gin.H{"authenticated": true, "supplier": p.Supplier, "csrf_token": supplier.CSRF(raw)})
}
func LogoutSupplierPortal(c *gin.Context) {
	p, ok := supplierPrincipal(c, true)
	if !ok {
		return
	}
	if err := supplierService().Logout(p.Session.ID); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierCookie(c, "", -1)
	supplierSuccess(c, gin.H{"authenticated": false})
}
func ChangeSupplierPortalPassword(c *gin.Context) {
	p, ok := supplierPrincipal(c, true)
	if !ok {
		return
	}
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !supplierDecode(c, &in) {
		return
	}
	if err := supplierService().Password(p.Supplier.ID, in.Password, in.Current, true); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierCookie(c, "", -1)
	supplierSuccess(c, gin.H{"authenticated": false})
}
func ListSupplierPortalBindings(c *gin.Context) {
	p, ok := supplierPrincipal(c, false)
	if !ok {
		return
	}
	items, err := supplierService().Bindings(p.Supplier.ID, true)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, items)
}
func ReadSupplierPortal(resource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := supplierPrincipal(c, false)
		if !ok {
			return
		}
		q := c.Request.URL.Query()
		id, err := strconv.ParseInt(q.Get("binding_id"), 10, 64)
		if err != nil || id <= 0 || len(q["binding_id"]) != 1 {
			supplierBadRequest(c, "supplier_invalid_binding")
			return
		}
		force := q.Get("refresh") == "1"
		q.Del("binding_id")
		q.Del("refresh")
		if resource == "usage" && q.Get("days") == "" {
			q.Set("days", "30")
		}
		c.Set("supplier_binding_id", id)
		data, err := supplierService().Read(c.Request.Context(), p, id, resource, q, force)
		if err != nil {
			supplierFailure(c, err)
			return
		}
		supplierSuccess(c, data)
	}
}
func WriteSupplierPortalProxy(c *gin.Context) {
	p, ok := supplierPrincipal(c, true)
	if !ok {
		return
	}
	var in struct {
		BindingID int64  `json:"binding_id"`
		Text      string `json:"text"`
		Status    string `json:"status"`
	}
	if c.Request.Method == "DELETE" {
		in.BindingID, _ = strconv.ParseInt(c.Query("binding_id"), 10, 64)
	} else if !supplierDecode(c, &in) {
		return
	}
	if in.BindingID <= 0 {
		supplierBadRequest(c, "supplier_invalid_binding")
		return
	}
	c.Set("supplier_binding_id", in.BindingID)
	action := ""
	if c.FullPath() == "/supplier-api/v1/proxies/:id/test" {
		action = "test"
	}
	data, err := supplierService().ProxyWrite(c.Request.Context(), p, in.BindingID, c.Request.Method, c.Param("id"), action, map[string]any{"text": in.Text, "status": in.Status})
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, data)
}
func StartSupplierPortalUpload(c *gin.Context) {
	p, ok := supplierPrincipal(c, true)
	if !ok {
		return
	}
	var in supplier.UploadInput
	if !supplierDecode(c, &in) {
		return
	}
	c.Set("supplier_binding_id", in.BindingID)
	data, err := supplierService().StartUpload(c.Request.Context(), p, in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, data)
}
func ExchangeSupplierPortalUpload(c *gin.Context) {
	p, ok := supplierPrincipal(c, true)
	if !ok {
		return
	}
	var in struct {
		FlowID   string `json:"flow_id"`
		Callback string `json:"callback"`
	}
	if !supplierDecode(c, &in) {
		return
	}
	c.Set("supplier_binding_id", supplierService().UploadBindingID(p, in.FlowID))
	data, err := supplierService().Exchange(c.Request.Context(), p, in.FlowID, in.Callback)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, data)
}

func ImportSupplierPortalAccounts(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := supplierPrincipal(c, true)
		if !ok {
			return
		}
		var in supplier.ImportAccountInput
		if !supplierDecode(c, &in) {
			return
		}
		c.Set("supplier_binding_id", in.BindingID)
		data, err := supplierService().ImportAccounts(c.Request.Context(), p, kind, in)
		if err != nil {
			supplierFailure(c, err)
			return
		}
		supplierSuccess(c, data)
	}
}
