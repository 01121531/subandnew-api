package managedinstance

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	managedInstanceAllowedCIDRsEnv = "MANAGED_INSTANCE_ALLOWED_CIDRS"
	defaultConnectorMaxBodyBytes   = int64(2 * 1024 * 1024)
)

var (
	ErrConnectorTargetBlocked = errors.New("managed instance connector target is blocked")
	ErrConnectorRedirect      = errors.New("managed instance connector redirect is blocked")
	ErrConnectorResponseLarge = errors.New("managed instance connector response is too large")
	connectorBlockedNetworks  = []*net.IPNet{
		mustConnectorNetwork("0.0.0.0/8"),
		mustConnectorNetwork("100.64.0.0/10"),
		mustConnectorNetwork("192.0.0.0/24"),
		mustConnectorNetwork("192.0.2.0/24"),
		mustConnectorNetwork("198.18.0.0/15"),
		mustConnectorNetwork("198.51.100.0/24"),
		mustConnectorNetwork("203.0.113.0/24"),
		mustConnectorNetwork("240.0.0.0/4"),
		mustConnectorNetwork("64:ff9b::/96"),
		mustConnectorNetwork("100::/64"),
		mustConnectorNetwork("2001:2::/48"),
		mustConnectorNetwork("2001:db8::/32"),
	}
)

type connectorResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type ConnectorPolicy struct {
	AllowedCIDRs []*net.IPNet
	Resolver     connectorResolver
	MaxBodyBytes int64
}

type Connector struct {
	baseURL      *url.URL
	client       *http.Client
	maxBodyBytes int64
}

type ConnectorResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func ConnectorPolicyFromEnvironment() (ConnectorPolicy, error) {
	allowed, err := parseAllowedCIDRs(os.Getenv(managedInstanceAllowedCIDRsEnv))
	if err != nil {
		return ConnectorPolicy{}, err
	}
	return ConnectorPolicy{AllowedCIDRs: allowed, Resolver: net.DefaultResolver, MaxBodyBytes: defaultConnectorMaxBodyBytes}, nil
}

func NewConnector(instance *model.ManagedInstance, policy ConnectorPolicy) (*Connector, error) {
	if instance == nil {
		return nil, ErrInvalidInstance
	}
	baseURL, err := url.Parse(instance.BaseURL)
	if err != nil || baseURL.Host == "" {
		return nil, ErrInvalidInstance
	}
	if policy.Resolver == nil {
		policy.Resolver = net.DefaultResolver
	}
	if policy.MaxBodyBytes <= 0 {
		policy.MaxBodyBytes = defaultConnectorMaxBodyBytes
	}
	timeout := time.Duration(instance.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !instance.TLSVerify}, // #nosec G402 -- explicitly controlled per managed instance.
	}
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := resolveAllowedIPs(ctx, policy.Resolver, host, policy.AllowedCIDRs)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = errors.New("managed instance connector resolved no addresses")
		}
		return nil, lastErr
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || len(via) == 0 || !sameOrigin(via[0].URL, request.URL) {
				return ErrConnectorRedirect
			}
			return nil
		},
	}
	return &Connector{baseURL: baseURL, client: client, maxBodyBytes: policy.MaxBodyBytes}, nil
}

func (c *Connector) DoJSON(ctx context.Context, method string, path string, headers http.Header, requestBody any) (*ConnectorResponse, error) {
	if c == nil || c.client == nil || c.baseURL == nil {
		return nil, errors.New("managed instance connector is nil")
	}
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, c.maxBodyBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > c.maxBodyBytes {
		return nil, ErrConnectorResponseLarge
	}
	return &ConnectorResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: responseBody}, nil
}

func parseAllowedCIDRs(raw string) ([]*net.IPNet, error) {
	parts := strings.Split(raw, ",")
	allowed := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			part = part + "/" + strconv.Itoa(bits)
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("invalid managed instance allowed CIDR %q", part)
		}
		allowed = append(allowed, network)
	}
	return allowed, nil
}

func resolveAllowedIPs(ctx context.Context, resolver connectorResolver, host string, allowedCIDRs []*net.IPNet) ([]net.IP, error) {
	host = strings.Trim(host, "[]")
	var ips []net.IP
	if parsed := net.ParseIP(host); parsed != nil {
		ips = []net.IP{parsed}
	} else {
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			ips = append(ips, address.IP)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("managed instance connector resolved no addresses")
	}
	for _, ip := range ips {
		if !connectorIPAllowed(ip, allowedCIDRs) {
			return nil, fmt.Errorf("%w: %s", ErrConnectorTargetBlocked, ip.String())
		}
	}
	return ips, nil
}

func connectorIPAllowed(ip net.IP, allowedCIDRs []*net.IPNet) bool {
	for _, network := range allowedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	for _, network := range connectorBlockedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast())
}

func mustConnectorNetwork(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Hostname(), right.Hostname()) && effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if value.Scheme == "https" {
		return "443"
	}
	return "80"
}
