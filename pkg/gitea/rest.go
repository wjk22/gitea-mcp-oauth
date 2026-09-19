package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

const (
	httpClientTimeout  = 60 * time.Second
	errBodySnippetSize = 8192
)

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
}

type AttachmentResponse struct {
	Body        io.ReadCloser
	ContentType string
	StatusCode  int
}

func tokenFromContext(ctx context.Context) string {
	if ctx != nil {
		if token, ok := ctx.Value(mcpContext.TokenContextKey).(string); ok && token != "" {
			return token
		}
	}
	return flag.Token
}

var (
	restClientOnce sync.Once
	restClient     *http.Client
)

func restHTTPClient() *http.Client {
	restClientOnce.Do(func() {
		restClient = &http.Client{
			Transport:     sharedTransport(),
			Timeout:       httpClientTimeout,
			CheckRedirect: checkRedirect,
		}
	})
	return restClient
}

func buildAPIURL(path string, query url.Values) (string, error) {
	host := strings.TrimRight(flag.Host, "/")
	if host == "" {
		return "", errors.New("gitea host is empty")
	}
	p := strings.TrimLeft(path, "/")
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/%s", host, p))
	if err != nil {
		return "", err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func buildAttachmentURL(attachmentPath string) (string, error) {
	host := strings.TrimRight(flag.Host, "/")
	if host == "" {
		return "", errors.New("gitea host is empty")
	}
	baseURL, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	if attachmentPath == "" {
		return "", errors.New("attachment path is empty")
	}
	if strings.Contains(attachmentPath, "://") {
		return "", errors.New("attachment path must not be a URL")
	}
	if !strings.HasPrefix(attachmentPath, "/attachments/") {
		return "", errors.New("attachment path must start with /attachments/")
	}
	cleanPath := path.Clean(attachmentPath)
	if !strings.HasPrefix(cleanPath, "/attachments/") {
		return "", errors.New("attachment path must stay within /attachments/")
	}
	if cleanPath == "/attachments" || cleanPath == "/attachments/" {
		return "", errors.New("attachment uuid is required")
	}
	joinedPath := strings.TrimRight(baseURL.Path, "/") + cleanPath
	if joinedPath == "" {
		joinedPath = cleanPath
	}
	baseURL.Path = joinedPath
	baseURL.RawPath = joinedPath
	return baseURL.String(), nil
}

// DoJSON performs an API request and decodes a JSON response into respOut (if non-nil).
// It returns the HTTP status code.
func DoJSON(ctx context.Context, method, path string, query url.Values, body, respOut any) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	u, err := buildAPIURL(path, query)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	token := tokenFromContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if respOut != nil {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := restHTTPClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodySnippet, err := io.ReadAll(io.LimitReader(resp.Body, errBodySnippetSize+1))
		if err != nil {
			return resp.StatusCode, fmt.Errorf("read response: %w", err)
		}
		if len(bodySnippet) > errBodySnippetSize {
			bodySnippet = bodySnippet[:errBodySnippetSize]
		}
		return resp.StatusCode, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(bodySnippet))}
	}

	if respOut == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}

	if err := json.NewDecoder(resp.Body).Decode(respOut); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}

	return resp.StatusCode, nil
}

func attachmentHTTPClient(origin *url.URL) *http.Client {
	return &http.Client{
		Transport: sharedTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := checkRedirect(req, via); err != nil {
				return err
			}
			if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
				return errors.New("attachment redirect changed origin")
			}
			return nil
		},
	}
}

func OpenAttachment(ctx context.Context, attachmentPath, accept string) (*AttachmentResponse, error) {
	u, err := buildAttachmentURL(attachmentPath)
	if err != nil {
		return nil, err
	}
	origin, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	token := tokenFromContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := attachmentHTTPClient(origin).Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		bodySnippet, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodySnippetSize+1))
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}
		if len(bodySnippet) > errBodySnippetSize {
			bodySnippet = bodySnippet[:errBodySnippetSize]
		}
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(bodySnippet))}
	}
	return &AttachmentResponse{Body: resp.Body, ContentType: resp.Header.Get("Content-Type"), StatusCode: resp.StatusCode}, nil
}

// DownloadAttachment downloads attachment bytes from a same-host attachment path.
func DownloadAttachment(ctx context.Context, attachmentPath, accept string) ([]byte, string, int, error) {
	resp, err := OpenAttachment(ctx, attachmentPath, accept)
	if err != nil {
		if httpErr, ok := errors.AsType[*HTTPError](err); ok {
			return nil, "", httpErr.StatusCode, err
		}
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return respBytes, resp.ContentType, resp.StatusCode, nil
}

func WriteAttachment(body io.Reader, outputPath string) (int64, error) {
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return 0, err
	}

	written, err := io.Copy(file, body)
	if err != nil {
		return written, err
	}
	return written, file.Close()
}

// DoBytes performs an API request and returns the raw response bytes.
func DoBytes(ctx context.Context, method, path string, query url.Values, body any, accept string) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	u, err := buildAPIURL(path, query)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	token := tokenFromContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := restHTTPClient().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodySnippet := respBytes
		if len(bodySnippet) > errBodySnippetSize {
			bodySnippet = bodySnippet[:errBodySnippetSize]
		}
		return nil, resp.StatusCode, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(bodySnippet))}
	}
	return respBytes, resp.StatusCode, nil
}
