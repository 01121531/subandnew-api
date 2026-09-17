package billingalert

import (
	"bytes"
	"encoding/base64"
	"errors"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/01121531/subandnew-api/model"
)

const MaxSMTPAttachmentBytes = 15 * 1024 * 1024

type SMTPAttachment struct {
	Name string
	Data []byte
}

// Error never includes an SMTP response, address, password, or message body.
type SMTPDeliveryError struct {
	Uncertain bool
	Cause     error
}

func (e *SMTPDeliveryError) Error() string {
	if e.Uncertain {
		return "smtp_delivery_uncertain"
	}
	return "smtp_delivery_rejected"
}

func (e *SMTPDeliveryError) Unwrap() error { return e.Cause }

func classifySMTPFinalResponse(err error) error {
	var reply *textproto.Error
	return &SMTPDeliveryError{Uncertain: !errors.As(err, &reply), Cause: err}
}

// SMTPFailureClass intentionally permits automatic retries only for explicit
// temporary negative SMTP replies. Transport failures require human review.
func SMTPFailureClass(err error) string {
	if err == nil {
		return "sent"
	}
	var delivery *SMTPDeliveryError
	if errors.As(err, &delivery) && delivery.Uncertain {
		return "uncertain"
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		if reply.Code >= 400 && reply.Code < 500 {
			return "temporary"
		}
		return "permanent"
	}
	if errors.Is(err, ErrSMTPNotConfigured) || errors.Is(err, ErrInvalidBillingInput) {
		return "permanent"
	}
	return "uncertain"
}

func validateSMTPAttachments(attachments []SMTPAttachment) error {
	total := 0
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Name) == "" || strings.ContainsAny(attachment.Name, "\r\n\x00/\\") || !strings.HasSuffix(strings.ToLower(attachment.Name), ".xlsx") {
			return ErrInvalidBillingInput
		}
		if len(attachment.Data) == 0 || len(attachment.Data) > MaxSMTPAttachmentBytes-total {
			return ErrInvalidBillingInput
		}
		total += len(attachment.Data)
	}
	return nil
}

func buildSMTPAttachmentMessage(setting *model.SMTPSetting, message SMTPMessage) []byte {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	from := mail.Address{Name: setting.FromName, Address: setting.FromAddress}
	buffer.WriteString("From: " + from.String() + "\r\n")
	buffer.WriteString("To: " + strings.Join(message.Recipients, ", ") + "\r\n")
	if setting.ReplyTo != "" {
		buffer.WriteString("Reply-To: " + setting.ReplyTo + "\r\n")
	}
	buffer.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", message.Subject) + "\r\n")
	buffer.WriteString("MIME-Version: 1.0\r\n")
	buffer.WriteString("Content-Type: " + mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": writer.Boundary()}) + "\r\n\r\n")
	header := textproto.MIMEHeader{}
	var body bytes.Buffer
	if message.HTMLBody != "" {
		alternative := multipart.NewWriter(&body)
		header.Set("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternative.Boundary()}))
		for _, content := range []struct{ kind, text string }{{"text/plain", message.TextBody}, {"text/html", message.HTMLBody}} {
			subheader := textproto.MIMEHeader{}
			subheader.Set("Content-Type", content.kind+"; charset=UTF-8")
			part, _ := alternative.CreatePart(subheader)
			_, _ = part.Write([]byte(content.text))
		}
		_ = alternative.Close()
	} else {
		header.Set("Content-Type", "text/plain; charset=UTF-8")
		body.WriteString(message.TextBody)
	}
	part, _ := writer.CreatePart(header)
	_, _ = part.Write(body.Bytes())
	for _, attachment := range message.Attachments {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Name}))
		header.Set("Content-Transfer-Encoding", "base64")
		part, _ := writer.CreatePart(header)
		encoded := base64.StdEncoding.EncodeToString(attachment.Data)
		for len(encoded) > 76 {
			_, _ = part.Write([]byte(encoded[:76] + "\r\n"))
			encoded = encoded[76:]
		}
		_, _ = part.Write([]byte(encoded + "\r\n"))
	}
	_ = writer.Close()
	return buffer.Bytes()
}
