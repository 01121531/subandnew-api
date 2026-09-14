package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func adminDataContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousRedis, previousMaster := model.DB, common.RedisEnabled, common.IsMasterNode
	previousMode := gin.Mode()
	model.DB, common.RedisEnabled, common.IsMasterNode = db, false, true
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.IsMasterNode = previousDB, previousRedis, previousMaster
		gin.SetMode(previousMode)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.ManagedInstance{}, &model.CasbinRule{}, &model.AuthzRole{}))
	return db
}

func adminDataContractRequest(t *testing.T, handler gin.HandlerFunc, role int, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", role)
	c.Set("id", 999)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/", strings.NewReader(string(encoded)))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return w
}

func adminDataContractContext(t *testing.T, access *authz.DataAccess) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/managed-instances", nil)
	c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), access))
	return c, w
}

func TestRBACContractCreateAdminDefaultsAndPartialPermissions(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			db := adminDataContractDB(t)
			require.NoError(t, authz.Init(db))
			body := map[string]any{"username": "new-rbac-admin", "password": "password123", "role": common.RoleAdminUser}
			if partial {
				body["admin_permissions"] = authz.PermissionsMap{authz.ResourceManagedInstance: {authz.ManagedInstanceActionUpdate: true}}
			}
			response := adminDataContractRequest(t, CreateUser, common.RoleRootUser, body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.True(t, setupSucceeded(t, response), response.Body.String())
			var user model.User
			require.NoError(t, db.Where("username = ?", "new-rbac-admin").First(&user).Error)
			access, err := authz.LoadDataAccess(db, user.Id)
			require.NoError(t, err)
			assert.Equal(t, "selected", access.Policy.InstanceScope)
			assert.Empty(t, access.Policy.InstanceIDs)
			assert.Empty(t, access.Policy.Fields)
			assert.EqualValues(t, 1, access.Policy.Revision)
			for resource, actions := range authz.Capabilities(user.Id, user.Role) {
				for action, allowed := range actions {
					want := partial && resource == authz.ResourceManagedInstance && action == authz.ManagedInstanceActionUpdate
					assert.Equal(t, want, allowed, "%s.%s", resource, action)
				}
			}
			require.NoError(t, model.MigrateAdminDataPolicies(db))
			afterRestart, err := authz.LoadDataAccess(db, user.Id)
			require.NoError(t, err)
			assert.Equal(t, access, afterRestart)
		})
	}
}

func TestRBACContractOldClientOmissionPreservesPolicy(t *testing.T) {
	for _, explicitNull := range []bool{false, true} {
		t.Run(fmt.Sprintf("null=%t", explicitNull), func(t *testing.T) {
			db := adminDataContractDB(t)
			user := model.User{Username: "existing-admin", Password: "unchanged-hash", Role: common.RoleAdminUser, AuthorizationVersion: 9}
			require.NoError(t, db.Create(&user).Error)
			policy := model.FullAdminDataPolicy()
			policy.UserID, policy.Revision = user.Id, 4
			require.NoError(t, db.Create(&policy).Error)
			body := map[string]any{"id": user.Id, "username": user.Username, "display_name": "Edited", "authorization_version": 1}
			if explicitNull {
				body["admin_data_policy"] = nil
				body["admin_permissions"] = nil
			}
			response := adminDataContractRequest(t, UpdateUser, common.RoleRootUser, body)
			require.True(t, setupSucceeded(t, response), response.Body.String())
			var stored model.User
			require.NoError(t, db.First(&stored, user.Id).Error)
			assert.Equal(t, "Edited", stored.DisplayName)
			assert.Equal(t, user.Password, stored.Password)
			assert.Equal(t, user.AuthorizationVersion, stored.AuthorizationVersion)
			var got model.AdminDataPolicy
			require.NoError(t, db.First(&got, "user_id = ?", user.Id).Error)
			assert.Equal(t, policy, got)
		})
	}
}

func TestRBACContractUserPolicyConflictRollsBackProfileAndPermissions(t *testing.T) {
	db := adminDataContractDB(t)
	require.NoError(t, authz.Init(db))
	user := model.User{Username: "conflict-admin", Password: "unchanged-hash", DisplayName: "Before", Role: common.RoleAdminUser}
	require.NoError(t, db.Create(&user).Error)
	policy := model.FullAdminDataPolicy()
	policy.UserID, policy.Revision = user.Id, 3
	require.NoError(t, db.Create(&policy).Error)
	beforePermissions := authz.Capabilities(user.Id, user.Role)
	stale := model.EmptyAdminDataPolicy()
	stale.Revision = policy.Revision - 1
	response := adminDataContractRequest(t, UpdateUser, common.RoleRootUser, map[string]any{
		"id": user.Id, "username": user.Username, "display_name": "After", "admin_data_policy": stale,
		"admin_permissions": authz.DenyAllPermissions(),
	})
	assert.False(t, setupSucceeded(t, response), response.Body.String())
	assert.Contains(t, response.Body.String(), authz.ErrAuthorizationChanged.Error())
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, user.DisplayName, stored.DisplayName)
	assert.Equal(t, user.AuthorizationVersion, stored.AuthorizationVersion)
	var got model.AdminDataPolicy
	require.NoError(t, db.First(&got, "user_id = ?", user.Id).Error)
	assert.Equal(t, policy, got)
	require.NoError(t, authz.ReloadPolicy())
	assert.Equal(t, beforePermissions, authz.Capabilities(user.Id, user.Role))
}

func TestRBACContractResponseAndStreamRecheckRevocation(t *testing.T) {
	db := adminDataContractDB(t)
	user := model.User{Username: "stream-admin", Role: common.RoleAdminUser}
	require.NoError(t, db.Create(&user).Error)
	policy := model.FullAdminDataPolicy()
	policy.UserID = user.Id
	require.NoError(t, db.Create(&policy).Error)
	access, err := authz.LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	ctx, _ := adminDataContractContext(t, access)
	payload, err := adminEventPayload(ctx, map[string]any{"amount": 123})
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":123}`, string(payload))
	require.NoError(t, authz.BumpAuthorizationVersion(db, user.Id))
	payload, err = adminEventPayload(ctx, map[string]any{"amount": 123})
	assert.ErrorIs(t, err, authz.ErrAuthorizationChanged)
	assert.Nil(t, payload)
	ctx, response := adminDataContractContext(t, access)
	adminDataJSON(ctx, http.StatusOK, map[string]any{"amount": 123})
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.NotContains(t, response.Body.String(), "amount")
	ctx, response = adminDataContractContext(t, access)
	assert.False(t, adminStreamCurrent(ctx))
	assert.Equal(t, "event: authorization_revoked\ndata: {}\n\n", response.Body.String())
	assert.True(t, response.Flushed)
}

func TestRBACContractExplicitInstanceDenial(t *testing.T) {
	db := adminDataContractDB(t)
	user := model.User{Username: "scope-admin", Role: common.RoleAdminUser}
	require.NoError(t, db.Create(&user).Error)
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceIDs = user.Id, []int64{1}
	require.NoError(t, db.Create(&policy).Error)
	access, err := authz.LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	ctx, response := adminDataContractContext(t, access)
	assert.False(t, adminInstancesAllowed(ctx, []int64{1, 2}))
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.True(t, ctx.IsAborted())
	ctx, _ = adminDataContractContext(t, access)
	assert.True(t, adminInstancesAllowed(ctx, []int64{1}))
}

func TestRBACContractActualRealtimeDTOProjection(t *testing.T) {
	value := 42.0
	metric := managedinstance.MetricSample{Value: &value, Unit: "test", CollectionStatus: "succeeded"}
	dto := managedinstance.RealtimeMetricsResult{
		RPM: metric, RPMCapacity: metric, SuccessRate: metric, SuccessRateSampleCount: 42,
		ConcurrencyUsed: metric, ConcurrencyMax: metric, TodayCost: metric, Cost7D: metric, Cost30D: metric,
		AccountsTotal: 42, AccountsAvailable: 42, AccountsReporting: 42, ActiveSessions: 42,
	}
	for _, group := range []string{"rpm", "rates", "concurrency", "amount", "accounts"} {
		t.Run(group, func(t *testing.T) {
			access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
			access.Policy.Fields[group] = true
			projected, err := access.Project(dto)
			require.NoError(t, err)
			fields := projected.(map[string]any)
			encoded, err := json.Marshal(dto)
			require.NoError(t, err)
			var original map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &original))
			for key, originalValue := range original {
				if authz.DataFieldGroup(key) != group {
					assert.NotContains(t, fields, key)
					continue
				}
				require.Contains(t, fields, key)
				got, err := json.Marshal(fields[key])
				require.NoError(t, err)
				assert.JSONEq(t, string(originalValue), string(got), key)
			}
		})
	}
}

func TestRBACContractRootInstanceDTORetainsExistingSecretRedaction(t *testing.T) {
	db := adminDataContractDB(t)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstanceCredential{}))
	instance := model.ManagedInstance{Name: "root-view", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://connection.example.test"}
	require.NoError(t, db.Create(&instance).Error)
	credential := model.ManagedInstanceCredential{
		InstanceId: instance.Id, AuthType: "admin_token", Ciphertext: "ciphertext-must-not-appear",
		KeyVersion: "internal-key-version", Fingerprint: "hidden-prefix-12345678",
	}
	require.NoError(t, db.Create(&credential).Error)
	view, err := managedinstance.Get(instance.Id)
	require.NoError(t, err)
	before, err := json.Marshal(view)
	require.NoError(t, err)
	root := &authz.DataAccess{Role: common.RoleRootUser, Policy: model.FullAdminDataPolicy()}
	projected, err := root.Project(view)
	require.NoError(t, err)
	assert.Same(t, view, projected, "root preserves the existing DTO, not the stored credential model")
	after, err := json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
	for _, secret := range []string{credential.Ciphertext, credential.KeyVersion, "hidden-prefix", `"secret"`, `"password"`, `"access_token"`} {
		assert.NotContains(t, string(after), secret)
	}
	assert.Contains(t, string(after), `"fingerprint":"12345678"`)
	assert.Contains(t, string(after), instance.BaseURL, "root keeps the pre-existing connection-management view")
	admin := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.FullAdminDataPolicy()}
	projected, err = admin.Project(view)
	require.NoError(t, err)
	assert.NotContains(t, projected, "credential")
	assert.NotContains(t, projected, "base_url")
	assert.Equal(t, instance.BaseURL, view.BaseURL, "non-root projection must not mutate a DTO shared with root")
}
