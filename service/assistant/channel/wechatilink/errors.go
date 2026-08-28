package wechatilink

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorKindInvalid          ErrorKind = "invalid"
	ErrorKindCanceled         ErrorKind = "canceled"
	ErrorKindTimeout          ErrorKind = "timeout"
	ErrorKindDNS              ErrorKind = "dns"
	ErrorKindTLS              ErrorKind = "tls"
	ErrorKindTCP              ErrorKind = "tcp"
	ErrorKindResponseTooLarge ErrorKind = "response_too_large"
	ErrorKindAuthentication   ErrorKind = "authentication"
	ErrorKindRateLimit        ErrorKind = "rate_limit"
	ErrorKindHTTP             ErrorKind = "http"
	ErrorKindDecode           ErrorKind = "decode"
	ErrorKindAPI              ErrorKind = "api"
	ErrorKindSessionExpired   ErrorKind = "session_expired"
)

var (
	ErrResponseTooLarge = errors.New("ilink response is too large")
	ErrSessionExpired   = errors.New("ilink session expired")
	ErrAPI              = errors.New("ilink api error")
)

type Error struct {
	Operation  string
	Kind       ErrorKind
	StatusCode int
	Ret        int
	ErrCode    int
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	detail := ""
	switch {
	case e.StatusCode != 0:
		detail = fmt.Sprintf(" status=%d", e.StatusCode)
	case e.ErrCode != 0:
		detail = fmt.Sprintf(" errcode=%d", e.ErrCode)
	case e.Ret != 0:
		detail = fmt.Sprintf(" ret=%d", e.Ret)
	}
	if e.Message != "" {
		detail += " message=" + e.Message
	}
	return fmt.Sprintf("ilink %s failed (%s)%s", e.Operation, e.Kind, detail)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func KindOf(err error) ErrorKind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	return ""
}
