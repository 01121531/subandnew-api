package channel

import (
	"context"
	"time"
)

// Channel isolates the assistant from provider-specific SDKs and protocols.
type Channel interface {
	Type() string
	Login(ctx context.Context, channelID int64) (*LoginSession, error)
	Run(ctx context.Context, channelID int64, handler InboundHandler) error
	Send(ctx context.Context, delivery Delivery) (*SendResult, error)
	SetTyping(ctx context.Context, conversation ConversationRef, active bool) error
	Health(ctx context.Context, channelID int64) Health
}

type InboundHandler func(ctx context.Context, message InboundMessage) error

type LoginState string

const (
	LoginStatePending  LoginState = "pending"
	LoginStateScanned  LoginState = "scanned"
	LoginStateVerified LoginState = "verified"
	LoginStateExpired  LoginState = "expired"
)

type LoginSession struct {
	ID        string
	QRCode    string
	QRImage   string
	State     LoginState
	ExpiresAt time.Time
}

type ConversationRef struct {
	ChannelID    int64
	AccountID    string
	PeerID       string
	ContextToken string
}

type InboundMessage struct {
	ID           string
	Conversation ConversationRef
	Text         string
	ReceivedAt   time.Time
}

type Delivery struct {
	Conversation ConversationRef
	ClientID     string
	Text         string
}

type SendResult struct {
	MessageID string
}

type HealthState string

const (
	HealthUnknown        HealthState = "unknown"
	HealthOnline         HealthState = "online"
	HealthDegraded       HealthState = "degraded"
	HealthReauthRequired HealthState = "reauth_required"
)

type Health struct {
	State     HealthState
	Message   string
	CheckedAt time.Time
}
