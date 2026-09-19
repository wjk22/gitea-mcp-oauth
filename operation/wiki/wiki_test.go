package wiki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestWikiWriteBase64Encoding(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		content string
	}{
		{"create ascii", "create", "Hello, World!"},
		{"create unicode", "create", "日本語テスト 🎉"},
		{"create multiline", "create", "line1\nline2\nline3"},
		{"update ascii", "update", "Updated content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"title":"test"}`))
			}))
			defer srv.Close()

			origHost := flag.Host
			flag.Host = srv.URL
			defer func() { flag.Host = origHost }()

			ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

			args := map[string]any{
				"method":   tt.method,
				"owner":    "org",
				"repo":     "repo",
				"content":  tt.content,
				"pageName": "TestPage",
				"title":    "TestPage",
			}

			result, err := wikiWriteFn(ctx, args)
			if err != nil {
				t.Fatalf("wikiWriteFn() error: %v", err)
			}
			if result.IsError {
				t.Fatalf("wikiWriteFn() returned error result")
			}

			got := gotBody["content_base64"]
			want := base64.StdEncoding.EncodeToString([]byte(tt.content))
			if got != want {
				t.Errorf("content_base64 = %q, want %q", got, want)
			}
		})
	}
}
