package gitea

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"gitea.dev/sdk"
)

var (
	clientCache     sync.Map // token -> *gitea.Client
	sharedTransOnce sync.Once
	guardedTrans    http.RoundTripper
)

func sharedTransport() http.RoundTripper {
	sharedTransOnce.Do(func() {
		base := http.DefaultTransport.(*http.Transport).Clone()
		if flag.Insecure {
			base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-requested insecure mode
		}
		guardedTrans = NewReadOnlyTransport(base)
	})
	return guardedTrans
}

// NewClient returns a cached *gitea.Client keyed by host+token. The SDK's per-client
// version cache and the shared transport let us reuse keep-alive connections
// and avoid the SDK's /api/v1/version preflight on every tool call.
func NewClient(token string) (*gitea.Client, error) {
	key := flag.Host + "\x00" + token
	if v, ok := clientCache.Load(key); ok {
		return v.(*gitea.Client), nil
	}

	httpClient := &http.Client{
		Transport:     sharedTransport(),
		CheckRedirect: checkRedirect,
	}
	opts := []gitea.ClientOption{
		gitea.SetToken(token),
		gitea.SetHTTPClient(httpClient),
	}
	if flag.Debug {
		opts = append(opts, gitea.SetDebugMode())
	}
	client, err := gitea.NewClient(flag.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("create gitea client err: %w", err)
	}
	client.SetUserAgent("gitea-mcp-server/" + flag.Version)

	actual, _ := clientCache.LoadOrStore(key, client)
	return actual.(*gitea.Client), nil
}

// checkRedirect prevents Go from silently changing mutating requests (POST, PATCH, etc.)
// to GET when following 301/302/303 redirects, which would drop the request body and
// make writes appear to succeed when they didn't. In read-only mode, it also refuses
// redirects to any host other than the configured Gitea host.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
		return http.ErrUseLastResponse
	}
	if flag.ReadOnly {
		if err := verifyGiteaHost(req.URL); err != nil {
			return err
		}
	}
	return nil
}

func verifyHost(host string, targetURL *url.URL) error {
	if host == "" {
		return errors.New("redirect refused: configured host is empty")
	}
	configured, err := url.Parse(host)
	if err != nil || configured.Host == "" {
		return errors.New("redirect refused: configured host is invalid")
	}
	if targetURL == nil || targetURL.Host == "" {
		return errors.New("redirect refused: target URL is missing host")
	}
	if configured.Scheme == "" || !strings.EqualFold(targetURL.Scheme, configured.Scheme) {
		return fmt.Errorf("redirect scheme %q does not match configured scheme %q", targetURL.Scheme, configured.Scheme)
	}
	if !strings.EqualFold(targetURL.Host, configured.Host) {
		return fmt.Errorf("redirect to external host %q refused in read-only mode", targetURL.Host)
	}
	return nil
}

func verifyGiteaHost(targetURL *url.URL) error {
	return verifyHost(flag.Host, targetURL)
}

// NewOAuthClient returns an un-cached *gitea.Client configured with an enforced
// read-only transport and host-verifying redirect check for OAuth mode (RS-4).
// It bypasses clientCache entirely so no per-user token is stored in global state.
func NewOAuthClient(token string) (*gitea.Client, error) {
	return NewOAuthClientWithHost(flag.Host, token)
}

// NewOAuthClientWithHost returns an un-cached *gitea.Client for the specified host
// configured with an enforced read-only transport and host-verifying redirect check for OAuth mode (RS-4).
// It bypasses clientCache entirely so no per-user token is stored in global state.
func NewOAuthClientWithHost(host, token string) (*gitea.Client, error) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	if flag.Insecure {
		base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-requested insecure mode
	}
	httpClient := &http.Client{
		Transport:     NewEnforcedReadOnlyTransport(base),
		CheckRedirect: makeCheckRedirectOAuth(host),
	}
	opts := []gitea.ClientOption{
		gitea.SetToken(token),
		gitea.SetHTTPClient(httpClient),
	}
	if flag.Debug {
		opts = append(opts, gitea.SetDebugMode())
	}
	client, err := gitea.NewClient(host, opts...)
	if err != nil {
		return nil, fmt.Errorf("create gitea oauth client err: %w", err)
	}
	client.SetUserAgent("gitea-mcp-server/" + flag.Version)
	return client, nil
}

func makeCheckRedirectOAuth(host string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
			return http.ErrUseLastResponse
		}
		return verifyHost(host, req.URL)
	}
}

func ClientFromContext(ctx context.Context) (*gitea.Client, error) {
	if flag.OAuthClientID != "" {
		if c, ok := ctx.Value(mcpContext.OAuthClientContextKey).(*gitea.Client); ok && c != nil {
			return c, nil
		}
		return nil, errors.New("oauth client missing from context")
	}
	token, ok := ctx.Value(mcpContext.TokenContextKey).(string)
	if !ok {
		token = flag.Token
	}
	return NewClient(token)
}
