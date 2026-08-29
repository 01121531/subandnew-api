package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/accountdataapi"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOpenAccountDataProjectsFieldsAndHonorsETag(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedInstanceSnapshot{},
		&model.ManagedAccountSnapshot{}, &model.SystemTask{}, &model.SystemTaskScopeLock{},
		&model.ManagedAccountAPI{}, &model.ManagedAccountAPIInstance{}, &model.ManagedAccountAPIKey{},
		&model.ManagedAccountAPIAccessLog{},
	))
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
