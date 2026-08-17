package managedinstance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var managedInstanceTestDBSequence atomic.Uint64

func newManagedInstanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	resetSub2RealtimeCacheForTest()
	dsn := fmt.Sprintf("file:managed-instance-%d?mode=memory&cache=shared", managedInstanceTestDBSequence.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedInstanceAudit{},
		&model.ManagedInstanceSnapshot{}, &model.ManagedInstanceAlert{},
		&model.SMTPSetting{},
		&model.ManagedRPMHistory{},
		&model.ManagedConfigTemplate{}, &model.ManagedInstanceConfigBinding{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	t.Setenv(managedInstanceSecretKeyEnv, base64.StdEncoding.EncodeToString(key))
	t.Setenv(managedInstanceSecretKeyVersionEnv, "test-v1")
	t.Setenv(managedInstanceAllowedPortsEnv, "*")
	return db
}

func TestManagedInstanceCRUDKeepsCredentialEncrypted(t *testing.T) {
	db := newManagedInstanceTestDB(t)

	created, err := Create(CreateInput{
		Name: "Production New API", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com/",
		Environment: "production", Labels: map[string]string{"region": "cn-east"}, ManagementMode: model.ManagedInstanceModeObserve,
		TLSVerify: true, ActorID: 1,
		Credential: &CredentialInput{AuthType: "bearer_pat", Secret: "remote-admin-token", ExpiresAt: 12345},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com", created.BaseURL)
	require.Equal(t, "cn-east", created.Labels["region"])
	require.NotNil(t, created.Credential)
	require.Equal(t, "bearer_pat", created.Credential.AuthType)
	require.Len(t, created.Credential.Fingerprint, 8)
	encodedView, err := json.Marshal(created)
	require.NoError(t, err)
	require.NotContains(t, string(encodedView), "remote-admin-token")
	require.NotContains(t, strings.ToLower(string(encodedView)), "ciphertext")

	var stored model.ManagedInstanceCredential
	require.NoError(t, db.Where("instance_id = ?", created.Id).First(&stored).Error)
	require.NotContains(t, stored.Ciphertext, "remote-admin-token")
	require.Equal(t, "test-v1", stored.KeyVersion)

	list, err := List(ListFilter{Kind: model.ManagedInstanceKindNewAPI, Search: "Production", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), list.Total)
	require.Len(t, list.Items, 1)

	updated, err := Update(created.Id, UpdateInput{
		Name: "Production Gateway", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com/root/",
		Environment: "staging", Labels: map[string]string{"region": "cn-north"}, ManagementMode: model.ManagedInstanceModeOperate,
		TLSVerify: true, RequestTimeoutSeconds: 15, CheckIntervalSeconds: 120, ActorID: 2, AllowConnectionChange: true,
		AllowWriteMode: true,
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/root", updated.BaseURL)
	require.Equal(t, model.ManagedInstanceModeOperate, updated.ManagementMode)
	require.Equal(t, 15, updated.RequestTimeoutSeconds)

	rotated, err := RotateCredential(created.Id, CredentialInput{AuthType: "legacy_access_token", Secret: "new-token", UserID: "9"}, 2)
	require.NoError(t, err)
	require.Equal(t, "legacy_access_token", rotated.AuthType)
	require.Len(t, rotated.Fingerprint, 8)

	require.NoError(t, Delete(created.Id, 2))
	_, err = Get(created.Id)
	require.ErrorIs(t, err, ErrInstanceNotFound)
	var credentialCount int64
	require.NoError(t, db.Model(&model.ManagedInstanceCredential{}).Where("instance_id = ?", created.Id).Count(&credentialCount).Error)
	require.Zero(t, credentialCount)
	var bindingCount int64
	require.NoError(t, db.Model(&model.ManagedInstanceConfigBinding{}).Where("instance_id = ?", created.Id).Count(&bindingCount).Error)
	require.Zero(t, bindingCount)
	var audits []model.ManagedInstanceAudit
	require.NoError(t, db.Where("instance_id = ?", created.Id).Order("id asc").Find(&audits).Error)
	require.Len(t, audits, 4)
	require.Equal(t, []string{"create", "update", "credential_rotate", "delete"}, []string{audits[0].Action, audits[1].Action, audits[2].Action, audits[3].Action})
	require.NotContains(t, audits[2].Details, "new-token")

	auditPage, err := ListAudits(created.Id, 1, 2)
	require.NoError(t, err)
	require.Equal(t, int64(4), auditPage.Total)
	require.Len(t, auditPage.Items, 2)
	require.Equal(t, "delete", auditPage.Items[0].Action)
}

func TestManagedInstanceWriteModeRequiresExplicitRootAuthorization(t *testing.T) {
	newManagedInstanceTestDB(t)
	_, err := Create(CreateInput{
		Name: "forbidden-operate", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeOperate, TLSVerify: true, ActorID: 2,
	})
	require.ErrorIs(t, err, ErrWriteModeForbidden)

	created, err := Create(CreateInput{
		Name: "observe", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://observe.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
	})
	require.NoError(t, err)
	_, err = Update(created.Id, UpdateInput{
		Name: created.Name, Kind: created.Kind, BaseURL: created.BaseURL, Environment: created.Environment,
		Labels: created.Labels, ManagementMode: model.ManagedInstanceModeEnforce, TLSVerify: true,
		RequestTimeoutSeconds: 10, CheckIntervalSeconds: 60, ActorID: 2,
	})
	require.ErrorIs(t, err, ErrWriteModeForbidden)
}

func TestCreateRejectsDuplicateNameOrBaseURL(t *testing.T) {
	newManagedInstanceTestDB(t)
	_, err := Create(CreateInput{
		Name: "primary", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
	})
	require.NoError(t, err)

	tests := []CreateInput{
		{
			Name: "primary", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://other.example.com",
			Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
		},
		{
			Name: "secondary", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com/",
			Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
		},
	}
	for _, input := range tests {
		_, err := Create(input)
		require.ErrorIs(t, err, ErrInstanceAlreadyExists)
	}
}

func TestManagedInstanceListDoesNotSearchHiddenConnectionByDefault(t *testing.T) {
	newManagedInstanceTestDB(t)
	_, err := Create(CreateInput{
		Name: "edge", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://hidden-control.example.com/private",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
	})
	require.NoError(t, err)

	redactedSearch, err := List(ListFilter{Search: "hidden-control", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Zero(t, redactedSearch.Total)
	rootSearch, err := List(ListFilter{Search: "hidden-control", Page: 1, PageSize: 20, SearchConnection: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), rootSearch.Total)
}

func TestCreatePersistsSuccessfulPreflightIdentityAndCapabilities(t *testing.T) {
	newManagedInstanceTestDB(t)
	created, err := Create(CreateInput{
		Name: "detected", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://detected.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, ActorID: 1,
		Credential: &CredentialInput{AuthType: "admin_token", Secret: "verified-secret"},
		Preflight: &ProbeResult{
			Kind: model.ManagedInstanceKindSub2API, Version: "v2", Status: model.ManagedInstanceStatusHealthy,
			Capabilities: []string{"health.read", "accounts.list"}, CheckedAt: 12345,
		},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, created.Kind)
	require.Equal(t, "v2", created.Version)
	require.Equal(t, model.ManagedInstanceStatusHealthy, created.Status)
	require.Equal(t, int64(12345), created.LastSeenAt)
	require.Contains(t, created.Capabilities, "accounts.list")
	require.Equal(t, int64(12345), created.Credential.LastVerifiedAt)
}

func TestUpdateRejectsConnectionChangeWithoutSecretPermission(t *testing.T) {
	newManagedInstanceTestDB(t)
	created, err := Create(CreateInput{
		Name: "protected", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true,
		RequestTimeoutSeconds: 10, CheckIntervalSeconds: 60, ActorID: 1,
		Credential: &CredentialInput{AuthType: "bearer_pat", Secret: "do-not-forward"},
	})
	require.NoError(t, err)

	_, err = Update(created.Id, UpdateInput{
		Name: created.Name, Kind: created.Kind, BaseURL: "https://attacker.example.com",
		Environment: created.Environment, Labels: created.Labels, ManagementMode: created.ManagementMode,
		TLSVerify: true, RequestTimeoutSeconds: 10, CheckIntervalSeconds: 60, ActorID: 2,
	})
	require.ErrorIs(t, err, ErrConnectionChangeForbidden)

	unchanged, err := Get(created.Id)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com", unchanged.BaseURL)
	require.NotNil(t, unchanged.Credential)
}

func TestLoadCredentialRejectsExpiredCredential(t *testing.T) {
	newManagedInstanceTestDB(t)
	created, err := Create(CreateInput{
		Name: "expired", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com",
		Environment: "production", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true,
		RequestTimeoutSeconds: 10, CheckIntervalSeconds: 60, ActorID: 1,
		Credential: &CredentialInput{AuthType: "bearer_pat", Secret: "expired-token", ExpiresAt: time.Now().Unix() - 1},
	})
	require.NoError(t, err)

	_, err = loadCredential(created.Id)
	var probeError *ProbeError
	require.ErrorAs(t, err, &probeError)
	require.Equal(t, ProbeErrorCredentialExpired, probeError.Code)
}

func TestRedactConnectionDetailsLeavesSourceUntouched(t *testing.T) {
	source := &InstanceView{
		ManagedInstance: &model.ManagedInstance{
			Id: 7, Name: "edge", BaseURL: "https://admin.example.com", Labels: `{"region":"east"}`,
			TLSVerify: true, RequestTimeoutSeconds: 15, CheckIntervalSeconds: 60, CreatedBy: 1, UpdatedBy: 2,
		},
		Labels: map[string]string{"region": "east"}, Capabilities: []string{"health.read"},
		Credential: &CredentialView{AuthType: "bearer_pat", Fingerprint: "12345678"},
	}

	redacted := RedactConnectionDetails(source)

	require.Empty(t, redacted.BaseURL)
	require.Empty(t, redacted.Labels)
	require.Nil(t, redacted.Credential)
	require.False(t, redacted.TLSVerify)
	require.Zero(t, redacted.RequestTimeoutSeconds)
	require.Zero(t, redacted.CheckIntervalSeconds)
	require.Zero(t, redacted.CreatedBy)
	require.Zero(t, redacted.UpdatedBy)
	require.Equal(t, []string{"health.read"}, redacted.Capabilities)
	require.Equal(t, "https://admin.example.com", source.BaseURL)
	require.NotNil(t, source.Credential)
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "https", input: " https://example.com/api/ ", want: "https://example.com/api"},
		{name: "http", input: "http://127.0.0.1:3000", want: "http://127.0.0.1:3000"},
		{name: "no scheme", input: "example.com", wantErr: true},
		{name: "userinfo", input: "https://user:pass@example.com", wantErr: true},
		{name: "query", input: "https://example.com?q=1", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeBaseURL(test.input)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
