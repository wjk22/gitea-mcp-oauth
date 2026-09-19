package repo

import (
	"slices"
	"testing"

	gitea_sdk "gitea.dev/sdk"
)

func TestSlimTree(t *testing.T) {
	tree := &gitea_sdk.GitTreeResponse{
		SHA:        "abc123",
		TotalCount: 2,
		Truncated:  false,
		Entries: []gitea_sdk.GitEntry{
			{Path: "src", Mode: "040000", Type: "tree", Size: 0, SHA: "def456"},
			{Path: "main.go", Mode: "100644", Type: "blob", Size: 42, SHA: "789abc"},
		},
	}

	m := slimTree(tree)
	if m["sha"] != "abc123" {
		t.Errorf("expected sha abc123, got %v", m["sha"])
	}
	if m["total_count"] != 2 {
		t.Errorf("expected total_count 2, got %v", m["total_count"])
	}
	entries := m["tree"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0]["path"] != "src" {
		t.Errorf("expected first entry path src, got %v", entries[0]["path"])
	}
	if entries[1]["type"] != "blob" {
		t.Errorf("expected second entry type blob, got %v", entries[1]["type"])
	}
}

func TestSlimTreeNil(t *testing.T) {
	if m := slimTree(nil); m != nil {
		t.Errorf("expected nil, got %v", m)
	}
}

func TestGetRepoTreeToolRequired(t *testing.T) {
	inputSchema := GetRepoTreeTool.InputSchema.(map[string]any)
	required, _ := inputSchema["required"].([]string)
	for _, field := range []string{"owner", "repo", "tree_sha"} {
		if !slices.Contains(required, field) {
			t.Errorf("expected %q to be required", field)
		}
	}
}
