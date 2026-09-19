package gitea

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestBuildAttachmentURLRejectsExternalURL(t *testing.T) {
	origHost := flag.Host
	flag.Host = "https://example.com"
	defer func() { flag.Host = origHost }()

	if _, err := buildAttachmentURL("https://evil.example.com/attachments/1"); err == nil {
		t.Fatal("expected error for full URL attachment path")
	}
}

func TestBuildAttachmentURLPreservesHostPathPrefix(t *testing.T) {
	origHost := flag.Host
	flag.Host = "https://example.com/gitea"
	defer func() { flag.Host = origHost }()

	got, err := buildAttachmentURL("/attachments/uuid-1")
	if err != nil {
		t.Fatalf("buildAttachmentURL() error = %v", err)
	}
	if got != "https://example.com/gitea/attachments/uuid-1" {
		t.Fatalf("buildAttachmentURL() = %q", got)
	}
}

func TestDownloadAttachmentUsesTokenAndReturnsContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attachments/uuid-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "*/*" {
			t.Fatalf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()

	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, "secret-token"
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	raw, contentType, status, err := DownloadAttachment(context.Background(), "/attachments/uuid-1", "*/*")
	if err != nil {
		t.Fatalf("DownloadAttachment() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("DownloadAttachment() status = %d, want %d", status, http.StatusOK)
	}
	if contentType != "image/png" {
		t.Fatalf("contentType = %q", contentType)
	}
	if string(raw) != "png-bytes" {
		t.Fatalf("body = %q", string(raw))
	}
}

func TestDownloadAttachmentRejectsCrossOriginRedirect(t *testing.T) {
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("cross-origin redirect should not be followed: %s", r.URL.String())
	}))
	defer redirected.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL+"/attachments/uuid-1", http.StatusFound)
	}))
	defer server.Close()

	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, "secret-token"
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	_, _, _, err := DownloadAttachment(context.Background(), "/attachments/uuid-1", "*/*")
	if err == nil {
		t.Fatal("expected error for cross-origin redirect")
	}
	want := "do request: Get \"" + redirected.URL + "/attachments/uuid-1\": attachment redirect changed origin"
	if err.Error() != want {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadAttachmentRejectsSameOriginRedirectLoopAtHopLimit(t *testing.T) {
	var serverURL string
	redirects := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects++
		http.Redirect(w, r, serverURL+"/attachments/uuid-1", http.StatusFound)
	}))
	defer server.Close()
	serverURL = server.URL

	origHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = origHost }()

	_, _, _, err := DownloadAttachment(context.Background(), "/attachments/uuid-1", "*/*")
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

func TestWriteAttachmentStreamsBodyToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attachment.bin")
	written, err := WriteAttachment(strings.NewReader(strings.Repeat("z", 64)), path)
	if err != nil {
		t.Fatalf("WriteAttachment() error = %v", err)
	}
	if written != 64 {
		t.Fatalf("written = %d, want 64", written)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != strings.Repeat("z", 64) {
		t.Fatalf("body mismatch")
	}
}

func TestWriteAttachmentCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attachment.bin")

	if _, err := WriteAttachment(strings.NewReader("secret"), path); err != nil {
		t.Fatalf("WriteAttachment() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm() & 0o077; got != 0 {
		t.Fatalf("permissions = %03o, want no group/other bits", info.Mode().Perm())
	}
}

func TestWriteAttachmentTightensExistingFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attachment.bin")
	if err := os.WriteFile(path, []byte("public"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := WriteAttachment(strings.NewReader("secret"), path); err != nil {
		t.Fatalf("WriteAttachment() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %03o, want 600", got)
	}
}

func TestDownloadAttachmentErrorsOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer server.Close()

	origHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = origHost }()

	_, _, status, err := DownloadAttachment(context.Background(), "/attachments/uuid-1", "*/*")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
	if _, ok := errors.AsType[*HTTPError](err); !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
}

func TestAttachmentHTTPClientHasNoTotalTimeout(t *testing.T) {
	origin := &url.URL{Scheme: "https", Host: "example.com"}

	client := attachmentHTTPClient(origin)

	if client.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0", client.Timeout)
	}
}
