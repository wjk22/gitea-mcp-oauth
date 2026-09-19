package oauth_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/oauth"
)

// T-RS-1: Expired, unknown, wrong-audience token -> 401 invalid_token.
func TestT_RS_1_TokenValidation(t *testing.T) {
	origHost := flag.Host
	t.Cleanup(func() { flag.Host = origHost })
	flag.Host = "https://git.example.com"

	publicURL := "https://mcp.example.com"
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	grant := &oauth.Grant{
		ID:                "grant-1",
		GiteaAccessToken:  "valid-gitea-access",
		GiteaRefreshToken: "valid-gitea-refresh",
		ExpiresAt:         now.Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	// Valid token
	validTokenStr := "valid-mcp-access-token"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(validTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  publicURL + "/mcp",
	})

	// Expired token
	expiredTokenStr := "expired-mcp-access-token"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(expiredTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(-10 * time.Minute),
		Audience:  publicURL + "/mcp",
	})

	// Wrong-audience token
	wrongAudTokenStr := "wrong-aud-mcp-access-token"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(wrongAudTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  "https://different-host.net/mcp",
	})

	innerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{
		PublicURL: publicURL,
		GiteaHost: flag.Host,
		Store:     store,
		NowFunc:   func() time.Time { return now },
	}, next)

	cases := []struct {
		name           string
		authHeader     string
		wantStatus     int
		wantAuthHeader string
		wantInner      bool
	}{
		{
			name:           "missing_token",
			authHeader:     "",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp"`,
			wantInner:      false,
		},
		{
			name:           "unknown_token",
			authHeader:     "Bearer random-unknown-token-123456",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`,
			wantInner:      false,
		},
		{
			name:           "expired_token",
			authHeader:     "Bearer " + expiredTokenStr,
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`,
			wantInner:      false,
		},
		{
			name:           "wrong_audience_token",
			authHeader:     "Bearer " + wrongAudTokenStr,
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`,
			wantInner:      false,
		},
		{
			name:           "valid_token",
			authHeader:     "Bearer " + validTokenStr,
			wantStatus:     http.StatusOK,
			wantAuthHeader: "",
			wantInner:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			innerCalled = false
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			gotAuth := rec.Header().Get("WWW-Authenticate")
			if gotAuth != tc.wantAuthHeader {
				t.Errorf("WWW-Authenticate = %q, want %q", gotAuth, tc.wantAuthHeader)
			}
			if innerCalled != tc.wantInner {
				t.Errorf("innerCalled = %v, want %v", innerCalled, tc.wantInner)
			}
		})
	}
}

// T-RS-4: Two concurrent requests for the same grant near expiry -> Exactly one upstream refresh.
func TestT_RS_4_ConcurrentRefresh(t *testing.T) {
	origHost := flag.Host
	t.Cleanup(func() { flag.Host = origHost })

	var refreshCalls atomic.Int64
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			refreshCalls.Add(1)
			// Simulate slight network delay to maximize concurrency overlap
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"refreshed-gitea-access","token_type":"bearer","expires_in":3600,"refresh_token":"refreshed-gitea-refresh"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeGitea.Close()

	flag.Host = fakeGitea.URL
	publicURL := "https://mcp.example.com"
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	// Grant expires in 2 minutes (< 5 min), so UP-4 requires refresh
	grant := &oauth.Grant{
		ID:                "grant-near-expiry",
		GiteaAccessToken:  "old-gitea-access",
		GiteaRefreshToken: "old-gitea-refresh",
		ExpiresAt:         now.Add(2 * time.Minute),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	mcpTokenStr := "valid-mcp-token-for-refresh"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(mcpTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  publicURL + "/mcp",
	})

	var innerCalls atomic.Int64
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	middleware := oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{
		PublicURL:         publicURL,
		GiteaHost:         fakeGitea.URL,
		GiteaClientID:     "gitea-client-id",
		GiteaClientSecret: "gitea-client-secret",
		Store:             store,
		HTTPClient:        fakeGitea.Client(),
		NowFunc:           func() time.Time { return now },
	}, next)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+mcpTokenStr)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				errCh <- fmt.Errorf("request %d failed with status %d: %s", idx, rec.Code, rec.Body.String())
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	if innerCalls.Load() != 2 {
		t.Errorf("expected 2 inner handler calls, got %d", innerCalls.Load())
	}

	// T-RS-4 requirement: Exactly one upstream refresh
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 upstream refresh, got %d", got)
	}

	// Verify grant was updated
	updatedGrant, ok := store.GetGrant(grant.ID)
	if !ok || updatedGrant.GiteaAccessToken != "refreshed-gitea-access" {
		t.Errorf("grant was not updated properly: got %v", updatedGrant)
	}
}

// Gap (f): after a failed upstream refresh deleted the grant, the old access token -> 401.
func TestRequireOAuthToken_FailedRefreshDeletesGrantAndOldTokenReturns401(t *testing.T) {
	origHost := flag.Host
	t.Cleanup(func() { flag.Host = origHost })

	// Fake Gitea returns 400 Bad Request on refresh
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeGitea.Close()

	flag.Host = fakeGitea.URL
	publicURL := "https://mcp.example.com"
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	// Grant expires in 2 minutes (< 5 min), so UP-4 requires refresh
	grant := &oauth.Grant{
		ID:                "grant-failed-refresh",
		GiteaAccessToken:  "old-gitea-access",
		GiteaRefreshToken: "old-gitea-refresh",
		ExpiresAt:         now.Add(2 * time.Minute),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	mcpTokenStr := "mcp-token-for-failed-refresh"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(mcpTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  publicURL + "/mcp",
	})

	innerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{
		PublicURL:         publicURL,
		GiteaHost:         fakeGitea.URL,
		GiteaClientID:     "gitea-client-id",
		GiteaClientSecret: "gitea-client-secret",
		Store:             store,
		HTTPClient:        fakeGitea.Client(),
		NowFunc:           func() time.Time { return now },
	}, next)

	// 1. First request triggers upstream refresh which fails.
	// UP-4: delete grant and all MCP tokens for it; client gets 401.
	req1 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req1.Header.Set("Authorization", "Bearer "+mcpTokenStr)
	rec1 := httptest.NewRecorder()
	middleware.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("first request: expected 401 on failed upstream refresh, got %d", rec1.Code)
	}
	if innerCalled {
		t.Fatal("first request: inner handler should not have been called")
	}

	// Verify grant and token are deleted from store
	if _, ok := store.GetGrant("grant-failed-refresh"); ok {
		t.Fatal("expected grant to be deleted after failed upstream refresh")
	}
	if _, ok := store.GetMCPToken(oauth.HashToken(mcpTokenStr)); ok {
		t.Fatal("expected MCP token to be deleted after failed upstream refresh")
	}

	// 2. Second request presenting the old access token gets 401; inner handler never invoked.
	innerCalled = false
	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.Header.Set("Authorization", "Bearer "+mcpTokenStr)
	rec2 := httptest.NewRecorder()
	middleware.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("second request: expected 401 for old access token, got %d", rec2.Code)
	}
	if innerCalled {
		t.Fatal("second request: inner handler should not have been called")
	}
}

// TestAuthResponseWriter_FlusherAndUnwrap asserts that authResponseWriter implements Unwrap() http.ResponseWriter
// and http.Flusher (delegating Flush() to the underlying writer), enabling http.ResponseController support.
func TestAuthResponseWriter_FlusherAndUnwrap(t *testing.T) {
	origHost := flag.Host
	t.Cleanup(func() { flag.Host = origHost })
	flag.Host = "https://git.example.com"

	publicURL := "https://mcp.example.com"
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	grant := &oauth.Grant{
		ID:                "grant-flush",
		GiteaAccessToken:  "token-1",
		GiteaRefreshToken: "refresh-1",
		ExpiresAt:         now.Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	mcpTokenStr := "mcp-token-flush"
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(mcpTokenStr),
		Type:      oauth.TokenTypeAccess,
		GrantID:   grant.ID,
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  publicURL + "/mcp",
	})

	innerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true

		// 1. Verify Unwrap() returns the underlying writer
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			t.Fatal("expected wrapped writer to implement Unwrap() http.ResponseWriter")
		}
		if unwrapper.Unwrap() == nil {
			t.Fatal("expected Unwrap() to return non-nil ResponseWriter")
		}

		// 2. Verify http.Flusher direct type assertion and Flush()
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected wrapped writer to implement http.Flusher")
		}
		flusher.Flush()

		// 3. Verify http.ResponseController support via Unwrap()
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			t.Fatalf("expected ResponseController.Flush() to succeed, got: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	})

	middleware := oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{
		PublicURL: publicURL,
		GiteaHost: flag.Host,
		Store:     store,
		NowFunc:   func() time.Time { return now },
	}, next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+mcpTokenStr)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if !innerCalled {
		t.Fatal("expected inner handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !rec.Flushed {
		t.Fatal("expected recorder to be flushed")
	}
}
