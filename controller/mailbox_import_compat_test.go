package controller

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/stretchr/testify/require"
)

func TestMailboxImportCompatibilityRequest(t *testing.T) {
	r, _, _ := mailboxControllerFixture(t)
	line := "compat@example.test|password|recovery@example.test|GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ|extra"
	for _, ignore := range []bool{false, true} {
		body, err := json.Marshal(map[string]any{"format": "text", "text": line, "ignore_extra_fields": ignore})
		require.NoError(t, err)
		w := mailboxRequest(r, "POST", "/api/mailbox-management/imports/preview", string(body), nil, "")
		require.Equal(t, 200, w.Code)
		var response struct {
			Data mailbox.ImportPreview `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, ignore, response.Data.Valid)
	}
	for _, value := range []string{"true", "false", "unexpected"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("ignore_extra_fields", value))
		file, err := writer.CreateFormFile("file", "sample.txt")
		require.NoError(t, err)
		_, err = file.Write([]byte(line))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		req := httptest.NewRequest("POST", "https://mailbox.example/api/mailbox-management/imports/preview", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", "https://mailbox.example")
		req.Header.Set("X-Mailbox-Request", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if value == "unexpected" {
			require.Equal(t, 400, w.Code)
			continue
		}
		require.Equal(t, 200, w.Code)
		var response struct {
			Data mailbox.ImportPreview `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, value == "true", response.Data.Valid)
	}
}
