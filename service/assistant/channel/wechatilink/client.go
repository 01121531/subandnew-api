package wechatilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL          = "https://ilinkai.weixin.qq.com"
	defaultRequestTimeout   = 15 * time.Second
	defaultLongPollTimeout  = 35 * time.Second
	defaultMaxResponseBytes = int64(2 * 1024 * 1024)
	defaultBotType          = 3
	maxLocalTokens          = 10
)

type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

type Config struct {
	BaseURL          string
	Token            string
	HTTPClient       HTTPDoer
	RequestTimeout   time.Duration
	LongPollTimeout  time.Duration
	MaxResponseBytes int64
	BotType          int
	AppID            string
	AppClientVersion uint32
	ChannelVersion   string
	BotAgent         string
}

type Client struct {
	baseURL          *url.URL
	token            string
	httpClient       HTTPDoer
	requestTimeout   time.Duration
	longPollTimeout  time.Duration
	maxResponseBytes int64
	botType          int
	appID            string
	appClientVersion uint32
	baseInfo         BaseInfo
}

func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, &Error{Operation: "new_client", Kind: ErrorKindInvalid, Message: "invalid base URL", Err: err}
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.LongPollTimeout <= 0 {
		config.LongPollTimeout = defaultLongPollTimeout
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.BotType <= 0 {
		config.BotType = defaultBotType
	}
	return &Client{
		baseURL:          parsed,
		token:            strings.TrimSpace(config.Token),
		httpClient:       config.HTTPClient,
		requestTimeout:   config.RequestTimeout,
		longPollTimeout:  config.LongPollTimeout,
		maxResponseBytes: config.MaxResponseBytes,
		botType:          config.BotType,
		appID:            strings.TrimSpace(config.AppID),
		appClientVersion: config.AppClientVersion,
		baseInfo: BaseInfo{
			ChannelVersion: strings.TrimSpace(config.ChannelVersion),
			BotAgent:       strings.TrimSpace(config.BotAgent),
		},
	}, nil
}

func (c *Client) GetQRCode(ctx context.Context, localTokens []string) (*QRCodeResponse, error) {
	if len(localTokens) > maxLocalTokens {
		return nil, &Error{Operation: "get_qrcode", Kind: ErrorKindInvalid, Message: "at most 10 local tokens are allowed"}
	}
	query := url.Values{"bot_type": {strconv.Itoa(c.botType)}}
	request := QRCodeRequest{LocalTokenList: append([]string(nil), localTokens...)}
	var response QRCodeResponse
	if err := c.doJSON(ctx, "get_qrcode", http.MethodPost, "/ilink/bot/get_bot_qrcode?"+query.Encode(), false, c.requestTimeout, request, &response); err != nil {
		return nil, err
	}
	if err := apiError("get_qrcode", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetLoginStatus(ctx context.Context, qrCode string, verifyCode string) (*LoginStatusResponse, error) {
	if strings.TrimSpace(qrCode) == "" {
		return nil, &Error{Operation: "get_login_status", Kind: ErrorKindInvalid, Message: "QR code is required"}
	}
	query := url.Values{"qrcode": {qrCode}}
	if strings.TrimSpace(verifyCode) != "" {
		query.Set("verify_code", verifyCode)
	}
	var response LoginStatusResponse
	if err := c.doJSON(ctx, "get_login_status", http.MethodGet, "/ilink/bot/get_qrcode_status?"+query.Encode(), false, c.requestTimeout, nil, &response); err != nil {
		return nil, err
	}
	if err := apiError("get_login_status", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetUpdates(ctx context.Context, updatesBuffer string) (*GetUpdatesResponse, error) {
	request := GetUpdatesRequest{UpdatesBuffer: updatesBuffer, BaseInfo: c.baseInfo}
	var response GetUpdatesResponse
	if err := c.doJSON(ctx, "get_updates", http.MethodPost, "/ilink/bot/getupdates", true, c.longPollTimeout, request, &response); err != nil {
		return nil, err
	}
	if err := apiError("get_updates", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) SendMessage(ctx context.Context, message Message) (*SendMessageResponse, error) {
	if strings.TrimSpace(message.ToUserID) == "" || strings.TrimSpace(message.ContextToken) == "" {
		return nil, &Error{Operation: "send_message", Kind: ErrorKindInvalid, Message: "recipient and context token are required"}
	}
	request := SendMessageRequest{Message: message, BaseInfo: c.baseInfo}
	var response SendMessageResponse
	if err := c.doJSON(ctx, "send_message", http.MethodPost, "/ilink/bot/sendmessage", true, c.requestTimeout, request, &response); err != nil {
		return nil, err
	}
	if err := apiError("send_message", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetConfig(ctx context.Context, userID string, contextToken string) (*GetConfigResponse, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, &Error{Operation: "get_config", Kind: ErrorKindInvalid, Message: "user ID is required"}
	}
	request := GetConfigRequest{UserID: userID, ContextToken: contextToken, BaseInfo: c.baseInfo}
	var response GetConfigResponse
	if err := c.doJSON(ctx, "get_config", http.MethodPost, "/ilink/bot/getconfig", true, c.requestTimeout, request, &response); err != nil {
		return nil, err
	}
	if err := apiError("get_config", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) SendTyping(ctx context.Context, userID string, typingTicket string, active bool) (*SendTypingResponse, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(typingTicket) == "" {
		return nil, &Error{Operation: "send_typing", Kind: ErrorKindInvalid, Message: "user ID and typing ticket are required"}
	}
	status := TypingStatusTyping
	if !active {
		status = TypingStatusCancel
	}
	request := SendTypingRequest{UserID: userID, TypingTicket: typingTicket, Status: status, BaseInfo: c.baseInfo}
	var response SendTypingResponse
	if err := c.doJSON(ctx, "send_typing", http.MethodPost, "/ilink/bot/sendtyping", true, c.requestTimeout, request, &response); err != nil {
		return nil, err
	}
	if err := apiError("send_typing", response.Ret, response.ErrCode, response.ErrMsg); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) doJSON(ctx context.Context, operation string, method string, path string, authenticated bool, timeout time.Duration, input any, output any) error {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return &Error{Operation: operation, Kind: ErrorKindInvalid, Message: "client is nil"}
	}
	if authenticated && c.token == "" {
		return &Error{Operation: operation, Kind: ErrorKindAuthentication, Message: "bot token is required"}
	}
	requestURL := *c.baseURL
	relative, err := url.Parse(path)
	if err != nil || relative.IsAbs() || relative.Host != "" {
		return &Error{Operation: operation, Kind: ErrorKindInvalid, Message: "invalid endpoint", Err: err}
	}
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(relative.Path, "/")
	requestURL.RawQuery = relative.RawQuery

	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return &Error{Operation: operation, Kind: ErrorKindInvalid, Message: "request encoding failed", Err: err}
		}
		body = bytes.NewReader(encoded)
	}
	requestContext := ctx
	cancel := func() {}
	if timeout > 0 {
		requestContext, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, method, requestURL.String(), body)
	if err != nil {
		return &Error{Operation: operation, Kind: ErrorKindInvalid, Message: "request creation failed", Err: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("iLink-App-Id", c.appID)
	request.Header.Set("iLink-App-ClientVersion", strconv.FormatUint(uint64(c.appClientVersion), 10))
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		request.Header.Set("AuthorizationType", "ilink_bot_token")
		request.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return classifyTransportError(operation, requestContext, err)
	}
	if response == nil || response.Body == nil {
		return &Error{Operation: operation, Kind: ErrorKindHTTP, Message: "empty HTTP response"}
	}
	defer response.Body.Close()
	responseBody, err := readResponseBody(response.Body, c.maxResponseBytes)
	if err != nil {
		kind := ErrorKindTCP
		if errors.Is(err, ErrResponseTooLarge) {
			kind = ErrorKindResponseTooLarge
		}
		return &Error{Operation: operation, Kind: kind, Err: err}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &Error{Operation: operation, Kind: classifyHTTPStatus(response.StatusCode), StatusCode: response.StatusCode}
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return &Error{Operation: operation, Kind: ErrorKindDecode, Message: "invalid JSON response", Err: err}
	}
	return nil
}

func readResponseBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}

func randomWechatUIN() string {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	decimal := strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[:])), 10)
	return base64.StdEncoding.EncodeToString([]byte(decimal))
}

func apiError(operation string, ret int, errCode int, message string) error {
	if ret == 0 && errCode == 0 {
		return nil
	}
	kind := ErrorKindAPI
	cause := ErrAPI
	if errCode == -14 {
		kind = ErrorKindSessionExpired
		cause = ErrSessionExpired
	}
	return &Error{Operation: operation, Kind: kind, Ret: ret, ErrCode: errCode, Message: message, Err: cause}
}

func classifyHTTPStatus(statusCode int) ErrorKind {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorKindAuthentication
	case http.StatusTooManyRequests:
		return ErrorKindRateLimit
	default:
		return ErrorKindHTTP
	}
}

func classifyTransportError(operation string, ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &Error{Operation: operation, Kind: ErrorKindCanceled, Err: err}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Operation: operation, Kind: ErrorKindTimeout, Err: err}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return &Error{Operation: operation, Kind: ErrorKindDNS, Err: err}
	}
	var recordHeaderError tls.RecordHeaderError
	var unknownAuthorityError x509.UnknownAuthorityError
	var certificateInvalidError x509.CertificateInvalidError
	if errors.As(err, &recordHeaderError) || errors.As(err, &unknownAuthorityError) || errors.As(err, &certificateInvalidError) {
		return &Error{Operation: operation, Kind: ErrorKindTLS, Err: err}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return &Error{Operation: operation, Kind: ErrorKindTimeout, Err: err}
	}
	return &Error{Operation: operation, Kind: ErrorKindTCP, Err: err}
}
