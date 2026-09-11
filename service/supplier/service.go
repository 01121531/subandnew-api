package supplier

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const SessionTTL = 12 * time.Hour

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string           { return e.Code }
func fail(status int, code string) error { return &Error{status, code} }
func HTTPError(err error) (int, string) {
	var local *Error
	if errors.As(err, &local) {
		return local.Status, local.Code
	}
	var remote *RemoteError
	if errors.As(err, &remote) {
		return remote.Status, remote.Code
	}
	return http.StatusInternalServerError, "supplier_service_unavailable"
}

type Service struct {
	DB            *gorm.DB
	RemoteFactory func(*model.ManagedInstance, string, string, string, string) (Remote, error)
	Now           func() time.Time
	cacheMu       sync.Mutex
	cache         map[string]cacheEntry
	reads         singleflight.Group
	loginMu       sync.Mutex
	logins        map[string]loginWindow
	cleanupMu     sync.Mutex
	lastCleanup   time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{DB: db, RemoteFactory: NewRemote, Now: time.Now, cache: make(map[string]cacheEntry), logins: make(map[string]loginWindow)}
}
func token() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func digest(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func CSRF(raw string) string { return digest("supplier-csrf:v1:" + raw) }
func cipher() (*managedinstance.CredentialCipher, error) {
	return managedinstance.NewCredentialCipherFromEnvironment()
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9@._+\-]{2,95}$`)

func normalizeUsername(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
func validPassword(v string) bool       { return len(v) >= 8 && len(v) <= 72 && strings.TrimSpace(v) != "" }
func (s *Service) Supplier(id int64) (*model.Supplier, error) {
	var item model.Supplier
	if err := s.DB.First(&item, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fail(404, "supplier_not_found")
	} else if err != nil {
		return nil, err
	}
	if err := s.decorateSupplier(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) MaybeCleanup() {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.Now().Sub(s.lastCleanup) < time.Hour {
		return
	}
	now := s.Now().Unix()
	for _, job := range []struct {
		Table     any
		Condition string
		Before    int64
	}{
		{&model.SupplierSession{}, "expires_at <= ?", now},
		{&model.SupplierOAuthFlow{}, "expires_at <= ?", now},
		{&model.SupplierAudit{}, "created_at < ?", now - int64(90*24*time.Hour/time.Second)},
	} {
		var ids []int64
		if s.DB.Model(job.Table).Where(job.Condition, job.Before).Order("id ASC").Limit(1000).Pluck("id", &ids).Error != nil {
			return
		}
		if len(ids) > 0 && s.DB.Where("id IN ?", ids).Delete(job.Table).Error != nil {
			return
		}
	}
	s.lastCleanup = s.Now()
}
