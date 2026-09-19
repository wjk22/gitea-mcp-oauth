package pull

import (
	"testing"
	"time"

	gitea_sdk "gitea.dev/sdk"
)

func TestSlimPullRequest(t *testing.T) {
	now := time.Now()
	additions := 10
	deletions := 5
	changedFiles := 3
	pr := &gitea_sdk.PullRequest{
		Index:     1,
		Title:     "Fix bug",
		Body:      "Fixes #123",
		State:     "open",
		Draft:     false,
		HasMerged: false,
		Mergeable: true,
		HTMLURL:   "https://gitea.com/org/repo/pulls/1",
		Poster:    &gitea_sdk.User{UserName: "bob"},
		Labels: []*gitea_sdk.Label{
			{Name: "bug"},
			{Name: "priority"},
		},
		Comments:     2,
		Created:      &now,
		Updated:      &now,
		Additions:    &additions,
		Deletions:    &deletions,
		ChangedFiles: &changedFiles,
		Head: &gitea_sdk.PRBranchInfo{
			Ref: "fix-branch",
			Sha: "abc123",
		},
		Base: &gitea_sdk.PRBranchInfo{
			Ref: "main",
			Sha: "def456",
		},
		Assignees: []*gitea_sdk.User{
			{UserName: "alice"},
		},
		Milestone: &gitea_sdk.Milestone{Title: "v1.0"},
	}

	m := slimPullRequest(pr)

	if m["number"] != int64(1) {
		t.Errorf("expected number 1, got %v", m["number"])
	}
	if m["title"] != "Fix bug" {
		t.Errorf("expected title Fix bug, got %v", m["title"])
	}
	if m["user"] != "bob" {
		t.Errorf("expected user bob, got %v", m["user"])
	}
	if m["additions"] != 10 {
		t.Errorf("expected additions 10, got %v", m["additions"])
	}
	if m["milestone"] != "v1.0" {
		t.Errorf("expected milestone v1.0, got %v", m["milestone"])
	}

	labels := m["labels"].([]string)
	if len(labels) != 2 || labels[0] != "bug" {
		t.Errorf("expected labels [bug priority], got %v", labels)
	}

	head := m["head"].(map[string]any)
	if head["ref"] != "fix-branch" {
		t.Errorf("expected head ref fix-branch, got %v", head["ref"])
	}

	assignees := m["assignees"].([]string)
	if len(assignees) != 1 || assignees[0] != "alice" {
		t.Errorf("expected assignees [alice], got %v", assignees)
	}

	// merged fields should not be present for unmerged PR
	if _, ok := m["merged_at"]; ok {
		t.Error("merged_at should not be present for unmerged PR")
	}
}

func TestSlimPullRequests_ListIsSlimmer(t *testing.T) {
	pr := &gitea_sdk.PullRequest{
		Index:   1,
		Title:   "PR title",
		State:   "open",
		HTMLURL: "https://gitea.com/org/repo/pulls/1",
		Poster:  &gitea_sdk.User{UserName: "bob"},
		Body:    "Full body text here",
		Head:    &gitea_sdk.PRBranchInfo{Ref: "feature"},
		Base:    &gitea_sdk.PRBranchInfo{Ref: "main"},
	}

	single := slimPullRequest(pr)
	list := slimPullRequests([]*gitea_sdk.PullRequest{pr})

	// Single has body, list does not
	if _, ok := single["body"]; !ok {
		t.Error("single PR should have body")
	}
	if _, ok := list[0]["body"]; ok {
		t.Error("list PR should not have body")
	}

	// List has head as string ref, single has head as map
	if _, ok := single["head"].(map[string]any); !ok {
		t.Error("single PR head should be a map")
	}
	if list[0]["head"] != "feature" {
		t.Errorf("list PR head should be string ref, got %v", list[0]["head"])
	}
}

func TestSlimPullRequests_Nil(t *testing.T) {
	if r := slimPullRequests(nil); len(r) != 0 {
		t.Errorf("expected empty slice, got %v", r)
	}
}
