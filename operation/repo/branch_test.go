package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRenameBranchFnMissingArgs(t *testing.T) {
	fullArgs := map[string]any{
		"owner":    "octo",
		"repo":     "demo",
		"branch":   "old-name",
		"new_name": "new-name",
	}

	for _, missing := range []string{"owner", "repo", "branch", "new_name"} {
		t.Run(missing, func(t *testing.T) {
			args := map[string]any{}
			for k, v := range fullArgs {
				if k != missing {
					args[k] = v
				}
			}

			result, err := RenameBranchFn(context.Background(), args)
			if err != nil {
				t.Fatalf("RenameBranchFn() error = %v", err)
			}
			if !result.IsError {
				t.Fatalf("RenameBranchFn() with missing %q, want error result", missing)
			}
		})
	}
}

func TestRenameBranchFn(t *testing.T) {
	const (
		owner   = "octo"
		repo    = "demo"
		branch  = "old-name"
		newName = "new-name"
	)

	for _, tc := range []struct {
		name         string
		serverStatus int
		wantErr      bool
		wantContains string
	}{
		{"success", http.StatusNoContent, false, "Branch renamed"},
		{"server error", http.StatusInternalServerError, true, "rename branch error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				gotBody map[string]any
			)

			renamePath := fmt.Sprintf("/api/v1/repos/%s/%s/branches/%s", owner, repo, branch)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/version":
					_, _ = w.Write([]byte(`{"version":"1.24.0"}`))
				case renamePath:
					mu.Lock()
					_ = json.NewDecoder(r.Body).Decode(&gotBody)
					mu.Unlock()
					w.WriteHeader(tc.serverStatus)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			origHost, origToken := flag.Host, flag.Token
			flag.Host, flag.Token = server.URL, ""
			defer func() { flag.Host, flag.Token = origHost, origToken }()

			result, err := RenameBranchFn(context.Background(), map[string]any{
				"owner":    owner,
				"repo":     repo,
				"branch":   branch,
				"new_name": newName,
			})
			if err != nil {
				t.Fatalf("RenameBranchFn() error = %v", err)
			}

			if result.IsError != tc.wantErr {
				t.Fatalf("RenameBranchFn() IsError = %v, want %v (result: %v)", result.IsError, tc.wantErr, result)
			}

			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.wantContains) {
				t.Fatalf("result = %s, want it to contain %q", text, tc.wantContains)
			}

			if !tc.wantErr {
				mu.Lock()
				defer mu.Unlock()
				if gotBody["name"] != newName {
					t.Fatalf("request body name = %v, want %s", gotBody["name"], newName)
				}
			}
		})
	}
}

func TestRenameBranchToolRegistration(t *testing.T) {
	found := false
	for _, registered := range BranchTool.WriteTools() {
		if registered.Tool.Name == RenameBranchToolName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%q is not registered as a write tool", RenameBranchToolName)
	}

	for _, registered := range BranchTool.ReadTools() {
		if registered.Tool.Name == RenameBranchToolName {
			t.Fatalf("%q is registered as a read tool, want write only", RenameBranchToolName)
		}
	}
}
