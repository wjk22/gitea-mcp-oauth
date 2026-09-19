package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

// TokenConfig holds configuration for the token endpoint.
type TokenConfig struct {
	SigningKey []byte
	PublicURL  string
	Store      *Store
	NowFunc    func() time.Time
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// HandleToken handles POST /oauth/token (AS-6, AS-7, task item 7).
func HandleToken(cfg TokenConfig) http.HandlerFunc {
	nowFunc := cfg.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
			return
		}

		// Authenticate client per AS-6
		var clientID, clientSecret string
		var isBasicAuth bool
		if user, pass, ok := r.BasicAuth(); ok {
			clientID = user
			clientSecret = pass
			isBasicAuth = true
		} else {
			clientID = r.PostFormValue("client_id")
			clientSecret = r.PostFormValue("client_secret")
		}

		if clientID == "" {
			writeJSONError(w, http.StatusUnauthorized, "invalid_client", "client_id is required")
			return
		}

		payload, err := VerifyClientID(cfg.SigningKey, clientID)
		if err != nil {
			// T-DCR-2: Tampered client ID rejected at token endpoint
			writeJSONError(w, http.StatusUnauthorized, "invalid_client", "invalid client_id signature or format")
			return
		}

		switch payload.TokenEndpointAuthMethod {
		case "none":
			if clientSecret != "" || isBasicAuth {
				writeJSONError(w, http.StatusUnauthorized, "invalid_client", "public client must not provide client secret")
				return
			}
		case "client_secret_basic":
			if !isBasicAuth {
				writeJSONError(w, http.StatusUnauthorized, "invalid_client", "client_secret_basic requires HTTP Basic authentication")
				return
			}
			expectedSecret := ComputeClientSecret(cfg.SigningKey, clientID)
			// Item 7: Constant-time secret comparison
			if subtle.ConstantTimeCompare([]byte(clientSecret), []byte(expectedSecret)) != 1 {
				writeJSONError(w, http.StatusUnauthorized, "invalid_client", "invalid client secret")
				return
			}
		case "client_secret_post":
			if isBasicAuth {
				writeJSONError(w, http.StatusUnauthorized, "invalid_client", "client_secret_post requires form parameters")
				return
			}
			expectedSecret := ComputeClientSecret(cfg.SigningKey, clientID)
			// Item 7: Constant-time secret comparison
			if subtle.ConstantTimeCompare([]byte(clientSecret), []byte(expectedSecret)) != 1 {
				writeJSONError(w, http.StatusUnauthorized, "invalid_client", "invalid client secret")
				return
			}
		default:
			writeJSONError(w, http.StatusUnauthorized, "invalid_client", "unsupported client authentication method")
			return
		}

		// AS-4: If resource is present, it must equal <public>/mcp
		resource := r.PostFormValue("resource")
		if resource != "" && resource != cfg.PublicURL+"/mcp" {
			writeJSONError(w, http.StatusBadRequest, "invalid_target", "resource must be "+cfg.PublicURL+"/mcp")
			return
		}

		grantType := r.PostFormValue("grant_type")
		switch grantType {
		case "authorization_code":
			code := r.PostFormValue("code")
			redirectURI := r.PostFormValue("redirect_uri")
			codeVerifier := r.PostFormValue("code_verifier")

			if code == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid_request", "code is required")
				return
			}

			// AS-5: Consumed atomically; single use (T-TK-1)
			authCode, ok := cfg.Store.ConsumeAuthCode(code)
			if !ok || authCode == nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid, expired, or already used")
				return
			}

			// T-TK-2: Check client_id, redirect_uri, and PKCE verifier
			if authCode.ClientID != clientID {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
				return
			}
			if authCode.RedirectURI != redirectURI {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
				return
			}
			if codeVerifier == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "code_verifier is required")
				return
			}
			vHash := sha256.Sum256([]byte(codeVerifier))
			vChallenge := base64.RawURLEncoding.EncodeToString(vHash[:])
			if subtle.ConstantTimeCompare([]byte(vChallenge), []byte(authCode.CodeChallenge)) != 1 {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "PKCE code_verifier mismatch")
				return
			}

			grant, ok := cfg.Store.GetGrant(authCode.GrantID)
			if !ok || grant == nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "associated grant no longer exists")
				return
			}

			// AS-7: Issue MCP access token (1 hour) and refresh token (30 days)
			accessToken, err := GenerateRandomString(32)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "server_error", "failed to generate access token")
				return
			}
			refreshToken, err := GenerateRandomString(32)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "server_error", "failed to generate refresh token")
				return
			}

			now := nowFunc()
			cfg.Store.PutMCPToken(&MCPToken{
				TokenHash: HashToken(accessToken),
				Type:      TokenTypeAccess,
				GrantID:   grant.ID,
				ClientID:  clientID,
				ExpiresAt: now.Add(1 * time.Hour),
				Audience:  cfg.PublicURL + "/mcp",
			})
			cfg.Store.PutMCPToken(&MCPToken{
				TokenHash: HashToken(refreshToken),
				Type:      TokenTypeRefresh,
				GrantID:   grant.ID,
				ClientID:  clientID,
				ExpiresAt: now.Add(30 * 24 * time.Hour),
				Audience:  cfg.PublicURL + "/mcp",
			})

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  accessToken,
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				RefreshToken: refreshToken,
				Scope:        "read:repository read:issue read:user read:organization",
			})

		case "refresh_token":
			refreshToken := r.PostFormValue("refresh_token")
			if refreshToken == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
				return
			}

			// AS-6, T-TK-3: Validate, delete and return refresh token under single lock
			tokenHash := HashToken(refreshToken)
			tok, ok := cfg.Store.ConsumeMCPRefreshToken(tokenHash, clientID)
			if !ok || tok == nil {
				// T-TK-3: Old or invalid refresh token rejected
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired refresh token")
				return
			}

			grant, ok := cfg.Store.GetGrant(tok.GrantID)
			if !ok || grant == nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_grant", "associated grant no longer exists")
				return
			}

			// Issue new pair
			newAccess, err := GenerateRandomString(32)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "server_error", "failed to generate access token")
				return
			}
			newRefresh, err := GenerateRandomString(32)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "server_error", "failed to generate refresh token")
				return
			}

			now := nowFunc()
			cfg.Store.PutMCPToken(&MCPToken{
				TokenHash: HashToken(newAccess),
				Type:      TokenTypeAccess,
				GrantID:   grant.ID,
				ClientID:  clientID,
				ExpiresAt: now.Add(1 * time.Hour),
				Audience:  cfg.PublicURL + "/mcp",
			})
			cfg.Store.PutMCPToken(&MCPToken{
				TokenHash: HashToken(newRefresh),
				Type:      TokenTypeRefresh,
				GrantID:   grant.ID,
				ClientID:  clientID,
				ExpiresAt: now.Add(30 * 24 * time.Hour),
				Audience:  cfg.PublicURL + "/mcp",
			})

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  newAccess,
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				RefreshToken: newRefresh,
				Scope:        "read:repository read:issue read:user read:organization",
			})

		default:
			writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
		}
	}
}
