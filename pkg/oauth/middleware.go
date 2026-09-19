package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/gitea"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// RequireOAuthTokenConfig holds configuration for the /mcp authentication middleware (RS-1, RS-2, RS-4, UP-4).
type RequireOAuthTokenConfig struct {
	PublicURL         string
	GiteaHost         string
	GiteaClientID     string
	GiteaClientSecret string
	Store             *Store
	HTTPClient        *http.Client
	NowFunc           func() time.Time
}

type authResponseWriter struct {
	http.ResponseWriter
	req          *http.Request
	resourceMeta string
	wroteHeader  bool
}

func (w *authResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if status == http.StatusUnauthorized {
			if w.req.Header.Get("Authorization") != "" {
				w.Header().Set("WWW-Authenticate", w.resourceMeta+`, error="invalid_token"`)
			} else {
				w.Header().Set("WWW-Authenticate", w.resourceMeta)
			}
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *authResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController support.
func (w *authResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush implements http.Flusher by delegating to the underlying writer if supported.
func (w *authResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RequireOAuthToken is the authentication and refresh middleware for /mcp in OAuth mode (RS-1, RS-2, RS-4, UP-4).
func RequireOAuthToken(cfg RequireOAuthTokenConfig, next http.Handler) http.Handler {
	nowFunc := cfg.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}
	resourceMeta := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, cfg.PublicURL)

	verifier := func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if cfg.Store == nil {
			return nil, auth.ErrInvalidToken
		}

		// RS-1, RS-2: Hash token and validate against store
		hash := HashToken(token)
		tok, ok := cfg.Store.GetMCPToken(hash)
		if !ok || tok == nil || tok.Type != TokenTypeAccess {
			return nil, auth.ErrInvalidToken
		}

		// Verify audience
		if tok.Audience != cfg.PublicURL+"/mcp" {
			return nil, auth.ErrInvalidToken
		}

		grant, ok := cfg.Store.GetGrant(tok.GrantID)
		if !ok || grant == nil {
			return nil, auth.ErrInvalidToken
		}

		// UP-4: If Gitea access token expires within 5 minutes, refresh it under grant mutex
		grant.Lock()
		if grant.ExpiresAt.Sub(nowFunc()) <= 5*time.Minute {
			if err := refreshGrantLocked(ctx, grant, cfg, nowFunc); err != nil {
				grant.Unlock()
				cfg.Store.DeleteGrant(grant.ID)
				return nil, auth.ErrInvalidToken
			}
		}
		giteaAccessToken := grant.GiteaAccessToken
		grant.Unlock()

		// RS-4: Build per-request Gitea client with enforced read-only transport
		client, err := gitea.NewOAuthClient(giteaAccessToken)
		if err != nil {
			return nil, fmt.Errorf("create oauth client: %w", err)
		}

		return &auth.TokenInfo{
			Expiration: tok.ExpiresAt,
			UserID:     grant.UserLogin,
			Extra: map[string]any{
				"oauth_client": client,
			},
		}, nil
	}

	bearerMiddleware := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp",
	})

	wrapped := bearerMiddleware(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &authResponseWriter{
			ResponseWriter: w,
			req:            r,
			resourceMeta:   resourceMeta,
		}
		wrapped.ServeHTTP(rw, r)
	})
}

func refreshGrantLocked(ctx context.Context, grant *Grant, cfg RequireOAuthTokenConfig, nowFunc func() time.Time) error {
	// Re-check under lock (T-RS-4)
	if grant.ExpiresAt.Sub(nowFunc()) > 5*time.Minute {
		return nil
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: checkRedirectUpstream(cfg.GiteaHost),
		}
	}

	form := url.Values{
		"client_id":     {cfg.GiteaClientID},
		"client_secret": {cfg.GiteaClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {grant.GiteaRefreshToken},
	}

	tokenURL := strings.TrimRight(cfg.GiteaHost, "/") + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream refresh rejected with status %d", resp.StatusCode)
	}

	var tokenResp giteaTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
		return fmt.Errorf("invalid upstream refresh response: %w", err)
	}

	grant.GiteaAccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		grant.GiteaRefreshToken = tokenResp.RefreshToken
	}
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	grant.ExpiresAt = nowFunc().Add(time.Duration(expiresIn) * time.Second)
	return nil
}
