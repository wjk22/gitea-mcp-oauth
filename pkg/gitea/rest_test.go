package gitea

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestTokenFromContext(t *testing.T) {
	orig := flag.Token
	defer func() { flag.Token = orig }()

	flag.Token = "flag-token"

	t.Run("context token wins", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "ctx-token")
		if got := tokenFromContext(ctx); got != "ctx-token" {
			t.Fatalf("tokenFromContext() = %q, want %q", got, "ctx-token")
		}
	})

	t.Run("fallback to flag token", func(t *testing.T) {
		ctx := context.Background()
		if got := tokenFromContext(ctx); got != "flag-token" {
			t.Fatalf("tokenFromContext() = %q, want %q", got, "flag-token")
		}
	})
}

func TestDoJSON_LimitsErrorResponseBody(t *testing.T) {
	payload := strings.Repeat("x", errBodySnippetSize+100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	origHost := flag.Host
	defer func() { flag.Host = origHost }()
	flag.Host = srv.URL

	status, err := DoJSON(context.Background(), http.MethodGet, "repos/owner/repo", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, status)
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if len(httpErr.Body) != errBodySnippetSize {
		t.Fatalf("expected body length %d, got %d", errBodySnippetSize, len(httpErr.Body))
	}
}
