package issue

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

func Test_listRepoIssuesFn_filters(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	var (
		mu       sync.Mutex
		gotQuery string
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case r.URL.Path == fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case r.URL.Path == fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			mu.Lock()
			gotQuery = r.URL.RawQuery
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	origHost := flag.Host
	origToken := flag.Token
	origVersion := flag.Version
	flag.Host = server.URL
	flag.Token = ""
	flag.Version = "test"
	defer func() {
		flag.Host = origHost
		flag.Token = origToken
		flag.Version = origVersion
	}()

	args := map[string]any{
		"owner":      owner,
		"repo":       repo,
		"type":       "issues",
		"labels":     []any{"bug", "enhancement"},
		"milestones": []any{"v1.0", "2"},
		"since":      "2026-01-01T00:00:00Z",
	}

	_, err := listRepoIssuesFn(context.Background(), args)
	if err != nil {
		t.Fatalf("listRepoIssuesFn() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !strings.Contains(gotQuery, "labels=bug%2Cenhancement") {
		t.Fatalf("expected labels query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "since=2026-01-01") {
		t.Fatalf("expected since query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "milestones=v1.0%2C2") {
		t.Fatalf("expected milestones query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "type=issues") {
		t.Fatalf("expected type query param, got %s", gotQuery)
	}
}

func Test_listRepoIssuesFn_includesMilestone(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"number": 1, "title": "with milestone", "state": "closed", "milestone": {"id": 5, "title": "v1.0"}},
				{"number": 2, "title": "without milestone", "state": "open"}
			]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{
		"owner": owner, "repo": repo,
	}
	res, err := listRepoIssuesFn(context.Background(), args)
	if err != nil {
		t.Fatalf("listRepoIssuesFn() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res.Content)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `"milestone"`) || !strings.Contains(body, `"v1.0"`) {
		t.Fatalf("expected milestone in list output, got: %s", body)
	}
}

func Test_createIssueFn_labels(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	var (
		mu      sync.Mutex
		gotBody map[string]any
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			mu.Lock()
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotBody = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":1,"title":"test","state":"open"}`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	origHost := flag.Host
	origToken := flag.Token
	origVersion := flag.Version
	flag.Host = server.URL
	flag.Token = ""
	flag.Version = "test"
	defer func() {
		flag.Host = origHost
		flag.Token = origToken
		flag.Version = origVersion
	}()

	args := map[string]any{
		"owner":    owner,
		"repo":     repo,
		"title":    "test issue",
		"body":     "body",
		"labels":   []any{float64(10), float64(20)},
		"deadline": "2026-06-01T00:00:00Z",
	}

	_, err := createIssueFn(context.Background(), args)
	if err != nil {
		t.Fatalf("createIssueFn() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %v", gotBody["labels"])
	}
	if labels[0] != float64(10) || labels[1] != float64(20) {
		t.Fatalf("expected labels [10,20], got %v", labels)
	}
	if gotBody["due_date"] == nil {
		t.Fatalf("expected due_date to be set")
	}
}

func Test_getIssueByIndexFn_includesAttachments(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues/42", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"number": 42,
				"title": "bug with screenshot",
				"body": "see attached",
				"state": "open",
				"assets": [
					{"id": 1, "name": "shot.png", "size": 1024, "browser_download_url": "https://example/shot.png"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{
		"owner": owner, "repo": repo, "issue_number": float64(42),
	}
	res, err := getIssueByIndexFn(context.Background(), args)
	if err != nil {
		t.Fatalf("getIssueByIndexFn() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res.Content)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `[shot.png](https://example/shot.png)`) {
		t.Fatalf("expected attachment markdown inlined in body, got: %s", body)
	}
	if strings.Contains(body, `"attachments"`) {
		t.Fatalf("attachments should be inlined into body, not a separate field: %s", body)
	}
}

func Test_getIssueCommentsByIndexFn_includesAttachments(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues/7/comments", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id": 1, "body": "see this", "assets": [
					{"id": 9, "name": "log.txt", "size": 200, "browser_download_url": "https://example/log.txt"}
				]},
				{"id": 2, "body": "no attachment", "assets": []}
			]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{
		"owner": owner, "repo": repo, "issue_number": float64(7),
	}
	res, err := getIssueCommentsByIndexFn(context.Background(), args)
	if err != nil {
		t.Fatalf("getIssueCommentsByIndexFn() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res.Content)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `[log.txt](https://example/log.txt)`) {
		t.Fatalf("expected attachment markdown inlined in body, got: %s", body)
	}
	if strings.Contains(body, `"attachments"`) {
		t.Fatalf("attachments should be inlined into body, not a separate field: %s", body)
	}
}
