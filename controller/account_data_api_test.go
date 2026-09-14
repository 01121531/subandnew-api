package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/accountdataapi"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAccountDataTakeoverIntentRequiresRootAndMarksAudit(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	t.Cleanup(func() { model.DB = previous; _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.ManagedAccountAPI{},
		&model.ManagedAccountAPIInstance{}, &model.ManagedAccountAPIKey{}, &model.ManagedAccountAPIPortalSession{}))
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	entry := model.ManagedAccountAPI{ID: 9, Name: "recover", Status: model.ManagedAccountAPIDisabled,
		CreatedBy: 999, Fields: `["name"]`, IncludeTerms: "[]", ExcludeTerms: "[]", Rules: "[]", AllowedCIDRs: "[]"}
	require.NoError(t, db.Create(&entry).Error)
	gin.SetMode(gin.TestMode)
	request := func(actor int) (*httptest.ResponseRecorder, *gin.Context) {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Set("id", actor)
		c.Params = gin.Params{{Key: "id", Value: "9"}}
		c.Request = httptest.NewRequest(http.MethodPut, "/api/account-data-apis/9", strings.NewReader(`{"takeover":true}`))
		c.Request.Header.Set("Content-Type", "application/json")
		UpdateAccountDataAPI(c)
		return r, c
	}
	denied, deniedContext := request(2)
	require.Equal(t, http.StatusForbidden, denied.Code)
	_, logged := deniedContext.Get(string(constant.ContextKeyAuditLogged))
	require.False(t, logged)
	allowed, allowedContext := request(7)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	loggedValue, logged := allowedContext.Get(string(constant.ContextKeyAuditLogged))
	require.True(t, logged)
	require.Equal(t, true, loggedValue)
	var after model.ManagedAccountAPI
	require.NoError(t, db.First(&after, 9).Error)
	require.Equal(t, 7, after.CreatedBy)
	require.Equal(t, entry.Name, after.Name)
	require.Equal(t, entry.Fields, after.Fields)
	require.Equal(t, entry.Status, after.Status)
}

func TestOpenAccountDataProjectsFieldsAndHonorsETag(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedInstanceSnapshot{},
		&model.ManagedAccountSnapshot{}, &model.SystemTask{}, &model.SystemTaskScopeLock{},
		&model.ManagedAccountAPI{}, &model.ManagedAccountAPIInstance{}, &model.ManagedAccountAPIKey{},
		&model.ManagedAccountAPIAccessLog{}, &model.User{}, &model.AdminDataPolicy{},
	))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	model.DB = db
	t.Cleanup(func() { model.DB = previous })

	instance := model.ManagedInstance{Name: "partner-source", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://example.invalid"}
	require.NoError(t, db.Create(&instance).Error)
	available := true
	now := time.Now().Unix()
	payload, err := json.Marshal(managedinstance.InventoryPage{ResourceKind: "account", Total: 1, Items: []managedinstance.InventoryItem{{
		IDText: "90071992547409931234", Name: "visible", Email: "visible@example.com", Note: "password=hidden", Enabled: &available,
	}}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory, RangeKey: "inventory",
		Timezone: managedaccount.TimezoneShanghai, SchemaVersion: 2, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	created, err := accountdataapi.Create(t.Context(), accountdataapi.ConfigInput{
		Name: "partner", Dataset: managedaccount.DatasetInventory, PresetDays: 7, InstanceIDs: []int64{instance.Id},
		Fields: []string{"name", "email", "note", "available"}, SortBy: "name", SortOrder: "asc", PageSize: 50,
		RateLimitPerMinute: 60,
	}, 1)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/open-api/v1/accounts", GetOpenAccountData)

	request := func(etag string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(http.MethodGet, "/open-api/v1/accounts?page=1&page_size=20", nil)
		httpRequest.RemoteAddr = "203.0.113.10:1234"
		httpRequest.Header.Set("Authorization", "Bearer "+created.Secret)
		if etag != "" {
			httpRequest.Header.Set("If-None-Match", etag)
			require.Equal(t, etag, httpRequest.Header.Get("If-None-Match"))
			require.True(t, accountDataETagMatches(httpRequest.Header.Get("If-None-Match"), etag))
		}
		engine.ServeHTTP(recorder, httpRequest)
		return recorder
	}

	first := request("")
	require.Equal(t, http.StatusOK, first.Code)
	require.NotEmpty(t, first.Header().Get("ETag"))
	var body struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, "90071992547409931234", body.Data[0]["account_id"])
	require.Equal(t, "visible@example.com", body.Data[0]["email"])
	require.Contains(t, body.Data[0]["note"], "[已隐藏]")
	require.NotContains(t, body.Data[0], "platform")

	second := request(first.Header().Get("ETag"))
	require.Equal(t, first.Header().Get("ETag"), second.Header().Get("ETag"))
	require.Equal(t, http.StatusNotModified, second.Code)
	var accessLogs int64
	require.NoError(t, db.Model(&model.ManagedAccountAPIAccessLog{}).Count(&accessLogs).Error)
	require.Equal(t, int64(2), accessLogs)
}
