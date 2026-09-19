package search

import (
	"slices"
	"testing"

	gitea_sdk "gitea.dev/sdk"
)

func TestSlimIssues(t *testing.T) {
	issues := []*gitea_sdk.Issue{
		{
			Index:       1,
			Title:       "Bug report",
			State:       gitea_sdk.StateOpen,
			HTMLURL:     "https://gitea.com/org/repo/issues/1",
			Poster:      &gitea_sdk.User{UserName: "alice"},
			Labels:      []*gitea_sdk.Label{{Name: "bug"}},
			Repository:  &gitea_sdk.RepositoryMeta{FullName: "org/repo"},
			PullRequest: nil,
		},
		{
			Index:       2,
			Title:       "Add feature",
			State:       gitea_sdk.StateOpen,
			Poster:      &gitea_sdk.User{UserName: "bob"},
			Repository:  &gitea_sdk.RepositoryMeta{FullName: "org/repo"},
			PullRequest: &gitea_sdk.PullRequestMeta{},
		},
	}

	result := slimIssues(issues)
	if len(result) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(result))
	}
	if result[0]["repository"] != "org/repo" {
		t.Errorf("expected repository org/repo, got %v", result[0]["repository"])
	}
	if result[0]["labels"].([]string)[0] != "bug" {
		t.Errorf("expected label bug, got %v", result[0]["labels"])
	}
	if _, ok := result[0]["is_pull"]; ok {
		t.Error("issue should not have is_pull")
	}
	if result[1]["is_pull"] != true {
		t.Error("PR should have is_pull=true")
	}
}

func TestSearchIssuesToolRequired(t *testing.T) {
	inputSchema := SearchIssuesTool.InputSchema.(map[string]any)
	required, _ := inputSchema["required"].([]string)
	if !slices.Contains(required, "query") {
		t.Error("search_issues should require query")
	}
}
