package managedinstance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
)

type staticResolver struct {
	addresses []net.IPAddr
	err       error
}

func (resolver staticResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	return resolver.addresses, resolver.err
}

func TestResolveAllowedIPsBlocksPrivateAndMixedDNS(t *testing.T) {
	_, err := resolveAllowedIPs(context.Background(), staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}, "example.test", nil)
	require.ErrorIs(t, err, ErrConnectorTargetBlocked)

	_, err = resolveAllowedIPs(context.Background(), staticResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("10.0.0.1")},
	}}, "example.test", nil)
	require.ErrorIs(t, err, ErrConnectorTargetBlocked)

	allowed, err := parseAllowedCIDRs("127.0.0.0/8,10.0.0.1")
	require.NoError(t, err)
	ips, err := resolveAllowedIPs(context.Background(), staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}}, "internal.test", allowed)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", ips[0].String())
}

func TestConnectorBlocksSpecialPurposeNetworksUnlessAllowed(t *testing.T) {
	for _, address := range []string{
		"0.1.2.3", "100.64.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1",
		"64:ff9b::1", "100::1", "2001:2::1", "2001:db8::1",
	} {
		t.Run(address, func(t *testing.T) {
			_, err := resolveAllowedIPs(context.Background(), staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP(address)}}}, "special.test", nil)
			require.ErrorIs(t, err, ErrConnectorTargetBlocked)
		})
	}

	allowed, err := parseAllowedCIDRs("100.64.0.0/10")
	require.NoError(t, err)
	_, err = resolveAllowedIPs(context.Background(), staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("100.64.0.1")}}}, "explicit.test", allowed)
	require.NoError(t, err)
}

func TestConnectorLimitsResponsesAndAllowsExplicitLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("123456"))
	}))
	defer server.Close()
	allowed, err := parseAllowedCIDRs("127.0.0.0/8")
	require.NoError(t, err)
	connector, err := NewConnector(&model.ManagedInstance{BaseURL: server.URL, RequestTimeoutSeconds: 2, TLSVerify: true}, ConnectorPolicy{
		AllowedCIDRs: allowed, Resolver: net.DefaultResolver, MaxBodyBytes: 5,
	})
	require.NoError(t, err)
	_, err = connector.DoJSON(context.Background(), http.MethodGet, "/health", nil, nil)
	require.ErrorIs(t, err, ErrConnectorResponseLarge)
}

func TestConnectorBlocksCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", target.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	allowed, err := parseAllowedCIDRs("127.0.0.0/8")
	require.NoError(t, err)
	connector, err := NewConnector(&model.ManagedInstance{BaseURL: source.URL, RequestTimeoutSeconds: 2, TLSVerify: true}, ConnectorPolicy{AllowedCIDRs: allowed})
	require.NoError(t, err)
	_, err = connector.DoJSON(context.Background(), http.MethodGet, "/redirect", nil, nil)
	require.True(t, errors.Is(err, ErrConnectorRedirect))
}

func TestParseAllowedCIDRsRejectsInvalidValue(t *testing.T) {
	_, err := parseAllowedCIDRs("not-a-cidr")
	require.Error(t, err)
}
