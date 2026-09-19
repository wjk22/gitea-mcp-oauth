package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestCheckRedirect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		wantErr error
	}{
		{"allows GET", http.MethodGet, nil},
		{"allows HEAD", http.MethodHead, nil},
		{"blocks PATCH", http.MethodPatch, http.ErrUseLastResponse},
		{"blocks POST", http.MethodPost, http.ErrUseLastResponse},
		{"blocks PUT", http.MethodPut, http.ErrUseLastResponse},
		{"blocks DELETE", http.MethodDelete, http.ErrUseLastResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			via := []*http.Request{{Method: tc.method}}
			err := checkRedirect(nil, via)
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("stops after 10 redirects", func(t *testing.T) {
		via := make([]*http.Request, 10)
		for i := range via {
			via[i] = &http.Request{Method: http.MethodGet}
		}
		err := checkRedirect(nil, via)
		if err == nil || err == http.ErrUseLastResponse {
			t.Fatalf("expected redirect limit error, got %v", err)
		}
	})
}

// TestDoJSON_RepoRenameRedirect is a regression test for the bug where a PATCH
// request to a renamed repo got a 301 redirect, Go's http.Client silently
// changed the method to GET, and the write appeared to succeed without error.
func TestDoJSON_RepoRenameRedirect(t *testing.T) {
	// Simulate a Gitea API that returns 301 for the old repo name (like a renamed repo).
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/v1/repos/owner/old-name/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/repos/owner/new-name/pulls/1", http.StatusMovedPermanently)
	})
	mux.HandleFunc("PATCH /api/v1/repos/owner/new-name/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":1,"title":"updated"}`)
	})
	mux.HandleFunc("GET /api/v1/repos/owner/new-name/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":1,"title":"not-updated"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origHost := flag.Host
	defer func() { flag.Host = origHost }()
	flag.Host = srv.URL

	var result map[string]any
	status, err := DoJSON(context.Background(), http.MethodPatch, "repos/owner/old-name/pulls/1", nil, map[string]string{"title": "updated"}, &result)
	if err != nil {
		// The redirect should be blocked, returning the 301 response directly.
		// DoJSON treats non-2xx as an error, which is the correct behavior.
		if status != http.StatusMovedPermanently {
			t.Fatalf("expected status 301, got %d (err: %v)", status, err)
		}
		return
	}

	// If we reach here without error, the redirect was followed. Verify the
	// method was preserved (title should be "updated", not "not-updated").
	title, _ := result["title"].(string)
	if title == "not-updated" {
		t.Fatal("PATCH was silently converted to GET on 301 redirect — write was lost")
	}
}

// TestDoJSON_GETRedirectFollowed verifies that GET requests still follow redirects normally.
func TestDoJSON_GETRedirectFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/owner/old-name/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/repos/owner/new-name/pulls/1", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /api/v1/repos/owner/new-name/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "title": "found"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origHost := flag.Host
	defer func() { flag.Host = origHost }()
	flag.Host = srv.URL

	var result map[string]any
	status, err := DoJSON(context.Background(), http.MethodGet, "repos/owner/old-name/pulls/1", nil, nil, &result)
	if err != nil {
		t.Fatalf("GET redirect should be followed, got error: %v (status %d)", err, status)
	}
	title, _ := result["title"].(string)
	if title != "found" {
		t.Fatalf("expected title 'found', got %q", title)
	}
}
