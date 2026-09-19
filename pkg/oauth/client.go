package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ClientIDPayload contains the stateless registration metadata embedded in a client ID (AS-1).
type ClientIDPayload struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	IssuedAt                int64    `json:"iat"`
}

// SignClientID generates a stateless client ID containing the payload and HMAC-SHA256 signature (AS-1).
func SignClientID(key []byte, payload ClientIDPayload) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("signing key must be 32 bytes, got %d", len(key))
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal client id payload: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(jsonBytes)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadB64 + "." + sigB64, nil
}

// VerifyClientID verifies the HMAC signature of a client ID and decodes its payload (AS-1, AS-2).
func VerifyClientID(key []byte, clientID string) (*ClientIDPayload, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("signing key must be 32 bytes, got %d", len(key))
	}
	parts := strings.Split(clientID, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid client id format")
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	expectedSig := mac.Sum(nil)

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode client id signature: %w", err)
	}
	if !hmac.Equal(sig, expectedSig) {
		return nil, errors.New("invalid client id signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode client id payload: %w", err)
	}
	var payload ClientIDPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal client id payload: %w", err)
	}
	return &payload, nil
}

// ComputeClientSecret computes the deterministic client secret for confidential methods (AS-1).
func ComputeClientSecret(key []byte, clientID string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("secret:" + clientID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidateRedirectURIs checks that all redirect URIs are HTTPS and exact matches in allowlist (AS-1).
func ValidateRedirectURIs(uris, allowlist []string) error {
	if len(uris) == 0 {
		return errors.New("redirect_uris must not be empty")
	}

	allowset := make(map[string]struct{}, len(allowlist))
	for _, a := range allowlist {
		allowset[a] = struct{}{}
	}

	for _, uri := range uris {
		u, err := url.Parse(uri)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("redirect_uri %q must be valid absolute HTTPS URL", uri)
		}
		if _, ok := allowset[uri]; !ok {
			return fmt.Errorf("redirect_uri %q is not on allowlist", uri)
		}
	}
	return nil
}
