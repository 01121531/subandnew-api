package channelservice

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelLoginStoresOnlyEncryptedCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = response.Write([]byte(`{"qrcode":"qr-secret","qrcode_img_content":"qr-image"}`))
		case "/ilink/bot/get_qrcode_status":
			require.Equal(t, "qr-secret", request.URL.Query().Get("qrcode"))
			_, _ = response.Write([]byte(`{"status":"confirmed","bot_token":"bot-secret","ilink_bot_id":"bot-1","ilink_user_id":"user-1"}`))
		case "/ilink/bot/getupdates":
			require.Equal(t, "Bearer bot-secret", request.Header.Get("Authorization"))
			_, _ = response.Write([]byte(`{"msgs":[{"seq":1,"message_id":99,"from_user_id":"wx-user","message_type":1,"context_token":"context-secret","item_list":[{"type":1,"msg_id":"item-99","text_item":{"text":"查询实例"}}]}],"get_updates_buf":"cursor-1"}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssistantChannel{}, &model.AssistantChannelSecret{}, &model.AssistantChannelLease{}, &model.AssistantInboundEvent{}))
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{9}, 32)}, "v1")
	require.NoError(t, err)
	service, err := NewService(db, cipher, Config{BaseURL: server.URL, HTTPClient: server.Client()})
	require.NoError(t, err)

	login, err := service.StartLogin(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, loginStatePending, login.State)
	require.Equal(t, "qr-image", login.QRImage)
	var secret model.AssistantChannelSecret
	require.NoError(t, db.Where("channel_id = ?", login.ChannelID).First(&secret).Error)
	require.NotContains(t, secret.Ciphertext, "qr-secret")

	connected, err := service.CheckLogin(t.Context(), login.ChannelID, "")
	require.NoError(t, err)
	require.Equal(t, loginStateConnected, connected.State)
	require.Equal(t, "bot-1", connected.Channel.AccountID)
	require.True(t, connected.Channel.Enabled)
	require.NoError(t, db.Where("channel_id = ?", login.ChannelID).First(&secret).Error)
	require.NotContains(t, secret.Ciphertext, "bot-secret")
	plaintext, err := cipher.Decrypt(channelSecretPurpose, strconv.FormatInt(login.ChannelID, 10), secret.KeyVersion, secret.Ciphertext)
	require.NoError(t, err)
	require.Contains(t, string(plaintext), "bot-secret")
	require.NotContains(t, string(plaintext), "qr-secret")

	encoded, err := json.Marshal(connected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "bot-secret")

	ids, err := service.PollOnce(t.Context(), login.ChannelID)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	duplicateIDs, err := service.PollOnce(t.Context(), login.ChannelID)
	require.NoError(t, err)
	require.Empty(t, duplicateIDs)
	var events []model.AssistantInboundEvent
	require.NoError(t, db.Find(&events).Error)
	require.Len(t, events, 1)
	require.NotContains(t, events[0].Payload, "查询实例")
	require.NotContains(t, events[0].Payload, "context-secret")
	_, inbound, err := service.LoadInbound(t.Context(), ids[0])
	require.NoError(t, err)
	require.Equal(t, "查询实例", inbound.Text)
	require.Equal(t, "context-secret", inbound.ContextToken)
	require.NoError(t, db.Create(&model.AssistantChannelLease{ChannelID: login.ChannelID, OwnerID: "worker", LockedUntil: 1}).Error)
	require.NoError(t, service.RemoveCredential(t.Context(), login.ChannelID, 9))
	var removed model.AssistantChannel
	require.NoError(t, db.First(&removed, login.ChannelID).Error)
	require.False(t, removed.Enabled)
	require.Equal(t, model.AssistantChannelStatusUnbound, removed.Status)
	require.Equal(t, 9, removed.UpdatedBy)
	require.Equal(t, int64(0), countRows(t, db, &model.AssistantChannelSecret{}))
	require.Equal(t, int64(0), countRows(t, db, &model.AssistantChannelLease{}))
}

func TestChannelListDoesNotRequireSecretCipher(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssistantChannel{}))
	require.NoError(t, db.Create(&model.AssistantChannel{
		Type: model.AssistantChannelTypeWechatILink, AccountID: "bot-1", Status: model.AssistantChannelStatusConnected,
	}).Error)

	service, err := NewService(db, nil, Config{})
	require.NoError(t, err)
	channels, err := service.List(t.Context())
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, "bot-1", channels[0].AccountID)
	_, err = service.StartLogin(t.Context(), 1)
	require.ErrorIs(t, err, ErrChannelSecret)
}

func TestCheckLoginTreatsExpiredSessionAsLoginState(t *testing.T) {
	statusRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = response.Write([]byte(`{"qrcode":"expired-qr","qrcode_img_content":"qr-image"}`))
		case "/ilink/bot/get_qrcode_status":
			statusRequests++
			_, _ = response.Write([]byte(`{"ret":1,"errcode":-14,"errmsg":"session expired"}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssistantChannel{}, &model.AssistantChannelSecret{}))
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)}, "v1")
	require.NoError(t, err)
	service, err := NewService(db, cipher, Config{BaseURL: server.URL, HTTPClient: server.Client()})
	require.NoError(t, err)

	login, err := service.StartLogin(t.Context(), 7)
	require.NoError(t, err)
	expired, err := service.CheckLogin(t.Context(), login.ChannelID, "")
	require.NoError(t, err)
	require.Equal(t, loginStateExpired, expired.State)
	require.Equal(t, model.AssistantChannelStatusReauthRequired, expired.Channel.Status)
	require.False(t, expired.Channel.Enabled)
	require.Equal(t, "login_expired", expired.Channel.ReauthReason)

	again, err := service.CheckLogin(t.Context(), login.ChannelID, "")
	require.NoError(t, err)
	require.Equal(t, loginStateExpired, again.State)
	require.Equal(t, 1, statusRequests)

	var secret model.AssistantChannelSecret
	require.NoError(t, db.Where("channel_id = ?", login.ChannelID).First(&secret).Error)
	plaintext, err := cipher.Decrypt(channelSecretPurpose, strconv.FormatInt(login.ChannelID, 10), secret.KeyVersion, secret.Ciphertext)
	require.NoError(t, err)
	require.NotContains(t, string(plaintext), "expired-qr")
}

func countRows(t *testing.T, db *gorm.DB, value any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(value).Count(&count).Error)
	return count
}

func TestValidatedILinkBaseURLRejectsUntrustedRedirects(t *testing.T) {
	value, err := validatedILinkBaseURL("https://ilinkai.weixin.qq.com", "https://region.weixin.qq.com")
	require.NoError(t, err)
	require.Equal(t, "https://region.weixin.qq.com", value)
	_, err = validatedILinkBaseURL("https://ilinkai.weixin.qq.com", "http://region.weixin.qq.com")
	require.Error(t, err)
	_, err = validatedILinkBaseURL("https://ilinkai.weixin.qq.com", "https://weixin.qq.com.attacker.example")
	require.Error(t, err)
	_, err = validatedILinkBaseURL("https://ilinkai.weixin.qq.com", "https://127.0.0.1")
	require.Error(t, err)
}
