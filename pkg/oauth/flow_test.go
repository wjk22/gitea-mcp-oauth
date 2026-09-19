package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/oauth"
)

// fakeGiteaServer returns an httptest.Server emulating Gitea OAuth and User API.
// The token response matches real Gitea (no scope field).
func newFakeGiteaServer(t *testing.T, allowedUser, accessToken, refreshToken string, expiresIn int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"token_type":    "bearer",
			"expires_in":    expiresIn,
			"refresh_token": refreshToken,
		})
	})

	mux.HandleFunc("/api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.Contains(authHeader, accessToken) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login": allowedUser,
		})
	})

	return httptest.NewServer(mux)
}

// T-DCR-2: Client ID with tampered payload or signature rejected at authorize and token.
func TestT_DCR_2_TamperedClientID(t *testing.T) {
	key := []byte("01234567890123456789012345678901") // 32 bytes
	store := oauth.NewStore(nil)
	publicURL := "https://mcp.example.com"
	redirectURI := "https://claude.ai/api/mcp/auth_callback"

	validID, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID failed: %v", err)
	}

	parts := strings.Split(validID, ".")
	tamperedSig := parts[0] + "." + "corruptedSignature123456789"
	tamperedPayload := "corruptedPayload." + parts[1]

	authHandler := oauth.HandleAuthorize(oauth.AuthorizeConfig{
		SigningKey:    key,
		PublicURL:     publicURL,
		GiteaHost:     "https://git.example.com",
		GiteaClientID: "gitea-client-id",
		Store:         store,
	})

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  publicURL,
		Store:      store,
	})

	// 1. Authorize: Tampered signature -> HTML error, NO redirect
	for _, badID := range []string{tamperedSig, tamperedPayload} {
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id="+url.QueryEscape(badID)+"&redirect_uri="+url.QueryEscape(redirectURI), nil)
		rec := httptest.NewRecorder()
		authHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Authorize with bad client ID: expected status 400, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("Authorize with bad client ID: expected no redirect (Location empty), got %q", loc)
		}
	}

	// 2. Token: Tampered signature -> 401 invalid_client
	for _, badID := range []string{tamperedSig, tamperedPayload} {
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {badID},
			"code":          {"some-code"},
			"redirect_uri":  {redirectURI},
			"code_verifier": {"some-verifier"},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		tokenHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Token with bad client ID: expected status 401, got %d", rec.Code)
		}
		var errResp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp["error"] != "invalid_client" {
			t.Errorf("Token with bad client ID: expected error='invalid_client', got %v", errResp["error"])
		}
	}
}

// T-AZ-1: Authorize with unregistered redirect URI (incl. trailing slash / case / query variants) -> Error page, no Location header.
func TestT_AZ_1_UnregisteredRedirectURI(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	registeredURI := "https://claude.ai/api/mcp/auth_callback"

	clientID, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{registeredURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID failed: %v", err)
	}

	authHandler := oauth.HandleAuthorize(oauth.AuthorizeConfig{
		SigningKey:    key,
		PublicURL:     "https://mcp.example.com",
		GiteaHost:     "https://git.example.com",
		GiteaClientID: "gitea-client-id",
		Store:         store,
	})

	variants := []string{
		"https://claude.ai/api/mcp/auth_callback/",              // trailing slash
		"https://CLAUDE.AI/api/mcp/auth_callback",               // case variant
		"https://claude.ai/api/mcp/auth_callback?extra=1",       // query variant
		"https://chatgpt.com/connector_platform_oauth_redirect", // completely different
		"http://claude.ai/api/mcp/auth_callback",                // http scheme
		"",                                                      // empty
	}

	for _, uri := range variants {
		t.Run("uri_"+uri, func(t *testing.T) {
			target := fmt.Sprintf("/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&code_challenge=xyz&code_challenge_method=S256",
				url.QueryEscape(clientID), url.QueryEscape(uri))
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			authHandler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for unregistered redirect_uri %q, got %d", uri, rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("expected NO Location header for unregistered redirect_uri %q, got %q", uri, loc)
			}
		})
	}
}

// T-AZ-2: Missing PKCE or plain method -> Rejected.
func TestT_AZ_2_MissingOrPlainPKCERejected(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	registeredURI := "https://claude.ai/api/mcp/auth_callback"

	clientID, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{registeredURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID failed: %v", err)
	}

	authHandler := oauth.HandleAuthorize(oauth.AuthorizeConfig{
		SigningKey:    key,
		PublicURL:     "https://mcp.example.com",
		GiteaHost:     "https://git.example.com",
		GiteaClientID: "gitea-client-id",
		Store:         store,
	})

	cases := []struct {
		name      string
		challenge string
		method    string
	}{
		{"missing_both", "", ""},
		{"missing_method", "some-challenge", ""},
		{"plain_method", "some-challenge", "plain"},
		{"unsupported_method", "some-challenge", "S512"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := fmt.Sprintf("/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&code_challenge=%s&code_challenge_method=%s&state=client-state",
				url.QueryEscape(clientID), url.QueryEscape(registeredURI), url.QueryEscape(tc.challenge), url.QueryEscape(tc.method))
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			authHandler(rec, req)

			// Since redirect_uri is trusted, errors redirect back to client with RFC 6749 error
			if rec.Code != http.StatusFound {
				t.Fatalf("expected redirect to client with error, got status %d", rec.Code)
			}
			loc := rec.Header().Get("Location")
			u, err := url.Parse(loc)
			if err != nil {
				t.Fatalf("failed to parse redirect location %q: %v", loc, err)
			}
			if u.Query().Get("error") != "invalid_request" {
				t.Errorf("expected error='invalid_request', got %q", u.Query().Get("error"))
			}
			if u.Query().Get("state") != "client-state" {
				t.Errorf("expected state='client-state', got %q", u.Query().Get("state"))
			}
		})
	}
}

// T-CB-1: Callback with unknown, expired or reused state -> Error page, no redirect.
func TestT_CB_1_UnknownExpiredOrReusedState(t *testing.T) {
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	callbackHandler := oauth.HandleCallback(oauth.CallbackConfig{
		GiteaHost:         "https://git.example.com",
		GiteaClientID:     "gitea-client-id",
		GiteaClientSecret: "gitea-secret",
		PublicURL:         "https://mcp.example.com",
		AllowedUser:       "alice",
		Store:             store,
		NowFunc:           func() time.Time { return now },
	})

	// 1. Unknown state
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=unknown-state&code=test-code", nil)
	rec := httptest.NewRecorder()
	callbackHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Unknown state: expected 400, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Unknown state: expected no redirect (Location empty), got %q", loc)
	}

	// 2. Expired state
	store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState: "expired-state",
		RedirectURI:   "https://claude.ai/cb",
		ExpiresAt:     now.Add(-1 * time.Minute),
	})
	req = httptest.NewRequest(http.MethodGet, "/oauth/callback?state=expired-state&code=test-code", nil)
	rec = httptest.NewRecorder()
	callbackHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expired state: expected 400, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Expired state: expected no redirect, got %q", loc)
	}

	// 3. Reused state
	store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState: "reused-state",
		RedirectURI:   "https://claude.ai/cb",
		ExpiresAt:     now.Add(10 * time.Minute),
	})
	// First consume via ConsumePendingAuth
	_, ok := store.ConsumePendingAuth("reused-state")
	if !ok {
		t.Fatal("failed initial consume")
	}
	// Reused request
	req = httptest.NewRequest(http.MethodGet, "/oauth/callback?state=reused-state&code=test-code", nil)
	rec = httptest.NewRecorder()
	callbackHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Reused state: expected 400, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Reused state: expected no redirect, got %q", loc)
	}
}

// T-CB-2: Gitea user != allowed user -> Rejected, tokens discarded.
func TestT_CB_2_UserMismatch(t *testing.T) {
	fakeGitea := newFakeGiteaServer(t, "unauthorized-intruder", "gitea-tok-123", "gitea-ref-123", 3600)
	defer fakeGitea.Close()

	store := oauth.NewStore(nil)
	store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState:    "valid-state",
		ClientID:         "client-1",
		RedirectURI:      "https://claude.ai/cb",
		ClientState:      "client-state",
		UpstreamVerifier: "upstream-verifier",
		ExpiresAt:        time.Now().Add(10 * time.Minute),
	})

	callbackHandler := oauth.HandleCallback(oauth.CallbackConfig{
		GiteaHost:         fakeGitea.URL,
		GiteaClientID:     "gitea-client-id",
		GiteaClientSecret: "gitea-secret",
		PublicURL:         "https://mcp.example.com",
		AllowedUser:       "alice", // expected user is alice, but fakeGitea returns unauthorized-intruder
		Store:             store,
		HTTPClient:        fakeGitea.Client(),
	})

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=valid-state&code=gitea-code", nil)
	rec := httptest.NewRecorder()
	callbackHandler(rec, req)

	// UP-3, T-CB-2: Rejected, tokens discarded, error page, NO redirect
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden on user mismatch, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("expected no redirect to client on user mismatch, got Location=%q", loc)
	}

	// Verify no grant was created in store
	// (Check that store has zero grants)
	for i := range 10 {
		if _, ok := store.GetGrant(fmt.Sprintf("grant-%d", i)); ok {
			t.Error("expected no grant in store")
		}
	}
}

// Helper to create an auth code for token testing
func setupAuthCode(t *testing.T, store *oauth.Store, clientID, codeVerifier string, expiresAt time.Time) string {
	t.Helper()
	grant := &oauth.Grant{
		ID:                "grant-test-1",
		GiteaAccessToken:  "gitea-access-secret-token",
		GiteaRefreshToken: "gitea-refresh-secret-token",
		ExpiresAt:         time.Now().Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	vHash := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(vHash[:])

	code, _ := oauth.GenerateRandomString(32)
	store.PutAuthCode(&oauth.AuthCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   "https://claude.ai/cb",
		CodeChallenge: challenge,
		GrantID:       grant.ID,
		ExpiresAt:     expiresAt,
	})
	return code
}

// T-TK-1: Code reused -> invalid_grant.
func TestT_TK_1_CodeReused(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	redirectURI := "https://claude.ai/cb"
	clientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})

	verifier := "my-secret-verifier-12345678901234567890"
	code := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  "https://mcp.example.com",
		Store:      store,
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	// First exchange: Success
	req1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec1 := httptest.NewRecorder()
	tokenHandler(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first token exchange failed with status %d: %s", rec1.Code, rec1.Body.String())
	}

	// Second exchange with same code: Fails with invalid_grant (T-TK-1)
	req2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	tokenHandler(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("second exchange expected 400 Bad Request, got %d", rec2.Code)
	}
	var errResp map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &errResp)
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected error='invalid_grant', got %v", errResp["error"])
	}
}

// T-TK-2: Code with wrong client, wrong redirect URI, wrong verifier, or expired -> invalid_grant / invalid_client.
func TestT_TK_2_CodeValidationFailures(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	redirectURI := "https://claude.ai/cb"
	clientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})

	otherClientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix() - 100,
	})

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  "https://mcp.example.com",
		Store:      store,
	})

	verifier := "correct-verifier-12345678901234567890"

	// 1. Wrong client
	c1 := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))
	form1 := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {otherClientID},
		"code":          {c1},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form1.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec1 := httptest.NewRecorder()
	tokenHandler(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("wrong client: expected 400, got %d", rec1.Code)
	}

	// 2. Wrong redirect URI
	c2 := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))
	form2 := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {c2},
		"redirect_uri":  {"https://claude.ai/other"},
		"code_verifier": {verifier},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	tokenHandler(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("wrong redirect_uri: expected 400, got %d", rec2.Code)
	}

	// 3. Wrong verifier
	c3 := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))
	form3 := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {c3},
		"redirect_uri":  {redirectURI},
		"code_verifier": {"wrong-verifier-12345678901234567890"},
	}
	req3 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form3.Encode()))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec3 := httptest.NewRecorder()
	tokenHandler(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("wrong verifier: expected 400, got %d", rec3.Code)
	}

	// 4. Expired code
	c4 := setupAuthCode(t, store, clientID, verifier, time.Now().Add(-1*time.Second))
	form4 := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {c4},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req4 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form4.Encode()))
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec4 := httptest.NewRecorder()
	tokenHandler(rec4, req4)
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("expired code: expected 400, got %d", rec4.Code)
	}
}

// T-TK-3: Refresh: new pair issued; old refresh token rejected.
func TestT_TK_3_RefreshTokenRotation(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	redirectURI := "https://claude.ai/cb"
	clientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})

	verifier := "my-verifier-12345678901234567890"
	code := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  "https://mcp.example.com",
		Store:      store,
	})

	// Initial token exchange
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial exchange failed: %d", rec.Code)
	}

	var tokResp1 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tokResp1)
	refresh1 := tokResp1["refresh_token"].(string)
	access1 := tokResp1["access_token"].(string)

	// Refresh exchange
	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refresh1},
	}
	reqRef := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refreshForm.Encode()))
	reqRef.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recRef := httptest.NewRecorder()
	tokenHandler(recRef, reqRef)
	if recRef.Code != http.StatusOK {
		t.Fatalf("refresh exchange failed: %d", recRef.Code)
	}

	var tokResp2 map[string]any
	_ = json.Unmarshal(recRef.Body.Bytes(), &tokResp2)
	refresh2 := tokResp2["refresh_token"].(string)
	access2 := tokResp2["access_token"].(string)

	if refresh2 == refresh1 {
		t.Error("expected rotated new refresh token, got identical token")
	}
	if access2 == access1 {
		t.Error("expected new access token, got identical token")
	}

	// Try to reuse old refresh token refresh1 -> must be rejected with invalid_grant (T-TK-3)
	reqOld := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refreshForm.Encode()))
	reqOld.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recOld := httptest.NewRecorder()
	tokenHandler(recOld, reqOld)
	if recOld.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when reusing old refresh token, got %d", recOld.Code)
	}
	var errResp map[string]any
	_ = json.Unmarshal(recOld.Body.Bytes(), &errResp)
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected error='invalid_grant', got %v", errResp["error"])
	}
}

// TestT_TK_3_RefreshTokenRotation_Concurrent verifies that when multiple concurrent requests
// present the same refresh token, exactly one succeeds and the others get invalid_grant.
func TestT_TK_3_RefreshTokenRotation_Concurrent(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	redirectURI := "https://claude.ai/cb"
	clientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})

	verifier := "my-verifier-12345678901234567890"
	code := setupAuthCode(t, store, clientID, verifier, time.Now().Add(60*time.Second))

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  "https://mcp.example.com",
		Store:      store,
	})

	// Initial token exchange to get valid refresh token
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial exchange failed: %d", rec.Code)
	}

	var tokResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tokResp)
	refreshToken := tokResp["refresh_token"].(string)

	// Launch concurrent requests presenting the same refresh token
	const numConcurrent = 8
	var wg sync.WaitGroup
	start := make(chan struct{})

	var (
		successCount      atomic.Int32
		invalidGrantCount atomic.Int32
		otherCount        atomic.Int32
	)

	for range numConcurrent {
		wg.Go(func() {
			<-start

			refreshForm := url.Values{
				"grant_type":    {"refresh_token"},
				"client_id":     {clientID},
				"refresh_token": {refreshToken},
			}
			r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refreshForm.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			tokenHandler(w, r)

			switch w.Code {
			case http.StatusOK:
				successCount.Add(1)
			case http.StatusBadRequest:
				var errResp map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &errResp)
				if errResp["error"] == "invalid_grant" {
					invalidGrantCount.Add(1)
				} else {
					otherCount.Add(1)
				}
			default:
				otherCount.Add(1)
			}
		})
	}

	close(start)
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 success, got %d", got)
	}
	if got := invalidGrantCount.Load(); got != numConcurrent-1 {
		t.Errorf("expected %d invalid_grant responses, got %d", numConcurrent-1, got)
	}
	if got := otherCount.Load(); got != 0 {
		t.Errorf("expected 0 other responses, got %d", got)
	}
}

// T-TK-4: Token response contains no substring of the Gitea tokens.
func TestT_TK_4_NoGiteaTokenLeakInResponse(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	redirectURI := "https://claude.ai/cb"
	clientID, _ := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})

	giteaAccess := "gitea_access_secret_token_val_1234567890"
	giteaRefresh := "gitea_refresh_secret_token_val_1234567890"

	grant := &oauth.Grant{
		ID:                "grant-1",
		GiteaAccessToken:  giteaAccess,
		GiteaRefreshToken: giteaRefresh,
		ExpiresAt:         time.Now().Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	verifier := "my-verifier-12345678901234567890"
	vHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(vHash[:])
	code, _ := oauth.GenerateRandomString(32)
	store.PutAuthCode(&oauth.AuthCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: challenge,
		GrantID:       grant.ID,
		ExpiresAt:     time.Now().Add(60 * time.Second),
	})

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  "https://mcp.example.com",
		Store:      store,
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tokenHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("token exchange failed: %d", rec.Code)
	}

	bodyStr := rec.Body.String()
	if strings.Contains(bodyStr, giteaAccess) {
		t.Fatalf("leak detected: response body contains Gitea access token %q", giteaAccess)
	}
	if strings.Contains(bodyStr, giteaRefresh) {
		t.Fatalf("leak detected: response body contains Gitea refresh token %q", giteaRefresh)
	}
}

// AS-5, UP-1: Filling pending authorizations to the cap yields 503; expired entries are swept.
func TestAuthorize_CapReachedReturns503(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	now := time.Now()
	currentTime := now
	store := oauth.NewStore(func() time.Time { return currentTime })
	publicURL := "https://mcp.example.com"
	redirectURI := "https://claude.ai/api/mcp/auth_callback"

	clientID, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                now.Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID failed: %v", err)
	}

	authHandler := oauth.HandleAuthorize(oauth.AuthorizeConfig{
		SigningKey:    key,
		PublicURL:     publicURL,
		GiteaHost:     "https://git.example.com",
		GiteaClientID: "gitea-client-id",
		Store:         store,
		NowFunc:       func() time.Time { return currentTime },
	})

	// Fill store to cap (1000)
	for i := range oauth.MaxPendingAuths {
		store.PutPendingAuth(&oauth.PendingAuth{
			UpstreamState: fmt.Sprintf("state-%d", i),
			ClientID:      clientID,
			RedirectURI:   redirectURI,
			ExpiresAt:     now.Add(10 * time.Minute),
		})
	}

	// 1001st request: authorize endpoint returns 503
	target := fmt.Sprintf("/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&code_challenge=challenge&code_challenge_method=S256",
		url.QueryEscape(clientID), url.QueryEscape(redirectURI))
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	authHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when cap reached, got %d", rec.Code)
	}

	// Advance time past expiry
	currentTime = now.Add(11 * time.Minute)

	// Next request sweeps expired entries and succeeds (302 Found redirect to Gitea)
	rec = httptest.NewRecorder()
	authHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect to Gitea after sweep, got %d", rec.Code)
	}
}

// AS-5: When authorization codes reach cap, callback returns 503.
func TestCallback_CapReachedReturns503(t *testing.T) {
	fakeGitea := newFakeGiteaServer(t, "alice", "gitea-tok-123", "gitea-ref-123", 3600)
	defer fakeGitea.Close()

	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })
	store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState:    "valid-state",
		ClientID:         "client-1",
		RedirectURI:      "https://claude.ai/cb",
		ClientState:      "client-state",
		UpstreamVerifier: "upstream-verifier",
		ExpiresAt:        now.Add(10 * time.Minute),
	})

	// Fill store with 1000 auth codes
	for i := range oauth.MaxAuthCodes {
		store.PutAuthCode(&oauth.AuthCode{
			Code:      fmt.Sprintf("code-%d", i),
			ClientID:  "client-1",
			ExpiresAt: now.Add(60 * time.Second),
		})
	}

	callbackHandler := oauth.HandleCallback(oauth.CallbackConfig{
		GiteaHost:         fakeGitea.URL,
		GiteaClientID:     "gitea-client-id",
		GiteaClientSecret: "gitea-secret",
		PublicURL:         "https://mcp.example.com",
		AllowedUser:       "alice",
		Store:             store,
		HTTPClient:        fakeGitea.Client(),
		NowFunc:           func() time.Time { return now },
	})

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=valid-state&code=gitea-code", nil)
	rec := httptest.NewRecorder()
	callbackHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when auth codes cap reached, got %d", rec.Code)
	}

	// Assert no orphaned grant exists in the store after 503
	if count := store.GrantCount(); count != 0 {
		t.Fatalf("expected 0 grants in store after cap failure, got %d", count)
	}
}

// Gap (e): Client B presenting Client A's refresh token -> invalid_grant.
func TestToken_ClientBPresentingClientARefreshToken(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	publicURL := "https://mcp.example.com"
	redirectURI := "https://claude.ai/cb"

	clientA, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID A failed: %v", err)
	}

	clientB, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                time.Now().Unix() + 10,
	})
	if err != nil {
		t.Fatalf("SignClientID B failed: %v", err)
	}

	// Create grant and refresh token belonging to Client A
	grant := &oauth.Grant{
		ID:                "grant-client-a",
		GiteaAccessToken:  "gitea-tok-a",
		GiteaRefreshToken: "gitea-ref-a",
		ExpiresAt:         time.Now().Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	refTokenA, _ := oauth.GenerateRandomString(32)
	store.PutMCPToken(&oauth.MCPToken{
		TokenHash: oauth.HashToken(refTokenA),
		Type:      oauth.TokenTypeRefresh,
		GrantID:   grant.ID,
		ClientID:  clientA,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		Audience:  publicURL + "/mcp",
	})

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  publicURL,
		Store:      store,
	})

	// Client B presents Client A's refresh token
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientB},
		"refresh_token": {refTokenA},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tokenHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
	var errResp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp["error"] != "invalid_grant" {
		t.Fatalf("expected error='invalid_grant', got %q", errResp["error"])
	}
}

// Gap (n): Confidential client without secret, with wrong secret, and with a different auth method than registered -> invalid_client.
func TestToken_ConfidentialClient_AuthFailures(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	store := oauth.NewStore(nil)
	publicURL := "https://mcp.example.com"
	redirectURI := "https://claude.ai/cb"

	tokenHandler := oauth.HandleToken(oauth.TokenConfig{
		SigningKey: key,
		PublicURL:  publicURL,
		Store:      store,
	})

	// 1. Client registered with client_secret_post
	clientPost, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "client_secret_post",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID client_secret_post failed: %v", err)
	}
	correctSecretPost := oauth.ComputeClientSecret(key, clientPost)

	// 1a. client_secret_post without secret
	form := url.Values{
		"grant_type": {"refresh_token"},
		"client_id":  {clientPost},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_post without secret: expected 401, got %d", rec.Code)
	}

	// 1b. client_secret_post with wrong secret
	form = url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientPost},
		"client_secret": {"wrong-secret"},
	}
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_post with wrong secret: expected 401, got %d", rec.Code)
	}

	// 1c. client_secret_post sending Basic Auth instead of POST parameters
	form = url.Values{"grant_type": {"refresh_token"}}
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientPost, correctSecretPost)
	rec = httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_post with Basic Auth: expected 401, got %d", rec.Code)
	}

	// 2. Client registered with client_secret_basic
	clientBasic, err := oauth.SignClientID(key, oauth.ClientIDPayload{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "client_secret_basic",
		IssuedAt:                time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("SignClientID client_secret_basic failed: %v", err)
	}
	correctSecretBasic := oauth.ComputeClientSecret(key, clientBasic)

	// 2a. client_secret_basic without secret (empty password)
	form = url.Values{"grant_type": {"refresh_token"}}
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientBasic, "")
	rec = httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_basic without secret: expected 401, got %d", rec.Code)
	}

	// 2b. client_secret_basic with wrong secret
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientBasic, "wrong-password")
	rec = httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_basic with wrong secret: expected 401, got %d", rec.Code)
	}

	// 2c. client_secret_basic sending POST parameters instead of Basic Auth
	form = url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientBasic},
		"client_secret": {correctSecretBasic},
	}
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	tokenHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client_secret_basic with POST parameters: expected 401, got %d", rec.Code)
	}
}

// Refuse external redirects in upstream HTTP requests (callback and refresh)
func TestOAuth_RefusesExternalRedirectUpstream(t *testing.T) {
	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer externalServer.Close()

	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, externalServer.URL, http.StatusFound)
	}))
	defer giteaServer.Close()

	store := oauth.NewStore(nil)
	store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState:    "state-1",
		ClientID:         "client-1",
		RedirectURI:      "https://claude.ai/cb",
		UpstreamVerifier: "verifier",
		ExpiresAt:        time.Now().Add(10 * time.Minute),
	})

	// Callback endpoint with default httpClient (which has checkRedirectUpstream)
	callbackHandler := oauth.HandleCallback(oauth.CallbackConfig{
		GiteaHost:         giteaServer.URL,
		GiteaClientID:     "cid",
		GiteaClientSecret: "sec",
		PublicURL:         "https://mcp.example.com",
		AllowedUser:       "alice",
		Store:             store,
	})

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=state-1&code=gitea-code", nil)
	rec := httptest.NewRecorder()
	callbackHandler(rec, req)

	// External redirect refused -> token exchange fails -> 502 Bad Gateway
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway on upstream redirect refusal, got %d", rec.Code)
	}

	// Fail-closed verification: empty or unparsable Gitea host must refuse all redirects
	t.Run("fails closed with empty or unparsable host", func(t *testing.T) {
		dummyReq := httptest.NewRequest(http.MethodGet, "https://example.com/oauth/callback", nil)
		for _, badHost := range []string{"", "://invalid-host", "   "} {
			checkFn := oauth.CheckRedirectUpstreamForTest(badHost)
			if err := checkFn(dummyReq, []*http.Request{dummyReq}); err == nil {
				t.Errorf("expected checkRedirectUpstream to refuse redirect when host is %q, got nil", badHost)
			}
		}
	})

	t.Run("refuses scheme downgrade redirect", func(t *testing.T) {
		checkFn := oauth.CheckRedirectUpstreamForTest("https://git.example.com")
		httpReq := httptest.NewRequest(http.MethodGet, "http://git.example.com/login/oauth/access_token", nil)
		err := checkFn(httpReq, []*http.Request{httpReq})
		if err == nil {
			t.Fatal("expected HTTPS->HTTP redirect to be refused, got nil")
		}
		if !strings.Contains(err.Error(), `redirect scheme "http" does not match configured scheme "https"`) {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
