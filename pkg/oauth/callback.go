package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/gitea"
)

// CallbackConfig holds configuration for the callback endpoint.
type CallbackConfig struct {
	GiteaHost         string
	GiteaClientID     string
	GiteaClientSecret string
	PublicURL         string
	AllowedUser       string
	Store             *Store
	HTTPClient        *http.Client
	NowFunc           func() time.Time
}

type giteaTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// redirectClientSuccess redirects the user agent to the client's redirect URI with code and state (UP-3, AS-5).
func redirectClientSuccess(w http.ResponseWriter, r *http.Request, redirectURI, code, clientState string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeHTMLError(w, http.StatusBadRequest, "Invalid Redirect URI", "Failed to parse client redirect URI.")
		return
	}
	q := u.Query()
	q.Set("code", code)
	if clientState != "" {
		q.Set("state", clientState)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// checkRedirectUpstream ensures upstream OAuth HTTP requests refuse redirects to hosts other than Gitea (fails closed).
func checkRedirectUpstream(giteaHost string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if giteaHost == "" {
			return errors.New("redirect refused: configured Gitea host is empty")
		}
		configured, err := url.Parse(giteaHost)
		if err != nil || configured.Host == "" {
			return errors.New("redirect refused: configured Gitea host is unparsable")
		}
		if req.URL == nil || req.URL.Host == "" {
			return errors.New("redirect refused: target URL is missing host")
		}
		if configured.Scheme == "" || !strings.EqualFold(req.URL.Scheme, configured.Scheme) {
			return fmt.Errorf("redirect scheme %q does not match configured scheme %q", req.URL.Scheme, configured.Scheme)
		}
		if !strings.EqualFold(req.URL.Host, configured.Host) {
			return fmt.Errorf("redirect to external host %q refused", req.URL.Host)
		}
		return nil
	}
}

// HandleCallback handles GET /oauth/callback (UP-2, UP-3, AS-5).
func HandleCallback(cfg CallbackConfig) http.HandlerFunc {
	nowFunc := cfg.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       10 * time.Second, // UP-2: explicit 10s timeout
			CheckRedirect: checkRedirectUpstream(cfg.GiteaHost),
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		upstreamState := q.Get("state")
		code := q.Get("code")
		upstreamError := q.Get("error")

		// UP-2: Validate upstream state. Unknown, expired or reused state -> HTML error, no redirect (T-CB-1).
		if upstreamState == "" {
			writeHTMLError(w, http.StatusBadRequest, "Invalid State", "Missing upstream state.")
			return
		}
		pending, ok := cfg.Store.ConsumePendingAuth(upstreamState)
		if !ok || pending == nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid State", "Upstream state is invalid, expired, or already used.")
			return
		}

		// UP-2: If Gitea returned an error (e.g. access_denied), redirect back to client with access_denied.
		if upstreamError != "" {
			redirectClientError(w, r, pending.RedirectURI, "access_denied", "User denied authorization upstream", pending.ClientState)
			return
		}

		if code == "" {
			writeHTMLError(w, http.StatusBadRequest, "Invalid Request", "No authorization code returned by upstream.")
			return
		}

		// UP-2: Code exchange with Gitea with explicit 10-second timeout.
		tokenURL := strings.TrimRight(cfg.GiteaHost, "/") + "/login/oauth/access_token"
		form := url.Values{
			"client_id":     {cfg.GiteaClientID},
			"client_secret": {cfg.GiteaClientSecret},
			"code":          {code},
			"grant_type":    {"authorization_code"},
			"redirect_uri":  {cfg.PublicURL + "/oauth/callback"},
			"code_verifier": {pending.UpstreamVerifier},
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			writeHTMLError(w, http.StatusInternalServerError, "Internal Error", "Failed to create upstream token request.")
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			writeHTMLError(w, http.StatusBadGateway, "Gateway Error", "Failed to exchange code with Gitea.")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			writeHTMLError(w, http.StatusBadGateway, "Token Exchange Failed",
				fmt.Sprintf("Gitea rejected token exchange with status %d: %s", resp.StatusCode, string(body)))
			return
		}

		var tokenResp giteaTokenResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
			writeHTMLError(w, http.StatusBadGateway, "Invalid Upstream Response", "Malformed token response from Gitea.")
			return
		}

		// UP-3: Fetch authenticated user info to verify login equals --oauth-allowed-user (T-CB-2).
		// Uses the read-only Gitea client rather than an exempted HTTP client.
		roClient, err := gitea.NewOAuthClientWithHost(cfg.GiteaHost, tokenResp.AccessToken)
		if err != nil {
			writeHTMLError(w, http.StatusInternalServerError, "Internal Error", "Failed to create read-only Gitea client.")
			return
		}
		user, _, err := roClient.Users.GetMyUserInfo(r.Context())
		if err != nil {
			writeHTMLError(w, http.StatusBadGateway, "User Info Failed", "Failed to verify user identity with Gitea.")
			return
		}

		if !strings.EqualFold(user.UserName, cfg.AllowedUser) {
			// UP-3: User check failed; tokens are discarded and error page is shown (no redirect).
			writeHTMLError(w, http.StatusForbidden, "Access Denied", "Authenticated user is not authorized to use this connector.")
			return
		}

		// Create Grant
		grantID, err := GenerateRandomString(32)
		if err != nil {
			writeHTMLError(w, http.StatusInternalServerError, "Internal Error", "Failed to generate grant ID.")
			return
		}

		expiresIn := tokenResp.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 3600 // default 1 hour if unspecified
		}
		grant := &Grant{
			ID:                grantID,
			GiteaAccessToken:  tokenResp.AccessToken,
			GiteaRefreshToken: tokenResp.RefreshToken,
			ExpiresAt:         nowFunc().Add(time.Duration(expiresIn) * time.Second),
			UserLogin:         user.UserName,
		}

		// AS-5: Issue 32 random bytes authorization code, single use, 60 seconds (capped at 1000)
		authCodeStr, err := GenerateRandomString(32)
		if err != nil {
			writeHTMLError(w, http.StatusInternalServerError, "Internal Error", "Failed to generate authorization code.")
			return
		}
		if !cfg.Store.PutAuthCode(&AuthCode{
			Code:          authCodeStr,
			ClientID:      pending.ClientID,
			RedirectURI:   pending.RedirectURI,
			CodeChallenge: pending.CodeChallenge,
			GrantID:       grantID,
			ExpiresAt:     nowFunc().Add(60 * time.Second),
		}) {
			writeHTMLError(w, http.StatusServiceUnavailable, "Service Unavailable", "Server busy.")
			return
		}

		// Store grant only after authorization code is successfully created to prevent orphaned grants
		cfg.Store.PutGrant(grant)

		// Redirect to client with code and state
		redirectClientSuccess(w, r, pending.RedirectURI, authCodeStr, pending.ClientState)
	}
}
