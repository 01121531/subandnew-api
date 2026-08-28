package channelservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const channelSecretPurpose = "wechat-ilink-channel"

const inboundPayloadPurpose = "wechat-ilink-inbound"

const (
	pendingAccountPrefix = "pending-"
	pendingLoginTTL      = 5 * time.Minute
)

var (
	ErrChannelNotFound      = errors.New("assistant channel not found")
	ErrChannelSecret        = errors.New("assistant channel secret is unavailable")
	ErrLoginExpired         = errors.New("assistant channel login expired")
	ErrLoginAlreadyComplete = errors.New("assistant channel login already completed")
)

type Config struct {
	BaseURL    string
	HTTPClient wechatilink.HTTPDoer
}

type Service struct {
	db         *gorm.DB
	cipher     *secrets.Cipher
	baseURL    string
	httpClient wechatilink.HTTPDoer
}

type LoginView struct {
	ChannelID int64                   `json:"channel_id"`
	State     modelLoginState         `json:"state"`
	QRImage   string                  `json:"qr_image,omitempty"`
	ExpiresAt int64                   `json:"expires_at,omitempty"`
	Channel   *model.AssistantChannel `json:"channel,omitempty"`
}

type modelLoginState string

const (
	loginStatePending        modelLoginState = "pending"
	loginStateScanned        modelLoginState = "scanned"
	loginStateVerifyRequired modelLoginState = "verify_required"
	loginStateConnected      modelLoginState = "connected"
	loginStateExpired        modelLoginState = "expired"
)

type storedChannelSecret struct {
	QRCode   string `json:"qr_code,omitempty"`
	BotToken string `json:"bot_token,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
}

type InboundPayload struct {
	Text         string `json:"text"`
	ContextToken string `json:"context_token,omitempty"`
	CreateTimeMS int64  `json:"create_time_ms,omitempty"`
}

func NewService(db *gorm.DB, cipher *secrets.Cipher, config Config) (*Service, error) {
	if db == nil {
		return nil, errors.New("assistant channel database is nil")
	}
	return &Service{db: db, cipher: cipher, baseURL: strings.TrimSpace(config.BaseURL), httpClient: config.HTTPClient}, nil
}

func (s *Service) StartLogin(ctx context.Context, actorID int) (*LoginView, error) {
	if err := s.requireCipher(); err != nil {
		return nil, err
	}
	client, err := s.client("", s.baseURL)
	if err != nil {
		return nil, err
	}
	response, err := client.GetQRCode(ctx, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.QRCode) == "" || strings.TrimSpace(response.QRCodeImageContent) == "" {
		return nil, errors.New("ilink QR response is incomplete")
	}
	channel := model.AssistantChannel{
		Type: model.AssistantChannelTypeWechatILink, AccountID: pendingAccountPrefix + uuid.NewString(),
		Status: model.AssistantChannelStatusQRIssued, Enabled: false, Config: "{}", CreatedBy: actorID, UpdatedBy: actorID,
	}
	secretPayload := storedChannelSecret{QRCode: response.QRCode, BaseURL: normalizedBaseURL(s.baseURL)}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deletePendingLogins(tx, "created_by = ?", actorID); err != nil {
			return err
		}
		if err := tx.Create(&channel).Error; err != nil {
			return err
		}
		secret, err := s.encryptSecret(channel.ID, secretPayload)
		if err != nil {
			return err
		}
		return tx.Create(secret).Error
	})
	if err != nil {
		return nil, err
	}
	return &LoginView{
		ChannelID: channel.ID, State: loginStatePending,
		QRImage: response.QRCodeImageContent, ExpiresAt: time.Now().Add(5 * time.Minute).Unix(), Channel: &channel,
	}, nil
}

func (s *Service) CheckLogin(ctx context.Context, channelID int64, verifyCode string) (*LoginView, error) {
	if err := s.requireCipher(); err != nil {
		return nil, err
	}
	channel, stored, secretRow, err := s.loadChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if stored.QRCode == "" {
		if channel.Status == model.AssistantChannelStatusReauthRequired {
			return &LoginView{ChannelID: channel.ID, State: loginStateExpired, Channel: channel}, nil
		}
		return nil, ErrLoginExpired
	}
	client, err := s.client("", stored.BaseURL)
	if err != nil {
		return nil, err
	}
	response, err := client.GetLoginStatus(ctx, stored.QRCode, strings.TrimSpace(verifyCode))
	if err != nil && !errors.Is(err, wechatilink.ErrSessionExpired) {
		return nil, err
	}
	state, status := loginStateExpired, model.AssistantChannelStatusReauthRequired
	if err == nil {
		state, status = loginStatus(response.Status)
	}
	if state == loginStateExpired {
		if err := s.CancelLogin(ctx, channel.ID); err != nil && !errors.Is(err, ErrChannelNotFound) {
			return nil, err
		}
		return &LoginView{ChannelID: channel.ID, State: loginStateExpired}, nil
	}

	channel.Status = status
	channel.UpdatedBy = channel.CreatedBy
	if state == loginStateConnected {
		if strings.TrimSpace(response.BotToken) == "" || strings.TrimSpace(response.BotID) == "" {
			return nil, errors.New("ilink confirmed login response is incomplete")
		}
		stored.BotToken = response.BotToken
		stored.BotID = response.BotID
		stored.UserID = response.UserID
		redirectURL, err := validatedILinkBaseURL(stored.BaseURL, firstNonEmpty(response.BaseURL, response.RedirectHost, stored.BaseURL))
		if err != nil {
			return nil, err
		}
		stored.BaseURL = redirectURL
		stored.QRCode = ""
		channel.AccountID = response.BotID
		channel.Enabled = true
		channel.ReauthReason = ""
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.AssistantChannel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND enabled = ? AND account_id LIKE ?", channel.ID, false, pendingAccountPrefix+"%").
			First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelNotFound
			}
			return err
		}
		current.Status = channel.Status
		current.UpdatedBy = channel.UpdatedBy
		if state == loginStateConnected {
			current.AccountID = channel.AccountID
			current.Enabled = true
			current.ReauthReason = ""
		}
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		if state != loginStateConnected {
			*channel = current
			return nil
		}
		updated, err := s.encryptSecret(channel.ID, *stored)
		if err != nil {
			return err
		}
		updated.ID = secretRow.ID
		updated.CreatedAt = secretRow.CreatedAt
		if err := tx.Save(updated).Error; err != nil {
			return err
		}
		*channel = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &LoginView{ChannelID: channel.ID, State: state, Channel: channel}, nil
}

func (s *Service) List(ctx context.Context) ([]model.AssistantChannel, error) {
	channels := make([]model.AssistantChannel, 0)
	err := s.db.WithContext(ctx).
		Where("account_id NOT LIKE ?", pendingAccountPrefix+"%").
		Order("id DESC").Find(&channels).Error
	return channels, err
}

// CancelLogin removes an unfinished QR login and all of its temporary state.
// Connected channels must use RemoveCredential so their audit record remains.
func (s *Service) CancelLogin(ctx context.Context, channelID int64) error {
	if channelID <= 0 {
		return ErrChannelNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var channel model.AssistantChannel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&channel, channelID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if channel.Enabled || !strings.HasPrefix(channel.AccountID, pendingAccountPrefix) {
			return ErrLoginAlreadyComplete
		}
		return deletePendingLogins(tx, "id = ?", channelID)
	})
}

// CleanupExpiredLogins removes abandoned QR attempts. The operation is
// idempotent and deliberately excludes every channel that completed login.
func (s *Service) CleanupExpiredLogins(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-pendingLoginTTL).Unix()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deletePendingLogins(tx, "created_at <= ?", cutoff)
	})
}

func deletePendingLogins(tx *gorm.DB, condition string, args ...any) error {
	var ids []int64
	query := tx.Model(&model.AssistantChannel{}).
		Where("enabled = ? AND account_id LIKE ?", false, pendingAccountPrefix+"%")
	if strings.TrimSpace(condition) != "" {
		query = query.Where(condition, args...)
	}
	if err := query.Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return err
	}
	if err := tx.Where("channel_id IN ?", ids).Delete(&model.AssistantChannelLease{}).Error; err != nil {
		return err
	}
	if err := tx.Where("channel_id IN ?", ids).Delete(&model.AssistantChannelSecret{}).Error; err != nil {
		return err
	}
	return tx.Where("id IN ?", ids).Delete(&model.AssistantChannel{}).Error
}

// RemoveCredential revokes a channel locally while retaining its non-secret
// audit record. The encrypted credential and active poll lease are removed.
func (s *Service) RemoveCredential(ctx context.Context, channelID int64, actorID int) error {
	if channelID <= 0 || actorID <= 0 {
		return ErrChannelNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.AssistantChannel{}).Where("id = ?", channelID).Updates(map[string]any{
			"enabled": false, "status": model.AssistantChannelStatusUnbound, "reauth_reason": "credential_removed", "updated_by": actorID,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrChannelNotFound
		}
		if err := tx.Where("channel_id = ?", channelID).Delete(&model.AssistantChannelSecret{}).Error; err != nil {
			return err
		}
		return tx.Where("channel_id = ?", channelID).Delete(&model.AssistantChannelLease{}).Error
	})
}

func (s *Service) ConnectedClient(ctx context.Context, channelID int64) (*wechatilink.Client, *model.AssistantChannel, error) {
	if err := s.requireCipher(); err != nil {
		return nil, nil, err
	}
	channel, stored, _, err := s.loadChannel(ctx, channelID)
	if err != nil {
		return nil, nil, err
	}
	if channel.Status != model.AssistantChannelStatusConnected || !channel.Enabled || stored.BotToken == "" {
		return nil, nil, ErrChannelSecret
	}
	client, err := s.client(stored.BotToken, stored.BaseURL)
	return client, channel, err
}

// PollOnce performs one iLink long poll and commits the resulting durable
// inbox rows and cursor in the same database transaction. Duplicate external
// messages are ignored by the inbox unique key.
func (s *Service) PollOnce(ctx context.Context, channelID int64) ([]int64, error) {
	if err := s.requireCipher(); err != nil {
		return nil, err
	}
	channel, stored, secretRow, err := s.loadChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if channel.Status != model.AssistantChannelStatusConnected || !channel.Enabled || stored.BotToken == "" {
		return nil, ErrChannelSecret
	}
	client, err := s.client(stored.BotToken, stored.BaseURL)
	if err != nil {
		return nil, err
	}
	updates, err := client.GetUpdates(ctx, stored.Cursor)
	if err != nil {
		return nil, err
	}

	createdIDs := make([]int64, 0, len(updates.Messages))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, message := range updates.Messages {
			payload, ok := inboundPayload(message)
			if !ok {
				continue
			}
			event := model.AssistantInboundEvent{
				ChannelID: channel.ID, AccountID: channel.AccountID, ExternalMessageID: externalMessageID(message),
				Seq: message.Sequence, PeerID: message.FromUserID, ExternalUserID: message.FromUserID, Payload: "pending-encryption",
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			plaintext, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			ciphertext, keyVersion, _, err := s.cipher.Encrypt(inboundPayloadPurpose, fmt.Sprint(event.ID), plaintext)
			if err != nil {
				return err
			}
			if err := tx.Model(&event).Updates(map[string]any{"payload": ciphertext, "payload_key_version": keyVersion}).Error; err != nil {
				return err
			}
			createdIDs = append(createdIDs, event.ID)
		}

		stored.Cursor = updates.UpdatesBuffer
		updatedSecret, err := s.encryptSecret(channel.ID, *stored)
		if err != nil {
			return err
		}
		updatedSecret.ID = secretRow.ID
		updatedSecret.CreatedAt = secretRow.CreatedAt
		if err := tx.Save(updatedSecret).Error; err != nil {
			return err
		}
		return tx.Model(channel).Updates(map[string]any{"last_seen_at": time.Now().Unix(), "status": model.AssistantChannelStatusConnected}).Error
	})
	if err != nil {
		return nil, err
	}
	return createdIDs, nil
}

func (s *Service) LoadInbound(ctx context.Context, eventID int64) (*model.AssistantInboundEvent, *InboundPayload, error) {
	if err := s.requireCipher(); err != nil {
		return nil, nil, err
	}
	var event model.AssistantInboundEvent
	if eventID <= 0 || s.db.WithContext(ctx).First(&event, eventID).Error != nil {
		return nil, nil, errors.New("assistant inbound event not found")
	}
	plaintext, err := s.cipher.Decrypt(inboundPayloadPurpose, fmt.Sprint(event.ID), event.PayloadKeyVersion, event.Payload)
	if err != nil {
		return nil, nil, ErrChannelSecret
	}
	var payload InboundPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, nil, ErrChannelSecret
	}
	return &event, &payload, nil
}

func (s *Service) client(token string, baseURL string) (*wechatilink.Client, error) {
	baseURL = normalizedBaseURL(baseURL)
	httpClient := s.httpClient
	if httpClient == nil {
		restricted, err := managedinstance.NewRestrictedHTTPClient(baseURL, 40*time.Second)
		if err != nil {
			return nil, err
		}
		httpClient = restricted
	}
	return wechatilink.NewClient(wechatilink.Config{BaseURL: baseURL, Token: token, HTTPClient: httpClient})
}

func (s *Service) loadChannel(ctx context.Context, channelID int64) (*model.AssistantChannel, *storedChannelSecret, *model.AssistantChannelSecret, error) {
	if channelID <= 0 {
		return nil, nil, nil, ErrChannelNotFound
	}
	var channel model.AssistantChannel
	if err := s.db.WithContext(ctx).First(&channel, channelID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrChannelNotFound
		}
		return nil, nil, nil, err
	}
	var secret model.AssistantChannelSecret
	if err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&secret).Error; err != nil {
		return nil, nil, nil, ErrChannelSecret
	}
	plaintext, err := s.cipher.Decrypt(channelSecretPurpose, fmt.Sprint(channelID), secret.KeyVersion, secret.Ciphertext)
	if err != nil {
		return nil, nil, nil, ErrChannelSecret
	}
	var stored storedChannelSecret
	if err := json.Unmarshal(plaintext, &stored); err != nil {
		return nil, nil, nil, ErrChannelSecret
	}
	return &channel, &stored, &secret, nil
}

func (s *Service) encryptSecret(channelID int64, stored storedChannelSecret) (*model.AssistantChannelSecret, error) {
	if err := s.requireCipher(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	ciphertext, version, fingerprint, err := s.cipher.Encrypt(channelSecretPurpose, fmt.Sprint(channelID), payload)
	if err != nil {
		return nil, err
	}
	return &model.AssistantChannelSecret{ChannelID: channelID, Ciphertext: ciphertext, KeyVersion: version, Fingerprint: fingerprint}, nil
}

func (s *Service) requireCipher() error {
	if s == nil || s.cipher == nil {
		return ErrChannelSecret
	}
	return nil
}

func loginStatus(status wechatilink.LoginStatus) (modelLoginState, string) {
	switch status {
	case wechatilink.LoginStatusScanned, wechatilink.LoginStatusScannedRedirect:
		return loginStateScanned, model.AssistantChannelStatusScanned
	case wechatilink.LoginStatusVerifyCodeRequired, wechatilink.LoginStatusVerifyCodeBlocked:
		return loginStateVerifyRequired, model.AssistantChannelStatusVerifyRequired
	case wechatilink.LoginStatusConfirmed, wechatilink.LoginStatusAlreadyBound:
		return loginStateConnected, model.AssistantChannelStatusConnected
	case wechatilink.LoginStatusExpired:
		return loginStateExpired, model.AssistantChannelStatusReauthRequired
	default:
		return loginStatePending, model.AssistantChannelStatusQRIssued
	}
}

func normalizedBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return wechatilink.DefaultBaseURL
	}
	if !strings.Contains(value, "://") {
		return "https://" + value
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validatedILinkBaseURL(current string, candidate string) (string, error) {
	currentURL, currentErr := url.Parse(normalizedBaseURL(current))
	candidateURL, candidateErr := url.Parse(normalizedBaseURL(candidate))
	if currentErr != nil || candidateErr != nil || currentURL.Host == "" || candidateURL.Host == "" || candidateURL.User != nil || candidateURL.RawQuery != "" || candidateURL.Fragment != "" {
		return "", errors.New("ilink redirect host is invalid")
	}
	if candidateURL.Scheme != "https" && !(currentURL.Scheme == "http" && candidateURL.Scheme == "http") {
		return "", errors.New("ilink redirect must use https")
	}
	currentHost := strings.ToLower(currentURL.Hostname())
	candidateHost := strings.ToLower(candidateURL.Hostname())
	if candidateHost != currentHost && !(currentHost == "ilinkai.weixin.qq.com" && (candidateHost == "weixin.qq.com" || strings.HasSuffix(candidateHost, ".weixin.qq.com"))) {
		return "", errors.New("ilink redirect host is not trusted")
	}
	return strings.TrimRight(candidateURL.String(), "/"), nil
}

func inboundPayload(message wechatilink.Message) (InboundPayload, bool) {
	if message.MessageType != wechatilink.MessageTypeUser || strings.TrimSpace(message.FromUserID) == "" {
		return InboundPayload{}, false
	}
	parts := make([]string, 0, len(message.Items))
	for _, item := range message.Items {
		if item.Type == wechatilink.MessageItemTypeText && item.Text != nil && strings.TrimSpace(item.Text.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text.Text))
		}
	}
	if len(parts) == 0 {
		return InboundPayload{}, false
	}
	return InboundPayload{Text: strings.Join(parts, "\n"), ContextToken: message.ContextToken, CreateTimeMS: message.CreateTimeMS}, true
}

func externalMessageID(message wechatilink.Message) string {
	if message.MessageID > 0 {
		return fmt.Sprint(message.MessageID)
	}
	for _, item := range message.Items {
		if strings.TrimSpace(item.MessageID) != "" {
			return item.MessageID
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%d", message.Sequence, message.CreateTimeMS, message.FromUserID, message.MessageType)))
	return "derived-" + hex.EncodeToString(digest[:16])
}
