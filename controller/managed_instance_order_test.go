package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func instanceOrderRequest(t *testing.T, method string, role int, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("role", role); c.Set("id", 1) })
	r.GET("/order", GetManagedInstanceOrder)
	r.PUT("/order", PutManagedInstanceOrder)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/order", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, request)
	return w
}

func TestManagedInstanceOrderRejectsNonRootBeforeReadingData(t *testing.T) {
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			response := instanceOrderRequest(t, method, role, "{}")
			require.Equal(t, http.StatusForbidden, response.Code)
			require.NotContains(t, response.Body.String(), "items")
		}
	}
}

func TestManagedInstanceOrderHTTPContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "order.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.ManagedInstanceOrderState{}, &model.ManagedInstanceAudit{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	for _, name := range []string{"first", "second"} {
		require.NoError(t, db.Create(&model.ManagedInstance{Name: name, Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://" + name + ".private.example"}).Error)
	}
	response := instanceOrderRequest(t, http.MethodGet, common.RoleRootUser, "")
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "private.example")
	var initial struct {
		Data struct {
			Version int64 `json:"version"`
			Items   []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &initial))
	require.Equal(t, int64(2), initial.Data.Items[0].ID)
	body, err := json.Marshal(map[string]any{"version": initial.Data.Version, "instance_ids": []int64{1, 2}})
	require.NoError(t, err)
	response = instanceOrderRequest(t, http.MethodPut, common.RoleRootUser, string(body))
	require.Equal(t, http.StatusOK, response.Code)
	response = instanceOrderRequest(t, http.MethodPut, common.RoleRootUser, string(body))
	require.Equal(t, http.StatusConflict, response.Code)
	for _, invalid := range []string{"{}", "null", `{"version":1}`, `{"version":1,"instance_ids":null}`, `{"version":0,"instance_ids":[]}`, `{"version":2,"instance_ids":[1,1]}`} {
		response = instanceOrderRequest(t, http.MethodPut, common.RoleRootUser, invalid)
		require.Equal(t, http.StatusBadRequest, response.Code, invalid)
	}
}
