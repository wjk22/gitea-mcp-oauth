package packages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPackageReadList(t *testing.T) {
	var mu sync.Mutex
	var gotQuery map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = map[string]string{}
		for k, v := range r.URL.Query() {
			gotQuery[k] = v[0]
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"type":"container","name":"myrepo/myimage","version":"v1.0.0","html_url":"http://example.com","created_at":"2025-01-01T00:00:00Z","owner":{"login":"test-org"},"creator":{"login":"admin"}}]`))
	}))
	defer srv.Close()

	origHost := flag.Host
	flag.Host = srv.URL
	defer func() { flag.Host = origHost }()

	ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

	t.Run("basic list", func(t *testing.T) {
		args := map[string]any{
			"method": "list",
			"owner":  "test-org",
		}

		result, err := packageReadFn(ctx, args)
		if err != nil {
			t.Fatalf("packageReadFn() error: %v", err)
		}
		if result.IsError {
			t.Fatal("packageReadFn() returned error result")
		}

		text := result.Content[0].(*mcp.TextContent).Text
		var packages []map[string]any
		if err := json.Unmarshal([]byte(text), &packages); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if len(packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(packages))
		}
		if packages[0]["name"] != "myrepo/myimage" {
			t.Errorf("expected name 'myrepo/myimage', got %v", packages[0]["name"])
		}
		if packages[0]["owner"] != "test-org" {
			t.Errorf("expected owner 'test-org', got %v", packages[0]["owner"])
		}
	})

	t.Run("with type and query filters", func(t *testing.T) {
		args := map[string]any{
			"method": "list",
			"owner":  "test-org",
			"type":   "container",
			"q":      "myimage",
		}

		_, err := packageReadFn(ctx, args)
		if err != nil {
			t.Fatalf("packageReadFn() error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if gotQuery["type"] != "container" {
			t.Errorf("expected type=container, got %q", gotQuery["type"])
		}
		if gotQuery["q"] != "myimage" {
			t.Errorf("expected q=myimage, got %q", gotQuery["q"])
		}
	})

	t.Run("with pagination", func(t *testing.T) {
		args := map[string]any{
			"method":   "list",
			"owner":    "test-org",
			"page":     float64(2),
			"per_page": float64(10),
		}

		_, err := packageReadFn(ctx, args)
		if err != nil {
			t.Fatalf("packageReadFn() error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if gotQuery["page"] != "2" {
			t.Errorf("expected page=2, got %q", gotQuery["page"])
		}
		if gotQuery["limit"] != "10" {
			t.Errorf("expected limit=10, got %q", gotQuery["limit"])
		}
	})
}

func TestPackageReadListVersions(t *testing.T) {
	var mu sync.Mutex
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.RawPath
		if gotPath == "" {
			gotPath = r.URL.Path
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"type":"container","name":"myrepo/myimage","version":"v1.0.0"},{"id":2,"type":"container","name":"myrepo/myimage","version":"v2.0.0"}]`))
	}))
	defer srv.Close()

	origHost := flag.Host
	flag.Host = srv.URL
	defer func() { flag.Host = origHost }()

	ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

	tests := []struct {
		testName string
		name     string
	}{
		{"raw slash", "myrepo/myimage"},
		{"pre-encoded slash", "myrepo%2Fmyimage"},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			args := map[string]any{
				"method": "list_versions",
				"owner":  "test-org",
				"type":   "container",
				"name":   tt.name,
			}

			result, err := packageReadFn(ctx, args)
			if err != nil {
				t.Fatalf("packageReadFn() error: %v", err)
			}
			if result.IsError {
				t.Fatal("packageReadFn() returned error result")
			}

			mu.Lock()
			wantPath := "/api/v1/packages/test-org/container/myrepo%2Fmyimage"
			if gotPath != wantPath {
				t.Errorf("request path = %q, want %q", gotPath, wantPath)
			}
			mu.Unlock()

			text := result.Content[0].(*mcp.TextContent).Text
			var versions []map[string]any
			if err := json.Unmarshal([]byte(text), &versions); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if len(versions) != 2 {
				t.Fatalf("expected 2 versions, got %d", len(versions))
			}
		})
	}
}

func TestPackageReadGet(t *testing.T) {
	var mu sync.Mutex
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.RawPath
		if gotPath == "" {
			gotPath = r.URL.Path
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"type":"container","name":"myrepo/myimage","version":"v1.0.0","html_url":"http://example.com","created_at":"2025-01-01T00:00:00Z","owner":{"login":"test-org"}}`))
	}))
	defer srv.Close()

	origHost := flag.Host
	flag.Host = srv.URL
	defer func() { flag.Host = origHost }()

	ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

	tests := []struct {
		testName string
		name     string
	}{
		{"raw slash", "myrepo/myimage"},
		{"pre-encoded slash", "myrepo%2Fmyimage"},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			args := map[string]any{
				"method":  "get",
				"owner":   "test-org",
				"type":    "container",
				"name":    tt.name,
				"version": "v1.0.0",
			}

			result, err := packageReadFn(ctx, args)
			if err != nil {
				t.Fatalf("packageReadFn() error: %v", err)
			}
			if result.IsError {
				t.Fatal("packageReadFn() returned error result")
			}

			mu.Lock()
			wantPath := "/api/v1/packages/test-org/container/myrepo%2Fmyimage/v1.0.0"
			if gotPath != wantPath {
				t.Errorf("request path = %q, want %q", gotPath, wantPath)
			}
			mu.Unlock()

			text := result.Content[0].(*mcp.TextContent).Text
			var pkg map[string]any
			if err := json.Unmarshal([]byte(text), &pkg); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if pkg["name"] != "myrepo/myimage" {
				t.Errorf("expected name 'myrepo/myimage', got %v", pkg["name"])
			}
			if pkg["version"] != "v1.0.0" {
				t.Errorf("expected version 'v1.0.0', got %v", pkg["version"])
			}
		})
	}
}

func TestPackageWriteDelete(t *testing.T) {
	var mu sync.Mutex
	var gotMethod string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod = r.Method
		gotPath = r.URL.RawPath
		if gotPath == "" {
			gotPath = r.URL.Path
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	origHost := flag.Host
	flag.Host = srv.URL
	defer func() { flag.Host = origHost }()

	ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

	args := map[string]any{
		"method":  "delete",
		"owner":   "test-org",
		"type":    "container",
		"name":    "myrepo/myimage",
		"version": "v1.0.0",
	}

	result, err := packageWriteFn(ctx, args)
	if err != nil {
		t.Fatalf("packageWriteFn() error: %v", err)
	}
	if result.IsError {
		t.Fatal("packageWriteFn() returned error result")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotMethod != "DELETE" {
		t.Errorf("expected DELETE method, got %q", gotMethod)
	}
	wantPath := "/api/v1/packages/test-org/container/myrepo%2Fmyimage/v1.0.0"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
}

func TestPackageUnknownMethod(t *testing.T) {
	for name, fn := range map[string]func(context.Context, map[string]any) (*mcp.CallToolResult, error){
		"packageReadFn":  packageReadFn,
		"packageWriteFn": packageWriteFn,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := fn(context.Background(), map[string]any{"method": "bogus", "owner": "test-org"})
			if err != nil {
				t.Fatalf("%s() error = %v", name, err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("%s() result = %#v, want an error result", name, result)
			}
		})
	}
}

func TestEscapePackageName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple name", "mypackage", "mypackage"},
		{"raw slash", "myrepo/myimage", "myrepo%2Fmyimage"},
		{"pre-encoded slash", "myrepo%2Fmyimage", "myrepo%2Fmyimage"},
		{"pre-encoded uppercase", "myrepo%2Fmyimage", "myrepo%2Fmyimage"},
		{"multiple slashes", "a/b/c", "a%2Fb%2Fc"},
		{"pre-encoded multiple slashes", "a%2Fb%2Fc", "a%2Fb%2Fc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapePackageName(tt.input)
			if got != tt.expected {
				t.Errorf("escapePackageName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSlimPackage(t *testing.T) {
	raw := map[string]any{
		"id":         float64(1),
		"type":       "container",
		"name":       "test-repo/test-image",
		"version":    "v1.0.0",
		"html_url":   "http://example.com/pkg",
		"created_at": "2025-01-01T00:00:00Z",
		"owner":      map[string]any{"login": "test-org", "id": float64(2), "email": ""},
		"creator":    map[string]any{"login": "admin", "id": float64(1)},
		"repository": map[string]any{"full_name": "test-org/test-repo", "id": float64(3)},
	}

	slim := slimPackage(raw)
	if slim["owner"] != "test-org" {
		t.Errorf("expected owner 'test-org', got %v", slim["owner"])
	}
	if slim["creator"] != "admin" {
		t.Errorf("expected creator 'admin', got %v", slim["creator"])
	}
	if slim["repository"] != "test-org/test-repo" {
		t.Errorf("expected repository 'test-org/test-repo', got %v", slim["repository"])
	}
	if _, ok := slim["owner"].(map[string]any); ok {
		t.Error("expected owner to be a string, not a map")
	}
}
