package oauth

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestMetadataTMETA verifies T-META: metadata fields and host-spoofing immunity (AS-8).
func TestMetadataTMETA(t *testing.T) {
	publicURL := "https://mcp.example.com"

	// 1. Authorization server metadata (RFC 8414)
	t.Run("authorization server metadata exact fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		req.Host = "evil.com"
		req.Header.Set("X-Forwarded-Host", "attacker.com")
		w := httptest.NewRecorder()

		handler := HandleAuthorizationServerMetadata(publicURL)
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("T-META: expected 200 OK, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("T-META: expected Content-Type application/json, got %q", ct)
		}

		var meta AuthorizationServerMetadata
		if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
			t.Fatalf("T-META: failed to unmarshal AS metadata: %v", err)
		}

		want := AuthorizationServerMetadata{
			Issuer:                            "https://mcp.example.com",
			AuthorizationEndpoint:             "https://mcp.example.com/oauth/authorize",
			TokenEndpoint:                     "https://mcp.example.com/oauth/token",
			RegistrationEndpoint:              "https://mcp.example.com/oauth/register",
			ResponseTypesSupported:            []string{"code"},
			GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
			CodeChallengeMethodsSupported:     []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_post", "client_secret_basic"},
		}

		if !reflect.DeepEqual(meta, want) {
			t.Errorf("T-META: AS metadata mismatch:\ngot  %+v\nwant %+v", meta, want)
		}
	})

	// 2. Protected resource metadata (RFC 9728)
	t.Run("protected resource metadata exact fields", func(t *testing.T) {
		for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "spoofed.org"
			req.Header.Set("X-Forwarded-Host", "spoofed.org")
			w := httptest.NewRecorder()

			handler := HandleProtectedResourceMetadata(publicURL)
			handler(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("T-META: %s expected 200 OK, got %d", path, w.Code)
			}
			var resMeta ProtectedResourceMetadata
			if err := json.Unmarshal(w.Body.Bytes(), &resMeta); err != nil {
				t.Fatalf("T-META: failed to unmarshal resource metadata: %v", err)
			}

			want := ProtectedResourceMetadata{
				Resource:               "https://mcp.example.com/mcp",
				AuthorizationServers:   []string{"https://mcp.example.com"},
				BearerMethodsSupported: []string{"header"},
			}
			if !reflect.DeepEqual(resMeta, want) {
				t.Errorf("T-META: resource metadata mismatch on %s:\ngot  %+v\nwant %+v", path, resMeta, want)
			}
		}
	})

	// 3. OpenID configuration returns 404
	t.Run("openid-configuration returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
		w := httptest.NewRecorder()
		HandleOpenIDConfiguration()(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("T-META: expected 404 for openid-configuration, got %d", w.Code)
		}
	})

	// 4. Endpoints advertised but not implemented return 501
	t.Run("unimplemented endpoints return 501", func(t *testing.T) {
		for _, ep := range []string{"/oauth/authorize", "/oauth/token"} {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			HandleNotImplemented(ep)(w, req)
			if w.Code != http.StatusNotImplemented {
				t.Errorf("T-META: expected 501 for %s, got %d", ep, w.Code)
			}
		}
	})
}

// TestRegistrationTDCR1 verifies T-DCR-1: client registration with redirect allowlist and signing (AS-1).
func TestRegistrationTDCR1(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	allowlist := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}

	fixedNow := time.Unix(1770000000, 0)
	nowFunc := func() time.Time { return fixedNow }

	handler := HandleRegister(key, allowlist, nowFunc)

	tests := []struct {
		name           string
		body           map[string]any
		wantStatus     int
		wantErrorCode  string
		wantAuthMethod string
		wantSecret     bool
	}{
		{
			name: "rejects non-allowlisted redirect URI",
			body: map[string]any{
				"redirect_uris": []string{"https://evil.com/callback"},
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_redirect_uri",
		},
		{
			name: "rejects non-HTTPS redirect URI",
			body: map[string]any{
				"redirect_uris": []string{"http://claude.ai/api/mcp/auth_callback"},
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_redirect_uri",
		},
		{
			name: "rejects empty redirect URIs list",
			body: map[string]any{
				"redirect_uris": []string{},
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_redirect_uri",
		},
		{
			name: "rejects unsupported token endpoint auth method",
			body: map[string]any{
				"redirect_uris":              []string{"https://claude.ai/api/mcp/auth_callback"},
				"token_endpoint_auth_method": "private_key_jwt",
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_client_metadata",
		},
		{
			name: "registers public client with method none",
			body: map[string]any{
				"redirect_uris":              []string{"https://claude.ai/api/mcp/auth_callback"},
				"token_endpoint_auth_method": "none",
			},
			wantStatus:     http.StatusCreated,
			wantAuthMethod: "none",
			wantSecret:     false,
		},
		{
			name: "registers confidential client with default method client_secret_basic",
			body: map[string]any{
				"redirect_uris": []string{"https://chatgpt.com/connector_platform_oauth_redirect"},
			},
			wantStatus:     http.StatusCreated,
			wantAuthMethod: "client_secret_basic",
			wantSecret:     true,
		},
		{
			name: "registers confidential client with method client_secret_post",
			body: map[string]any{
				"redirect_uris":              []string{"https://claude.com/api/mcp/auth_callback"},
				"token_endpoint_auth_method": "client_secret_post",
			},
			wantStatus:     http.StatusCreated,
			wantAuthMethod: "client_secret_post",
			wantSecret:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/oauth/register", bytes.NewReader(jsonBody))
			w := httptest.NewRecorder()

			handler(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("T-DCR-1 %s: status = %d, want %d; body = %s", tt.name, w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantErrorCode != "" {
				var errResp errorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("T-DCR-1 %s: unmarshal error response: %v", tt.name, err)
				}
				if errResp.Error != tt.wantErrorCode {
					t.Errorf("T-DCR-1 %s: error code = %q, want %q", tt.name, errResp.Error, tt.wantErrorCode)
				}
				return
			}

			var resp RegistrationResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("T-DCR-1 %s: unmarshal response: %v", tt.name, err)
			}

			if resp.ClientID == "" {
				t.Errorf("T-DCR-1 %s: client_id must not be empty", tt.name)
			}
			if resp.TokenEndpointAuthMethod != tt.wantAuthMethod {
				t.Errorf("T-DCR-1 %s: auth method = %q, want %q", tt.name, resp.TokenEndpointAuthMethod, tt.wantAuthMethod)
			}
			if resp.ClientIDIssuedAt != fixedNow.Unix() {
				t.Errorf("T-DCR-1 %s: issued_at = %d, want %d", tt.name, resp.ClientIDIssuedAt, fixedNow.Unix())
			}

			if tt.wantSecret {
				expectedSecret := ComputeClientSecret(key, resp.ClientID)
				if resp.ClientSecret != expectedSecret {
					t.Errorf("T-DCR-1 %s: client_secret mismatch: got %q, want %q", tt.name, resp.ClientSecret, expectedSecret)
				}
			} else if resp.ClientSecret != "" {
				t.Errorf("T-DCR-1 %s: public client must not have client_secret, got %q", tt.name, resp.ClientSecret)
			}

			// Verify registered client ID decodes back correctly
			payload, err := VerifyClientID(key, resp.ClientID)
			if err != nil {
				t.Fatalf("T-DCR-1 %s: VerifyClientID failed: %v", tt.name, err)
			}
			if payload.TokenEndpointAuthMethod != tt.wantAuthMethod {
				t.Errorf("T-DCR-1 %s: verified payload auth method = %q, want %q", tt.name, payload.TokenEndpointAuthMethod, tt.wantAuthMethod)
			}
		})
	}
}

// TestClientIDVerificationAndTampering verifies client ID validation and tampering rejection (AS-1, AS-2, T-DCR-2 prep).
func TestClientIDVerificationAndTampering(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	payload := ClientIDPayload{
		RedirectURIs:            []string{"https://claude.ai/api/mcp/auth_callback"},
		TokenEndpointAuthMethod: "none",
		IssuedAt:                1770000000,
	}

	clientID, err := SignClientID(key, payload)
	if err != nil {
		t.Fatalf("SignClientID failed: %v", err)
	}

	// 1. Valid client ID verifies
	verified, err := VerifyClientID(key, clientID)
	if err != nil {
		t.Fatalf("expected valid client ID to verify, got error: %v", err)
	}
	if !reflect.DeepEqual(verified, &payload) {
		t.Errorf("verified payload mismatch:\ngot  %+v\nwant %+v", verified, payload)
	}

	dotIdx := strings.Index(clientID, ".")
	tamperedSig := clientID[:dotIdx+1] + "X" + clientID[dotIdx+2:]
	if tamperedSig == clientID {
		tamperedSig = clientID[:dotIdx+1] + "Y" + clientID[dotIdx+2:]
	}
	if _, err := VerifyClientID(key, tamperedSig); err == nil {
		t.Errorf("expected tampered signature to fail verification")
	}

	// 3. Tampered payload fails
	rawBytes := []byte(clientID)
	rawBytes[2] ^= 0x01 // flip bit in payload
	if _, err := VerifyClientID(key, string(rawBytes)); err == nil {
		t.Errorf("expected tampered payload to fail verification")
	}

	// 4. Wrong key fails
	wrongKey := make([]byte, 32)
	_, _ = rand.Read(wrongKey)
	if _, err := VerifyClientID(wrongKey, clientID); err == nil {
		t.Errorf("expected verification with wrong key to fail")
	}

	// 5. Malformed format fails
	for _, malformed := range []string{"", "no-dot", "one.two.three"} {
		if _, err := VerifyClientID(key, malformed); err == nil {
			t.Errorf("expected malformed client ID %q to fail verification", malformed)
		}
	}
}
