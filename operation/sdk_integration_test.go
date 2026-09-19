package operation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	projectTo "gitea.com/gitea/gitea-mcp/pkg/to"
	projectTool "gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pin negotiated versions so SDK upgrades require compatibility review.
const (
	testServerVersion       = "test-version"
	expectedProtocolVersion = "2026-07-28"
)

func exposeAllTools(t *testing.T) {
	t.Helper()
	originalReadOnly := flag.ReadOnly
	originalAllowedTools := flag.AllowedTools
	originalAllowedScopes := flag.AllowedScopes
	originalVersion := flag.Version
	t.Cleanup(func() {
		flag.ReadOnly = originalReadOnly
		flag.AllowedTools = originalAllowedTools
		flag.AllowedScopes = originalAllowedScopes
		flag.Version = originalVersion
	})
	flag.ReadOnly = false
	flag.AllowedTools = nil
	flag.AllowedScopes = nil
	flag.Version = testServerVersion
}

// registeredToolCount is what the registry exposes under the current flags, so
// the transport assertions track tool additions without being edited.
func registeredToolCount() int {
	count := 0
	for _, domain := range domainTools {
		count += len(domain.Tools())
	}
	return count
}

// stdioCommandEnvironment removes variables that override subprocess flags.
func stdioCommandEnvironment() []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GITEA_READONLY", "GITEA_SCOPES", "GITEA_TOOLS", "MCP_MODE":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want *mcp.TextContent", result.Content[0])
	}
	return content.Text
}

// listAndCallVersion is the round trip every transport must support. wantText
// differs per transport: the stdio subprocess resolves its version from the VCS
// build info (main.go:14), so only the in-process servers have a known one.
func listAndCallVersion(ctx context.Context, t *testing.T, session *mcp.ClientSession, wantText string) *mcp.ListToolsResult {
	t.Helper()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if want := registeredToolCount(); len(result.Tools) != want {
		t.Fatalf("ListTools() count = %d, want %d", len(result.Tools), want)
	}
	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_gitea_mcp_server_version",
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if got := textContent(t, callResult); !strings.Contains(got, wantText) {
		t.Errorf("version tool result = %q, want it to contain %q", got, wantText)
	}
	return result
}

func assertToolsOnlyCapabilities(t *testing.T, capabilities *mcp.ServerCapabilities) {
	t.Helper()
	wireCapabilities, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatalf("Marshal(server capabilities) error = %v", err)
	}
	// Any extra capability, or listChanged, changes these bytes.
	if want := `{"tools":{}}`; string(wireCapabilities) != want {
		t.Errorf("server capabilities = %s, want %s", wireCapabilities, want)
	}
}

type rawRPCResponse struct {
	status int
	header http.Header
	body   []byte
}

type rpcRequest struct {
	protocolVersion string
	methodHeader    string
	nameHeader      string
	method          string
	params          map[string]any
}

// postRPCRequest exists because the SDK client cannot be pinned to an old
// protocol version or made to send mismatched headers.
func postRPCRequest(t *testing.T, server *httptest.Server, call rpcRequest) rawRPCResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  call.method,
		"params":  call.params,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if call.protocolVersion != "" {
		request.Header.Set("Mcp-Protocol-Version", call.protocolVersion)
	}
	if call.methodHeader != "" {
		request.Header.Set("Mcp-Method", call.methodHeader)
	}
	if call.nameHeader != "" {
		request.Header.Set("Mcp-Name", call.nameHeader)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", call.method, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return rawRPCResponse{status: response.StatusCode, header: response.Header.Clone(), body: responseBody}
}

func modernRequestMeta(protocolVersion string) map[string]any {
	return map[string]any{
		mcp.MetaKeyProtocolVersion:    protocolVersion,
		mcp.MetaKeyClientInfo:         map[string]any{"name": "gitea-mcp-wire-test", "version": "1"},
		mcp.MetaKeyClientCapabilities: map[string]any{},
	}
}

func rpcPayload(response rawRPCResponse) []byte {
	payload := bytes.TrimSpace(response.body)
	for line := range bytes.SplitSeq(payload, []byte("\n")) {
		if data, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			return data
		}
	}
	return payload
}

func rpcResult(t *testing.T, response rawRPCResponse) json.RawMessage {
	t.Helper()
	var wire struct {
		Result json.RawMessage  `json:"result"`
		Error  *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(rpcPayload(response), &wire); err != nil {
		t.Fatalf("Unmarshal(JSON-RPC response) error = %v; body = %s", err, response.body)
	}
	if wire.Error != nil {
		t.Fatalf("JSON-RPC response has error %s", *wire.Error)
	}
	if len(wire.Result) == 0 {
		t.Fatalf("JSON-RPC response has no result: %s", response.body)
	}
	return wire.Result
}

func rpcErrorCode(t *testing.T, response rawRPCResponse) int {
	t.Helper()
	payload := rpcPayload(response)
	var wire struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", payload, err)
	}
	if wire.Error == nil {
		t.Fatalf("response has no JSON-RPC error: %s", response.body)
	}
	return wire.Error.Code
}

// Regression test for https://gitea.com/gitea/gitea-mcp/issues/229
func callMissingRequiredArgument(ctx context.Context, t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_issues",
		Arguments: map[string]any{"state": "open"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v, want a tool result", err)
	}
	if !result.IsError {
		t.Errorf("IsError = false, want true for a call without the required query")
	}
	if got := textContent(t, result); !strings.Contains(got, "query is required") {
		t.Errorf("result = %q, want it to name the missing argument", got)
	}
}

func TestOfficialSDKInMemory(t *testing.T) {
	exposeAllTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(testServerVersion)
	RegisterTool(server)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "gitea-mcp-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := session.InitializeResult().ProtocolVersion; got != expectedProtocolVersion {
		t.Errorf("protocol version = %q, want %q", got, expectedProtocolVersion)
	}
	assertToolsOnlyCapabilities(t, session.InitializeResult().Capabilities)
	listAndCallVersion(ctx, t, session, testServerVersion)
	callMissingRequiredArgument(ctx, t, session)
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("server Run() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("server did not stop after the client session closed")
	}
}

func TestStreamableHTTP(t *testing.T) {
	exposeAllTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := newMCPServer(testServerVersion)
	RegisterTool(server)
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "gitea-mcp-http-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpTestServer.URL + "/mcp",
		HTTPClient:           httpTestServer.Client(),
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()
	if got := session.InitializeResult().ProtocolVersion; got != expectedProtocolVersion {
		t.Errorf("protocol version = %q, want %q", got, expectedProtocolVersion)
	}
	assertToolsOnlyCapabilities(t, session.InitializeResult().Capabilities)
	firstList := listAndCallVersion(ctx, t, session, testServerVersion)

	secondList, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("second ListTools() error = %v", err)
	}
	if !slices.EqualFunc(firstList.Tools, secondList.Tools, func(a, b *mcp.Tool) bool { return a.Name == b.Name }) {
		t.Error("tools/list order changed between requests")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpTestServer.URL+"/not-mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest(GET outside /mcp) error = %v", err)
	}
	response, err := httpTestServer.Client().Do(request)
	if err != nil {
		t.Fatalf("GET outside /mcp error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("GET outside /mcp status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request, err := http.NewRequestWithContext(ctx, method, httpTestServer.URL+"/mcp", nil)
		if err != nil {
			t.Fatalf("NewRequest(%s) error = %v", method, err)
		}
		response, err := httpTestServer.Client().Do(request)
		if err != nil {
			t.Fatalf("%s /mcp error = %v", method, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp status = %d, want %d", method, response.StatusCode, http.StatusMethodNotAllowed)
		}
		if allow := response.Header.Get("Allow"); allow != http.MethodPost {
			t.Errorf("%s /mcp Allow = %q, want %q", method, allow, http.MethodPost)
		}
	}
}

func TestStreamableHTTP20260728Wire(t *testing.T) {
	invoked := make(chan struct{}, 1)
	server := newMCPServer(testServerVersion)
	definition := &mcp.Tool{
		Name:        "test_execution_error",
		Description: "Record valid calls and return a controlled tool execution error.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
	server.AddTool(definition, projectTool.ServerTool{
		Tool: definition,
		Handler: func(context.Context, map[string]any) (*mcp.CallToolResult, error) {
			invoked <- struct{}{}
			return projectTo.ErrorResult(errors.New("controlled execution failure"))
		},
	}.MCPHandler())
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	callParams := map[string]any{
		"_meta":     modernRequestMeta(expectedProtocolVersion),
		"name":      definition.Name,
		"arguments": map[string]any{},
	}

	t.Run("discover", func(t *testing.T) {
		discover := postRPCRequest(t, httpTestServer, rpcRequest{
			protocolVersion: expectedProtocolVersion,
			methodHeader:    "server/discover",
			method:          "server/discover",
			params:          map[string]any{"_meta": modernRequestMeta(expectedProtocolVersion)},
		})
		if discover.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", discover.status, http.StatusOK, discover.body)
		}
		if sessionID := discover.header.Get("Mcp-Session-Id"); sessionID != "" {
			t.Errorf("Mcp-Session-Id = %q, want empty", sessionID)
		}
		var result struct {
			Meta              map[string]json.RawMessage `json:"_meta"`
			SupportedVersions []string                   `json:"supportedVersions"`
			Capabilities      *mcp.ServerCapabilities    `json:"capabilities"`
		}
		if err := json.Unmarshal(rpcResult(t, discover), &result); err != nil {
			t.Fatalf("Unmarshal(server/discover) error = %v", err)
		}
		if !slices.Contains(result.SupportedVersions, expectedProtocolVersion) {
			t.Errorf("supportedVersions = %v, want %q", result.SupportedVersions, expectedProtocolVersion)
		}
		assertToolsOnlyCapabilities(t, result.Capabilities)
		var serverInfo mcp.Implementation
		if err := json.Unmarshal(result.Meta[mcp.MetaKeyServerInfo], &serverInfo); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", mcp.MetaKeyServerInfo, err)
		}
		if serverInfo.Name != "Gitea MCP Server" || serverInfo.Version != testServerVersion {
			t.Errorf("serverInfo = %+v, want Gitea MCP Server %s", serverInfo, testServerVersion)
		}
	})

	t.Run("tool execution error", func(t *testing.T) {
		call := postRPCRequest(t, httpTestServer, rpcRequest{
			protocolVersion: expectedProtocolVersion,
			methodHeader:    "tools/call",
			nameHeader:      definition.Name,
			method:          "tools/call",
			params:          callParams,
		})
		if call.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", call.status, http.StatusOK, call.body)
		}
		if sessionID := call.header.Get("Mcp-Session-Id"); sessionID != "" {
			t.Errorf("Mcp-Session-Id = %q, want empty", sessionID)
		}
		var result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(rpcResult(t, call), &result); err != nil {
			t.Fatalf("Unmarshal(tools/call) error = %v", err)
		}
		if !result.IsError {
			t.Error("isError = false, want true")
		}
		if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text != "controlled execution failure" {
			t.Errorf("content = %+v, want controlled execution failure text", result.Content)
		}
		select {
		case <-invoked:
		default:
			t.Error("valid tools/call did not invoke the tool handler")
		}
	})

	for _, test := range []struct {
		name            string
		protocolVersion string
		methodHeader    string
		nameHeader      string
	}{
		{name: "missing protocol", methodHeader: "tools/call", nameHeader: "test_execution_error"},
		{name: "mismatched protocol", protocolVersion: "2025-11-25", methodHeader: "tools/call", nameHeader: "test_execution_error"},
		{name: "missing method", protocolVersion: expectedProtocolVersion, nameHeader: "test_execution_error"},
		{name: "mismatched method", protocolVersion: expectedProtocolVersion, methodHeader: "tools/list", nameHeader: "test_execution_error"},
		{name: "missing name", protocolVersion: expectedProtocolVersion, methodHeader: "tools/call"},
		{name: "mismatched name", protocolVersion: expectedProtocolVersion, methodHeader: "tools/call", nameHeader: "wrong_tool"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := postRPCRequest(t, httpTestServer, rpcRequest{
				protocolVersion: test.protocolVersion,
				methodHeader:    test.methodHeader,
				nameHeader:      test.nameHeader,
				method:          "tools/call",
				params:          callParams,
			})
			if response.status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", response.status, http.StatusBadRequest)
			}
			if code := rpcErrorCode(t, response); code != mcp.CodeHeaderMismatch {
				t.Errorf("error code = %d, want %d", code, mcp.CodeHeaderMismatch)
			}
			select {
			case <-invoked:
				t.Error("invalid headers invoked the tool handler")
			default:
			}
		})
	}

	t.Run("unsupported protocol", func(t *testing.T) {
		futureVersion := "2027-01-01"
		response := postRPCRequest(t, httpTestServer, rpcRequest{
			protocolVersion: futureVersion,
			methodHeader:    "server/discover",
			method:          "server/discover",
			params:          map[string]any{"_meta": modernRequestMeta(futureVersion)},
		})
		if response.status != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", response.status, http.StatusBadRequest)
		}
		if code := rpcErrorCode(t, response); code != mcp.CodeUnsupportedProtocolVersion {
			t.Errorf("error code = %d, want %d", code, mcp.CodeUnsupportedProtocolVersion)
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		response := postRPCRequest(t, httpTestServer, rpcRequest{
			protocolVersion: expectedProtocolVersion,
			methodHeader:    "test/unknown",
			method:          "test/unknown",
			params:          map[string]any{"_meta": modernRequestMeta(expectedProtocolVersion)},
		})
		if response.status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", response.status, http.StatusNotFound)
		}
		if code := rpcErrorCode(t, response); code != jsonrpc.CodeMethodNotFound {
			t.Errorf("error code = %d, want %d", code, jsonrpc.CodeMethodNotFound)
		}
	})
}

func TestStreamableHTTPLegacyCore(t *testing.T) {
	exposeAllTools(t)
	server := newMCPServer(testServerVersion)
	RegisterTool(server)
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	for _, protocolVersion := range []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"} {
		t.Run(protocolVersion, func(t *testing.T) {
			initialize := postRPCRequest(t, httpTestServer, rpcRequest{
				method: "initialize",
				params: map[string]any{
					"protocolVersion": protocolVersion,
					"clientInfo":      map[string]any{"name": "gitea-mcp-legacy-test", "version": "1"},
					"capabilities":    map[string]any{},
				},
			})
			if initialize.status != http.StatusOK {
				t.Fatalf("initialize status = %d, want %d; body = %s", initialize.status, http.StatusOK, initialize.body)
			}
			var initializeResult struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if err := json.Unmarshal(rpcResult(t, initialize), &initializeResult); err != nil {
				t.Fatalf("Unmarshal(initialize result) error = %v", err)
			}
			if initializeResult.ProtocolVersion != protocolVersion {
				t.Errorf("initialize protocolVersion = %q, want %q", initializeResult.ProtocolVersion, protocolVersion)
			}
			if sessionID := initialize.header.Get("Mcp-Session-Id"); sessionID != "" {
				t.Errorf("initialize Mcp-Session-Id = %q, want empty", sessionID)
			}

			list := postRPCRequest(t, httpTestServer, rpcRequest{
				protocolVersion: protocolVersion,
				method:          "tools/list",
				params:          map[string]any{},
			})
			if list.status != http.StatusOK {
				t.Fatalf("tools/list status = %d, want %d; body = %s", list.status, http.StatusOK, list.body)
			}
			var listResult struct {
				Tools []json.RawMessage `json:"tools"`
			}
			if err := json.Unmarshal(rpcResult(t, list), &listResult); err != nil {
				t.Fatalf("Unmarshal(tools/list result) error = %v", err)
			}
			if len(listResult.Tools) != registeredToolCount() {
				t.Errorf("tools/list count = %d, want %d", len(listResult.Tools), registeredToolCount())
			}
		})
	}

	// The call path does not vary by version, unlike the two requests above.
	call := postRPCRequest(t, httpTestServer, rpcRequest{
		protocolVersion: "2025-11-25",
		method:          "tools/call",
		params: map[string]any{
			"name":      "get_gitea_mcp_server_version",
			"arguments": map[string]any{},
		},
	})
	if call.status != http.StatusOK {
		t.Fatalf("tools/call status = %d, want %d; body = %s", call.status, http.StatusOK, call.body)
	}
	var callResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rpcResult(t, call), &callResult); err != nil {
		t.Fatalf("Unmarshal(tools/call result) error = %v", err)
	}
	if len(callResult.Content) != 1 || !strings.Contains(callResult.Content[0].Text, testServerVersion) {
		t.Errorf("tools/call content = %+v, want version %q", callResult.Content, testServerVersion)
	}
}

// spaceReader yields an endless run of spaces, so oversized bodies can be sent
// without allocating them.
type spaceReader struct{}

func (spaceReader) Read(p []byte) (int, error) {
	for index := range p {
		p[index] = ' '
	}
	return len(p), nil
}

func TestStreamableHTTPRequestBodyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := newMCPServer(testServerVersion)
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	for _, test := range []struct {
		name     string
		size     int64
		tooLarge bool
	}{
		{name: "above the SDK default", size: mcp.DefaultMaxRequestBodyBytes + 1},
		{name: "above our own limit", size: maxRequestBodyBytes + 1, tooLarge: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, httpTestServer.URL+"/mcp", io.LimitReader(spaceReader{}, test.size))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			request.ContentLength = test.size
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			response, err := httpTestServer.Client().Do(request)
			if err != nil {
				t.Fatalf("POST %d bytes error = %v", test.size, err)
			}
			defer response.Body.Close()
			if gotTooLarge := response.StatusCode == http.StatusRequestEntityTooLarge; gotTooLarge != test.tooLarge {
				t.Errorf("POST %d bytes status = %d, want %d = %v", test.size, response.StatusCode, http.StatusRequestEntityTooLarge, test.tooLarge)
			}
		})
	}
}

func TestStreamableHTTPOriginProtection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpTestServer := httptest.NewServer(newHTTPServer("", newMCPServer(testServerVersion)).Handler)
	defer httpTestServer.Close()

	for _, test := range []struct {
		name          string
		method        string
		origin        string
		wantForbidden bool
	}{
		{name: "native client without origin", method: http.MethodPost},
		{name: "same origin", method: http.MethodPost, origin: httpTestServer.URL},
		{name: "cross origin POST", method: http.MethodPost, origin: "https://attacker.example", wantForbidden: true},
		{name: "cross origin GET", method: http.MethodGet, origin: "https://attacker.example", wantForbidden: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(ctx, test.method, httpTestServer.URL+"/mcp", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response, err := httpTestServer.Client().Do(request)
			if err != nil {
				t.Fatalf("%s /mcp error = %v", test.method, err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if forbidden := response.StatusCode == http.StatusForbidden; forbidden != test.wantForbidden {
				t.Errorf("status = %d, want forbidden = %v", response.StatusCode, test.wantForbidden)
			}
		})
	}
}

func TestStreamableHTTPCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	started := make(chan struct{})
	handlerCanceled := make(chan struct{})
	server := newMCPServer(testServerVersion)
	server.AddTool(
		&mcp.Tool{
			Name:        "test_cancellation",
			Description: "Wait for the request context to be canceled.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			close(started)
			<-ctx.Done()
			close(handlerCanceled)
			return nil, ctx.Err()
		},
	)
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "gitea-mcp-cancellation-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpTestServer.URL + "/mcp",
		HTTPClient:           httpTestServer.Client(),
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	callCtx, cancelCall := context.WithCancel(ctx)
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: "test_cancellation"})
		callDone <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("tool handler did not start")
	}
	cancelCall()
	select {
	case <-handlerCanceled:
	case <-ctx.Done():
		t.Fatal("HTTP request cancellation did not reach the tool handler")
	}
	select {
	case err := <-callDone:
		if err == nil {
			t.Error("CallTool() error = nil after cancellation")
		}
	case <-ctx.Done():
		t.Fatal("CallTool() did not return after cancellation")
	}
}

type authorizationTransport struct {
	base  http.RoundTripper
	mu    sync.RWMutex
	value string
}

func (t *authorizationTransport) set(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.value = value
}

func (t *authorizationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context()) // Clone already copies the header
	t.mu.RLock()
	value := t.value
	t.mu.RUnlock()
	if value != "" {
		clone.Header.Set("Authorization", value)
	}
	return t.base.RoundTrip(clone)
}

func authContextValue(ctx context.Context, session *mcp.ClientSession) (string, error) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "test_auth_context"})
	if err != nil {
		return "", err
	}
	if len(result.Content) != 1 {
		return "", fmt.Errorf("content count = %d, want 1", len(result.Content))
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return "", fmt.Errorf("content type = %T, want *mcp.TextContent", result.Content[0])
	}
	return content.Text, nil
}

func TestHTTPAuthPerRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := newMCPServer(testServerVersion)
	server.AddTool(
		&mcp.Tool{
			Name:        "test_auth_context",
			Description: "Return the request-scoped authentication token.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			token, _ := ctx.Value(mcpContext.TokenContextKey).(string)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: token}},
			}, nil
		},
	)
	httpTestServer := httptest.NewServer(newHTTPServer("", server).Handler)
	defer httpTestServer.Close()

	baseTransport := httpTestServer.Client().Transport
	auth := &authorizationTransport{base: baseTransport}
	auth.set("Bearer first-token")
	baseClient := &http.Client{Transport: auth}
	client := mcp.NewClient(&mcp.Implementation{Name: "gitea-mcp-auth-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpTestServer.URL + "/mcp",
		HTTPClient:           baseClient,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	for _, test := range []struct {
		header string
		want   string
	}{
		{header: "Bearer first-token", want: "first-token"},
		{header: "token second-token", want: "second-token"},
		{header: "Basic ignored", want: ""},
	} {
		auth.set(test.header)
		token, err := authContextValue(ctx, session)
		if err != nil {
			t.Fatalf("CallTool() with %q error = %v", test.header, err)
		}
		if token != test.want {
			t.Errorf("CallTool() token = %q, want %q", token, test.want)
		}
	}

	type authenticatedSession struct {
		session *mcp.ClientSession
		want    string
	}
	concurrentSessions := make([]authenticatedSession, 0, 2)
	for index, token := range []string{"parallel-one", "parallel-two"} {
		transport := &authorizationTransport{base: baseTransport}
		transport.set("Bearer " + token)
		httpClient := &http.Client{Transport: transport}
		parallelClient := mcp.NewClient(&mcp.Implementation{
			Name:    fmt.Sprintf("gitea-mcp-auth-parallel-%d", index),
			Version: "1",
		}, nil)
		parallelSession, err := parallelClient.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             httpTestServer.URL + "/mcp",
			HTTPClient:           httpClient,
			DisableStandaloneSSE: true,
			MaxRetries:           -1,
		}, nil)
		if err != nil {
			t.Fatalf("parallel Connect() error = %v", err)
		}
		defer parallelSession.Close()
		concurrentSessions = append(concurrentSessions, authenticatedSession{session: parallelSession, want: token})
	}

	var waitGroup sync.WaitGroup
	errorsCh := make(chan error, 20)
	for _, authenticated := range concurrentSessions {
		for range 10 {
			waitGroup.Go(func() {
				got, err := authContextValue(ctx, authenticated.session)
				if err != nil {
					errorsCh <- err
					return
				}
				if got != authenticated.want {
					errorsCh <- fmt.Errorf("parallel token = %q, want %q", got, authenticated.want)
				}
			})
		}
	}
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func TestStdioCommandTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in short mode")
	}
	for name, value := range map[string]string{
		"GITEA_READONLY": "true",
		"GITEA_SCOPES":   "user",
		"GITEA_TOOLS":    "get_me",
		"MCP_MODE":       "http",
	} {
		t.Setenv(name, value)
	}
	exposeAllTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	binary := filepath.Join(t.TempDir(), "gitea-mcp")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stdio test binary: %v\n%s", err, output)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "gitea-mcp-stdio-test", Version: "1"}, nil)
	command := exec.CommandContext(ctx, binary, "--transport", "stdio")
	command.Env = stdioCommandEnvironment()
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command:           command,
		TerminateDuration: 2 * time.Second,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()
	if got := session.InitializeResult().ProtocolVersion; got != expectedProtocolVersion {
		t.Errorf("protocol version = %q, want %q", got, expectedProtocolVersion)
	}
	assertToolsOnlyCapabilities(t, session.InitializeResult().Capabilities)
	listAndCallVersion(ctx, t, session, "Gitea MCP Server version:")
}
