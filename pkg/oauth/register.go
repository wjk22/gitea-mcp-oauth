package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// RegistrationRequest holds RFC 7591 client registration request fields (AS-1).
type RegistrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
}

// RegistrationResponse holds RFC 7591 registration response fields (AS-1).
type RegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeJSONError(w http.ResponseWriter, status int, errCode, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error:            errCode,
		ErrorDescription: desc,
	})
}

// HandleRegister handles RFC 7591 dynamic client registration at POST /oauth/register (AS-1).
func HandleRegister(signingKey []byte, allowlist []string, nowFunc func() time.Time) http.HandlerFunc {
	if nowFunc == nil {
		nowFunc = time.Now
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "error reading request body")
			return
		}

		var req RegistrationRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
			return
		}

		// AS-1: Supported methods: none, client_secret_post, client_secret_basic. Default is client_secret_basic.
		authMethod := req.TokenEndpointAuthMethod
		if authMethod == "" {
			authMethod = "client_secret_basic"
		}
		switch authMethod {
		case "none", "client_secret_post", "client_secret_basic":
			// supported
		default:
			writeJSONError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
			return
		}

		// AS-1: Every redirect_uri must exactly match an entry on the allowlist (HTTPS, no wildcard, no prefix match).
		if err := ValidateRedirectURIs(req.RedirectURIs, allowlist); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}

		iat := nowFunc().Unix()
		payload := ClientIDPayload{
			RedirectURIs:            req.RedirectURIs,
			TokenEndpointAuthMethod: authMethod,
			IssuedAt:                iat,
		}

		clientID, err := SignClientID(signingKey, payload)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server_error", "failed to sign client ID")
			return
		}

		resp := RegistrationResponse{
			ClientID:                clientID,
			ClientIDIssuedAt:        iat,
			RedirectURIs:            req.RedirectURIs,
			TokenEndpointAuthMethod: authMethod,
		}

		if authMethod == "client_secret_post" || authMethod == "client_secret_basic" {
			resp.ClientSecret = ComputeClientSecret(signingKey, clientID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
