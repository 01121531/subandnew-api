package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func supplierControllerTest(t *testing.T) (*gin.Engine, *supplier.Service) {
	t.Helper()
	old := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "supplier.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Supplier{}, &model.SupplierSession{}, &model.SupplierBinding{}, &model.SupplierOAuthFlow{}, &model.SupplierAudit{}, &model.SupplierPolicyDefault{}))
	model.DB = db
	t.Cleanup(func() { model.DB = old; sqlDB, _ := db.DB(); sqlDB.Close() })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/supplier-api/v1")
	group.Use(SupplierAuditTrail())
	group.POST("/auth/login", LoginSupplierPortal)
	group.GET("/auth/session", GetSupplierPortalSession)
	group.POST("/auth/logout", LogoutSupplierPortal)
	group.POST("/auth/password", ChangeSupplierPortalPassword)
	group.GET("/accounts", ReadSupplierPortal("accounts"))
	group.GET("/bindings", ListSupplierPortalBindings)
	return engine, supplierService()
}
func supplierControllerRequest(engine *gin.Engine, method, path, body string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://portal.example"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}
func TestSupplierPortalCookieIsolationAndCSRF(t *testing.T) {
	r, s := supplierControllerTest(t)
	_, err := s.Save(0, supplier.SupplierInput{Name: "Test supplier", Username: "vendor@example.com", Password: "test-password-42"})
	require.NoError(t, err)
	probe := supplierControllerRequest(r, "GET", "/supplier-api/v1/auth/session", "", nil, "", "")
	require.Equal(t, 200, probe.Code)
	require.Contains(t, probe.Body.String(), `"authenticated":false`)
	require.Contains(t, probe.Body.String(), `"title":"工作台"`)
	require.NotContains(t, probe.Body.String(), "upload_methods")
	consoleCookie := &http.Cookie{Name: "session", Value: "not-a-supplier-session"}
	require.Equal(t, 401, supplierControllerRequest(r, "GET", "/supplier-api/v1/accounts?binding_id=1", "", consoleCookie, "", "").Code)
	loginBody := `{"username":"vendor@example.com","password":"test-password-42"}`
	require.Equal(t, 403, supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/login", loginBody, nil, "", "https://attacker.example").Code)
	login := supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/login", loginBody, nil, "", "https://portal.example")
	require.Equal(t, 200, login.Code, login.Body.String())
	require.Equal(t, "no-store", login.Header().Get("Cache-Control"))
	cookies := login.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, "supplier_session", cookie.Name)
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, "/supplier-api/v1", cookie.Path)
	require.Equal(t, 43200, cookie.MaxAge)
	var response struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &response))
	require.Len(t, response.Data.CSRF, 64)
	require.NotContains(t, login.Body.String(), "password_hash")
	require.NotContains(t, login.Body.String(), "test-password-42")
	require.NotContains(t, login.Body.String(), "Test supplier")
	require.NotContains(t, login.Body.String(), "vendor@example.com")
	require.Contains(t, login.Body.String(), "upload_methods")
	require.Equal(t, 403, supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/logout", `{}`, cookie, "", "").Code)
	require.Equal(t, 403, supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/logout", `{}`, cookie, response.Data.CSRF, "https://attacker.example").Code)
	logout := supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/logout", `{}`, cookie, response.Data.CSRF, "https://portal.example")
	require.Equal(t, 200, logout.Code)
	require.Contains(t, logout.Header().Get("Set-Cookie"), "Max-Age=0")
	require.Equal(t, 401, supplierControllerRequest(r, "GET", "/supplier-api/v1/bindings", "", cookie, "", "").Code)
	var logs []model.SupplierAudit
	require.NoError(t, s.DB.Find(&logs).Error)
	require.GreaterOrEqual(t, len(logs), 8)
	serialized, err := json.Marshal(logs)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "test-password-42")
	require.NotContains(t, string(serialized), cookie.Value)
}
func TestSupplierPortalInvalidCookieAndRequestBody(t *testing.T) {
	r, _ := supplierControllerTest(t)
	probe := supplierControllerRequest(r, "GET", "/supplier-api/v1/auth/session", "", &http.Cookie{Name: supplierCookieName, Value: "invalid"}, "", "")
	require.Equal(t, 200, probe.Code)
	require.Contains(t, probe.Header().Get("Set-Cookie"), "Max-Age=0")
	for _, body := range []string{`{"username":"a","password":"b","supplier_id":2}`, `{} {}`} {
		require.Equal(t, 400, supplierControllerRequest(r, "POST", "/supplier-api/v1/auth/login", body, nil, "", "").Code)
	}
}

func TestSupplierDefaultPolicyAuditAndValidation(t *testing.T) {
	r, s := supplierControllerTest(t)
	admin := r.Group("/api/suppliers", SupplierAuditTrail())
	admin.GET("/default-policy", GetSupplierDefaultPolicy)
	admin.PUT("/default-policy", SaveSupplierDefaultPolicy)
	admin.GET("/:id/audits", ListSupplierAudits)
	defaults, err := s.DefaultPolicy()
	require.NoError(t, err)
	denied := false
	defaults.Policy["account.email"] = &denied
	body, err := json.Marshal(defaults)
	require.NoError(t, err)
	w := supplierControllerRequest(r, "PUT", "/api/suppliers/default-policy", string(body), nil, "", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"account.email":false`)
	w = supplierControllerRequest(r, "GET", "/api/suppliers/1/audits", "", nil, "", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `policy_changes`)
	require.Contains(t, w.Body.String(), `PUT /api/suppliers/default-policy`)
	w = supplierControllerRequest(r, "PUT", "/api/suppliers/default-policy", string(body), nil, "", "")
	require.Equal(t, 409, w.Code)
	w = supplierControllerRequest(r, "PUT", "/api/suppliers/default-policy", `{"policy":{"unknown":true},"revision":2}`, nil, "", "")
	require.Equal(t, 400, w.Code)
}

func TestSupplierNamingAuditAndStrictConfig(t *testing.T) {
	r, s := supplierControllerTest(t)
	admin := r.Group("/api/suppliers", SupplierAuditTrail())
	admin.POST("", SaveSupplier)
	admin.PUT("/:id", SaveSupplier)
	admin.GET("/:id", GetSupplier)
	body := `{"name":"Test","username":"naming-test","password":"synthetic-password","naming_rule":{"prefix":"V-","suffix":"-S"}}`
	w := supplierControllerRequest(r, "POST", "/api/suppliers", body, nil, "", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_naming"`)
	require.NotContains(t, w.Body.String(), "synthetic-password")
	var logs []model.SupplierAudit
	require.NoError(t, s.DB.Where("naming_changes <> ''").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.JSONEq(t, `{"before":{"prefix":"","suffix":""},"after":{"prefix":"V-","suffix":"-S"}}`, logs[0].NamingChanges)
	for _, rule := range []string{`{"prefix":"X","unknown":true}`, `{"prefix":"\n"}`, `{"suffix":false}`} {
		w = supplierControllerRequest(r, "PUT", "/api/suppliers/1", `{"name":"Test","username":"naming-test","naming_rule":`+rule+`}`, nil, "", "")
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	w = supplierControllerRequest(r, "GET", "/api/suppliers/1", "", nil, "", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"prefix":"V-"`)
}
