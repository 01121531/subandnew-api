package supplier

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

type Principal struct {
	Supplier model.Supplier
	Session  model.SupplierSession
}
type loginWindow struct {
	Count int
	Until time.Time
}

func (s *Service) reserveLogin(ctx context.Context, key string) error {
	if common.RedisEnabled && common.RDB != nil {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		count, err := common.RDB.Eval(bounded, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],900) end; return n`, []string{"supplier-login:" + key}).Int()
		if err != nil {
			return fail(503, "supplier_login_limiter_unavailable")
		}
		if count > 5 {
			return fail(429, "supplier_login_rate_limited")
		}
		return nil
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := s.Now()
	for k, v := range s.logins {
		if !v.Until.After(now) {
			delete(s.logins, k)
		}
	}
	v := s.logins[key]
	if v.Count >= 5 {
		return fail(429, "supplier_login_rate_limited")
	}
	if v.Count == 0 {
		if len(s.logins) >= 10000 {
			return fail(429, "supplier_login_rate_limited")
		}
		v.Until = now.Add(15 * time.Minute)
	}
	v.Count++
	s.logins[key] = v
	return nil
}
func (s *Service) clearLogin(ctx context.Context, key string) {
	if common.RedisEnabled && common.RDB != nil {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		common.RDB.Del(bounded, "supplier-login:"+key)
	}
	s.loginMu.Lock()
	delete(s.logins, key)
	s.loginMu.Unlock()
}

func (s *Service) Login(ctx context.Context, username, password, ip string) (*Principal, string, error) {
	username = normalizeUsername(username)
	if !usernamePattern.MatchString(username) || len(password) > 72 {
		return nil, "", fail(401, "supplier_invalid_credentials")
	}
	key := digest(username + "\x00" + ip)
	if err := s.reserveLogin(ctx, key); err != nil {
		return nil, "", err
	}
	var item model.Supplier
	err := s.DB.Where("username = ?", username).First(&item).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}
	// An unknown account performs the same bcrypt work as an existing one.
	hash := item.PasswordHash
	if hash == "" {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	}
	valid := common.ValidatePasswordAndHash(password, hash)
	if err != nil || !item.Enabled || !valid {
		return nil, "", fail(401, "supplier_invalid_credentials")
	}
	if _, err := s.ownerAccess(&item); err != nil {
		return nil, "", err
	}
	raw, err := token()
	if err != nil {
		return nil, "", err
	}
	now := s.Now().Unix()
	session := model.SupplierSession{SupplierID: item.ID, TokenHash: digest(raw), AuthVersion: item.AuthVersion, ExpiresAt: now + int64(SessionTTL/time.Second), LastUsedAt: now}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var current model.Supplier
		if tx.Where("id = ? AND enabled = ? AND auth_version = ?", item.ID, true, item.AuthVersion).First(&current).Error != nil {
			return fail(401, "supplier_invalid_credentials")
		}
		return tx.Create(&session).Error
	})
	if err != nil {
		return nil, "", err
	}
	s.clearLogin(ctx, key)
	if err := s.decorateSupplier(&item); err != nil {
		return nil, "", err
	}
	return &Principal{Supplier: item, Session: session}, raw, nil
}

func (s *Service) Authenticate(raw string) (*Principal, error) {
	if len(raw) != 43 {
		return nil, fail(401, "supplier_unauthenticated")
	}
	var session model.SupplierSession
	if err := s.DB.Where("token_hash = ? AND expires_at > ?", digest(raw), s.Now().Unix()).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(401, "supplier_unauthenticated")
		}
		return nil, err
	}
	var item model.Supplier
	if err := s.DB.Where("id = ? AND enabled = ? AND auth_version = ?", session.SupplierID, true, session.AuthVersion).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(401, "supplier_unauthenticated")
		}
		return nil, err
	}
	if s.Now().Unix()-session.LastUsedAt >= 60 {
		s.DB.Model(&session).UpdateColumn("last_used_at", s.Now().Unix())
	}
	if _, err := s.ownerAccess(&item); err != nil {
		return nil, err
	}
	if err := s.decorateSupplier(&item); err != nil {
		return nil, err
	}
	return &Principal{Supplier: item, Session: session}, nil
}
func CheckCSRF(raw, csrf string) bool {
	return len(csrf) == 64 && subtle.ConstantTimeCompare([]byte(CSRF(raw)), []byte(csrf)) == 1
}
func (s *Service) Logout(sessionID int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.SupplierSession{}, sessionID).Error; err != nil {
			return err
		}
		return tx.Where("session_id = ?", sessionID).Delete(&model.SupplierOAuthFlow{}).Error
	})
}
func (s *Service) authorize(p *Principal, bindingID int64, capability string) (*model.SupplierBinding, error) {
	var session model.SupplierSession
	if s.DB.Where("id = ? AND supplier_id = ? AND expires_at > ?", p.Session.ID, p.Supplier.ID, s.Now().Unix()).First(&session).Error != nil {
		return nil, fail(401, "supplier_unauthenticated")
	}
	item, err := s.Supplier(p.Supplier.ID)
	if err != nil || !item.Enabled || item.AuthVersion != session.AuthVersion {
		return nil, fail(401, "supplier_unauthenticated")
	}
	var b model.SupplierBinding
	if s.DB.Where("id = ? AND supplier_id = ? AND enabled = ?", bindingID, item.ID, true).First(&b).Error != nil {
		return nil, fail(404, "supplier_binding_not_found")
	}
	defaults, err := defaultPolicy(s.DB)
	if err != nil {
		return nil, err
	}
	b.EffectivePolicy = model.ResolveSupplierPolicy(defaults, *item, &b)
	a, err := s.ownerAccess(item)
	if err != nil {
		return nil, err
	}
	if a.CheckInstances([]int64{b.InstanceID}) != nil {
		return nil, fail(403, "supplier_permission_denied")
	}
	intersectPolicy(b.EffectivePolicy, a)
	b.EffectiveNaming = model.ResolveSupplierNaming(*item, &b)
	allowed := false
	switch capability {
	case "accounts":
		allowed = b.EffectivePolicy.Values["view_accounts"]
	case "usage":
		allowed = b.EffectivePolicy.Values["view_usage"]
	case "proxies":
		allowed = b.EffectivePolicy.Values["manage_proxies"]
	case "upload":
		allowed = b.EffectivePolicy.Values["upload_accounts"]
	}
	if !allowed {
		return nil, fail(403, "supplier_permission_denied")
	}
	return &b, nil
}
