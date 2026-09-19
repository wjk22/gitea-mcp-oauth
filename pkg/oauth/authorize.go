package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// writeHTMLError writes a safe HTML error page when redirecting to client is not permitted (AS-2, UP-2).
func writeHTMLError(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<!DOCTYPE html><html><head><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>",
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

// redirectClientError redirects the user agent back to the client's redirect URI with an RFC 6749 error (AS-2).
func redirectClientError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, errDesc, clientState string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeHTMLError(w, http.StatusBadRequest, "Invalid Redirect URI", "Failed to parse redirect URI.")
		return
	}
	q := u.Query()
	q.Set("error", errCode)
	if errDesc != "" {
		q.Set("error_description", errDesc)
	}
	if clientState != "" {
		q.Set("state", clientState)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// AuthorizeConfig holds configuration for the authorization endpoint.
type AuthorizeConfig struct {
	SigningKey    []byte
	PublicURL     string
	GiteaHost     string
	GiteaClientID string
	Store         *Store
	NowFunc       func() time.Time
}

// HandleAuthorize handles GET /oauth/authorize (AS-2, AS-3, AS-4, UP-1).
func HandleAuthorize(cfg AuthorizeConfig) http.HandlerFunc {
	nowFunc := cfg.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		clientID := q.Get("client_id")
		redirectURI := q.Get("redirect_uri")
		clientState := q.Get("state")
		responseType := q.Get("response_type")
		codeChallenge := q.Get("code_challenge")
		codeChallengeMethod := q.Get("code_challenge_method")
		resource := q.Get("resource")

		// AS-2: Validate client_id signature and redirect_uri.
		// If either is invalid, return an HTML error with NO redirect.
		if clientID == "" {
			writeHTMLError(w, http.StatusBadRequest, "Invalid Request", "Missing client_id.")
			return
		}
		payload, err := VerifyClientID(cfg.SigningKey, clientID)
		if err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid Client", "Client ID signature is invalid or corrupted.")
			return
		}

		if redirectURI == "" {
			writeHTMLError(w, http.StatusBadRequest, "Invalid Request", "Missing redirect_uri.")
			return
		}
		// Exact match against registered redirect URIs (T-AZ-1: case, trailing slash, query variants rejected)
		if !slices.Contains(payload.RedirectURIs, redirectURI) {
			writeHTMLError(w, http.StatusBadRequest, "Invalid Redirect URI", "The redirect_uri is not registered for this client.")
			return
		}

		// At this point, redirect_uri is trusted. Further errors redirect back with RFC 6749 error parameters.
		if responseType != "code" {
			redirectClientError(w, r, redirectURI, "unsupported_response_type", "response_type must be 'code'", clientState)
			return
		}

		// AS-3: PKCE code_challenge required, code_challenge_method must be S256 (T-AZ-2).
		if codeChallenge == "" || codeChallengeMethod != "S256" {
			redirectClientError(w, r, redirectURI, "invalid_request", "code_challenge required with method S256", clientState)
			return
		}

		// AS-4: If resource is present, it must equal <public>/mcp.
		if resource != "" && resource != cfg.PublicURL+"/mcp" {
			redirectClientError(w, r, redirectURI, "invalid_target", "resource must be "+cfg.PublicURL+"/mcp", clientState)
			return
		}

		// UP-1: Generate fresh random state and PKCE verifier toward Gitea.
		upstreamState, err := GenerateRandomString(32)
		if err != nil {
			http.Error(w, "Failed to generate upstream state", http.StatusInternalServerError)
			return
		}

		upstreamVerifier, err := GenerateRandomString(32)
		if err != nil {
			http.Error(w, "Failed to generate upstream PKCE verifier", http.StatusInternalServerError)
			return
		}
		vHash := sha256.Sum256([]byte(upstreamVerifier))
		upstreamChallenge := base64.RawURLEncoding.EncodeToString(vHash[:])

		// Save pending authorization (10 minutes, single use, capped at 1000)
		if !cfg.Store.PutPendingAuth(&PendingAuth{
			UpstreamState:       upstreamState,
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			ClientState:         clientState,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			UpstreamVerifier:    upstreamVerifier,
			ExpiresAt:           nowFunc().Add(10 * time.Minute),
		}) {
			writeHTMLError(w, http.StatusServiceUnavailable, "Service Unavailable", "Server busy.")
			return
		}

		// Redirect to Gitea authorize endpoint with own state, own PKCE, and fixed read scopes.
		// The client's state is never sent to Gitea.
		giteaAuthURL, err := url.Parse(strings.TrimRight(cfg.GiteaHost, "/") + "/login/oauth/authorize")
		if err != nil {
			http.Error(w, "Failed to construct Gitea authorize URL", http.StatusInternalServerError)
			return
		}

		gq := giteaAuthURL.Query()
		gq.Set("client_id", cfg.GiteaClientID)
		gq.Set("redirect_uri", cfg.PublicURL+"/oauth/callback")
		gq.Set("response_type", "code")
		gq.Set("state", upstreamState)
		gq.Set("code_challenge", upstreamChallenge)
		gq.Set("code_challenge_method", "S256")
		gq.Set("scope", "read:repository read:issue read:user read:organization")
		giteaAuthURL.RawQuery = gq.Encode()

		http.Redirect(w, r, giteaAuthURL.String(), http.StatusFound)
	}
}
