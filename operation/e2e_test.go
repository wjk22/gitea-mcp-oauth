package operation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/log"
	"gitea.com/gitea/gitea-mcp/pkg/oauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap/zapcore"
)

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type fakeGiteaServerState struct {
	mu                    sync.Mutex
	userCallAuthTokens    []string
	tokenEndpointRequests []string
}

func newFakeGiteaE2EServer(t *testing.T, state *fakeGiteaServerState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		grantType := r.FormValue("grant_type")

		state.mu.Lock()
		state.tokenEndpointRequests = append(state.tokenEndpointRequests, grantType)
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch grantType {
		case "authorization_code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "gitea-access-tok-1",
				"token_type":    "bearer",
				"expires_in":    3600,
				"refresh_token": "gitea-refresh-tok-1",
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "gitea-access-tok-refreshed",
				"token_type":    "bearer",
				"expires_in":    3600,
				"refresh_token": "gitea-refresh-tok-2",
			})
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		}
	})

	mux.HandleFunc("/api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		state.mu.Lock()
		state.userCallAuthTokens = append(state.userCallAuthTokens, authHeader)
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        1,
			"login":     "alice",
			"username":  "alice",
			"full_name": "Alice",
			"email":     "alice@example.com",
		})
	})

	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version": "1.27.3"}`))
	})

	return httptest.NewServer(mux)
}

// T-E2E: Happy path through a fake Gitea: register -> authorize -> callback -> token -> tool call
// with two tool calls in one session with an upstream token refresh in between.
func TestT_E2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	origHost := flag.Host
	origClientID := flag.OAuthClientID
	origClientSecret := flag.OAuthClientSecret
	origPublicURL := flag.OAuthPublicURL
	origSigningKey := flag.OAuthSigningKey
	origAllowedURIs := flag.OAuthAllowedRedirectURIs
	origAllowedUser := flag.OAuthAllowedUser
	origMode := flag.Mode

	t.Cleanup(func() {
		flag.Host = origHost
		flag.OAuthClientID = origClientID
		flag.OAuthClientSecret = origClientSecret
		flag.OAuthPublicURL = origPublicURL
		flag.OAuthSigningKey = origSigningKey
		flag.OAuthAllowedRedirectURIs = origAllowedURIs
		flag.OAuthAllowedUser = origAllowedUser
		flag.Mode = origMode
	})

	giteaState := &fakeGiteaServerState{}
	fakeGitea := newFakeGiteaE2EServer(t, giteaState)
	defer fakeGitea.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen err: %v", err)
	}

	serverURL := "http://" + l.Addr().String()
	flag.Host = fakeGitea.URL
	flag.OAuthClientID = "gitea-app-client-id"
	flag.OAuthClientSecret = "gitea-app-client-secret-12345"
	flag.OAuthPublicURL = serverURL
	flag.OAuthSigningKey = []byte("01234567890123456789012345678901")
	flag.OAuthAllowedRedirectURIs = []string{"https://claude.ai/api/mcp/auth_callback"}
	flag.OAuthAllowedUser = "alice"
	flag.Mode = "http"

	mcpS := newMCPServer("test-version")
	RegisterTool(mcpS)

	store := oauth.NewStore(nil)
	httpServer := newHTTPServerWithStore(l.Addr().String(), mcpS, nil, store)
	ts := &httptest.Server{
		Listener: l,
		Config:   httpServer,
	}
	ts.Start()
	defer ts.Close()

	httpClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // do not follow redirects automatically
		},
	}

	// 1. Dynamic Client Registration (POST /oauth/register)
	regBody := `{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"client_name":"Claude Desktop","token_endpoint_auth_method":"none"}`
	resp, err := httpClient.Post(serverURL+"/oauth/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register status = %d, want 201: %s", resp.StatusCode, string(body))
	}

	var regResp struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	clientID := regResp.ClientID

	// 2. Authorize (GET /oauth/authorize)
	codeVerifier := "01234567890123456789012345678901234567890123456789"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	clientState := "client-state-uuid-12345"

	authURL := fmt.Sprintf("%s/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		serverURL,
		url.QueryEscape(clientID),
		url.QueryEscape("https://claude.ai/api/mcp/auth_callback"),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(clientState),
	)

	resp, err = httpClient.Get(authURL)
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", resp.StatusCode)
	}

	giteaRedirectLoc := resp.Header.Get("Location")
	if !strings.HasPrefix(giteaRedirectLoc, fakeGitea.URL+"/login/oauth/authorize") {
		t.Fatalf("authorize redirect = %q, want prefix %q", giteaRedirectLoc, fakeGitea.URL)
	}

	u, err := url.Parse(giteaRedirectLoc)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}
	upstreamState := u.Query().Get("state")
	if upstreamState == "" {
		t.Fatal("authorize redirect missing upstream state")
	}

	// 3. Callback (GET /oauth/callback from Gitea)
	callbackURL := fmt.Sprintf("%s/oauth/callback?code=upstream-gitea-code-999&state=%s",
		serverURL,
		url.QueryEscape(upstreamState),
	)
	resp, err = httpClient.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}

	clientRedirectLoc := resp.Header.Get("Location")
	if !strings.HasPrefix(clientRedirectLoc, "https://claude.ai/api/mcp/auth_callback") {
		t.Fatalf("callback redirect = %q, want client redirect URI", clientRedirectLoc)
	}

	cu, err := url.Parse(clientRedirectLoc)
	if err != nil {
		t.Fatalf("parse client redirect URL: %v", err)
	}
	if cu.Query().Get("state") != clientState {
		t.Errorf("callback returned state = %q, want %q", cu.Query().Get("state"), clientState)
	}
	mcpCode := cu.Query().Get("code")
	if mcpCode == "" {
		t.Fatal("callback redirect missing code")
	}

	// 4. Token Exchange (POST /oauth/token)
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {mcpCode},
		"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
		"code_verifier": {codeVerifier},
	}
	resp, err = httpClient.PostForm(serverURL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatalf("token exchange request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token exchange status = %d, want 200: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
		t.Fatalf("token response missing tokens: %+v", tokenResp)
	}

	// 5. Connect MCP Client over Streamable HTTP (/mcp)
	authTrans := &authorizationTransport{base: ts.Client().Transport}
	authTrans.set("Bearer " + tokenResp.AccessToken)
	mcpHTTPClient := &http.Client{Transport: authTrans}

	client := mcp.NewClient(&mcp.Implementation{Name: "claude-desktop-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             serverURL + "/mcp",
		HTTPClient:           mcpHTTPClient,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("MCP client Connect() err: %v", err)
	}
	defer session.Close()

	// 6. Tool Call 1 in session: get_me
	res1, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_me"})
	if err != nil {
		t.Fatalf("tool call 1 get_me err: %v", err)
	}
	if res1.IsError {
		t.Fatalf("tool call 1 returned error: %+v", res1)
	}
	if len(res1.Content) == 0 {
		t.Fatal("tool call 1 returned empty content")
	}
	txtContent1, ok := res1.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(txtContent1.Text, "alice") {
		t.Fatalf("tool call 1 expected user alice, got: %v", res1.Content[0])
	}

	// Verify tool call 1 used initial Gitea access token
	giteaState.mu.Lock()
	initialUserTokens := append([]string(nil), giteaState.userCallAuthTokens...)
	giteaState.mu.Unlock()
	if len(initialUserTokens) < 2 { // 1 for UP-3 callback user verification + 1 for tool call 1
		t.Fatalf("expected at least 2 user calls, got %d: %v", len(initialUserTokens), initialUserTokens)
	}
	toolCall1AuthHeader := initialUserTokens[len(initialUserTokens)-1]
	if !strings.Contains(toolCall1AuthHeader, "gitea-access-tok-1") {
		t.Errorf("tool call 1 did not use gitea-access-tok-1, used: %q", toolCall1AuthHeader)
	}

	// 7. Upstream Token Refresh in between: advance grant expiry to near future (<= 5 minutes)
	tokHash := oauth.HashToken(tokenResp.AccessToken)
	tok, ok := store.GetMCPToken(tokHash)
	if !ok || tok == nil {
		t.Fatalf("failed to find MCP token in store")
	}
	grant, ok := store.GetGrant(tok.GrantID)
	if !ok || grant == nil {
		t.Fatalf("failed to find grant in store")
	}

	grant.Lock()
	grant.ExpiresAt = time.Now().Add(1 * time.Minute) // triggers UP-4 refresh on next call
	grant.Unlock()

	// 8. Tool Call 2 in SAME session: get_me
	res2, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_me"})
	if err != nil {
		t.Fatalf("tool call 2 get_me err: %v", err)
	}
	if res2.IsError {
		t.Fatalf("tool call 2 returned error: %+v", res2)
	}
	txtContent2, ok := res2.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(txtContent2.Text, "alice") {
		t.Fatalf("tool call 2 expected user alice, got: %v", res2.Content[0])
	}

	// 9. Verify upstream refresh occurred and tool call 2 used the refreshed token
	giteaState.mu.Lock()
	finalTokenRequests := append([]string(nil), giteaState.tokenEndpointRequests...)
	finalUserTokens := append([]string(nil), giteaState.userCallAuthTokens...)
	giteaState.mu.Unlock()

	if !slices.Contains(finalTokenRequests, "refresh_token") {
		t.Errorf("fake Gitea never received refresh_token request: %v", finalTokenRequests)
	}
	toolCall2AuthHeader := finalUserTokens[len(finalUserTokens)-1]
	if !strings.Contains(toolCall2AuthHeader, "gitea-access-tok-refreshed") {
		t.Errorf("tool call 2 did not use refreshed token, used: %q", toolCall2AuthHeader)
	}
}

// T-LOG-1: Full flow with debug logging -> No token, code, secret or verifier in logs.
func TestT_LOG_1_FullFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	origHost := flag.Host
	origClientID := flag.OAuthClientID
	origClientSecret := flag.OAuthClientSecret
	origPublicURL := flag.OAuthPublicURL
	origSigningKey := flag.OAuthSigningKey
	origAllowedURIs := flag.OAuthAllowedRedirectURIs
	origAllowedUser := flag.OAuthAllowedUser
	origMode := flag.Mode
	origDebug := flag.Debug

	t.Cleanup(func() {
		flag.Host = origHost
		flag.OAuthClientID = origClientID
		flag.OAuthClientSecret = origClientSecret
		flag.OAuthPublicURL = origPublicURL
		flag.OAuthSigningKey = origSigningKey
		flag.OAuthAllowedRedirectURIs = origAllowedURIs
		flag.OAuthAllowedUser = origAllowedUser
		flag.Mode = origMode
		flag.Debug = origDebug
	})

	origLogger := log.Default()
	t.Cleanup(func() {
		log.SetDefault(origLogger)
	})

	logBuf := &safeBuffer{}
	flag.Debug = true
	prodLogger := log.NewProductionLogger(zapcore.AddSync(logBuf))
	log.SetDefault(prodLogger)

	giteaState := &fakeGiteaServerState{}
	fakeGitea := newFakeGiteaE2EServer(t, giteaState)
	defer fakeGitea.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen err: %v", err)
	}

	serverURL := "http://" + l.Addr().String()
	flag.Host = fakeGitea.URL
	flag.OAuthClientID = "gitea-app-client-id"
	flag.OAuthClientSecret = "secret-gitea-app-client-xyz-777"
	flag.OAuthPublicURL = serverURL
	flag.OAuthSigningKey = []byte("01234567890123456789012345678901")
	flag.OAuthAllowedRedirectURIs = []string{"https://claude.ai/api/mcp/auth_callback"}
	flag.OAuthAllowedUser = "alice"
	flag.Mode = "http"

	mcpS := newMCPServer("test-version")
	RegisterTool(mcpS)

	store := oauth.NewStore(nil)
	httpServer := newHTTPServerWithStore(l.Addr().String(), mcpS, nil, store)
	ts := &httptest.Server{
		Listener: l,
		Config:   httpServer,
	}
	ts.Start()
	defer ts.Close()

	httpClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Dynamic Client Registration with confidential client (client_secret issued)
	regBody := `{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"client_name":"Claude Desktop Confidential","token_endpoint_auth_method":"client_secret_post"}`
	resp, err := httpClient.Post(serverURL+"/oauth/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer resp.Body.Close()

	var regResp struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	clientID := regResp.ClientID
	clientSecret := regResp.ClientSecret

	// 2. Authorize
	codeVerifier := "unique-code-verifier-string-secret-12345678901234567890"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	clientState := "unique-client-state-uuid-abcdef"

	authURL := fmt.Sprintf("%s/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		serverURL,
		url.QueryEscape(clientID),
		url.QueryEscape("https://claude.ai/api/mcp/auth_callback"),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(clientState),
	)
	resp, err = httpClient.Get(authURL)
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	resp.Body.Close()

	giteaRedirectLoc := resp.Header.Get("Location")
	u, _ := url.Parse(giteaRedirectLoc)
	upstreamState := u.Query().Get("state")

	// 3. Callback
	upstreamCode := "secret-upstream-gitea-code-888"
	callbackURL := fmt.Sprintf("%s/oauth/callback?code=%s&state=%s",
		serverURL,
		url.QueryEscape(upstreamCode),
		url.QueryEscape(upstreamState),
	)
	resp, err = httpClient.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	resp.Body.Close()

	clientRedirectLoc := resp.Header.Get("Location")
	cu, _ := url.Parse(clientRedirectLoc)
	mcpCode := cu.Query().Get("code")

	// 4. Token Exchange
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {mcpCode},
		"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
		"code_verifier": {codeVerifier},
	}
	resp, err = httpClient.PostForm(serverURL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatalf("token exchange request failed: %v", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tokenResp)

	// 5. Connect MCP client and call tool
	authTrans := &authorizationTransport{base: ts.Client().Transport}
	authTrans.set("Bearer " + tokenResp.AccessToken)
	mcpHTTPClient := &http.Client{Transport: authTrans}

	client := mcp.NewClient(&mcp.Implementation{Name: "claude-log-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             serverURL + "/mcp",
		HTTPClient:           mcpHTTPClient,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("Connect err: %v", err)
	}
	defer session.Close()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_me"})
	if err != nil {
		t.Fatalf("tool call get_me err: %v", err)
	}

	// 6. Upstream refresh and tool call 2
	tokHash := oauth.HashToken(tokenResp.AccessToken)
	tok, _ := store.GetMCPToken(tokHash)
	grant, _ := store.GetGrant(tok.GrantID)
	grant.Lock()
	grant.ExpiresAt = time.Now().Add(1 * time.Minute)
	grant.Unlock()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_me"})
	if err != nil {
		t.Fatalf("tool call 2 get_me err: %v", err)
	}

	// Explicitly log flow URLs and tokens through production logger at debug level
	log.Debugf("flow step: authorize URL %s", authURL)
	log.Debugf("flow step: callback URL %s", callbackURL)
	log.Debugf("flow step: Authorization: Bearer %s", tokenResp.AccessToken)

	// 7. Verify LOG-1 Redaction
	logOutput := logBuf.String()
	if len(logOutput) == 0 {
		t.Fatal("expected log output from server and SDK, got empty buffer")
	}

	sensitiveSecrets := []struct {
		name  string
		value string
	}{
		{"Gitea client secret", flag.OAuthClientSecret},
		{"Client secret", clientSecret},
		{"Code verifier", codeVerifier},
		{"MCP auth code", mcpCode},
		{"Upstream auth code", upstreamCode},
		{"MCP access token", tokenResp.AccessToken},
		{"MCP refresh token", tokenResp.RefreshToken},
		{"Gitea access token 1", "gitea-access-tok-1"},
		{"Gitea access token refreshed", "gitea-access-tok-refreshed"},
		{"Gitea refresh token 1", "gitea-refresh-tok-1"},
		{"Gitea refresh token 2", "gitea-refresh-tok-2"},
	}

	for _, s := range sensitiveSecrets {
		if s.value != "" && strings.Contains(logOutput, s.value) {
			t.Errorf("LOG-1 violation: log output contains secret %s (%q):\n%s", s.name, s.value, logOutput)
		}
	}

	if !strings.Contains(logOutput, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in log output, got:\n%s", logOutput)
	}
}
