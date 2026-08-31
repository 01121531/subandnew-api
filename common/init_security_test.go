package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSessionSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		valid  bool
	}{
		{name: "missing", secret: ""},
		{name: "short", secret: "short-secret"},
		{name: "public example", secret: "replace-with-a-long-random-string"},
		{name: "repeated pattern", secret: "abcdabcdabcdabcdabcdabcdabcdabcd"},
		{name: "surrounding whitespace", secret: " 0123456789abcdef0123456789ABCDEF "},
		{name: "valid", secret: "0123456789abcdef0123456789ABCDEF", valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSessionSecret(test.secret)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestInitSessionSecurityFailsClosedInProduction(t *testing.T) {
	previousSecret := SessionSecret
	previousSecure := SessionCookieSecure
	previousTrustedURLs := SessionCookieTrustedURLs
	t.Cleanup(func() {
		SessionSecret = previousSecret
		SessionCookieSecure = previousSecure
		SessionCookieTrustedURLs = previousTrustedURLs
	})

	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789ABCDEF")
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "")
	require.ErrorContains(t, initSessionSecurity(), "APP_ENV=production")

	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "https://control.example.com")
	require.NoError(t, initSessionSecurity())
	require.True(t, SessionCookieSecure)
	require.Equal(t, []string{"https://control.example.com"}, SessionCookieTrustedURLs)
}
