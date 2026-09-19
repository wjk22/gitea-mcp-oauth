package repo

import (
	"testing"

	gitea_sdk "gitea.dev/sdk"
)

func TestSlimTag(t *testing.T) {
	tag := &gitea_sdk.Tag{
		Name:    "v1.0.0",
		Message: "Release v1.0.0",
		Commit:  &gitea_sdk.CommitMeta{SHA: "abc123"},
	}

	m := slimTag(tag)
	if m["name"] != "v1.0.0" {
		t.Errorf("expected name v1.0.0, got %v", m["name"])
	}
	if m["message"] != "Release v1.0.0" {
		t.Errorf("expected message, got %v", m["message"])
	}

	// List variant omits message
	list := slimTags([]*gitea_sdk.Tag{tag})
	if _, ok := list[0]["message"]; ok {
		t.Error("Tags list should omit message")
	}
	if list[0]["name"] != "v1.0.0" {
		t.Errorf("expected name in list, got %v", list[0]["name"])
	}
}

func TestSlimRelease(t *testing.T) {
	r := &gitea_sdk.Release{
		ID:        1,
		TagName:   "v1.0.0",
		Title:     "First Release",
		Note:      "Release notes",
		IsDraft:   false,
		Publisher: &gitea_sdk.User{UserName: "alice"},
	}

	m := slimRelease(r)
	if m["tag_name"] != "v1.0.0" {
		t.Errorf("expected tag_name v1.0.0, got %v", m["tag_name"])
	}
	if m["body"] != "Release notes" {
		t.Errorf("expected body from Note field, got %v", m["body"])
	}
	if m["author"] != "alice" {
		t.Errorf("expected author alice, got %v", m["author"])
	}
}

func TestSlimContents(t *testing.T) {
	content := "package main"
	encoding := "base64"
	htmlURL := "https://gitea.com/org/repo/src/branch/main/main.go"
	c := &gitea_sdk.ContentsResponse{
		Name:     "main.go",
		Path:     "main.go",
		SHA:      "abc123",
		Type:     "file",
		Size:     12,
		Content:  &content,
		Encoding: &encoding,
		HTMLURL:  &htmlURL,
	}

	m := slimContents(c)
	if m["name"] != "main.go" {
		t.Errorf("expected name main.go, got %v", m["name"])
	}
	if m["content"] != "package main" {
		t.Errorf("expected content, got %v", m["content"])
	}
}

func TestSlimDirEntries(t *testing.T) {
	entries := []*gitea_sdk.ContentsResponse{
		{Name: "src", Path: "src", Type: "dir", Size: 0},
		{Name: "main.go", Path: "main.go", Type: "file", Size: 100},
	}

	result := slimDirEntries(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result[0]["name"] != "src" {
		t.Errorf("expected first entry name src, got %v", result[0]["name"])
	}
	// Dir entries should not have content
	if _, ok := result[0]["content"]; ok {
		t.Error("dir entries should not have content field")
	}
}

func TestSlimTags_Nil(t *testing.T) {
	if r := slimTags(nil); len(r) != 0 {
		t.Errorf("expected empty slice, got %v", r)
	}
}

func TestSlimReleases_Nil(t *testing.T) {
	if r := slimReleases(nil); len(r) != 0 {
		t.Errorf("expected empty slice, got %v", r)
	}
}
