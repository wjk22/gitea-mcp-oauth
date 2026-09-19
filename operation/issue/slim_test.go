package issue

import (
	"testing"

	gitea_sdk "gitea.dev/sdk"
)

func TestSlimIssue(t *testing.T) {
	i := &gitea_sdk.Issue{
		Index:   42,
		Title:   "Bug report",
		Body:    "Something is broken",
		State:   "open",
		HTMLURL: "https://gitea.com/org/repo/issues/42",
		Poster:  &gitea_sdk.User{UserName: "alice"},
		Labels:  []*gitea_sdk.Label{{Name: "bug"}},
		Milestone: &gitea_sdk.Milestone{
			ID:    1,
			Title: "v1.0",
		},
		PullRequest: &gitea_sdk.PullRequestMeta{HasMerged: false},
	}

	m := slimIssue(i)

	if m["number"] != int64(42) {
		t.Errorf("expected number 42, got %v", m["number"])
	}
	if m["body"] != "Something is broken" {
		t.Errorf("expected body, got %v", m["body"])
	}
	if m["is_pull"] != true {
		t.Error("expected is_pull true for issue with PullRequest")
	}

	ms := m["milestone"].(map[string]any)
	if ms["title"] != "v1.0" {
		t.Errorf("expected milestone title v1.0, got %v", ms["title"])
	}
}

func TestSlimIssues_ListIsSlimmer(t *testing.T) {
	i := &gitea_sdk.Issue{
		Index:  1,
		Title:  "Issue",
		State:  "open",
		Body:   "Full body",
		Poster: &gitea_sdk.User{UserName: "alice"},
		Labels: []*gitea_sdk.Label{{Name: "enhancement"}},
	}

	single := slimIssue(i)
	list := slimIssues([]*gitea_sdk.Issue{i})

	// Single has body, list does not
	if _, ok := single["body"]; !ok {
		t.Error("single issue should have body")
	}
	if _, ok := list[0]["body"]; ok {
		t.Error("list issue should not have body")
	}
}

func TestSlimIssues_Nil(t *testing.T) {
	if r := slimIssues(nil); len(r) != 0 {
		t.Errorf("expected empty slice, got %v", r)
	}
}
