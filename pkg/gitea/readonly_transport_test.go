package gitea_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
)

type countingTransport struct {
	calls atomic.Int64
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// T-RO-1: POST, PUT, PATCH, DELETE, OPTIONS and a non-standard method all fail;
// a counting fake transport proves zero dispatches. GET/HEAD pass.
func TestT_RO_1_MutatingMethodsFailAndCountingTransport(t *testing.T) {
	origRO := flag.ReadOnly
	t.Cleanup(func() { flag.ReadOnly = origRO })
	flag.ReadOnly = true

	fake := &countingTransport{}
	client, err := gitea.NewClient("test-token")
	if err != nil {
		t.Fatalf("create gitea client err: %v", err)
	}
	_ = client

	// Direct test on readOnlyTransport with counting fake backend
	guarded := gitea.NewReadOnlyTransport(fake)

	mutatingMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		"PURGE", // non-standard method
	}

	for _, method := range mutatingMethods {
		t.Run("reject_"+method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), method, "http://127.0.0.1:3000/api/v1/user", nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			resp, err := guarded.RoundTrip(req)
			if err == nil {
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				t.Fatalf("expected error for method %s, got nil", method)
			}
			if !errors.Is(err, gitea.ErrReadOnlyMethodNotAllowed) {
				t.Errorf("expected ErrReadOnlyMethodNotAllowed, got %v", err)
			}
		})
	}

	if count := fake.calls.Load(); count != 0 {
		t.Fatalf("expected 0 dispatches to underlying transport for mutating methods, got %d", count)
	}

	// GET and HEAD must pass and increment counter
	allowedMethods := []string{http.MethodGet, http.MethodHead}
	for _, method := range allowedMethods {
		t.Run("allow_"+method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), method, "http://127.0.0.1:3000/api/v1/user", nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			resp, err := guarded.RoundTrip(req)
			if err != nil {
				t.Fatalf("expected method %s to pass, got error: %v", method, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}
		})
	}

	if count := fake.calls.Load(); count != 2 {
		t.Fatalf("expected 2 dispatches to underlying transport for GET and HEAD, got %d", count)
	}
}

// Redirect test: a Gitea response redirecting to another host is refused in read-only mode.
func TestRedirectRefusedToExternalHostInReadOnlyMode(t *testing.T) {
	origRO, origHost := flag.ReadOnly, flag.Host
	t.Cleanup(func() {
		flag.ReadOnly = origRO
		flag.Host = origHost
	})

	flag.ReadOnly = true

	// External target server that should never receive the redirected request
	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer externalServer.Close()

	// Gitea mock server redirecting to external server
	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, externalServer.URL, http.StatusFound)
	}))
	defer giteaServer.Close()

	flag.Host = giteaServer.URL

	// Execute request via DoJSON
	var out map[string]any
	_, err := gitea.DoJSON(context.Background(), http.MethodGet, "test", nil, nil, &out)
	if err == nil {
		t.Fatalf("expected error on redirect to external host %s, got nil", externalServer.URL)
	}

	extURL, _ := url.Parse(externalServer.URL)
	if !strings.Contains(err.Error(), extURL.Host) || !strings.Contains(err.Error(), "refused in read-only mode") {
		t.Errorf("error %q did not mention refused redirect to external host %s", err.Error(), extURL.Host)
	}
}

// TestReadOnlyTransport_PassesPOSTWhenReadOnlyFalse verifies that when flag.ReadOnly is false,
// mutating methods such as POST pass through the transport and dispatch to the underlying transport.
func TestReadOnlyTransport_PassesPOSTWhenReadOnlyFalse(t *testing.T) {
	origRO := flag.ReadOnly
	t.Cleanup(func() { flag.ReadOnly = origRO })
	flag.ReadOnly = false

	fake := &countingTransport{}
	guarded := gitea.NewReadOnlyTransport(fake)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:3000/api/v1/user/repos", strings.NewReader(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := guarded.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected POST to pass when read-only is false, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if count := fake.calls.Load(); count != 1 {
		t.Fatalf("expected 1 dispatch to underlying transport, got %d", count)
	}
}

// TestOAuthClient_BlocksPOSTWhenReadOnlyFalse asserts that an OAuth-mode client blocks POST
// even when the global flag.ReadOnly is false (construction-time enforcement).
func TestOAuthClient_BlocksPOSTWhenReadOnlyFalse(t *testing.T) {
	origRO, origHost := flag.ReadOnly, flag.Host
	t.Cleanup(func() {
		flag.ReadOnly = origRO
		flag.Host = origHost
	})
	flag.ReadOnly = false

	fake := &countingTransport{}
	guarded := gitea.NewEnforcedReadOnlyTransport(fake)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:3000/api/v1/user/repos", strings.NewReader(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := guarded.RoundTrip(req)
	if err == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected POST to be blocked by enforced read-only transport when flag.ReadOnly = false, got nil error")
	}
	if !errors.Is(err, gitea.ErrReadOnlyMethodNotAllowed) {
		t.Errorf("expected ErrReadOnlyMethodNotAllowed, got %v", err)
	}
	if count := fake.calls.Load(); count != 0 {
		t.Fatalf("expected 0 dispatches to underlying transport, got %d", count)
	}
}

// TestClientFromContext_OAuthMode verifies that in OAuth mode:
// 1. If OAuth client is missing from context, an error is returned and it NEVER falls through to PAT.
// 2. If OAuth client is present in context, it is returned directly.
func TestClientFromContext_OAuthMode(t *testing.T) {
	origOAuthClientID := flag.OAuthClientID
	origToken := flag.Token
	origHost := flag.Host
	t.Cleanup(func() {
		flag.OAuthClientID = origOAuthClientID
		flag.Token = origToken
		flag.Host = origHost
	})

	flag.OAuthClientID = "test-oauth-client-id"
	flag.Token = "pat-token-that-must-never-be-used"
	flag.Host = "http://127.0.0.1:3000"

	// 1. Missing from context -> must return error, never fall through to PAT
	client, err := gitea.ClientFromContext(context.Background())
	if err == nil {
		t.Fatal("expected error from ClientFromContext in OAuth mode when client missing from context, got nil")
	}
	if client != nil {
		t.Errorf("expected nil client on missing OAuth client, got non-nil")
	}

	// Also verify with PAT token in context -> must still fail, never fall through
	ctxWithPAT := context.WithValue(context.Background(), mcpContext.TokenContextKey, "ctx-pat-token")
	client, err = gitea.ClientFromContext(ctxWithPAT)
	if err == nil {
		t.Fatal("expected error from ClientFromContext in OAuth mode even if TokenContextKey is set, got nil")
	}
	if client != nil {
		t.Errorf("expected nil client on missing OAuth client, got non-nil")
	}

	// 2. Present in context -> returned directly
	expectedClient, err := gitea.NewOAuthClient("oauth-user-token")
	if err != nil {
		t.Fatalf("NewOAuthClient failed: %v", err)
	}
	ctxWithOAuth := context.WithValue(context.Background(), mcpContext.OAuthClientContextKey, expectedClient)
	client, err = gitea.ClientFromContext(ctxWithOAuth)
	if err != nil {
		t.Fatalf("expected success with OAuth client in context, got: %v", err)
	}
	if client != expectedClient {
		t.Errorf("expected client %p, got %p", expectedClient, client)
	}
}

// TestOAuthClient_RefusesExternalRedirectWhenReadOnlyFalse asserts that an OAuth client
// refuses redirects to an external host even when flag.ReadOnly is false.
func TestOAuthClient_RefusesExternalRedirectWhenReadOnlyFalse(t *testing.T) {
	origRO, origHost := flag.ReadOnly, flag.Host
	t.Cleanup(func() {
		flag.ReadOnly = origRO
		flag.Host = origHost
	})
	flag.ReadOnly = false

	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer externalServer.Close()

	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, externalServer.URL, http.StatusFound)
	}))
	defer giteaServer.Close()

	flag.Host = giteaServer.URL

	client, err := gitea.NewOAuthClient("oauth-token")
	if err != nil {
		t.Fatalf("NewOAuthClient failed: %v", err)
	}

	_, _, err = client.Users.GetMyUserInfo(context.Background())
	if err == nil {
		t.Fatal("expected redirect to external host to fail for OAuth client when flag.ReadOnly = false, got nil")
	}

	extURL, _ := url.Parse(externalServer.URL)
	if !strings.Contains(err.Error(), extURL.Host) || !strings.Contains(err.Error(), "refused in read-only mode") {
		t.Errorf("expected error mentioning refused redirect to %s, got: %v", extURL.Host, err)
	}
}

// TestRedirectRefusedOnSchemeDowngrade verifies that verifyHost and makeCheckRedirectOAuth
// refuse redirects where the scheme differs from the configured Gitea URL (e.g. HTTPS to HTTP downgrade).
func TestRedirectRefusedOnSchemeDowngrade(t *testing.T) {
	origRO, origHost := flag.ReadOnly, flag.Host
	t.Cleanup(func() {
		flag.ReadOnly = origRO
		flag.Host = origHost
	})
	flag.ReadOnly = true
	flag.Host = "https://git.example.com"

	targetURL, _ := url.Parse("http://git.example.com/api/v1/user")
	err := gitea.VerifyHostForTest(flag.Host, targetURL)
	if err == nil {
		t.Fatal("expected HTTPS->HTTP redirect to be refused by verifyHost, got nil")
	}
	if !strings.Contains(err.Error(), `redirect scheme "http" does not match configured scheme "https"`) {
		t.Errorf("expected scheme mismatch error, got: %v", err)
	}

	// Also verify makeCheckRedirectOAuth
	checkOAuth := gitea.MakeCheckRedirectOAuthForTest("https://git.example.com")
	req := httptest.NewRequest(http.MethodGet, "http://git.example.com/api/v1/user", nil)
	via := []*http.Request{httptest.NewRequest(http.MethodGet, "https://git.example.com/api/v1/user", nil)}
	err = checkOAuth(req, via)
	if err == nil {
		t.Fatal("expected HTTPS->HTTP redirect to be refused by makeCheckRedirectOAuth, got nil")
	}
	if !strings.Contains(err.Error(), `redirect scheme "http" does not match configured scheme "https"`) {
		t.Errorf("expected scheme mismatch error from makeCheckRedirectOAuth, got: %v", err)
	}
}
