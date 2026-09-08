package managedinstance

import (
	"io"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/model"
)

// NewSupplierConnector shares the restricted transport policy, not collector
// cookies or tokens. Supplier calls supply their own 30/60 second contexts.
func NewSupplierConnector(instance *model.ManagedInstance) (*Connector, error) {
	if instance == nil {
		return nil, ErrInvalidInstance
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, err
	}
	policy.MaxBodyBytes = 8 * 1024 * 1024
	copy := *instance
	copy.RequestTimeoutSeconds = 60
	c, err := NewConnector(&copy, policy)
	if err != nil {
		return nil, err
	}
	c.client.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrConnectorRedirect }
	c.client.Transport = supplierNoReplayTransport{c.client.Transport}
	return c, nil
}

type supplierNoReplayTransport struct{ http.RoundTripper }

func (t supplierNoReplayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		req = req.Clone(req.Context())
		// Disable net/http's automatic replay on a stale pooled connection,
		// including DELETE and writes whose original body was empty.
		req.GetBody = nil
		if req.Body == nil || req.Body == http.NoBody {
			req.Body = io.NopCloser(strings.NewReader(""))
			req.ContentLength = -1
		}
	}
	return t.RoundTripper.RoundTrip(req)
}

// ResetSupplierSession clears only this connector's private cookie jar.
func (c *Connector) ResetSupplierSession() error { return c.resetCookies() }
