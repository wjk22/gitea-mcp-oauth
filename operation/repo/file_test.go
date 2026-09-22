package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	gitea_sdk "gitea.dev/sdk"
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

// T-FR-1: a multi-line UTF-8 fixture round-trips through ShapeFileContent: base64 in, identical text out.
// The FormatFileContentResult map also lacks encoding, html_url, download_url (additional check).
// The handler-level assertions (run via GetFileContentFn) are in T-FR-8.
func TestT_FR_1(t *testing.T) {
	fixture := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"
	b64 := base64.StdEncoding.EncodeToString([]byte(fixture))

	shaped, err := ShapeFileContent([]byte(b64), ShapeOptions{})
	if err != nil {
		t.Fatalf("ShapeFileContent() error = %v", err)
	}

	// Round-trip: base64 in, identical text out
	if shaped.Content != fixture {
		t.Fatalf("shaped.Content = %q, want %q", shaped.Content, fixture)
	}

	res := FormatFileContentResult("main.go", "main.go", "abc123sha", "file", int64(len(fixture)), shaped)
	jsonBytes, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var gotMap map[string]any
	if err := json.Unmarshal(jsonBytes, &gotMap); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if gotContent, ok := gotMap["content"].(string); !ok || gotContent != fixture {
		t.Errorf("gotMap['content'] = %q, want %q", gotContent, fixture)
	}
	// Additional check: FormatFileContentResult map never contains these fields
	if _, ok := gotMap["encoding"]; ok {
		t.Errorf("JSON output contains forbidden field 'encoding'")
	}
	if _, ok := gotMap["html_url"]; ok {
		t.Errorf("JSON output contains forbidden field 'html_url'")
	}
	if _, ok := gotMap["download_url"]; ok {
		t.Errorf("JSON output contains forbidden field 'download_url'")
	}
}

// T-FR-2: a fixture with a NUL byte and a fixture with invalid UTF-8 both give binary: true, no content, no error.
func TestT_FR_2(t *testing.T) {
	nulFixture := []byte("hello\x00world\nsecond line\n")
	nulB64 := base64.StdEncoding.EncodeToString(nulFixture)
	shapedNul, err := ShapeFileContent([]byte(nulB64), ShapeOptions{})
	if err != nil {
		t.Fatalf("ShapeFileContent(nul) error = %v", err)
	}
	if !shapedNul.Binary {
		t.Errorf("ShapeFileContent(nul) Binary = false, want true")
	}
	if shapedNul.Content != "" {
		t.Errorf("ShapeFileContent(nul) Content = %q, want empty", shapedNul.Content)
	}
	resNul := FormatFileContentResult("file.bin", "file.bin", "sha1", "file", int64(len(nulFixture)), shapedNul)
	if _, hasContent := resNul["content"]; hasContent {
		t.Errorf("FormatFileContentResult(nul) contains content field: %v", resNul["content"])
	}
	if resNul["binary"] != true {
		t.Errorf("FormatFileContentResult(nul) binary = %v, want true", resNul["binary"])
	}

	invFixture := []byte{0xff, 0xfe, 0xfd, 0x80}
	invB64 := base64.StdEncoding.EncodeToString(invFixture)
	shapedInv, err := ShapeFileContent([]byte(invB64), ShapeOptions{})
	if err != nil {
		t.Fatalf("ShapeFileContent(invalid UTF-8) error = %v", err)
	}
	if !shapedInv.Binary {
		t.Errorf("ShapeFileContent(invalid UTF-8) Binary = false, want true")
	}
	if shapedInv.Content != "" {
		t.Errorf("ShapeFileContent(invalid UTF-8) Content = %q, want empty", shapedInv.Content)
	}
	resInv := FormatFileContentResult("file.bin", "file.bin", "sha2", "file", int64(len(invFixture)), shapedInv)
	if _, hasContent := resInv["content"]; hasContent {
		t.Errorf("FormatFileContentResult(invalid UTF-8) contains content field: %v", resInv["content"])
	}
	if resInv["binary"] != true {
		t.Errorf("FormatFileContentResult(invalid UTF-8) binary = %v, want true", resInv["binary"])
	}
}

// T-FR-3: whole file, middle range, end_line beyond the end (clamped),
// start_line beyond the end (error names total_lines), start_line < 1,
// end_line < start_line, file with and without trailing newline, empty file.
func TestT_FR_3(t *testing.T) {
	fiveLines := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	fiveB64 := base64.StdEncoding.EncodeToString([]byte(fiveLines))

	trailingNL := "line 1\nline 2\n"
	trailingB64 := base64.StdEncoding.EncodeToString([]byte(trailingNL))

	noTrailingNL := "line 1\nline 2"
	noTrailingB64 := base64.StdEncoding.EncodeToString([]byte(noTrailingNL))

	emptyB64 := base64.StdEncoding.EncodeToString([]byte(""))

	tests := []struct {
		name           string
		inputB64       string
		opts           ShapeOptions
		wantErrSubstr  string
		wantTotalLines int
		wantStartLine  int
		wantEndLine    int
		wantContent    string
	}{
		{
			name:           "whole file",
			inputB64:       fiveB64,
			opts:           ShapeOptions{},
			wantTotalLines: 5,
			wantStartLine:  1,
			wantEndLine:    5,
			wantContent:    fiveLines,
		},
		{
			name:           "middle range",
			inputB64:       fiveB64,
			opts:           ShapeOptions{StartLine: new(2), EndLine: new(4)},
			wantTotalLines: 5,
			wantStartLine:  2,
			wantEndLine:    4,
			wantContent:    "line 2\nline 3\nline 4\n",
		},
		{
			name:           "end_line beyond the end (clamped)",
			inputB64:       fiveB64,
			opts:           ShapeOptions{StartLine: new(3), EndLine: new(10)},
			wantTotalLines: 5,
			wantStartLine:  3,
			wantEndLine:    5,
			wantContent:    "line 3\nline 4\nline 5\n",
		},
		{
			name:          "start_line beyond the end (error names total_lines)",
			inputB64:      fiveB64,
			opts:          ShapeOptions{StartLine: new(6)},
			wantErrSubstr: "total lines (5)",
		},
		{
			name:          "start_line < 1",
			inputB64:      fiveB64,
			opts:          ShapeOptions{StartLine: new(0)},
			wantErrSubstr: "start_line must be greater than or equal to 1",
		},
		{
			name:          "end_line < start_line",
			inputB64:      fiveB64,
			opts:          ShapeOptions{StartLine: new(3), EndLine: new(2)},
			wantErrSubstr: "end_line (2) cannot be less than start_line (3)",
		},
		{
			name:           "file with trailing newline",
			inputB64:       trailingB64,
			opts:           ShapeOptions{},
			wantTotalLines: 2,
			wantStartLine:  1,
			wantEndLine:    2,
			wantContent:    trailingNL,
		},
		{
			name:           "file without trailing newline",
			inputB64:       noTrailingB64,
			opts:           ShapeOptions{},
			wantTotalLines: 2,
			wantStartLine:  1,
			wantEndLine:    2,
			wantContent:    noTrailingNL,
		},
		{
			name:           "empty file",
			inputB64:       emptyB64,
			opts:           ShapeOptions{},
			wantTotalLines: 0,
			wantStartLine:  0,
			wantEndLine:    0,
			wantContent:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shaped, err := ShapeFileContent([]byte(tc.inputB64), tc.opts)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ShapeFileContent() unexpected error: %v", err)
			}
			if shaped.TotalLines != tc.wantTotalLines {
				t.Errorf("TotalLines = %d, want %d", shaped.TotalLines, tc.wantTotalLines)
			}
			if shaped.StartLine != tc.wantStartLine {
				t.Errorf("StartLine = %d, want %d", shaped.StartLine, tc.wantStartLine)
			}
			if shaped.EndLine != tc.wantEndLine {
				t.Errorf("EndLine = %d, want %d", shaped.EndLine, tc.wantEndLine)
			}
			if shaped.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", shaped.Content, tc.wantContent)
			}
		})
	}
}

// T-FR-4: a file larger than max_bytes gives truncated: true and a correct next_start_line;
// following next_start_line repeatedly and concatenating the pages reproduces the original file exactly.
// The round-trip is checked for several max_bytes values (37, 100, 150, 400).
// A single line of multi-byte characters longer than the cap is cut on a character boundary and has no next_start_line.
// Default and maximum are asserted, and max_bytes < 1 is an error.
func TestT_FR_4(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&sb, "Line %03d: abcdefghijklmnopqrstuvwxyz\n", i)
	}
	original := sb.String()
	b64 := []byte(base64.StdEncoding.EncodeToString([]byte(original)))

	// Each line is 37 bytes ("Line 001: abcdefghijklmnopqrstuvwxyz\n").
	// Paging round-trip for several max_bytes values: assembled content must equal original byte-for-byte.
	for _, maxBytes := range []int{37, 100, 150, 400} {
		var assembled strings.Builder
		start := 1
		for {
			s, err := ShapeFileContent(b64, ShapeOptions{StartLine: new(start), MaxBytes: &maxBytes})
			if err != nil {
				t.Fatalf("paging error at line %d (max_bytes=%d): %v", start, maxBytes, err)
			}
			assembled.WriteString(s.Content)
			if !s.Truncated {
				break
			}
			// NextStartLine must be set when truncated after at least one full line
			if s.NextStartLine == 0 {
				t.Fatalf("paging (max_bytes=%d): Truncated=true but NextStartLine=0 at start=%d", maxBytes, start)
			}
			start = s.NextStartLine
		}
		if assembled.String() != original {
			t.Fatalf("paging (max_bytes=%d): assembled content != original file.\nGot:\n%s\nWant:\n%s",
				maxBytes, assembled.String(), original)
		}
	}

	// Single line of multi-byte characters longer than cap
	multiByteLine := "你好世界\n" // 4 x 3 bytes + 1 byte = 13 bytes
	mbB64 := []byte(base64.StdEncoding.EncodeToString([]byte(multiByteLine)))
	shapedMB, err := ShapeFileContent(mbB64, ShapeOptions{MaxBytes: new(5)})
	if err != nil {
		t.Fatalf("multi-byte cap error: %v", err)
	}
	if !shapedMB.Truncated {
		t.Errorf("multi-byte Truncated = false, want true")
	}
	if shapedMB.NextStartLine != 0 {
		t.Errorf("multi-byte NextStartLine = %d, want 0 (omitted)", shapedMB.NextStartLine)
	}
	if shapedMB.Content != "你" {
		t.Errorf("multi-byte Content = %q, want '你'", shapedMB.Content)
	}
	if !utf8.ValidString(shapedMB.Content) {
		t.Errorf("multi-byte Content is not valid UTF-8: %q", shapedMB.Content)
	}

	// Default cap (32768) asserted
	var bigBuf strings.Builder
	for range 1000 {
		bigBuf.WriteString("abcdefghijklmnopqrstuvwxyz0123456789\n") // 37 bytes * 1000 = 37000 bytes
	}
	bigB64 := []byte(base64.StdEncoding.EncodeToString([]byte(bigBuf.String())))
	shapedDef, err := ShapeFileContent(bigB64, ShapeOptions{})
	if err != nil {
		t.Fatalf("default cap error: %v", err)
	}
	if !shapedDef.Truncated {
		t.Errorf("default cap Truncated = false, want true")
	}
	if len(shapedDef.Content) > DefaultMaxBytes {
		t.Errorf("default cap length = %d exceeds DefaultMaxBytes %d", len(shapedDef.Content), DefaultMaxBytes)
	}
	shaped32k, err := ShapeFileContent(bigB64, ShapeOptions{MaxBytes: new(32768)})
	if err != nil {
		t.Fatalf("explicit 32768 cap error: %v", err)
	}
	if shapedDef.Content != shaped32k.Content {
		t.Errorf("default cap content differs from explicit 32768 cap")
	}

	// Maximum cap (262144) asserted
	var hugeBuf strings.Builder
	for range 8000 {
		hugeBuf.WriteString("abcdefghijklmnopqrstuvwxyz0123456789\n") // 37 bytes * 8000 = 296,000 bytes
	}
	hugeB64 := []byte(base64.StdEncoding.EncodeToString([]byte(hugeBuf.String())))
	shapedMax, err := ShapeFileContent(hugeB64, ShapeOptions{MaxBytes: new(500000)})
	if err != nil {
		t.Fatalf("clamped 500000 cap error: %v", err)
	}
	if !shapedMax.Truncated {
		t.Errorf("max cap Truncated = false, want true")
	}
	if len(shapedMax.Content) > MaxMaxBytes {
		t.Errorf("max cap length = %d exceeds MaxMaxBytes %d", len(shapedMax.Content), MaxMaxBytes)
	}
	shapedExplicitMax, err := ShapeFileContent(hugeB64, ShapeOptions{MaxBytes: new(262144)})
	if err != nil {
		t.Fatalf("explicit 262144 cap error: %v", err)
	}
	if shapedMax.Content != shapedExplicitMax.Content {
		t.Errorf("clamped 500000 cap differs from explicit 262144 cap")
	}

	// max_bytes < 1 is an error
	_, err = ShapeFileContent(bigB64, ShapeOptions{MaxBytes: new(0)})
	if err == nil || !strings.Contains(err.Error(), "max_bytes must be at least 1") {
		t.Errorf("max_bytes=0 error = %v, want error with 'max_bytes must be at least 1'", err)
	}
	_, err = ShapeFileContent(bigB64, ShapeOptions{MaxBytes: new(-1)})
	if err == nil || !strings.Contains(err.Error(), "max_bytes must be at least 1") {
		t.Errorf("max_bytes=-1 error = %v, want error with 'max_bytes must be at least 1'", err)
	}
}

// T-FR-5: prefix format N<TAB>text; numbering follows the original file inside a requested range; the cap counts the prefixes.
func TestT_FR_5(t *testing.T) {
	content := "first line\nsecond line\n"
	b64 := []byte(base64.StdEncoding.EncodeToString([]byte(content)))

	// Prefix format N<TAB>text
	shaped, err := ShapeFileContent(b64, ShapeOptions{WithLines: true})
	if err != nil {
		t.Fatalf("ShapeFileContent error: %v", err)
	}
	want := "1\tfirst line\n2\tsecond line\n"
	if shaped.Content != want {
		t.Fatalf("shaped.Content = %q, want %q", shaped.Content, want)
	}

	// Numbering follows original file inside requested range
	fiveLines := "line1\nline2\nline3\nline4\nline5\n"
	fiveB64 := []byte(base64.StdEncoding.EncodeToString([]byte(fiveLines)))
	shapedRange, err := ShapeFileContent(fiveB64, ShapeOptions{StartLine: new(3), EndLine: new(4), WithLines: true})
	if err != nil {
		t.Fatalf("ShapeFileContent range error: %v", err)
	}
	wantRange := "3\tline3\n4\tline4\n"
	if shapedRange.Content != wantRange {
		t.Fatalf("shapedRange.Content = %q, want %q", shapedRange.Content, wantRange)
	}

	// The cap counts the prefixes.
	// "12345678\n" is 9 bytes; with prefix "N\t" it becomes 11 bytes per line.
	// max_bytes=20: with prefixes 1+11=11<=20 but 11+11=22>20, so only line 1 fits (truncated, next_start_line=2).
	// Without prefixes: 9+9=18<=20, so both lines fit (not truncated).
	// Together they fail if prefixes stop counting toward the cap.
	twoLines := "12345678\n12345678\n"
	twoB64 := []byte(base64.StdEncoding.EncodeToString([]byte(twoLines)))

	shapedCap, err := ShapeFileContent(twoB64, ShapeOptions{WithLines: true, MaxBytes: new(20)})
	if err != nil {
		t.Fatalf("ShapeFileContent prefix cap error: %v", err)
	}
	if !shapedCap.Truncated { // fails if prefixes do not count toward cap
		t.Errorf("prefix cap (withLines=true, max_bytes=20): Truncated = false, want true")
	}
	if shapedCap.EndLine != 1 {
		t.Errorf("prefix cap (withLines=true, max_bytes=20): EndLine = %d, want 1", shapedCap.EndLine)
	}
	if shapedCap.NextStartLine != 2 {
		t.Errorf("prefix cap (withLines=true, max_bytes=20): NextStartLine = %d, want 2", shapedCap.NextStartLine)
	}
	if shapedCap.Content != "1\t12345678\n" {
		t.Errorf("prefix cap (withLines=true, max_bytes=20): Content = %q, want %q", shapedCap.Content, "1\t12345678\n")
	}

	// Same file, same cap, without prefixes: both lines (9+9=18 bytes) fit.
	shapedNoCap, err := ShapeFileContent(twoB64, ShapeOptions{WithLines: false, MaxBytes: new(20)})
	if err != nil {
		t.Fatalf("ShapeFileContent no-prefix cap error: %v", err)
	}
	if shapedNoCap.Truncated { // fails if content without prefixes is incorrectly truncated
		t.Errorf("no-prefix cap (withLines=false, max_bytes=20): Truncated = true, want false")
	}
	if shapedNoCap.Content != twoLines {
		t.Errorf("no-prefix cap (withLines=false, max_bytes=20): Content = %q, want %q", shapedNoCap.Content, twoLines)
	}
}

// T-FR-6: get_dir_contents output is byte-identical to before (existing slim tests pass unchanged);
// a nil-content response yields metadata only; the read-only tests in operation/readonly_test.go still pass.
func TestT_FR_6(t *testing.T) {
	origHost, origToken := flag.Host, flag.Token
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	// 1. get_dir_contents output matches slimDirEntries
	dirEntries := []*gitea_sdk.ContentsResponse{
		{Name: "src", Path: "src", Type: "dir", Size: 0},
		{Name: "main.go", Path: "main.go", Type: "file", Size: 100},
	}
	dirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dirEntries)
	}))
	defer dirServer.Close()

	flag.Host = dirServer.URL
	flag.Token = "test-token"

	dirResult, err := GetDirContentFn(context.Background(), map[string]any{
		"owner": "testowner",
		"repo":  "testrepo",
		"ref":   "main",
		"path":  "src",
	})
	if err != nil {
		t.Fatalf("GetDirContentFn() error = %v", err)
	}
	dirText := dirResult.Content[0].(*mcp.TextContent).Text
	var gotDirEntries []map[string]any
	if err := json.Unmarshal([]byte(dirText), &gotDirEntries); err != nil {
		t.Fatalf("Unmarshal dir entries error: %v", err)
	}
	if len(gotDirEntries) != 2 {
		t.Fatalf("len(gotDirEntries) = %d, want 2", len(gotDirEntries))
	}
	if gotDirEntries[0]["name"] != "src" || gotDirEntries[1]["name"] != "main.go" {
		t.Errorf("unexpected dir entries: %v", gotDirEntries)
	}
	if _, hasContent := gotDirEntries[0]["content"]; hasContent {
		t.Errorf("dir entry has unexpected content field")
	}

	// 2. nil-content response yields metadata only
	nilContentFile := &gitea_sdk.ContentsResponse{
		Name:    "submodule",
		Path:    "submodule",
		SHA:     "submodulesha",
		Type:    "submodule",
		Size:    0,
		Content: nil,
	}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(nilContentFile)
	}))
	defer fileServer.Close()

	flag.Host = fileServer.URL

	fileResult, err := GetFileContentFn(context.Background(), map[string]any{
		"owner": "testowner",
		"repo":  "testrepo",
		"ref":   "main",
		"path":  "submodule",
	})
	if err != nil {
		t.Fatalf("GetFileContentFn() nil-content error = %v", err)
	}
	fileText := fileResult.Content[0].(*mcp.TextContent).Text
	var gotMeta map[string]any
	if err := json.Unmarshal([]byte(fileText), &gotMeta); err != nil {
		t.Fatalf("Unmarshal nil-content response error: %v", err)
	}
	if gotMeta["name"] != "submodule" {
		t.Errorf("gotMeta['name'] = %v, want 'submodule'", gotMeta["name"])
	}
	if gotMeta["sha"] != "submodulesha" {
		t.Errorf("gotMeta['sha'] = %v, want 'submodulesha'", gotMeta["sha"])
	}
	if gotMeta["type"] != "submodule" {
		t.Errorf("gotMeta['type'] = %v, want 'submodule'", gotMeta["type"])
	}
	if _, hasContent := gotMeta["content"]; hasContent {
		t.Errorf("nil-content response contains unexpected 'content' field: %v", gotMeta["content"])
	}
	if _, hasBinary := gotMeta["binary"]; hasBinary {
		t.Errorf("nil-content response contains unexpected 'binary' field: %v", gotMeta["binary"])
	}
	if _, hasEncoding := gotMeta["encoding"]; hasEncoding {
		t.Errorf("nil-content response contains unexpected 'encoding' field: %v", gotMeta["encoding"])
	}
}

// T-FR-7: the tool schema differs from before only by the three new optional parameters, which are not in required.
func TestT_FR_7(t *testing.T) {
	schema, ok := GetFileContentTool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("GetFileContentTool.InputSchema is not map[string]any, got %T", GetFileContentTool.InputSchema)
	}

	requiredRaw, _ := schema["required"].([]string)
	wantRequired := []string{"owner", "repo", "ref", "path"}
	if !slices.Equal(requiredRaw, wantRequired) {
		t.Errorf("required = %v, want %v", requiredRaw, wantRequired)
	}

	if slices.Contains(requiredRaw, "start_line") {
		t.Errorf("start_line must not be in required")
	}
	if slices.Contains(requiredRaw, "end_line") {
		t.Errorf("end_line must not be in required")
	}
	if slices.Contains(requiredRaw, "max_bytes") {
		t.Errorf("max_bytes must not be in required")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not map[string]any, got %T", schema["properties"])
	}

	expectedProps := []string{"owner", "repo", "ref", "path", "withLines", "start_line", "end_line", "max_bytes"}
	if len(props) != len(expectedProps) {
		t.Errorf("properties count = %d, want %d (%v)", len(props), len(expectedProps), expectedProps)
	}

	for _, p := range expectedProps {
		if _, exists := props[p]; !exists {
			t.Errorf("property %q is missing from GetFileContentTool schema", p)
		}
	}

	for _, numParam := range []string{"start_line", "end_line", "max_bytes"} {
		prop, ok := props[numParam].(map[string]any)
		if !ok {
			t.Errorf("property %q schema is not map[string]any", numParam)
			continue
		}
		if prop["type"] != "number" {
			t.Errorf("property %q type = %v, want number", numParam, prop["type"])
		}
		desc, _ := prop["description"].(string)
		if desc == "" {
			t.Errorf("property %q description must not be empty", numParam)
		}
	}
}

// T-FR-8 (FR-7, handler level): call GetFileContentFn against an httptest Gitea stub.
// Asserts float64 arguments, null arguments, forbidden fields in handler output, and rejection of bad types.
func TestT_FR_8(t *testing.T) {
	origHost, origToken := flag.Host, flag.Token
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	// Five-line fixture served by the stub as base64-encoded content.
	const fixture = "line1\nline2\nline3\nline4\nline5\n"
	b64Content := base64.StdEncoding.EncodeToString([]byte(fixture))
	enc := "base64"
	fileResp := map[string]any{
		"name":     "test.txt",
		"path":     "test.txt",
		"sha":      "deadbeef",
		"type":     "file",
		"size":     int64(len(fixture)),
		"encoding": enc,
		"content":  b64Content,
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fileResp)
	}))
	defer stub.Close()
	flag.Host = stub.URL
	flag.Token = "test-token"

	baseArgs := map[string]any{
		"owner": "testowner",
		"repo":  "testrepo",
		"ref":   "main",
		"path":  "test.txt",
	}

	// Case 1: start_line=float64(2), end_line=float64(3), withLines=true.
	// Expect JSON with start_line=2, end_line=3, total_lines, content "2\tline2\n3\tline3\n",
	// and no encoding, html_url, download_url fields.
	args1 := map[string]any{
		"owner":      "testowner",
		"repo":       "testrepo",
		"ref":        "main",
		"path":       "test.txt",
		"start_line": float64(2),
		"end_line":   float64(3),
		"withLines":  true,
	}
	res1, err := GetFileContentFn(context.Background(), args1)
	if err != nil {
		t.Fatalf("Case 1: GetFileContentFn() error = %v", err)
	}
	text1 := res1.Content[0].(*mcp.TextContent).Text
	var got1 map[string]any
	if err := json.Unmarshal([]byte(text1), &got1); err != nil {
		t.Fatalf("Case 1: Unmarshal error: %v (raw: %s)", err, text1)
	}
	if got1["start_line"] != float64(2) {
		t.Errorf("Case 1: start_line = %v, want 2", got1["start_line"])
	}
	if got1["end_line"] != float64(3) {
		t.Errorf("Case 1: end_line = %v, want 3", got1["end_line"])
	}
	if got1["total_lines"] == nil {
		t.Errorf("Case 1: total_lines must be present")
	}
	wantContent1 := "2\tline2\n3\tline3\n"
	if got1["content"] != wantContent1 { // fails if float64 args are not accepted or withLines wiring is broken
		t.Errorf("Case 1: content = %q, want %q", got1["content"], wantContent1)
	}
	if _, ok := got1["encoding"]; ok { // fails if FR-1 forbidden field is present
		t.Errorf("Case 1: output contains forbidden field 'encoding'")
	}
	if _, ok := got1["html_url"]; ok {
		t.Errorf("Case 1: output contains forbidden field 'html_url'")
	}
	if _, ok := got1["download_url"]; ok {
		t.Errorf("Case 1: output contains forbidden field 'download_url'")
	}

	// Case 2: start_line=nil, end_line=nil, max_bytes=nil → same result as omitting those keys entirely.
	args2nil := map[string]any{
		"owner":      baseArgs["owner"],
		"repo":       baseArgs["repo"],
		"ref":        baseArgs["ref"],
		"path":       baseArgs["path"],
		"start_line": nil,
		"end_line":   nil,
		"max_bytes":  nil,
	}
	resNil, err := GetFileContentFn(context.Background(), args2nil)
	if err != nil {
		t.Fatalf("Case 2 (nil args): GetFileContentFn() error = %v", err)
	}
	resNoArgs, err := GetFileContentFn(context.Background(), baseArgs)
	if err != nil {
		t.Fatalf("Case 2 (no args): GetFileContentFn() error = %v", err)
	}
	textNil := resNil.Content[0].(*mcp.TextContent).Text
	textNoArgs := resNoArgs.Content[0].(*mcp.TextContent).Text
	if textNil != textNoArgs { // fails if explicit null is not treated as absent
		t.Errorf("Case 2: null args output differs from absent args.\nNull: %s\nAbsent: %s", textNil, textNoArgs)
	}

	// Case 3: every rejected argument must be reported as a tool error that names the
	// parameter and says why, and must never come back as a shaped file response.
	errCases := []struct {
		name string
		key  string
		val  any
		want string // required substring beyond the parameter name
	}{
		{"fractional start_line", "start_line", 1.5, "whole number"},
		{"fractional end_line", "end_line", 2.5, "whole number"},
		{"fractional max_bytes", "max_bytes", 100.5, "whole number"},
		{"NaN start_line", "start_line", math.NaN(), "whole number"},
		{"infinite end_line", "end_line", math.Inf(1), "whole number"},
		{"huge start_line", "start_line", 1e30, "out of range"},
		{"max_bytes at 2^63", "max_bytes", float64(1 << 63), "out of range"}, // FR-7a edge
		{"boolean end_line", "end_line", true, "whole number"},
		{"non-numeric start_line", "start_line", "abc", "whole number"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{
				"owner": "testowner",
				"repo":  "testrepo",
				"ref":   "main",
				"path":  "test.txt",
				tc.key:  tc.val,
			}
			res, err := GetFileContentFn(context.Background(), args)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if !res.IsError { // fails if the value is silently accepted
				t.Fatalf("IsError = false, want true (result %q)", res.Content[0].(*mcp.TextContent).Text)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.key) { // fails if the error does not name the parameter
				t.Errorf("error %q does not name %s", text, tc.key)
			}
			if !strings.Contains(text, tc.want) { // fails if the wrong rejection path was taken
				t.Errorf("error %q does not contain %q", text, tc.want)
			}
			if strings.Contains(text, `"content"`) { // fails if a shaped file response came back instead
				t.Errorf("error result %q contains a shaped file response", text)
			}
		})
	}

	// Case 3b: max_bytes as the numeric string "100" → accepted, byte-identical to max_bytes=100.
	args3b := map[string]any{
		"owner":     "testowner",
		"repo":      "testrepo",
		"ref":       "main",
		"path":      "test.txt",
		"max_bytes": "100",
	}
	res3b, err := GetFileContentFn(context.Background(), args3b)
	if err != nil {
		t.Fatalf("Case 3b: GetFileContentFn() error = %v", err)
	}
	if res3b.IsError { // fails if a numeric string is rejected
		t.Fatalf("Case 3b: IsError = true (result %q)", res3b.Content[0].(*mcp.TextContent).Text)
	}
	args3bNum := map[string]any{
		"owner":     "testowner",
		"repo":      "testrepo",
		"ref":       "main",
		"path":      "test.txt",
		"max_bytes": float64(100),
	}
	res3bNum, err := GetFileContentFn(context.Background(), args3bNum)
	if err != nil {
		t.Fatalf("Case 3b: GetFileContentFn() error = %v", err)
	}
	text3b := res3b.Content[0].(*mcp.TextContent).Text
	text3bNum := res3bNum.Content[0].(*mcp.TextContent).Text
	if text3b != text3bNum { // fails if the string and float forms are not parsed identically
		t.Errorf("Case 3b: string max_bytes output differs from float.\nString: %s\nFloat: %s", text3b, text3bNum)
	}
}

// T-FR-9: numeric argument bounds follow the platform int (FR-7b). Each row states its
// expectation for 32-bit (ok32) and 64-bit (ok64); want is the accepted value.
func TestT_FR_9(t *testing.T) {
	const key = "start_line"
	is32 := strconv.IntSize == 32
	tests := []struct {
		name         string
		val          any
		ok32, ok64   bool
		want32, want int64
	}{
		{"string 2^31-1", "2147483647", true, true, 2147483647, 2147483647},
		{"string 2^31", "2147483648", false, true, 0, 2147483648},
		{"string 2^32+1", "4294967297", false, true, 0, 4294967297},
		{"string MaxInt64", "9223372036854775807", false, true, 0, math.MaxInt64},
		{"string 2^63", "9223372036854775808", false, false, 0, 0},
		{"string below MinInt64", "-9223372036854775809", false, false, 0, 0},
		{"float MaxInt", float64(math.MaxInt), is32, false, math.MaxInt32, 0},
		{"float MinInt", float64(math.MinInt), true, true, math.MinInt32, math.MinInt64},
		{"float 2^31-1", float64(2147483647), true, true, 2147483647, 2147483647},
		{"float 2^31", float64(2147483648), false, true, 0, 2147483648},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, want := tt.ok64, tt.want
			if is32 {
				ok, want = tt.ok32, tt.want32
			}
			got, err := getOptionalFileArg(map[string]any{key: tt.val}, key)
			if !ok {
				if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "out of range") {
					t.Fatalf("IntSize %d: want out of range error naming %q, got value %v, err %v", strconv.IntSize, key, got, err)
				}
				return
			}
			if err != nil || got == nil || int64(*got) != want {
				t.Fatalf("IntSize %d: want %d, got %v, err %v", strconv.IntSize, want, got, err)
			}
		})
	}
}

// T-DR-1, T-DR-2: get_dir_contents requests exactly one contents endpoint per call: the root
// endpoint for omitted, "", "/" and "." paths, the nested endpoint otherwise.
func TestT_DR_1_2(t *testing.T) {
	origHost, origToken := flag.Host, flag.Token
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	const base = "/api/v1/repos/testowner/testrepo/contents/"
	tests := []struct {
		name     string
		path     any // nil means the key is omitted
		wantPath string
	}{
		{"omitted", nil, base},
		{"empty", "", base},
		{"slash", "/", base},
		{"dot", ".", base},
		{"nested", "docs/oauth", base + "docs/oauth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPaths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPaths = append(gotPaths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]*gitea_sdk.ContentsResponse{
					{Name: "main.go", Path: "main.go", Type: "file", Size: 100, SHA: "abc"},
				})
			}))
			defer server.Close()
			flag.Host, flag.Token = server.URL, "test-token"

			args := map[string]any{"owner": "testowner", "repo": "testrepo", "ref": "main"}
			if tt.path != nil {
				args["path"] = tt.path
			}
			result, err := GetDirContentFn(context.Background(), args)
			if err != nil {
				t.Fatalf("GetDirContentFn() error = %v", err)
			}
			if result.IsError {
				t.Fatalf("GetDirContentFn() IsError, text = %v", result.Content[0].(*mcp.TextContent).Text)
			}
			if len(gotPaths) != 1 || gotPaths[0] != tt.wantPath { // fails if the root is requested differently or the nested path changes
				t.Errorf("request paths = %v, want exactly [%q]", gotPaths, tt.wantPath)
			}
			var entries []map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &entries); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			want := map[string]any{"name": "main.go", "path": "main.go", "type": "file", "size": float64(100)}
			if len(entries) != 1 || !maps.Equal(entries[0], want) { // fails if the root listing is not slim
				t.Errorf("entries = %v, want [%v]", entries, want)
			}
		})
	}
}

// T-DR-3: only "path" stops being required on get_dir_contents; get_file_contents still requires it.
func TestT_DR_3(t *testing.T) {
	schema, ok := GetDirContentTool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema is %T, want map[string]any", GetDirContentTool.InputSchema)
	}
	required, _ := schema["required"].([]string)
	if want := []string{"owner", "repo", "ref"}; !slices.Equal(required, want) { // fails if path is required again or another param changes
		t.Errorf("get_dir_contents required = %v, want %v", required, want)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["path"]; !ok {
		t.Errorf("get_dir_contents schema lost the path property")
	}

	result, err := GetFileContentFn(context.Background(), map[string]any{"owner": "o", "repo": "r", "ref": "main", "path": ""})
	if err != nil {
		t.Fatalf("GetFileContentFn() error = %v", err)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !result.IsError || text != "path is required" { // fails if get_file_contents accepts an empty path
		t.Errorf("get_file_contents empty path: IsError = %v, text = %q, want error %q", result.IsError, text, "path is required")
	}
}
