package supplier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const corePassword = "local-test-password"

type coreRemote struct {
	identity                Identity
	verifyErr               error
	readErr                 error
	writeErr                error
	verifies, reads, writes int
	onRead                  func()
	method, resource        string
	body                    map[string]any
}

var _ Remote = (*coreRemote)(nil)

func (r *coreRemote) Verify(context.Context) (Identity, error) {
	r.verifies++
	return r.identity, r.verifyErr
}

func (r *coreRemote) Read(context.Context, string, url.Values) (map[string]any, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead()
	}
	return map[string]any{"items": []any{map[string]any{"id": "account-1"}}, "fetch": r.reads}, r.readErr
}

func (r *coreRemote) Write(_ context.Context, method, resource string, body any) (map[string]any, error) {
	r.writes++
	r.method, r.resource = method, resource
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	r.body = nil
	if err := json.Unmarshal(encoded, &r.body); err != nil {
		return nil, err
	}
	if r.writeErr != nil {
		return nil, r.writeErr
	}
	if resource == "account-upload/auth-url" {
		return map[string]any{"state": "test-state", "url": "https://oauth.invalid/authorize?state=test-state"}, nil
	}
	return map[string]any{"ok": true}, nil
}

type coreFixture struct {
	s                                               *Service
	db                                              *gorm.DB
	now                                             time.Time
	r                                               *coreRemote
	identifiers, passwords, expectedIDs, namespaces []string
}

type coreVerifyGate struct {
	*coreRemote
	ready   chan struct{}
	release chan struct{}
}

func (r *coreVerifyGate) Verify(ctx context.Context) (Identity, error) {
	select {
	case r.ready <- struct{}{}:
	case <-ctx.Done():
		return Identity{}, ctx.Err()
	}
	select {
	case <-r.release:
		return r.identity, nil
	case <-ctx.Done():
		return Identity{}, ctx.Err()
	}
}

func newCoreFixture(t *testing.T) *coreFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Supplier{}, &model.SupplierBinding{}, &model.SupplierSession{}, &model.SupplierOAuthFlow{}, &model.SupplierAudit{}, &model.ManagedInstance{}))
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY_VERSION", "core-test-v1")
	redisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = redisEnabled })
	f := &coreFixture{db: db, s: New(db), now: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), r: &coreRemote{identity: Identity{ID: "vendor-1", Username: "verified-vendor"}}}
	f.s.Now = func() time.Time { return f.now }
	f.s.RemoteFactory = func(_ *model.ManagedInstance, identifier, password, namespace, expected string) (Remote, error) {
		f.identifiers = append(f.identifiers, identifier)
		f.passwords = append(f.passwords, password)
		f.expectedIDs = append(f.expectedIDs, expected)
		f.namespaces = append(f.namespaces, namespace)
		return f.r, nil
	}
	return f
}

func (f *coreFixture) supplier(t *testing.T, name string, write bool) *model.Supplier {
	t.Helper()
	in := SupplierInput{Name: name, Username: name, Password: corePassword, ManageProxies: &write, UploadAccounts: &write}
	s, err := f.s.Save(0, in)
	require.NoError(t, err)
	return s
}

func (f *coreFixture) instance(t *testing.T, n int) *model.ManagedInstance {
	t.Helper()
	i := &model.ManagedInstance{Name: fmt.Sprintf("gateway-%d", n), BaseURL: fmt.Sprintf("https://gateway-%d.invalid", n), Kind: model.ManagedInstanceKindClaudeGateway}
	require.NoError(t, f.db.Create(i).Error)
	return i
}

func (f *coreFixture) binding(t *testing.T, supplierID, instanceID int64) *model.SupplierBinding {
	t.Helper()
	b, err := f.s.SaveBinding(context.Background(), supplierID, 0, BindingInput{InstanceID: instanceID, Identifier: "vendor-login", Password: "upstream-test-password"})
	require.NoError(t, err)
	return b
}

func (f *coreFixture) login(t *testing.T, username string) (*Principal, string) {
	t.Helper()
	p, raw, err := f.s.Login(context.Background(), username, corePassword, "192.0.2.1")
	require.NoError(t, err)
	return p, raw
}

func requireCoreError(t *testing.T, err error, status int, code string) {
	t.Helper()
	require.Error(t, err)
	gotStatus, gotCode := HTTPError(err)
	require.Equal(t, status, gotStatus)
	if code != "" {
		require.Equal(t, code, gotCode)
	}
}

func TestSupplierCoreCRUDAndPasswordValidation(t *testing.T) {
	f := newCoreFixture(t)
	for _, password := range []string{"", "short", strings.Repeat(" ", 8), strings.Repeat("x", 73)} {
		_, err := f.s.Save(0, SupplierInput{Name: "Vendor", Username: "vendor", Password: password})
		requireCoreError(t, err, 400, "supplier_invalid_password")
	}
	for _, username := range []string{"ab", "bad user", "../vendor", strings.Repeat("a", 97)} {
		_, err := f.s.Save(0, SupplierInput{Name: "Vendor", Username: username, Password: corePassword})
		requireCoreError(t, err, 400, "supplier_invalid_identity")
	}
	s, err := f.s.Save(0, SupplierInput{Name: " Vendor ", Username: " VENDOR ", Password: corePassword})
	require.NoError(t, err)
	require.Equal(t, "Vendor", s.Name)
	require.Equal(t, "vendor", s.Username)
	require.True(t, s.Enabled)
	require.True(t, s.ViewAccounts)
	require.True(t, s.ViewUsage)
	require.False(t, s.ManageProxies)
	require.False(t, s.UploadAccounts)
	require.NotEqual(t, corePassword, s.PasswordHash)
	require.True(t, common.ValidatePasswordAndHash(corePassword, s.PasswordHash))
	_, err = f.s.Save(0, SupplierInput{Name: "Duplicate", Username: "Vendor", Password: corePassword})
	requireCoreError(t, err, 409, "supplier_username_conflict")
	_, err = f.s.Save(s.ID, SupplierInput{Name: "Vendor", Username: "vendor", Password: "new-password"})
	requireCoreError(t, err, 400, "supplier_use_password_reset")
	updated, err := f.s.Save(s.ID, SupplierInput{Name: "Renamed", Username: "renamed"})
	require.NoError(t, err)
	require.Equal(t, s.AuthVersion+1, updated.AuthVersion)
	got, err := f.s.Supplier(s.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.Name)
	items, total, err := f.s.List(1, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, raw := f.login(t, "renamed")
	require.NoError(t, f.db.Create(&model.SupplierOAuthFlow{SupplierID: s.ID, SessionID: p.Session.ID, BindingID: b.ID, TokenHash: "delete-flow"}).Error)
	require.NoError(t, f.s.Delete(s.ID))
	_, err = f.s.Supplier(s.ID)
	requireCoreError(t, err, 404, "supplier_not_found")
	_, err = f.s.Authenticate(raw)
	requireCoreError(t, err, 401, "supplier_unauthenticated")
	for _, table := range []any{&model.SupplierBinding{}, &model.SupplierSession{}, &model.SupplierOAuthFlow{}} {
		var count int64
		require.NoError(t, f.db.Model(table).Where("supplier_id = ?", s.ID).Count(&count).Error)
		require.Zero(t, count)
	}
	_, err = f.s.Save(0, SupplierInput{Name: "Reused", Username: "renamed", Password: corePassword})
	requireCoreError(t, err, 409, "supplier_username_conflict")
}

func TestSupplierCoreBindingsIdentityEncryptionAndLimits(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	other := f.supplier(t, "other", false)
	i := f.instance(t, 1)
	b := f.binding(t, s.ID, i.Id)
	second := f.binding(t, s.ID, f.instance(t, 2).Id)
	require.NotEqual(t, b.ID, second.ID)
	require.Equal(t, "vendor-1", b.RemoteUserID)
	require.Equal(t, "verified-vendor", b.RemoteUsername)
	require.Equal(t, 2, f.r.verifies)
	require.Equal(t, "", f.expectedIDs[0])
	require.Contains(t, f.namespaces[0], "supplier-verify:")
	var stored model.SupplierBinding
	require.NoError(t, f.db.First(&stored, b.ID).Error)
	require.NotEmpty(t, stored.Ciphertext)
	require.NotContains(t, stored.Ciphertext, "upstream-test-password")
	require.NotContains(t, stored.Ciphertext, "vendor-login")
	c, err := cipher()
	require.NoError(t, err)
	plain, err := c.Decrypt(b.ID, "supplier-binding:v1", stored.KeyVersion, stored.Ciphertext)
	require.NoError(t, err)
	require.Equal(t, "vendor-login", plain.UserID)
	require.Equal(t, "upstream-test-password", plain.Secret)
	_, err = c.Decrypt(second.ID, "supplier-binding:v1", stored.KeyVersion, stored.Ciphertext)
	require.Error(t, err)
	encoded, err := json.Marshal(stored)
	require.NoError(t, err)
	for _, secret := range []string{"ciphertext", "key_version", "revision", "remote_user_id", stored.Ciphertext, "upstream-test-password", "vendor-login"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.NoError(t, f.s.TestBinding(context.Background(), s.ID, b.ID))
	require.Equal(t, "vendor-1", f.expectedIDs[len(f.expectedIDs)-1])
	require.Equal(t, "upstream-test-password", f.passwords[len(f.passwords)-1])
	_, err = f.s.SaveBinding(context.Background(), other.ID, 0, BindingInput{InstanceID: i.Id, Identifier: "alias-of-vendor", Password: "test-password"})
	requireCoreError(t, err, 409, "supplier_binding_conflict")
	f.r.identity.ID = "different-vendor"
	_, err = f.s.SaveBinding(context.Background(), s.ID, 0, BindingInput{InstanceID: i.Id, Identifier: "different-vendor", Password: "test-password"})
	requireCoreError(t, err, 409, "supplier_binding_conflict")
	for n := 3; n <= 20; n++ {
		f.binding(t, s.ID, f.instance(t, n).Id)
	}
	_, err = f.s.SaveBinding(context.Background(), s.ID, 0, BindingInput{InstanceID: f.instance(t, 21).Id, Identifier: "vendor-login", Password: "test-password"})
	requireCoreError(t, err, 400, "supplier_binding_limit")
	bindings, err := f.s.Bindings(s.ID, true)
	require.NoError(t, err)
	require.Len(t, bindings, 20)
	for _, binding := range bindings {
		require.Empty(t, binding.RemoteUsername)
	}
	oldRevision := b.Revision
	disabled := false
	b, err = f.s.SaveBinding(context.Background(), s.ID, b.ID, BindingInput{InstanceID: i.Id, Enabled: &disabled})
	require.NoError(t, err)
	require.NotEqual(t, oldRevision, b.Revision)
	require.False(t, b.Enabled)
	bindings, err = f.s.Bindings(s.ID, true)
	require.NoError(t, err)
	require.Len(t, bindings, 19)
	requireCoreError(t, f.s.DeleteBinding(other.ID, b.ID), 404, "supplier_binding_not_found")
	require.NoError(t, f.s.DeleteBinding(s.ID, b.ID))
}

func TestSupplierCoreBindingVerificationRejectsInvalidIdentity(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	i := f.instance(t, 1)
	input := BindingInput{InstanceID: i.Id, Identifier: "vendor-login", Password: "test-password"}
	f.r.verifyErr = &RemoteError{Status: 403, Code: "vendor_required"}
	_, err := f.s.SaveBinding(context.Background(), s.ID, 0, input)
	requireCoreError(t, err, 403, "vendor_required")
	f.r.verifyErr = nil
	for _, id := range []string{"", strings.Repeat("x", 129)} {
		f.r.identity.ID = id
		_, err = f.s.SaveBinding(context.Background(), s.ID, 0, input)
		requireCoreError(t, err, 502, "supplier_invalid_remote_identity")
	}
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierBinding{}).Count(&count).Error)
	require.Zero(t, count)
	f.r.identity.ID = "vendor-1"
	for _, password := range []string{"", " padded ", strings.Repeat("x", 1025)} {
		input.Password = password
		_, err = f.s.SaveBinding(context.Background(), s.ID, 0, input)
		requireCoreError(t, err, 400, "supplier_invalid_upstream_credentials")
	}
}

func TestSupplierCoreConcurrentBindingLimitsAndIdentity(t *testing.T) {
	for _, scenario := range []string{"max20", "remote-identity", "supplier-instance"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", false)
			supplierIDs := []int64{s.ID, s.ID}
			instanceIDs := []int64{}
			wantStatus, wantCode := 409, "supplier_binding_conflict"
			wantTotal := int64(1)
			if scenario == "max20" {
				for n := 1; n <= 19; n++ {
					f.binding(t, s.ID, f.instance(t, n).Id)
				}
				instanceIDs = []int64{f.instance(t, 20).Id, f.instance(t, 21).Id}
				wantStatus, wantCode, wantTotal = 400, "supplier_binding_limit", 20
			} else {
				i := f.instance(t, 1)
				instanceIDs = []int64{i.Id, i.Id}
				if scenario == "remote-identity" {
					supplierIDs[1] = f.supplier(t, "other", false).ID
				}
			}
			gate := &coreVerifyGate{coreRemote: &coreRemote{identity: Identity{ID: "concurrent-vendor", Username: "verified"}}, ready: make(chan struct{}, 2), release: make(chan struct{})}
			// Separate services share only the database; both verifies finish before either transaction starts.
			second := New(f.db)
			second.Now = f.s.Now
			factory := func(*model.ManagedInstance, string, string, string, string) (Remote, error) { return gate, nil }
			f.s.RemoteFactory, second.RemoteFactory = factory, factory
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			results := make(chan error, 2)
			for n, service := range []*Service{f.s, second} {
				go func(service *Service, supplierID, instanceID int64) {
					_, err := service.SaveBinding(ctx, supplierID, 0, BindingInput{InstanceID: instanceID, Identifier: "concurrent-login", Password: "concurrent-password"})
					results <- err
				}(service, supplierIDs[n], instanceIDs[n])
			}
			for n := 0; n < 2; n++ {
				select {
				case <-gate.ready:
				case <-ctx.Done():
					t.Fatal("concurrent Verify did not reach barrier")
				}
			}
			close(gate.release)
			succeeded := 0
			for n := 0; n < 2; n++ {
				select {
				case err := <-results:
					if err == nil {
						succeeded++
					} else {
						requireCoreError(t, err, wantStatus, wantCode)
					}
				case <-ctx.Done():
					t.Fatal("concurrent binding did not finish")
				}
			}
			require.Equal(t, 1, succeeded)
			var total int64
			require.NoError(t, f.db.Model(&model.SupplierBinding{}).Count(&total).Error)
			require.Equal(t, wantTotal, total)
		})
	}
}

func TestSupplierCoreCleanupExpiration(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	now := f.now.Unix()
	for n, expires := range []int64{now - 1, now, now + 1} {
		require.NoError(t, f.db.Create(&model.SupplierSession{SupplierID: 1, TokenHash: fmt.Sprintf("session-%d", n), ExpiresAt: expires}).Error)
		require.NoError(t, f.db.Create(&model.SupplierOAuthFlow{SupplierID: 1, SessionID: 1, TokenHash: fmt.Sprintf("flow-%d", n), ExpiresAt: expires}).Error)
	}
	cutoff := now - int64(90*24*time.Hour/time.Second)
	for _, at := range []int64{cutoff - 1, cutoff, cutoff + 1} {
		require.NoError(t, f.db.Create(&model.SupplierAudit{SupplierID: 1, Action: "test", CreatedAt: at}).Error)
	}
	f.s.MaybeCleanup()
	for _, table := range []any{&model.SupplierSession{}, &model.SupplierOAuthFlow{}} {
		var expired int64
		require.NoError(t, f.db.Model(table).Where("expires_at <= ?", now).Count(&expired).Error)
		require.Zero(t, expired, "rows expiring exactly now are already unauthenticatable and should be cleaned")
		var active int64
		require.NoError(t, f.db.Model(table).Where("expires_at > ?", now).Count(&active).Error)
		require.EqualValues(t, 1, active)
	}
	var audits []model.SupplierAudit
	require.NoError(t, f.db.Order("created_at").Find(&audits).Error)
	require.Len(t, audits, 2)
	require.Equal(t, cutoff, audits[0].CreatedAt)
	_, err := f.s.Supplier(s.ID)
	require.NoError(t, err)
	require.NoError(t, f.db.First(&model.SupplierBinding{}, b.ID).Error)
}

func TestSupplierCoreCleanupBoundedAndThrottled(t *testing.T) {
	f := newCoreFixture(t)
	rows := make([]model.SupplierSession, 1001)
	for n := range rows {
		rows[n] = model.SupplierSession{SupplierID: 1, TokenHash: fmt.Sprintf("expired-%d", n), ExpiresAt: f.now.Add(-time.Second).Unix()}
	}
	require.NoError(t, f.db.CreateInBatches(rows, 100).Error)
	count := func() int64 {
		var n int64
		require.NoError(t, f.db.Model(&model.SupplierSession{}).Count(&n).Error)
		return n
	}
	f.s.MaybeCleanup()
	require.EqualValues(t, 1, count(), "each cleanup is bounded to 1000 rows per table")
	f.now = f.now.Add(time.Hour - time.Nanosecond)
	f.s.MaybeCleanup()
	require.EqualValues(t, 1, count(), "cleanup runs at most hourly")
	f.now = f.now.Add(time.Nanosecond)
	f.s.MaybeCleanup()
	require.Zero(t, count())
}

func TestSupplierCoreSessionIsolationExpiryAndCSRF(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	p, raw := f.login(t, s.Username)
	_, secondRaw := f.login(t, s.Username)
	require.Len(t, raw, 43)
	require.NotEqual(t, raw, secondRaw)
	require.Equal(t, digest(raw), p.Session.TokenHash)
	require.NotEqual(t, raw, p.Session.TokenHash)
	require.Equal(t, f.now.Add(SessionTTL).Unix(), p.Session.ExpiresAt)
	require.True(t, CheckCSRF(raw, CSRF(raw)))
	require.False(t, CheckCSRF(secondRaw, CSRF(raw)))
	require.False(t, CheckCSRF(raw, ""))
	for _, invalid := range []string{"", "admin-session-cookie", strings.Repeat("a", 43), p.Session.TokenHash} {
		_, err := f.s.Authenticate(invalid)
		requireCoreError(t, err, 401, "supplier_unauthenticated")
	}
	f.now = f.now.Add(60 * time.Second)
	_, err := f.s.Authenticate(raw)
	require.NoError(t, err)
	var session model.SupplierSession
	require.NoError(t, f.db.First(&session, p.Session.ID).Error)
	require.Equal(t, f.now.Unix(), session.LastUsedAt)
	require.NoError(t, f.s.Logout(p.Session.ID))
	_, err = f.s.Authenticate(raw)
	requireCoreError(t, err, 401, "supplier_unauthenticated")
	_, err = f.s.Authenticate(secondRaw)
	require.NoError(t, err)
	f.now = time.Unix(p.Session.ExpiresAt, 0)
	_, err = f.s.Authenticate(secondRaw)
	requireCoreError(t, err, 401, "supplier_unauthenticated")
}

func TestSupplierCoreSessionRevocation(t *testing.T) {
	for _, change := range []string{"revoke", "password", "capability", "disable"} {
		t.Run(change, func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", true)
			other := f.supplier(t, "other", false)
			p, raw := f.login(t, s.Username)
			_, otherRaw := f.login(t, other.Username)
			require.NoError(t, f.db.Create(&model.SupplierOAuthFlow{SupplierID: s.ID, SessionID: p.Session.ID, TokenHash: "revoke-flow"}).Error)
			switch change {
			case "revoke":
				require.NoError(t, f.s.Revoke(s.ID))
			case "password":
				requireCoreError(t, f.s.Password(s.ID, "new-password", "wrong-current", true), 401, "supplier_current_password_incorrect")
				requireCoreError(t, f.s.Password(s.ID, "short", corePassword, true), 400, "supplier_invalid_password")
				_, err := f.s.Authenticate(raw)
				require.NoError(t, err)
				require.NoError(t, f.s.Password(s.ID, "new-password", corePassword, true))
				_, _, err = f.s.Login(context.Background(), s.Username, corePassword, "192.0.2.2")
				requireCoreError(t, err, 401, "supplier_invalid_credentials")
				_, _, err = f.s.Login(context.Background(), s.Username, "new-password", "192.0.2.2")
				require.NoError(t, err)
			default:
				no := false
				in := SupplierInput{Name: s.Name, Username: s.Username}
				if change == "disable" {
					in.Enabled = &no
				} else {
					in.UploadAccounts = &no
				}
				_, err := f.s.Save(s.ID, in)
				require.NoError(t, err)
			}
			_, err := f.s.Authenticate(raw)
			requireCoreError(t, err, 401, "supplier_unauthenticated")
			_, err = f.s.Authenticate(otherRaw)
			require.NoError(t, err)
			var count int64
			require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Where("supplier_id = ?", s.ID).Count(&count).Error)
			require.Zero(t, count)
			if change == "disable" {
				_, _, err = f.s.Login(context.Background(), s.Username, corePassword, "192.0.2.3")
				requireCoreError(t, err, 401, "supplier_invalid_credentials")
			}
		})
	}
}

func TestSupplierCoreFiveFailedLogins(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	ctx := context.Background()
	for attempt := 0; attempt < 5; attempt++ {
		_, _, err := f.s.Login(ctx, " VENDOR ", "wrong-password", "192.0.2.1")
		requireCoreError(t, err, 401, "supplier_invalid_credentials")
	}
	_, _, err := f.s.Login(ctx, s.Username, corePassword, "192.0.2.1")
	requireCoreError(t, err, 429, "supplier_login_rate_limited")
	_, _, err = f.s.Login(ctx, s.Username, corePassword, "192.0.2.2")
	require.NoError(t, err)
	f.now = f.now.Add(15 * time.Minute)
	f.login(t, s.Username)
	for attempt := 0; attempt < 4; attempt++ {
		_, _, err = f.s.Login(ctx, s.Username, "wrong-password", "192.0.2.1")
		requireCoreError(t, err, 401, "supplier_invalid_credentials")
	}
	f.login(t, s.Username)
	_, _, err = f.s.Login(ctx, s.Username, "wrong-password", "192.0.2.1")
	requireCoreError(t, err, 401, "supplier_invalid_credentials")
}

func TestSupplierCorePermissionsAndCrossSupplierTampering(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	other := f.supplier(t, "other", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	otherBinding := f.binding(t, other.ID, f.instance(t, 2).Id)
	p, _ := f.login(t, s.Username)
	op, _ := f.login(t, other.Username)
	ctx := context.Background()
	for _, resource := range []string{"accounts", "account-summary", "usage"} {
		q := url.Values{}
		if resource == "usage" {
			q.Set("days", "7")
		}
		_, err := f.s.Read(ctx, p, b.ID, resource, q, false)
		require.NoError(t, err)
	}
	for _, resource := range []string{"proxies", "account-upload/options", "unknown"} {
		_, err := f.s.Read(ctx, p, b.ID, resource, nil, false)
		requireCoreError(t, err, 403, "supplier_permission_denied")
	}
	_, err := f.s.Read(ctx, p, otherBinding.ID, "accounts", nil, false)
	requireCoreError(t, err, 404, "supplier_binding_not_found")
	_, err = f.s.ProxyWrite(ctx, p, b.ID, "POST", "", "", map[string]any{"text": "proxy"})
	requireCoreError(t, err, 403, "supplier_permission_denied")
	_, err = f.s.SaveBinding(ctx, s.ID, otherBinding.ID, BindingInput{InstanceID: otherBinding.InstanceID})
	requireCoreError(t, err, 404, "supplier_binding_not_found")
	requireCoreError(t, f.s.TestBinding(ctx, s.ID, otherBinding.ID), 404, "supplier_binding_not_found")
	_, err = f.s.ProxyWrite(ctx, op, b.ID, "DELETE", "proxy-1", "", nil)
	requireCoreError(t, err, 404, "supplier_binding_not_found")
	t.Run("session-must-belong-to-supplier", func(t *testing.T) {
		forged := &Principal{Supplier: op.Supplier, Session: p.Session}
		_, err := f.s.Read(ctx, forged, otherBinding.ID, "accounts", nil, false)
		requireCoreError(t, err, 401, "supplier_unauthenticated")
	})
	require.NoError(t, f.db.Model(b).Update("enabled", false).Error)
	_, err = f.s.Read(ctx, p, b.ID, "accounts", nil, false)
	requireCoreError(t, err, 404, "supplier_binding_not_found")
}

func TestSupplierCoreCacheBoundaries(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, s.Username)
	ctx := context.Background()
	start := f.now
	read := func(force bool) map[string]any {
		data, err := f.s.Read(ctx, p, b.ID, "accounts", nil, force)
		require.NoError(t, err)
		return data
	}
	first := read(false)
	require.Equal(t, false, first["stale"])
	first["items"].([]any)[0].(map[string]any)["id"] = "caller-mutated"
	f.now = start.Add(30*time.Second - time.Nanosecond)
	second := read(false)
	require.Equal(t, 1, f.r.reads)
	require.Equal(t, "account-1", second["items"].([]any)[0].(map[string]any)["id"])
	f.now = start.Add(30 * time.Second)
	read(false)
	require.Equal(t, 2, f.r.reads)
	read(true)
	require.Equal(t, 3, f.r.reads)
	f.r.readErr = &RemoteError{Status: 503, Code: "upstream_unavailable"}
	f.now = start.Add(30*time.Second + 15*time.Minute - time.Nanosecond)
	stale := read(false)
	require.Equal(t, true, stale["stale"])
	require.Equal(t, start.Add(30*time.Second).Unix(), stale["observed_at"])
	f.now = start.Add(30*time.Second + 15*time.Minute)
	_, err := f.s.Read(ctx, p, b.ID, "accounts", nil, false)
	requireCoreError(t, err, 503, "upstream_unavailable")
}

func TestSupplierCoreCacheFallbackStatuses(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 401, 403, 400, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", false)
			b := f.binding(t, s.ID, f.instance(t, 1).Id)
			p, _ := f.login(t, s.Username)
			_, err := f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
			require.NoError(t, err)
			f.now = f.now.Add(31 * time.Second)
			f.r.readErr = &RemoteError{Status: status, Code: "test_remote_error"}
			data, err := f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
			if status == 408 || status == 429 || status >= 500 {
				require.NoError(t, err)
				require.Equal(t, true, data["stale"])
			} else {
				requireCoreError(t, err, status, "test_remote_error")
				require.Empty(t, f.s.cache)
				f.r.readErr = &RemoteError{Status: 503, Code: "after_auth_failure"}
				_, err = f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
				requireCoreError(t, err, 503, "after_auth_failure")
			}
		})
	}
}

func TestSupplierCoreReadRechecksRevocation(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", false)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, s.Username)
	f.r.onRead = func() { require.NoError(t, f.s.Revoke(s.ID)) }
	_, err := f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
	requireCoreError(t, err, 401, "supplier_unauthenticated")
	reads := f.r.reads
	_, err = f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
	requireCoreError(t, err, 401, "supplier_unauthenticated")
	require.Equal(t, reads, f.r.reads)
}

func TestSupplierCoreCacheIsolationAndWriteInvalidation(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", true)
	other := f.supplier(t, "other", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	b2 := f.binding(t, other.ID, f.instance(t, 2).Id)
	p, _ := f.login(t, s.Username)
	op, _ := f.login(t, other.Username)
	ctx := context.Background()
	read := func(principal *Principal, bindingID int64, query url.Values) {
		_, err := f.s.Read(ctx, principal, bindingID, "proxies", query, false)
		require.NoError(t, err)
	}
	read(p, b.ID, nil)
	read(op, b2.ID, nil)
	read(p, b.ID, url.Values{"page": {"2"}})
	require.Equal(t, 3, f.r.reads)
	read(p, b.ID, nil)
	read(op, b2.ID, nil)
	require.Equal(t, 3, f.r.reads)
	_, err := f.s.ProxyWrite(ctx, p, b.ID, "PATCH", "proxy-1", "", map[string]any{"status": "disabled"})
	require.NoError(t, err)
	var stored model.SupplierBinding
	require.NoError(t, f.db.First(&stored, b.ID).Error)
	require.Equal(t, b.CacheVersion+1, stored.CacheVersion)
	read(op, b2.ID, nil)
	require.Equal(t, 3, f.r.reads, "other supplier cache survives invalidation")
	read(p, b.ID, nil)
	read(p, b.ID, url.Values{"page": {"2"}})
	require.Equal(t, 5, f.r.reads)
	_, err = f.s.SaveBinding(ctx, s.ID, b.ID, BindingInput{InstanceID: b.InstanceID})
	require.NoError(t, err)
	read(p, b.ID, nil)
	require.Equal(t, 6, f.r.reads, "new binding revision cannot reuse old cache")
	require.NoError(t, f.db.Model(&model.Supplier{}).Where("id = ?", s.ID).UpdateColumn("manage_proxies", false).Error)
	_, err = f.s.Read(ctx, p, b.ID, "proxies", nil, false)
	requireCoreError(t, err, 403, "supplier_permission_denied")
	require.Equal(t, 6, f.r.reads, "current permissions apply before serving cached results")
}

func TestSupplierCoreCacheCapacityEvictsOldest(t *testing.T) {
	for _, limit := range []string{"items", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", false)
			b := f.binding(t, s.ID, f.instance(t, 1).Id)
			p, _ := f.login(t, s.Username)
			if limit == "items" {
				for n := 0; n < 256; n++ {
					f.s.cache[fmt.Sprintf("seed-%d", n)] = cacheEntry{Data: []byte(`{}`), At: f.now.Add(time.Duration(n-256) * time.Second)}
				}
			} else {
				payload := make([]byte, 32*1024*1024)
				f.s.cache["seed-0"] = cacheEntry{Data: payload, At: f.now.Add(-2 * time.Second)}
				f.s.cache["seed-1"] = cacheEntry{Data: payload, At: f.now.Add(-time.Second)}
			}
			_, err := f.s.Read(context.Background(), p, b.ID, "accounts", nil, false)
			require.NoError(t, err)
			require.NotContains(t, f.s.cache, "seed-0")
			require.Contains(t, f.s.cache, "seed-1")
			require.LessOrEqual(t, len(f.s.cache), 256)
			size := 0
			for _, entry := range f.s.cache {
				size += len(entry.Data)
			}
			require.LessOrEqual(t, size, 64*1024*1024)
			count := len(f.s.cache)
			_, err = f.s.Read(context.Background(), p, b.ID, "accounts", nil, true)
			require.NoError(t, err)
			require.Len(t, f.s.cache, count, "replacing an existing entry must not evict an extra entry")
			require.Contains(t, f.s.cache, "seed-1")
			f.now = f.now.Add(15 * time.Minute)
			_, err = f.s.Read(context.Background(), p, b.ID, "accounts", nil, true)
			require.NoError(t, err)
			require.Len(t, f.s.cache, 1, "expired entries are pruned when storing a fresh response")
		})
	}
}

func TestSupplierCoreUploadBindingIDAuditScope(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", true)
	other := f.supplier(t, "other", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, s.Username)
	p2, _ := f.login(t, s.Username)
	op, _ := f.login(t, other.Username)
	response, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	require.NoError(t, err)
	raw := response["flow_id"].(string)
	require.Equal(t, b.ID, f.s.UploadBindingID(p, raw))
	require.Zero(t, f.s.UploadBindingID(p2, raw))
	require.Zero(t, f.s.UploadBindingID(op, raw))
	require.Zero(t, f.s.UploadBindingID(&Principal{Supplier: op.Supplier, Session: p.Session}, raw))
	for _, invalid := range []string{"", "malformed", strings.Repeat("x", 43), digest(raw)} {
		require.Zero(t, f.s.UploadBindingID(p, invalid))
	}
	var flow model.SupplierOAuthFlow
	require.NoError(t, f.db.Where("token_hash = ?", digest(raw)).First(&flow).Error)
	require.Zero(t, flow.ConsumedAt, "audit lookup must not consume the flow")
	require.NotEmpty(t, flow.Ciphertext)
	require.NoError(t, f.db.Model(&flow).Updates(map[string]any{"consumed_at": f.now.Unix(), "ciphertext": ""}).Error)
	require.Equal(t, b.ID, f.s.UploadBindingID(p, raw), "replay failures still have an audit binding")
	f.now = f.now.Add(10 * time.Minute)
	require.Equal(t, b.ID, f.s.UploadBindingID(p, raw), "expired flow failures still have an audit binding")
	require.Equal(t, 1, f.r.writes, "audit lookup must never dispatch an exchange")
	require.NoError(t, f.s.Logout(p.Session.ID))
	require.Zero(t, f.s.UploadBindingID(p, raw), "deleted flows cannot be resolved")
}

func coreUpload(bindingID int64) UploadInput {
	return UploadInput{BindingID: bindingID, Name: " Frozen account ", ProxyMode: "manual", ProxyID: "proxy-1", GroupIDs: []string{"group-1"}, PolicyID: "policy-1", TemplateID: "template-1", MaxRPM: 12, MaxTPM: 34, MaxConcurrent: 2, MaxSessions: 3}
}

func TestSupplierCoreOAuthFrozenSingleUseAndSessionBound(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, s.Username)
	p2, _ := f.login(t, s.Username)
	input := coreUpload(b.ID)
	input.MaxRPM, input.MaxSessions = 99, 0
	expected := input.payload()
	expected["group_ids"] = []string{"group-1"}
	response, err := f.s.StartUpload(context.Background(), p, input)
	require.NoError(t, err)
	raw := response["flow_id"].(string)
	require.Len(t, raw, 43)
	require.Equal(t, f.now.Add(10*time.Minute).Unix(), response["expires_at"])
	var flow model.SupplierOAuthFlow
	require.NoError(t, f.db.Where("token_hash = ?", digest(raw)).First(&flow).Error)
	require.Equal(t, p.Session.ID, flow.SessionID)
	require.Equal(t, b.Revision, flow.BindingRevision)
	require.NotContains(t, flow.Ciphertext, "test-state")
	require.NotContains(t, flow.Ciphertext, "Frozen account")
	input.Name = "tampered"
	input.GroupIDs[0] = "tampered-group"
	input.PolicyID, input.MaxRPM, input.MaxTPM, input.MaxConcurrent, input.MaxSessions = "tampered-policy", 0, 0, 0, 50
	_, err = f.s.Exchange(context.Background(), p2, raw, "code#test-state")
	requireCoreError(t, err, 409, "supplier_oauth_flow_expired_or_used")
	_, err = f.s.Exchange(context.Background(), p, raw, "code#wrong-state")
	requireCoreError(t, err, 400, "supplier_oauth_state_mismatch")
	require.Equal(t, 1, f.r.writes)
	_, err = f.s.Exchange(context.Background(), p, raw, "https://oauth.invalid/callback?code=oauth-code&state=test-state")
	require.NoError(t, err)
	require.Equal(t, "account-upload/exchange", f.r.resource)
	expected["code"], expected["pending_state"] = "oauth-code", "test-state"
	encoded, err := json.Marshal(expected)
	require.NoError(t, err)
	actual, err := json.Marshal(f.r.body)
	require.NoError(t, err)
	require.JSONEq(t, string(encoded), string(actual))
	require.NoError(t, f.db.First(&flow, flow.ID).Error)
	require.Equal(t, f.now.Unix(), flow.ConsumedAt)
	require.Empty(t, flow.Ciphertext)
	_, err = f.s.Exchange(context.Background(), p, raw, "code#test-state")
	requireCoreError(t, err, 409, "supplier_oauth_flow_expired_or_used")
	require.Equal(t, 2, f.r.writes)
}

func TestSupplierCoreOAuthExpiryInvalidationAndUncertainResult(t *testing.T) {
	for _, action := range []string{"expiry", "logout", "revoke", "binding-change", "replacement", "network-error"} {
		t.Run(action, func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", true)
			b := f.binding(t, s.ID, f.instance(t, 1).Id)
			p, _ := f.login(t, s.Username)
			response, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
			require.NoError(t, err)
			raw := response["flow_id"].(string)
			switch action {
			case "expiry":
				f.now = f.now.Add(10 * time.Minute)
			case "logout":
				require.NoError(t, f.s.Logout(p.Session.ID))
			case "revoke":
				require.NoError(t, f.s.Revoke(s.ID))
			case "binding-change":
				_, err = f.s.SaveBinding(context.Background(), s.ID, b.ID, BindingInput{InstanceID: b.InstanceID})
				require.NoError(t, err)
			case "replacement":
				_, err = f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
				require.NoError(t, err)
			case "network-error":
				f.r.writeErr = &RemoteError{Status: 504, Code: "uncertain_exchange"}
				_, err = f.s.Exchange(context.Background(), p, raw, "code#test-state")
				requireCoreError(t, err, 504, "uncertain_exchange")
			}
			writes := f.r.writes
			_, err = f.s.Exchange(context.Background(), p, raw, "code#test-state")
			requireCoreError(t, err, 409, "supplier_oauth_flow_expired_or_used")
			require.Equal(t, writes, f.r.writes)
		})
	}
}

func TestSupplierCoreMalformedOAuthCallbacks(t *testing.T) {
	for _, callback := range []string{
		"", "code", "#test-state", "code#wrong-state", "code#test-state#extra",
		"code\r\ninjected#test-state", strings.Repeat("x", 8193),
		"https://oauth.invalid/callback?state=test-state", "https://oauth.invalid/callback?code=code",
		"https://[invalid?code=code&state=test-state",
		"https://oauth.invalid/callback?code=first&code=second&state=test-state",
		"https://oauth.invalid/callback?code=code&state=test-state&state=other",
		"https://oauth.invalid/callback?code=code&state=test-state&bad=%ZZ",
	} {
		t.Run(fmt.Sprintf("callback-%d", len(callback))+"-"+digest(callback)[:8], func(t *testing.T) {
			_, err := callbackCode(callback, "test-state")
			requireCoreError(t, err, 400, "")
		})
	}
	for _, callback := range []string{"code#test-state", "https://oauth.invalid/callback?code=code&state=test-state"} {
		code, err := callbackCode(callback, "test-state")
		require.NoError(t, err)
		require.Equal(t, "code", code)
	}
}

func TestSupplierCoreProxyInputRestrictions(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, s.Username)
	ctx := context.Background()
	for _, tc := range []struct {
		name, method, id, action string
		body                     map[string]any
	}{
		{"empty-import", "POST", "", "", map[string]any{"text": ""}},
		{"non-string-import", "POST", "", "", map[string]any{"text": 123}},
		{"large-import", "POST", "", "", map[string]any{"text": strings.Repeat("x", 256*1024+1)}},
		{"too-many-lines", "POST", "", "", map[string]any{"text": strings.Repeat("proxy\n", 1000)}},
		{"bad-status", "PATCH", "proxy-1", "", map[string]any{"status": "deleted"}},
		{"traversal", "DELETE", "../other", "", nil},
		{"query-injection", "DELETE", "proxy?user_id=other", "", nil},
		{"method", "PUT", "proxy-1", "", map[string]any{"owner_id": "other"}},
		{"action", "POST", "proxy-1", "arbitrary", nil},
		{"patch-collection", "PATCH", "", "", map[string]any{"status": "enabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := f.r.writes
			_, err := f.s.ProxyWrite(ctx, p, b.ID, tc.method, tc.id, tc.action, tc.body)
			requireCoreError(t, err, 400, "")
			require.Equal(t, writes, f.r.writes)
		})
	}
	_, err := f.s.ProxyWrite(ctx, p, b.ID, "POST", "", "", map[string]any{"text": "http://proxy.invalid:8080", "supplier_id": 999, "url": "http://untrusted.invalid"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"text": "http://proxy.invalid:8080"}, f.r.body)
	_, err = f.s.ProxyWrite(ctx, p, b.ID, "PATCH", "proxy-1", "", map[string]any{"status": "disabled", "owner_id": "other"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"status": "disabled"}, f.r.body)
	_, err = f.s.ProxyWrite(ctx, p, b.ID, "POST", "proxy-1", "test", nil)
	require.NoError(t, err)
	require.Equal(t, "proxies/proxy-1/test", f.r.resource)
	_, err = f.s.ProxyWrite(ctx, p, b.ID, "DELETE", "proxy-1", "", nil)
	require.NoError(t, err)
}

func TestSupplierCoreQueryRestrictions(t *testing.T) {
	for _, q := range []url.Values{
		{"supplier_id": {"2"}}, {"user_id": {"other"}}, {"url": {"https://other.invalid"}},
		{"page": {"1", "2"}}, {"page": {"0"}}, {"page": {"100001"}}, {"page_size": {"101"}},
		{"page": {"1.5"}}, {"search": {strings.Repeat("x", 201)}},
	} {
		requireCoreError(t, validateQuery("accounts", q), 400, "")
	}
	for _, days := range []string{"", "0", "2", "31"} {
		requireCoreError(t, validateQuery("usage", url.Values{"days": {days}}), 400, "supplier_invalid_days")
	}
	for _, days := range []string{"1", "7", "30"} {
		require.NoError(t, validateQuery("usage", url.Values{"days": {days}}))
	}
}
