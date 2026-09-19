package milestone

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Test_milestoneWriteFn_dueOn(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
		id    = 42
		due   = "2026-05-18T23:59:59Z"
	)

	var (
		mu     sync.Mutex
		bodies = map[string]map[string]any{}
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/milestones", owner, repo),
			fmt.Sprintf("/api/v1/repos/%s/%s/milestones/%d", owner, repo, id):
			mu.Lock()
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies[r.Method] = body
			mu.Unlock()
			_, _ = w.Write(fmt.Appendf(nil, `{"id":%d,"title":"v1","due_on":%q}`, id, due))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{"owner": owner, "repo": repo, "due_on": due}

	cases := []struct {
		name   string
		fn     func(context.Context, map[string]any) (*mcp.CallToolResult, error)
		method string
		extra  map[string]any
	}{
		{"create", createMilestoneFn, http.MethodPost, map[string]any{"title": "v1"}},
		{"edit", editMilestoneFn, http.MethodPatch, map[string]any{"id": float64(id)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := map[string]any{}
			maps.Copy(a, args)
			maps.Copy(a, tc.extra)
			res, err := tc.fn(context.Background(), a)
			if err != nil || res.IsError {
				t.Fatalf("%s err=%v result=%v", tc.name, err, res)
			}
			mu.Lock()
			body := bodies[tc.method]
			mu.Unlock()
			if got, _ := body["due_on"].(string); got != due {
				t.Fatalf("%s: expected due_on=%q, got %v (body: %v)", tc.name, due, got, body)
			}
		})
	}
}
