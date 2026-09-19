package to

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTextResult(t *testing.T) {
	result, err := TextResult(map[string]any{"name": "gitea"})
	if err != nil {
		t.Fatalf("TextResult() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}
	if content.Text != `{"name":"gitea"}` {
		t.Errorf("Text = %q, want JSON object", content.Text)
	}
}

func TestErrorResult(t *testing.T) {
	want := errors.New("failed")
	result, err := ErrorResult(want)
	if err != nil {
		t.Fatalf("ErrorResult() error = %v", err)
	}
	if !result.IsError {
		t.Error("IsError = false, want true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
	if content, ok := result.Content[0].(*mcp.TextContent); !ok || content.Text != want.Error() {
		t.Errorf("Content[0] = %#v, want text %q", result.Content[0], want)
	}
}
