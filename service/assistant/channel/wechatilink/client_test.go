package wechatilink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/ilink/bot/get_bot_qrcode" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("bot_type") != "3" {
			t.Fatalf("unexpected bot_type %q", request.URL.Query().Get("bot_type"))
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("QR request must not send a bot token")
		}
		if request.Header.Get("AuthorizationType") != "ilink_bot_token" {
			t.Fatal("missing authorization type")
		}
		encodedUIN := request.Header.Get("X-WECHAT-UIN")
		decodedUIN, err := base64.StdEncoding.DecodeString(encodedUIN)
		if err != nil || len(decodedUIN) == 0 {
			t.Fatalf("invalid X-WECHAT-UIN %q", encodedUIN)
		}
		var body QRCodeRequest
		decodeRequest(t, request, &body)
		if len(body.LocalTokenList) != 2 || body.LocalTokenList[1] != "second" {
			t.Fatalf("unexpected local tokens %#v", body.LocalTokenList)
		}
		writeJSON(response, `{"qrcode":"qr-1","qrcode_img_content":"https://example.test/qr"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{})
	result, err := client.GetQRCode(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QRCode != "qr-1" || result.QRCodeImageContent == "" {
		t.Fatalf("unexpected response %#v", result)
	}
}

func TestGetLoginStatusEscapesParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ilink/bot/get_qrcode_status" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if request.URL.Query().Get("qrcode") != "a&b" || request.URL.Query().Get("verify_code") != "12 34" {
			t.Fatalf("unexpected query %q", request.URL.RawQuery)
		}
		if request.Header.Get("iLink-App-ClientVersion") != "65538" {
			t.Fatalf("unexpected client version %q", request.Header.Get("iLink-App-ClientVersion"))
		}
		writeJSON(response, `{"status":"confirmed","bot_token":"secret","ilink_bot_id":"bot","ilink_user_id":"user","baseurl":"https://ilink.example"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{AppClientVersion: 65538})
	result, err := client.GetLoginStatus(context.Background(), "a&b", "12 34")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LoginStatusConfirmed || result.BotID != "bot" || result.UserID != "user" {
		t.Fatalf("unexpected response %#v", result)
	}
}

func TestGetUpdatesUsesAuthenticationAndLongPollTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ilink/bot/getupdates" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer bot-token" {
			t.Fatalf("unexpected authorization %q", request.Header.Get("Authorization"))
		}
		var body GetUpdatesRequest
		decodeRequest(t, request, &body)
		if body.UpdatesBuffer != "cursor-1" || body.BaseInfo.BotAgent != "SubAndNew/phase0" {
			t.Fatalf("unexpected body %#v", body)
		}
		writeJSON(response, `{"ret":0,"get_updates_buf":"cursor-2","longpolling_timeout_ms":35000,"msgs":[{"message_id":42,"from_user_id":"peer","context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{Token: "bot-token", BotAgent: "SubAndNew/phase0"})
	result, err := client.GetUpdates(context.Background(), "cursor-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdatesBuffer != "cursor-2" || len(result.Messages) != 1 || result.Messages[0].Items[0].Text.Text != "hello" {
		t.Fatalf("unexpected response %#v", result)
	}
}

func TestGetUpdatesTimeoutIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writeJSON(response, `{"ret":0}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{Token: "token", LongPollTimeout: 20 * time.Millisecond})
	_, err := client.GetUpdates(context.Background(), "")
	if KindOf(err) != ErrorKindTimeout {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestSendMessageAndTyping(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.URL.Path
		switch request.URL.Path {
		case "/ilink/bot/sendmessage":
			var body SendMessageRequest
			decodeRequest(t, request, &body)
			if body.Message.ToUserID != "peer" || body.Message.ContextToken != "ctx" || body.Message.Items[0].Text.Text != "answer" {
				t.Fatalf("unexpected message %#v", body.Message)
			}
			writeJSON(response, `{"ret":0}`)
		case "/ilink/bot/getconfig":
			writeJSON(response, `{"ret":0,"typing_ticket":"ticket"}`)
		case "/ilink/bot/sendtyping":
			var body SendTypingRequest
			decodeRequest(t, request, &body)
			if body.Status != TypingStatusCancel || body.TypingTicket != "ticket" {
				t.Fatalf("unexpected typing request %#v", body)
			}
			writeJSON(response, `{"ret":0}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{Token: "token"})
	message := Message{ToUserID: "peer", ContextToken: "ctx", MessageType: MessageTypeBot, MessageState: MessageStateFinish, Items: []MessageItem{{Type: MessageItemTypeText, Text: &TextItem{Text: "answer"}}}}
	if _, err := client.SendMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	config, err := client.GetConfig(context.Background(), "peer", "ctx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendTyping(context.Background(), "peer", config.TypingTicket, false); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		<-requests
	}
}

func TestErrorsAreClassified(t *testing.T) {
	tests := []struct {
		name     string
		response func(http.ResponseWriter)
		kind     ErrorKind
		matches  error
	}{
		{name: "authentication", response: func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }, kind: ErrorKindAuthentication},
		{name: "rate limit", response: func(w http.ResponseWriter) { w.WriteHeader(http.StatusTooManyRequests) }, kind: ErrorKindRateLimit},
		{name: "remote http", response: func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }, kind: ErrorKindHTTP},
		{name: "decode", response: func(w http.ResponseWriter) { _, _ = io.WriteString(w, "not-json") }, kind: ErrorKindDecode},
		{name: "session expired", response: func(w http.ResponseWriter) { writeJSON(w, `{"ret":1,"errcode":-14,"errmsg":"expired"}`) }, kind: ErrorKindSessionExpired, matches: ErrSessionExpired},
		{name: "api", response: func(w http.ResponseWriter) { writeJSON(w, `{"ret":-2,"errmsg":"invalid context"}`) }, kind: ErrorKindAPI, matches: ErrAPI},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { test.response(response) }))
			defer server.Close()
			client := newTestClient(t, server.URL, Config{Token: "token"})
			_, err := client.GetUpdates(context.Background(), "")
			if KindOf(err) != test.kind {
				t.Fatalf("expected %s, got %v", test.kind, err)
			}
			if test.matches != nil && !errors.Is(err, test.matches) {
				t.Fatalf("expected errors.Is(%v), got %v", test.matches, err)
			}
		})
	}
}

func TestResponseBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, strings.Repeat("x", 33))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, Config{Token: "token", MaxResponseBytes: 32})
	_, err := client.GetUpdates(context.Background(), "")
	if KindOf(err) != ErrorKindResponseTooLarge || !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected response-too-large, got %v", err)
	}
}

func TestInjectedHTTPClientAndTransportClassification(t *testing.T) {
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "not found", Name: "ilink.invalid"}
	})
	client, err := NewClient(Config{HTTPClient: doer, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetUpdates(context.Background(), "")
	if KindOf(err) != ErrorKindDNS {
		t.Fatalf("expected DNS error, got %v", err)
	}
}

func TestClientConfigurationAndAuthenticationValidation(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "ftp://ilink.example"}); KindOf(err) != ErrorKindInvalid {
		t.Fatalf("expected invalid base URL, got %v", err)
	}
	client, err := NewClient(Config{HTTPClient: doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request must not be sent without authentication")
		return nil, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetUpdates(context.Background(), ""); KindOf(err) != ErrorKindAuthentication {
		t.Fatalf("expected authentication error, got %v", err)
	}
}

func TestCanceledContextIsClassified(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, err := NewClient(Config{Token: "token", HTTPClient: doerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetUpdates(ctx, "")
	if KindOf(err) != ErrorKindCanceled {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestValidationDoesNotSendRequests(t *testing.T) {
	calls := 0
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected")
	})
	client, err := NewClient(Config{HTTPClient: doer, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetLoginStatus(context.Background(), "", ""); KindOf(err) != ErrorKindInvalid {
		t.Fatalf("unexpected error %v", err)
	}
	if _, err := client.GetQRCode(context.Background(), make([]string, 11)); KindOf(err) != ErrorKindInvalid {
		t.Fatalf("unexpected error %v", err)
	}
	if _, err := client.SendMessage(context.Background(), Message{}); KindOf(err) != ErrorKindInvalid {
		t.Fatalf("unexpected error %v", err)
	}
	if calls != 0 {
		t.Fatalf("validation made %d HTTP calls", calls)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestClient(t *testing.T, baseURL string, overrides Config) *Client {
	t.Helper()
	overrides.BaseURL = baseURL
	if overrides.RequestTimeout == 0 {
		overrides.RequestTimeout = time.Second
	}
	if overrides.LongPollTimeout == 0 {
		overrides.LongPollTimeout = time.Second
	}
	client, err := NewClient(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func decodeRequest(t *testing.T, request *http.Request, target any) {
	t.Helper()
	defer request.Body.Close()
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(response, body)
}
