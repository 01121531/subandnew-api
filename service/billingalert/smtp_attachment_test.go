package billingalert

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
)

func TestSMTPAcceptedMessageDoesNotRetryLostQuitResponse(t *testing.T) {
	for _, tt := range []struct{ response, want string }{
		{"250 accepted\r\n", "sent"},
		{"451 temporary rejection\r\n", "temporary"},
		{"550 permanent rejection\r\n", "permanent"},
		{"", "uncertain"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = io.WriteString(conn, "220 localhost ESMTP\r\n")
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						done <- err
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
						_, _ = io.WriteString(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						_, _ = io.WriteString(conn, "354 continue\r\n")
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								done <- err
								return
							}
							if line == ".\r\n" {
								break
							}
						}
						if tt.response != "" {
							_, _ = io.WriteString(conn, tt.response)
						}
						// Close before QUIT even after successful DATA acceptance.
						done <- nil
						return
					default:
						done <- fmt.Errorf("unexpected SMTP command")
						return
					}
				}
			}()
			host, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = deliverSMTP(ctx, &model.SMTPSetting{Host: host, Port: port, FromAddress: "sender@example.com", Security: SMTPSecurityNone}, "", []string{"test@example.com"}, []byte("Subject: test\r\n\r\nbody\r\n"))
			if got := SMTPFailureClass(err); got != tt.want {
				t.Fatalf("got %s want %s (%v)", got, tt.want, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSMTPAttachmentRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 2, 255}, 100)
	message := SMTPMessage{Recipients: []string{"operator@example.com"}, Subject: "账号报表", TextBody: "报表附件", Attachments: []SMTPAttachment{{Name: "账号报表.xlsx", Data: data}}}
	if err := validateSMTPAttachments(message.Attachments); err != nil {
		t.Fatal(err)
	}
	raw := buildSMTPMessage(&model.SMTPSetting{FromAddress: "reports@example.com", FromName: "报表"}, message)
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	body, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(body)
	if string(text) != message.TextBody {
		t.Fatalf("body %q", text)
	}
	attachment, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if attachment.FileName() != "账号报表.xlsx" {
		t.Fatalf("filename %q", attachment.FileName())
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, attachment))
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("invalid attachment: %v", err)
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("extra part: %v", err)
	}
	if got := parsed.Header.Get("To"); got != "operator@example.com" {
		t.Fatal(got)
	}
}

func TestSMTPAttachmentValidation(t *testing.T) {
	for _, attachment := range []SMTPAttachment{
		{Name: "bad\r\nBcc: other.xlsx", Data: []byte("x")},
		{Name: "file.csv", Data: []byte("x")},
		{Name: "../file.xlsx", Data: []byte("x")},
		{Name: "empty.xlsx"},
		{Name: "large.xlsx", Data: make([]byte, MaxSMTPAttachmentBytes+1)},
	} {
		if validateSMTPAttachments([]SMTPAttachment{attachment}) == nil {
			t.Fatalf("accepted %q", attachment.Name)
		}
	}
}

func TestSMTPFailureClassification(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{nil, "sent"},
		{fmt.Errorf("rcpt: %w", &textproto.Error{Code: 450, Msg: "temporary"}), "temporary"},
		{classifySMTPFinalResponse(&textproto.Error{Code: 451, Msg: "temporary"}), "temporary"},
		{classifySMTPFinalResponse(&textproto.Error{Code: 550, Msg: "denied"}), "permanent"},
		{classifySMTPFinalResponse(io.EOF), "uncertain"},
		{&SMTPDeliveryError{Uncertain: true, Cause: &textproto.Error{Code: 450, Msg: "partial"}}, "uncertain"},
		{ErrSMTPNotConfigured, "permanent"},
		{errors.New("transport interrupted"), "uncertain"},
	} {
		if got := SMTPFailureClass(tt.err); got != tt.want {
			t.Fatalf("got %s want %s", got, tt.want)
		}
	}
	err := classifySMTPFinalResponse(errors.New("secret SMTP text"))
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("error leaks SMTP response")
	}
}
