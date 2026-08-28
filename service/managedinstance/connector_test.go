package managedinstance

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/model"
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

func TestConnectorAllowsFakeIPRangeOnlyWhenActiveOnLocalInterface(t *testing.T) {
	active := connectorLocalProxyCIDRsFromAddresses([]net.Addr{
		&net.IPNet{IP: net.ParseIP("198.18.0.1"), Mask: net.CIDRMask(30, 32)},
	})
	require.True(t, connectorIPAllowed(net.ParseIP("198.18.42.10"), active))

	inactive := connectorLocalProxyCIDRsFromAddresses([]net.Addr{
		&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)},
	})
	require.False(t, connectorIPAllowed(net.ParseIP("198.18.42.10"), inactive))
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
	require.ErrorIs(t, err, ErrConnectorRedirect)
}

func TestConnectorAllowsSameOriginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			response.Header().Set("Location", "/final")
			response.WriteHeader(http.StatusFound)
			return
		}
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	allowed, err := parseAllowedCIDRs("127.0.0.0/8")
	require.NoError(t, err)
	connector, err := NewConnector(&model.ManagedInstance{BaseURL: server.URL, RequestTimeoutSeconds: 2, TLSVerify: true}, ConnectorPolicy{AllowedCIDRs: allowed})
	require.NoError(t, err)
	result, err := connector.DoJSON(context.Background(), http.MethodGet, "/redirect", nil, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
}

func TestConnectorRejectsDisallowedConfiguredHostAndPort(t *testing.T) {
	_, err := NewConnector(&model.ManagedInstance{BaseURL: "https://api.example.com", TLSVerify: true}, ConnectorPolicy{AllowedHosts: []string{"models.example.com"}})
	require.ErrorIs(t, err, ErrConnectorTargetBlocked)

	ports, err := parseAllowedPorts("8443")
	require.NoError(t, err)
	_, err = NewConnector(&model.ManagedInstance{BaseURL: "https://models.example.com", TLSVerify: true}, ConnectorPolicy{AllowedHosts: []string{"models.example.com"}, AllowedPorts: ports})
	require.ErrorIs(t, err, ErrConnectorTargetBlocked)
}

func TestConnectorPreservesRelativeQueryAndRejectsAbsolutePath(t *testing.T) {
	var requestPath string
	var requestCursor string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.Path
		requestCursor = request.URL.Query().Get("cursor")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	allowed, err := parseAllowedCIDRs("127.0.0.0/8")
	require.NoError(t, err)
	connector, err := NewConnector(&model.ManagedInstance{BaseURL: server.URL + "/base", RequestTimeoutSeconds: 2, TLSVerify: true}, ConnectorPolicy{AllowedCIDRs: allowed})
	require.NoError(t, err)

	_, err = connector.DoJSON(context.Background(), http.MethodGet, "/resources?cursor=next%2Fpage", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "/base/resources", requestPath)
	require.Equal(t, "next/page", requestCursor)

	_, err = connector.DoJSON(context.Background(), http.MethodGet, "https://example.com/resources", nil, nil)
	require.Error(t, err)
}

func TestParseAllowedCIDRsRejectsInvalidValue(t *testing.T) {
	_, err := parseAllowedCIDRs("not-a-cidr")
	require.Error(t, err)
}

func TestConnectorAllowlistParsers(t *testing.T) {
	ports, err := parseAllowedPorts("443,8443")
	require.NoError(t, err)
	require.True(t, connectorHostAllowed("api.example.com", []string{"*.example.com"}))
	require.False(t, connectorHostAllowed("example.com", []string{"*.example.com"}))
	require.False(t, connectorHostAllowed("api.example.com.attacker.test", []string{"*.example.com"}))
	require.True(t, connectorPortAllowed("8443", ports))
	require.False(t, connectorPortAllowed("22", ports))

	_, err = parseAllowedPorts("0,not-a-port")
	require.Error(t, err)
}

func TestConnectorPolicyHonorsEnvironmentAndBlocksPrivateByDefault(t *testing.T) {
	t.Setenv(managedInstanceAllowedCIDRsEnv, "not-a-cidr")
	t.Setenv(managedInstanceAllowedHostsEnv, "api.example.com")
	t.Setenv(managedInstanceAllowedPortsEnv, "443")

	_, err := ConnectorPolicyFromEnvironment()
	require.Error(t, err)

	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	policy, err := ConnectorPolicyFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, []string{"api.example.com"}, policy.AllowedHosts)
	require.Contains(t, policy.AllowedPorts, "443")
	require.True(t, connectorIPAllowed(net.ParseIP("127.0.0.1"), policy.AllowedCIDRs))

	t.Setenv(managedInstanceAllowedCIDRsEnv, "")
	t.Setenv(managedInstanceAllowedHostsEnv, "")
	t.Setenv(managedInstanceAllowedPortsEnv, "")
	policy, err = ConnectorPolicyFromEnvironment()
	require.NoError(t, err)
	require.False(t, connectorIPAllowed(net.ParseIP("127.0.0.1"), policy.AllowedCIDRs))

	_, err = NewConnector(&model.ManagedInstance{
		BaseURL: "http://127.0.0.1:3100", RequestTimeoutSeconds: 2, TLSVerify: true,
	}, policy)
	require.NoError(t, err)
}
