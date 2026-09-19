package gitea

import (
	"errors"
	"fmt"
	"net/http"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

const (
	// GuardedTransportHeader is set on requests forwarded by readOnlyTransport in read-only mode.
	GuardedTransportHeader = "X-Guarded-Transport"
	// GuardedTransportValue is the expected header value asserting read-only transport guarding.
	GuardedTransportValue = "read-only"
)

// ErrReadOnlyMethodNotAllowed is returned when a mutating HTTP method is attempted in read-only mode.
var ErrReadOnlyMethodNotAllowed = errors.New("read-only mode: method not allowed")

type readOnlyTransport struct {
	base          http.RoundTripper
	alwaysEnforce bool
}

// NewReadOnlyTransport wraps base with dynamic flag.ReadOnly method enforcement.
func NewReadOnlyTransport(base http.RoundTripper) http.RoundTripper {
	return newReadOnlyTransport(base, false)
}

// NewEnforcedReadOnlyTransport wraps base with always-on read-only method enforcement.
func NewEnforcedReadOnlyTransport(base http.RoundTripper) http.RoundTripper {
	return newReadOnlyTransport(base, true)
}

func newReadOnlyTransport(base http.RoundTripper, alwaysEnforce bool) *readOnlyTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &readOnlyTransport{base: base, alwaysEnforce: alwaysEnforce}
}

// RoundTrip enforces that only GET and HEAD requests can pass in read-only mode.
// Any mutating method returns ErrReadOnlyMethodNotAllowed without calling the underlying transport.
func (t *readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.alwaysEnforce || flag.ReadOnly {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			return nil, fmt.Errorf("%w: %s", ErrReadOnlyMethodNotAllowed, req.Method)
		}
		clonedReq := req.Clone(req.Context())
		clonedReq.Header.Set(GuardedTransportHeader, GuardedTransportValue)
		return t.base.RoundTrip(clonedReq)
	}
	return t.base.RoundTrip(req)
}
