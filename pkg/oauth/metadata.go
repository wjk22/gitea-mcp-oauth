package oauth

import (
	"encoding/json"
	"net/http"
)

// AuthorizationServerMetadata holds RFC 8414 AS metadata fields (AS-8).
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// ProtectedResourceMetadata holds RFC 9728 protected resource metadata fields (AS-8).
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// HandleAuthorizationServerMetadata serves RFC 8414 metadata (AS-8).
func HandleAuthorizationServerMetadata(publicURL string) http.HandlerFunc {
	meta := AuthorizationServerMetadata{
		Issuer:                            publicURL,
		AuthorizationEndpoint:             publicURL + "/oauth/authorize",
		TokenEndpoint:                     publicURL + "/oauth/token",
		RegistrationEndpoint:              publicURL + "/oauth/register",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_post", "client_secret_basic"},
	}
	body, _ := json.Marshal(meta)

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// HandleProtectedResourceMetadata serves RFC 9728 metadata (AS-8).
func HandleProtectedResourceMetadata(publicURL string) http.HandlerFunc {
	meta := ProtectedResourceMetadata{
		Resource:               publicURL + "/mcp",
		AuthorizationServers:   []string{publicURL},
		BearerMethodsSupported: []string{"header"},
	}
	body, _ := json.Marshal(meta)

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// HandleOpenIDConfiguration returns 404 for OpenID discovery (AS-8).
func HandleOpenIDConfiguration() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

// HandleNotImplemented returns 501 for unbuilt OAuth endpoints.
func HandleNotImplemented(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, endpoint+" is not implemented yet", http.StatusNotImplemented)
	}
}
