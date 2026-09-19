package operation

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHTTPServerConfig(t *testing.T) {
	server := newHTTPServer(":12345", newMCPServer("test"))
	if server.Addr != ":12345" {
		t.Errorf("Addr = %q, want %q", server.Addr, ":12345")
	}
	if server.Handler == nil {
		t.Error("Handler is nil")
	}
	if server.ReadHeaderTimeout != httpReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", server.ReadHeaderTimeout, httpReadHeaderTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want zero for SSE", server.WriteTimeout)
	}
}

func TestHealthzEndpoint(t *testing.T) {
	server := newHTTPServer(":0", newMCPServer("test"))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("body is empty, want a non-empty health message")
	}
}

func TestParseAuthToken(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantOK    bool
	}{
		{
			name:      "valid Bearer token",
			header:    "Bearer validtoken",
			wantToken: "validtoken",
			wantOK:    true,
		},
		{
			name:      "lowercase bearer",
			header:    "bearer lowercase",
			wantToken: "lowercase",
			wantOK:    true,
		},
		{
			name:      "uppercase BEARER",
			header:    "BEARER uppercase",
			wantToken: "uppercase",
			wantOK:    true,
		},
		{
			name:      "token with spaces trimmed",
			header:    "Bearer   spacedToken ",
			wantToken: "spacedToken",
			wantOK:    true,
		},
		{
			name:      "bearer with no token",
			header:    "Bearer ",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "bearer with only spaces",
			header:    "Bearer     ",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "missing space after Bearer",
			header:    "Bearertoken",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "Gitea token format",
			header:    "token giteaapitoken",
			wantToken: "giteaapitoken",
			wantOK:    true,
		},
		{
			name:      "Gitea Token format capitalized",
			header:    "Token giteaapitoken",
			wantToken: "giteaapitoken",
			wantOK:    true,
		},
		{
			name:      "token with no value",
			header:    "token ",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "different auth type",
			header:    "Basic dXNlcjpwYXNz",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "empty header",
			header:    "",
			wantToken: "",
			wantOK:    false,
		},
		{
			name:      "bearer token with internal spaces",
			header:    "Bearer token with spaces",
			wantToken: "token with spaces",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToken, gotOK := parseAuthToken(tt.header)
			if gotToken != tt.wantToken {
				t.Errorf("parseAuthToken() token = %q, want %q", gotToken, tt.wantToken)
			}
			if gotOK != tt.wantOK {
				t.Errorf("parseAuthToken() ok = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}
