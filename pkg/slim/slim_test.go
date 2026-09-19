package slim

import (
	"testing"

	gitea_sdk "gitea.dev/sdk"
)

func TestUserDetail(t *testing.T) {
	u := &gitea_sdk.User{
		ID:        42,
		UserName:  "alice",
		FullName:  "Alice Smith",
		Email:     "alice@example.com",
		AvatarURL: "https://gitea.com/avatars/42",
		HTMLURL:   "https://gitea.com/alice",
		IsAdmin:   true,
	}
	m := UserDetail(u)

	if m["id"] != int64(42) {
		t.Errorf("expected id 42, got %v", m["id"])
	}
	if m["login"] != "alice" {
		t.Errorf("expected login alice, got %v", m["login"])
	}
	if m["full_name"] != "Alice Smith" {
		t.Errorf("expected full_name Alice Smith, got %v", m["full_name"])
	}
	if m["is_admin"] != true {
		t.Errorf("expected is_admin true, got %v", m["is_admin"])
	}
}

func TestUserDetail_Nil(t *testing.T) {
	if m := UserDetail(nil); m != nil {
		t.Errorf("expected nil for nil user, got %v", m)
	}
}

func TestLabel(t *testing.T) {
	l := &gitea_sdk.Label{
		ID:          1,
		Name:        "bug",
		Color:       "#d73a4a",
		Description: "Something isn't working",
		Exclusive:   false,
	}

	m := Label(l)
	if m["name"] != "bug" {
		t.Errorf("expected name bug, got %v", m["name"])
	}
	if m["color"] != "#d73a4a" {
		t.Errorf("expected color, got %v", m["color"])
	}
}

func TestRepo(t *testing.T) {
	r := &gitea_sdk.Repository{
		ID:            1,
		FullName:      "org/repo",
		Description:   "A test repo",
		HTMLURL:       "https://gitea.com/org/repo",
		CloneURL:      "https://gitea.com/org/repo.git",
		SSHURL:        "git@gitea.com:org/repo.git",
		DefaultBranch: "main",
		Language:      "Go",
		Stars:         10,
		Forks:         2,
		Owner:         &gitea_sdk.User{UserName: "org"},
		Topics:        []string{"mcp", "gitea"},
	}

	m := Repo(r)

	if m["full_name"] != "org/repo" {
		t.Errorf("expected full_name org/repo, got %v", m["full_name"])
	}
	if m["owner"] != "org" {
		t.Errorf("expected owner org, got %v", m["owner"])
	}
	topics := m["topics"].([]string)
	if len(topics) != 2 {
		t.Errorf("expected 2 topics, got %d", len(topics))
	}
}

func TestBodyWithAttachments(t *testing.T) {
	atts := []*gitea_sdk.Attachment{
		{Name: "shot.png", DownloadURL: "https://example/shot.png"},
		{Name: "log.txt", DownloadURL: "https://example/log.txt"},
	}
	got := BodyWithAttachments("see attached", atts)
	want := "see attached\n\n[shot.png](https://example/shot.png)\n[log.txt](https://example/log.txt)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if got := BodyWithAttachments("only body", nil); got != "only body" {
		t.Errorf("nil attachments should return body unchanged, got %q", got)
	}
	if got := BodyWithAttachments("", atts); got != "[shot.png](https://example/shot.png)\n[log.txt](https://example/log.txt)" {
		t.Errorf("empty body should drop separator, got %q", got)
	}
	skipped := []*gitea_sdk.Attachment{nil, {Name: "noop", DownloadURL: ""}}
	if got := BodyWithAttachments("body", skipped); got != "body" {
		t.Errorf("nil/empty-URL attachments should be skipped, got %q", got)
	}
}
