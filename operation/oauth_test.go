package operation

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/oauth"
)

// TestFailClosedMCPTRS3 verifies T-RS-3: fail-closed /mcp in OAuth mode (RS-3).
func TestFailClosedMCPTRS3(t *testing.T) {
	origClientID := flag.OAuthClientID
	origPublicURL := flag.OAuthPublicURL
	origToken := flag.Token
	t.Cleanup(func() {
		flag.OAuthClientID = origClientID
		flag.OAuthPublicURL = origPublicURL
		flag.Token = origToken
	})

	flag.OAuthClientID = "test-oauth-client"
	flag.OAuthPublicURL = "https://mcp.example.com"
	flag.Token = "configured-server-pat"

	s := newMCPServer("test-version")
	var innerInvoked bool
	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			innerInvoked = true
			next.ServeHTTP(w, r)
		})
	}
	server := newHTTPServerWithHandler("127.0.0.1:0", s, wrap)

	expectedNoTokenAuthHeader := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, flag.OAuthPublicURL)
	expectedTokenAuthHeader := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", error="invalid_token"`, flag.OAuthPublicURL)

	tests := []struct {
		name               string
		method             string
		path               string
		authHeader         string
		originHeader       string
		wantStatus         int
		wantAuthHeader     string
		assertHandlerNever bool
	}{
		{
			name:               "no token receives 401 with resource_metadata",
			path:               "/mcp",
			authHeader:         "",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedNoTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "GET on /mcp without token receives 401",
			method:             http.MethodGet,
			path:               "/mcp",
			authHeader:         "",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedNoTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "DELETE on /mcp without token receives 401",
			method:             http.MethodDelete,
			path:               "/mcp",
			authHeader:         "",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedNoTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "random bearer token receives 401 with invalid_token",
			path:               "/mcp",
			authHeader:         "Bearer random-invalid-token",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "valid gitea PAT format 'token <pat>' receives 401 with invalid_token",
			path:               "/mcp",
			authHeader:         "token valid-gitea-pat-12345",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "valid gitea PAT format 'Bearer <pat>' receives 401 with invalid_token",
			path:               "/mcp",
			authHeader:         "Bearer valid-gitea-pat-12345",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "request without Origin header receives 401 (not 403)",
			path:               "/mcp",
			authHeader:         "",
			originHeader:       "",
			wantStatus:         http.StatusUnauthorized,
			wantAuthHeader:     expectedNoTokenAuthHeader,
			assertHandlerNever: true,
		},
		{
			name:               "request to /mcp/ returns 404 and never invokes inner handler",
			path:               "/mcp/",
			authHeader:         "",
			wantStatus:         http.StatusNotFound,
			wantAuthHeader:     "",
			assertHandlerNever: true,
		},
		{
			name:               "request to /MCP returns 404 and never invokes inner handler",
			path:               "/MCP",
			authHeader:         "",
			wantStatus:         http.StatusNotFound,
			wantAuthHeader:     "",
			assertHandlerNever: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			innerInvoked = false
			p := tt.path
			if p == "" {
				p = "/mcp"
			}
			m := tt.method
			if m == "" {
				m = http.MethodPost
			}
			req := httptest.NewRequest(m, p, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			w := httptest.NewRecorder()
			server.Handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("T-RS-3 %s: status = %d, want %d", tt.name, w.Code, tt.wantStatus)
			}

			if gotAuth := w.Header().Get("WWW-Authenticate"); gotAuth != tt.wantAuthHeader {
				t.Errorf("T-RS-3 %s: WWW-Authenticate = %q, want %q", tt.name, gotAuth, tt.wantAuthHeader)
			}

			if tt.assertHandlerNever && innerInvoked {
				t.Errorf("T-RS-3 %s: inner MCP handler was invoked, want never invoked", tt.name)
			}
		})
	}

	// Directly verify that RequireOAuthToken never calls the underlying next handler
	t.Run("inner handler never invoked in OAuth mode", func(t *testing.T) {
		handlerInvoked := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerInvoked = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{PublicURL: flag.OAuthPublicURL}, next)
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if handlerInvoked {
			t.Errorf("T-RS-3: underlying MCP handler was invoked, want never invoked")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("T-RS-3: status = %d, want 401", w.Code)
		}
	})
}
