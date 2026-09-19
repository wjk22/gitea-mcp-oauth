package repo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateOrUpdateFileFnNewBranch(t *testing.T) {
	const (
		owner      = "octo"
		repo       = "demo"
		filePath   = "README.md"
		baseBranch = "main"
		newBranch  = "feature-x"
	)

	var (
		mu      sync.Mutex
		gotBody map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		gotBody = body
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	for _, tc := range []struct {
		name       string
		sha        string
		wantResult string
	}{
		{"create", "", "Create file success on branch " + newBranch},
		{"update", "blobsha", "Update file success on branch " + newBranch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CreateOrUpdateFileFn(context.Background(), map[string]any{
				"owner":           owner,
				"repo":            repo,
				"path":            filePath,
				"content":         "hello",
				"message":         "update readme",
				"branch_name":     baseBranch,
				"new_branch_name": newBranch,
				"sha":             tc.sha,
			})
			if err != nil {
				t.Fatalf("CreateOrUpdateFileFn() error = %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if gotBody["new_branch"] != newBranch {
				t.Fatalf("new_branch = %v, want %s", gotBody["new_branch"], newBranch)
			}
			if gotBody["branch"] != baseBranch {
				t.Fatalf("branch = %v, want %s", gotBody["branch"], baseBranch)
			}
			if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, tc.wantResult) {
				t.Fatalf("result = %s, want it to contain %q", text, tc.wantResult)
			}
		})
	}
}
