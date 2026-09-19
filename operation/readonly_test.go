package operation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/operation"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func collectReadOnlyToolEntries() []string {
	origRO := flag.ReadOnly
	defer func() { flag.ReadOnly = origRO }()
	flag.ReadOnly = true

	var entries []string
	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			if ms, ok := st.ScopeKind.(tool.MethodScope); ok {
				var methods []string
				for m := range ms.Methods {
					methods = append(methods, m)
				}
				slices.Sort(methods)
				for _, m := range methods {
					entries = append(entries, fmt.Sprintf("%s:%s", st.Tool.Name, m))
				}
			} else {
				entries = append(entries, st.Tool.Name)
			}
		}
	}
	slices.Sort(entries)
	return entries
}

// T-RO-2: registered read-only tool set equals the golden file.
func TestT_RO_2_GoldenFile(t *testing.T) {
	goldenPath := filepath.Join("..", "pkg", "tool", "testdata", "readonly_tools.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", goldenPath, err)
	}

	rawLines := strings.Split(strings.ReplaceAll(string(goldenData), "\r\n", "\n"), "\n")
	var want []string
	for _, l := range rawLines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			want = append(want, trimmed)
		}
	}

	got := collectReadOnlyToolEntries()

	if !slices.Equal(got, want) {
		t.Errorf("read-only tools mismatch golden file.\nGot (%d entries):\n%s\nWant (%d entries):\n%s",
			len(got), strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
	}
}

func collectOAuthToolEntries() []string {
	origCID := flag.OAuthClientID
	defer func() { flag.OAuthClientID = origCID }()
	flag.OAuthClientID = "test-client-id"

	var entries []string
	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			if ms, ok := st.ScopeKind.(tool.MethodScope); ok {
				var methods []string
				for m := range ms.Methods {
					methods = append(methods, m)
				}
				slices.Sort(methods)
				for _, m := range methods {
					entries = append(entries, fmt.Sprintf("%s:%s", st.Tool.Name, m))
				}
			} else {
				entries = append(entries, st.Tool.Name)
			}
		}
	}
	slices.Sort(entries)
	return entries
}

// T-RO-OAUTH: OAuth-mode tool set equals oauth_tools.golden (SPEC Layer 2).
func TestT_RO_OAUTH_ToolSet(t *testing.T) {
	origCID := flag.OAuthClientID
	t.Cleanup(func() { flag.OAuthClientID = origCID })
	flag.OAuthClientID = "test-client-id"

	goldenPath := filepath.Join("..", "pkg", "tool", "testdata", "oauth_tools.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", goldenPath, err)
	}

	rawLines := strings.Split(strings.ReplaceAll(string(goldenData), "\r\n", "\n"), "\n")
	var want []string
	for _, l := range rawLines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			want = append(want, trimmed)
		}
	}

	got := collectOAuthToolEntries()

	if !slices.Equal(got, want) {
		t.Errorf("oauth tools mismatch golden file.\nGot (%d entries):\n%s\nWant (%d entries):\n%s",
			len(got), strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
	}

	// Verify denied standalone tools are not registered
	deniedStandalone := []string{"actions_config_read", "notification_read", "package_read"}
	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			for _, d := range deniedStandalone {
				if st.Tool.Name == d {
					t.Errorf("denied standalone tool %q found in OAuth mode tools", d)
				}
			}
		}
	}

	// Verify compound tools remove denied methods from advertised schema enum
	// and reject invocation of denied methods like unknown methods
	deniedMethods := map[string][]string{
		"actions_run_read": {"download_job_log", "get_job_log_preview", "download_artifact"},
		"attachment_read":  {"download"},
	}

	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			methods, hasDeniedMethods := deniedMethods[st.Tool.Name]
			if !hasDeniedMethods {
				continue
			}

			// 1. Advertised schema enum must not contain denied methods
			schemaMap, _ := st.Tool.InputSchema.(map[string]any)
			props, _ := schemaMap["properties"].(map[string]any)
			methodProp, _ := props["method"].(map[string]any)
			var enums []string
			if enumVals, ok := methodProp["enum"].([]string); ok {
				enums = enumVals
			} else if enumAny, ok := methodProp["enum"].([]any); ok {
				for _, v := range enumAny {
					if s, ok := v.(string); ok {
						enums = append(enums, s)
					}
				}
			}
			for _, m := range methods {
				if slices.Contains(enums, m) {
					t.Errorf("tool %q advertised method %q in schema enum in OAuth mode", st.Tool.Name, m)
				}
			}

			// 2. Invoking denied method returns unknown method error
			handler := st.MCPHandler()
			for _, m := range methods {
				argsJSON, _ := json.Marshal(map[string]any{"method": m})
				res, err := handler(context.Background(), &mcp.CallToolRequest{
					Params: &mcp.CallToolParamsRaw{
						Name:      st.Tool.Name,
						Arguments: argsJSON,
					},
				})
				if err != nil {
					t.Errorf("tool %q method %q handler unexpected error: %v", st.Tool.Name, m, err)
					continue
				}
				if res == nil || !res.IsError {
					t.Errorf("tool %q method %q expected error result, got success", st.Tool.Name, m)
					continue
				}
				expectedErr := fmt.Sprintf("unknown method %q for tool %s", m, st.Tool.Name)
				if len(res.Content) == 0 {
					t.Errorf("tool %q method %q returned empty error content", st.Tool.Name, m)
					continue
				}
				textContent, ok := res.Content[0].(*mcp.TextContent)
				if !ok || !strings.Contains(textContent.Text, expectedErr) {
					t.Errorf("tool %q method %q error content mismatch: got %v, want %q", st.Tool.Name, m, res.Content[0], expectedErr)
				}
			}
		}
	}
}

// T-RO-3: every registered tool in read-only mode has ReadOnlyHint == true and DestructiveHint == false.
func TestT_RO_3_Annotations(t *testing.T) {
	origRO := flag.ReadOnly
	t.Cleanup(func() { flag.ReadOnly = origRO })
	flag.ReadOnly = true

	count := 0
	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			count++
			if st.Tool.Annotations == nil {
				t.Errorf("tool %q has nil Annotations in read-only mode", st.Tool.Name)
				continue
			}
			if !st.Tool.Annotations.ReadOnlyHint {
				t.Errorf("tool %q has ReadOnlyHint = false in read-only mode", st.Tool.Name)
			}
			if st.Tool.Annotations.DestructiveHint == nil {
				t.Errorf("tool %q has nil DestructiveHint in read-only mode", st.Tool.Name)
			} else if *st.Tool.Annotations.DestructiveHint {
				t.Errorf("tool %q has DestructiveHint = true in read-only mode", st.Tool.Name)
			}
		}
	}
	if count != 33 {
		t.Errorf("expected 33 read-only tools, got %d", count)
	}
}

// TestSchemaEnumMethodCoverage verifies that every compound tool with a "method" schema enum
// has every enum value explicitly declared in its MethodScope, with zero missing or orphaned methods.
func TestSchemaEnumMethodCoverage(t *testing.T) {
	for _, domain := range operation.DomainTools() {
		allTools := append(domain.ReadTools(), domain.WriteTools()...)
		for _, st := range allTools {
			schemaMap, ok := st.Tool.InputSchema.(map[string]any)
			if !ok {
				continue
			}
			props, ok := schemaMap["properties"].(map[string]any)
			if !ok {
				continue
			}
			methodProp, ok := props["method"].(map[string]any)
			if !ok {
				continue
			}
			enumRaw, ok := methodProp["enum"]
			if !ok {
				continue
			}

			ms, ok := st.ScopeKind.(tool.MethodScope)
			if !ok {
				t.Errorf("tool %q has method property but ScopeKind is %T, not MethodScope", st.Tool.Name, st.ScopeKind)
				continue
			}

			var enums []string
			switch v := enumRaw.(type) {
			case []string:
				enums = v
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						enums = append(enums, s)
					}
				}
			}

			// Verify every schema enum method is declared
			for _, m := range enums {
				if _, declared := ms.Methods[m]; !declared {
					t.Errorf("tool %q schema enum method %q is not declared in MethodScope", st.Tool.Name, m)
				}
			}

			// Verify no extra methods declared that are not in schema enum
			for m := range ms.Methods {
				if !slices.Contains(enums, m) {
					t.Errorf("tool %q declared method %q in MethodScope which is absent from schema enum", st.Tool.Name, m)
				}
			}
		}
	}
}

// TestPATModeUnchangedWhenReadOnlyFalse asserts that when read-only mode is off,
// write tools are present, total registered tools equals 55, and annotations
// are not stamped on write tools or destructive tools.
func TestPATModeUnchangedWhenReadOnlyFalse(t *testing.T) {
	origRO := flag.ReadOnly
	t.Cleanup(func() { flag.ReadOnly = origRO })
	flag.ReadOnly = false

	var all []string
	var writeToolsWithoutReadOnlyHint int
	var destructiveToolsWithTrueHint int
	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			all = append(all, st.Tool.Name)
			if st.AccessClass() == tool.AccessClassWrite {
				if st.Tool.Annotations != nil && st.Tool.Annotations.ReadOnlyHint {
					t.Errorf("write tool %q has ReadOnlyHint = true when read-only is off", st.Tool.Name)
				} else {
					writeToolsWithoutReadOnlyHint++
				}
				if st.Tool.Annotations != nil && st.Tool.Annotations.DestructiveHint != nil && *st.Tool.Annotations.DestructiveHint {
					destructiveToolsWithTrueHint++
				}
			}
		}
	}
	if len(all) != 55 {
		t.Errorf("expected 55 tools when flag.ReadOnly=false, got %d", len(all))
	}
	if writeToolsWithoutReadOnlyHint != 22 {
		t.Errorf("expected 22 write tools without ReadOnlyHint when read-only is off, got %d", writeToolsWithoutReadOnlyHint)
	}
	if destructiveToolsWithTrueHint != 9 {
		t.Errorf("expected 9 destructive tools with DestructiveHint=true when read-only is off, got %d", destructiveToolsWithTrueHint)
	}
}

// T-RO-4: run every read-only tool against a test server that records requests;
// assert every request carried the guarded transport's marker.
func TestT_RO_4_AllReadOnlyToolsReachGuardedTransport(t *testing.T) {
	origRO, origHost, origToken := flag.ReadOnly, flag.Host, flag.Token
	t.Cleanup(func() {
		flag.ReadOnly = origRO
		flag.Host = origHost
		flag.Token = origToken
	})

	flag.ReadOnly = true
	flag.Token = "test-token"

	var mu sync.Mutex
	var recordedRequests []*http.Request
	var nonVersionRequests []*http.Request
	var unguardedRequests []string
	var nonGetHeadRequests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		marker := r.Header.Get(gitea.GuardedTransportHeader)
		if marker != gitea.GuardedTransportValue {
			unguardedRequests = append(unguardedRequests, fmt.Sprintf("%s %s (marker=%q)", r.Method, r.URL.Path, marker))
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			nonGetHeadRequests = append(nonGetHeadRequests, fmt.Sprintf("%s %s", r.Method, r.URL.Path))
		}
		recordedRequests = append(recordedRequests, r.Clone(r.Context()))
		if r.URL.Path != "/api/v1/version" {
			nonVersionRequests = append(nonVersionRequests, r.Clone(r.Context()))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/api/v1/version" {
			_, _ = w.Write([]byte(`{"version": "1.27.3"}`))
			return
		}
		if strings.Contains(r.URL.Path, "search") {
			_, _ = w.Write([]byte(`{"ok": true, "data": []}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "s") {
			_, _ = w.Write([]byte(`[]`))
		} else {
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	flag.Host = server.URL

	dummyArgs := map[string]any{
		"owner":         "testowner",
		"repo":          "testrepo",
		"org":           "testorg",
		"query":         "testquery",
		"issue_number":  float64(1),
		"attachment_id": float64(1),
		"id":            float64(1),
		"sha":           "0123456789abcdef0123456789abcdef01234567",
		"tree_sha":      "0123456789abcdef0123456789abcdef01234567",
		"branch":        "main",
		"tag_name":      "v1.0.0",
		"path":          "README.md",
		"ref":           "main",
		"filepath":      "README.md",
		"dirpath":       ".",
		"name":          "testname",
		"pageName":      "Home",
		"workflow_id":   "build.yml",
		"run_id":        float64(1),
		"job_id":        float64(1),
		"artifact_id":   float64(1),
		"page":          float64(1),
		"per_page":      float64(10),
		"type":          "generic",
		"version":       "1.0.0",
		"pull_number":   float64(1),
		"review_id":     float64(1),
	}

	// Exemptions: tool entries that do not communicate over HTTP to Gitea.
	// get_gitea_mcp_server_version is purely in-memory (returns flag.Version).
	exemptions := map[string]string{
		"get_gitea_mcp_server_version": "purely in-memory tool returning flag.Version; does not query Gitea",
	}

	entryRequestCounts := make(map[string]int)

	for _, domain := range operation.DomainTools() {
		for _, st := range domain.Tools() {
			handler := st.MCPHandler()
			if ms, ok := st.ScopeKind.(tool.MethodScope); ok {
				var methods []string
				for method := range ms.Methods {
					methods = append(methods, method)
				}
				slices.Sort(methods)
				for _, method := range methods {
					entryKey := fmt.Sprintf("%s:%s", st.Tool.Name, method)
					mu.Lock()
					startNonVersionCount := len(nonVersionRequests)
					mu.Unlock()

					args := make(map[string]any, len(dummyArgs)+1)
					maps.Copy(args, dummyArgs)
					args["method"] = method
					argsJSON, _ := json.Marshal(args)
					res, err := handler(context.Background(), &mcp.CallToolRequest{
						Params: &mcp.CallToolParamsRaw{
							Name:      st.Tool.Name,
							Arguments: argsJSON,
						},
					})
					if err != nil && (errors.Is(err, gitea.ErrReadOnlyMethodNotAllowed) || strings.Contains(err.Error(), gitea.ErrReadOnlyMethodNotAllowed.Error())) {
						t.Errorf("entry %q returned ErrReadOnlyMethodNotAllowed: %v", entryKey, err)
					}
					if res != nil {
						for _, c := range res.Content {
							if tc, ok := c.(*mcp.TextContent); ok {
								if strings.Contains(tc.Text, gitea.ErrReadOnlyMethodNotAllowed.Error()) {
									t.Errorf("entry %q result content contains ErrReadOnlyMethodNotAllowed: %s", entryKey, tc.Text)
								}
							}
						}
					}
					mu.Lock()
					diff := len(nonVersionRequests) - startNonVersionCount
					mu.Unlock()
					entryRequestCounts[entryKey] = diff
					if diff == 0 {
						var msg string
						if res != nil && len(res.Content) > 0 {
							if tc, ok := res.Content[0].(*mcp.TextContent); ok {
								msg = tc.Text
							}
						}
						t.Logf("NO REQUEST: %s (isErr=%v, msg=%s)", entryKey, res != nil && res.IsError, msg)
					}
				}
			} else {
				entryKey := st.Tool.Name
				mu.Lock()
				startNonVersionCount := len(nonVersionRequests)
				mu.Unlock()

				argsJSON, _ := json.Marshal(dummyArgs)
				res, err := handler(context.Background(), &mcp.CallToolRequest{
					Params: &mcp.CallToolParamsRaw{
						Name:      st.Tool.Name,
						Arguments: argsJSON,
					},
				})
				if err != nil && (errors.Is(err, gitea.ErrReadOnlyMethodNotAllowed) || strings.Contains(err.Error(), gitea.ErrReadOnlyMethodNotAllowed.Error())) {
					t.Errorf("entry %q returned ErrReadOnlyMethodNotAllowed: %v", entryKey, err)
				}
				if res != nil {
					for _, c := range res.Content {
						if tc, ok := c.(*mcp.TextContent); ok {
							if strings.Contains(tc.Text, gitea.ErrReadOnlyMethodNotAllowed.Error()) {
								t.Errorf("entry %q result content contains ErrReadOnlyMethodNotAllowed: %s", entryKey, tc.Text)
							}
						}
					}
				}
				mu.Lock()
				diff := len(nonVersionRequests) - startNonVersionCount
				mu.Unlock()
				entryRequestCounts[entryKey] = diff
				if diff == 0 {
					var msg string
					if res != nil && len(res.Content) > 0 {
						if tc, ok := res.Content[0].(*mcp.TextContent); ok {
							msg = tc.Text
						}
					}
					t.Logf("NO REQUEST: %s (isErr=%v, msg=%s)", entryKey, res != nil && res.IsError, msg)
				}
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(unguardedRequests) > 0 {
		t.Fatalf("found requests reaching test server without guarded transport marker:\n%s",
			strings.Join(unguardedRequests, "\n"))
	}

	if len(nonGetHeadRequests) > 0 {
		t.Fatalf("found requests reaching test server with non-GET/HEAD methods:\n%s",
			strings.Join(nonGetHeadRequests, "\n"))
	}

	// Assert every golden entry produces at least one request, or is in the explicit exemptions map
	goldenEntries := collectReadOnlyToolEntries()
	var unrepresented []string
	for _, entry := range goldenEntries {
		count := entryRequestCounts[entry]
		t.Logf("ENTRY_COUNT %s = %d", entry, count)
		if count == 0 {
			if reason, ok := exemptions[entry]; ok {
				t.Logf("Exempted entry %q: %s", entry, reason)
			} else {
				unrepresented = append(unrepresented, entry)
			}
		}
	}
	if len(unrepresented) > 0 {
		t.Fatalf("The following %d golden entries produced zero requests to guarded transport:\n%s",
			len(unrepresented), strings.Join(unrepresented, "\n"))
	}

	t.Logf("T-RO-4 verified: %d total requests (%d non-version) reached test server across all %d golden entries (%d exempted)",
		len(recordedRequests), len(nonVersionRequests), len(goldenEntries), len(exemptions))
}

// TestT_RO_4_StaticHTTPClientAudit verifies via source code audit that no Gitea HTTP call
// bypasses the guarded sharedTransport across pkg/, operation/, and cmd/.
func TestT_RO_4_StaticHTTPClientAudit(t *testing.T) {
	rootDirs := []string{
		filepath.Join("..", "pkg"),
		filepath.Join("..", "operation"),
		filepath.Join("..", "cmd"),
	}
	prohibitedPatterns := []string{
		"http.DefaultClient",
		"http.DefaultTransport",
		"http.Client{",
		"&http.Client",
		"http.Get(",
		"http.Post(",
		"http.Head(",
		"http.PostForm(",
	}

	for _, root := range rootDirs {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			relPath, err := filepath.Rel("..", path)
			if err != nil {
				return err
			}
			relPath = filepath.ToSlash(relPath)

			// Skip client factory files in pkg/gitea where sharedTransport() is constructed:
			// - pkg/gitea/readonly_transport.go: defines readOnlyTransport and fallback to http.DefaultTransport
			// - pkg/gitea/gitea.go: clones http.DefaultTransport for sharedTransport() and builds SDK client
			// - pkg/gitea/rest.go: constructs restClient and attachmentHTTPClient wrapping sharedTransport()
			// - pkg/oauth: upstream OAuth handshake calls (UP-2, UP-3, UP-4) to Gitea's /login/oauth/access_token
			//   which require POST and are not tool operations.
			if relPath == "pkg/gitea/readonly_transport.go" ||
				relPath == "pkg/gitea/gitea.go" ||
				relPath == "pkg/gitea/rest.go" ||
				relPath == "pkg/oauth/callback.go" ||
				relPath == "pkg/oauth/middleware.go" {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(content)

			// In cmd/cmd.go, healthcheckClient = http.DefaultClient is used exclusively
			// to dial local http://127.0.0.1:<port>/healthz (Docker healthcheck, not Gitea).
			if relPath == "cmd/cmd.go" {
				for _, pattern := range prohibitedPatterns {
					if pattern == "http.DefaultClient" {
						if strings.Count(text, "http.DefaultClient") > 1 {
							t.Errorf("file %s has unexpected additional http.DefaultClient usage", relPath)
						}
						continue
					}
					if strings.Contains(text, pattern) {
						t.Errorf("file %s contains unguarded HTTP call %q", relPath, pattern)
					}
				}
				return nil
			}

			for _, pattern := range prohibitedPatterns {
				if strings.Contains(text, pattern) {
					t.Errorf("file %s contains unguarded HTTP call %q", relPath, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s err: %v", root, err)
		}
	}
}
