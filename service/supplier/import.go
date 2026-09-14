package supplier

import (
	"context"
	"strings"
	"unicode"
)

// Import credentials are request-scoped and never included in frozen OAuth records.
type ImportAccountInput struct {
	UploadInput
	RefreshToken string   `json:"refresh_token,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	SessionKeys  []string `json:"session_keys,omitempty"`
}

func validImportSecret(secret string) bool {
	return secret != "" && len(secret) <= 16384 && !strings.ContainsFunc(secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func (s *Service) ImportAccounts(ctx context.Context, p *Principal, kind string, in ImportAccountInput) (map[string]any, error) {
	if !in.UploadInput.valid() || in.OAuthFlow != "" || (kind != "rt" && kind != "sk") {
		return nil, fail(400, "supplier_invalid_upload_parameters")
	}
	b, err := s.authorize(p, in.BindingID, "upload")
	if err != nil {
		return nil, err
	}
	in.UploadInput, err = applyUploadNaming(in.UploadInput, b, kind == "sk", s.Now())
	if err != nil {
		return nil, err
	}
	body := in.UploadInput.payload()
	if kind == "rt" {
		in.RefreshToken = strings.TrimSpace(in.RefreshToken)
		in.AccessToken = strings.TrimSpace(in.AccessToken)
		if !validImportSecret(in.RefreshToken) || (in.AccessToken != "" && !validImportSecret(in.AccessToken)) || len(in.SessionKeys) != 0 {
			return nil, fail(400, "supplier_invalid_import_credentials")
		}
		body["refresh_token"] = in.RefreshToken
		if in.AccessToken != "" {
			body["access_token"] = in.AccessToken
		}
	} else {
		if in.RefreshToken != "" || in.AccessToken != "" || len(in.SessionKeys) == 0 || len(in.SessionKeys) > 20 {
			return nil, fail(400, "supplier_invalid_session_keys")
		}
		keys := make([]string, 0, len(in.SessionKeys))
		seen := map[string]bool{}
		for _, key := range in.SessionKeys {
			key = strings.TrimSpace(key)
			if !validImportSecret(key) {
				return nil, fail(400, "supplier_invalid_session_keys")
			}
			if !seen[key] {
				keys = append(keys, key)
				seen[key] = true
			}
		}
		body["session_keys"] = keys
	}
	remote, err := s.remoteFor(b)
	if err != nil {
		return nil, err
	}
	current, err := s.authorize(p, b.ID, "upload")
	if err != nil {
		return nil, err
	}
	if current.Revision != b.Revision {
		return nil, fail(409, "supplier_binding_changed")
	}
	if current.EffectiveNaming.Version != in.NamingRevision {
		return nil, fail(409, "supplier_naming_changed")
	}
	if _, err := s.checkUploadMethod(kind, in.PortalRevision); err != nil {
		return nil, err
	}
	defer s.invalidate(b)
	result, err := remote.Write(ctx, "POST", "account-upload/import-"+kind, body)
	if err == nil && result != nil {
		if kind == "sk" {
			result["resolved_name_prefix"] = in.Name
		} else {
			result["resolved_name"] = in.Name
		}
	}
	return result, err
}
