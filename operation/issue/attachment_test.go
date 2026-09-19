package issue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAttachmentFilename(t *testing.T) {
	tests := []struct {
		name string
		file string
		uuid string
		want string
	}{
		{"uses attachment name", "screenshot.png", "abc", "screenshot.png"},
		{"falls back for traversal", "../etc/passwd", "abc", "abc.bin"},
		{"falls back for empty name", "", "abc", "abc.bin"},
		{"uses generic fallback", "", "", "attachment.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := attachmentFilename(tt.file, tt.uuid); got != tt.want {
				t.Fatalf("attachmentFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferAttachmentMimeType(t *testing.T) {
	if got := inferAttachmentMimeType("shot.png"); got != "image/png" {
		t.Fatalf("inferAttachmentMimeType() = %q, want image/png", got)
	}
	if got := inferAttachmentMimeType("archive.unknownext"); got != "application/octet-stream" {
		t.Fatalf("inferAttachmentMimeType() = %q, want application/octet-stream", got)
	}
}

func TestDefaultAttachmentPath(t *testing.T) {
	got := defaultAttachmentPath("octo", "demo", "shot.png", "uuid-1")
	want := filepath.Join(".gitea-mcp", "attachments", "octo", "demo", "shot-uuid-1.png")
	if !strings.HasSuffix(got, want) {
		t.Fatalf("defaultAttachmentPath() = %q, want suffix %q", got, want)
	}
}

func TestAttachmentReadListIssueAttachments(t *testing.T) {
	const owner = "octo"
	const repo = "demo"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues/42/assets", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":1,"name":"shot.png","uuid":"uuid-1","size":10,"download_count":2}]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	res, err := attachmentReadFn(context.Background(), map[string]any{
		"method": "list", "owner": owner, "repo": repo, "issue_number": float64(42),
	})
	if err != nil {
		t.Fatalf("attachmentReadFn() error = %v", err)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `"mime_type":"image/png"`) || !strings.Contains(body, `"uuid":"uuid-1"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDownloadAttachmentRejectsRedirectLoopAtHopLimit(t *testing.T) {
	var serverURL string
	redirects := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attachments/uuid-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		redirects++
		http.Redirect(w, r, serverURL+"/attachments/uuid-1", http.StatusFound)
	}))
	defer server.Close()
	serverURL = server.URL

	origHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = origHost }()

	_, _, _, err := gitea.DownloadAttachment(context.Background(), "/attachments/uuid-1", "*/*")
	if err == nil {
		t.Fatal("expected redirect limit error")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("unexpected error: %v", err)
	}
	if redirects != 10 {
		t.Fatalf("redirects = %d, want 10", redirects)
	}
}

func TestAttachmentReadDownloadSavesLargeAttachmentToDefaultFile(t *testing.T) {
	const owner = "octo"
	const repo = "demo"
	const uuid = "uuid-1"
	const name = "large.bin"
	payload := strings.Repeat("a", 32)

	home := t.TempDir()
	t.Setenv("HOME", home)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues/42/assets/1", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": name, "uuid": uuid, "size": len(payload)})
		case "/attachments/" + uuid:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(payload))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion, origInline := flag.Host, flag.Token, flag.Version, flag.MaxInlineAttachmentBytes
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	flag.MaxInlineAttachmentBytes = 8
	defer func() {
		flag.Host, flag.Token, flag.Version, flag.MaxInlineAttachmentBytes = origHost, origToken, origVersion, origInline
	}()

	res, err := attachmentReadFn(context.Background(), map[string]any{
		"method": "download", "owner": owner, "repo": repo, "issue_number": float64(42), "attachment_id": float64(1),
	})
	if err != nil {
		t.Fatalf("attachmentReadFn() error = %v", err)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	wantPath := filepath.Join(home, ".gitea-mcp", "attachments", owner, repo, "large-uuid-1.bin")
	if !strings.Contains(body, wantPath) {
		t.Fatalf("result missing path %q: %s", wantPath, body)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", wantPath, err)
	}
	if string(data) != payload {
		t.Fatalf("saved payload mismatch")
	}
	if !strings.Contains(body, `"bytes":32`) {
		t.Fatalf("result missing bytes: %s", body)
	}
}

func TestAttachmentReadDownloadReturnsRawImageContent(t *testing.T) {
	const uuid = "uuid-1"
	payload := []byte{0, 1, 2, 250}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attachments/"+uuid {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	originalHost := flag.Host
	originalLimit := flag.MaxInlineAttachmentBytes
	flag.Host = server.URL
	flag.MaxInlineAttachmentBytes = len(payload)
	defer func() {
		flag.Host = originalHost
		flag.MaxInlineAttachmentBytes = originalLimit
	}()

	result, err := attachmentReadFn(context.Background(), map[string]any{
		"method":          "download",
		"owner":           "octo",
		"repo":            "demo",
		"attachment_uuid": uuid,
	})
	if err != nil {
		t.Fatalf("attachmentReadFn() error = %v", err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content count = %d, want 2", len(result.Content))
	}
	if _, ok := result.Content[0].(*mcp.TextContent); !ok {
		t.Fatalf("first content type = %T, want *mcp.TextContent", result.Content[0])
	}
	image, ok := result.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("second content type = %T, want *mcp.ImageContent", result.Content[1])
	}
	if image.MIMEType != "image/png" {
		t.Errorf("image MIME type = %q, want image/png", image.MIMEType)
	}
	if !bytes.Equal(image.Data, payload) {
		t.Errorf("image data = %v, want raw payload %v", image.Data, payload)
	}
	wire, err := json.Marshal(image)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	wantBase64 := base64.StdEncoding.EncodeToString(payload)
	if !strings.Contains(string(wire), `"data":"`+wantBase64+`"`) {
		t.Errorf("wire image = %s, want base64 data %q", wire, wantBase64)
	}
}
