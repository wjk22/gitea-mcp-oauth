package search

import (
	"gitea.com/gitea/gitea-mcp/pkg/slim"

	gitea_sdk "gitea.dev/sdk"
)

func slimUserDetails(users []*gitea_sdk.User) []map[string]any {
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, slim.UserDetail(u))
	}
	return out
}

func slimTeam(t *gitea_sdk.Team) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"id":          t.ID,
		"name":        t.Name,
		"description": t.Description,
		"permission":  t.Permission,
	}
}

func slimTeams(teams []*gitea_sdk.Team) []map[string]any {
	out := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		out = append(out, slimTeam(t))
	}
	return out
}

func slimIssues(issues []*gitea_sdk.Issue) []map[string]any {
	out := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		if i == nil {
			continue
		}
		m := map[string]any{
			"number":     i.Index,
			"title":      i.Title,
			"state":      i.State,
			"html_url":   i.HTMLURL,
			"user":       slim.UserLogin(i.Poster),
			"comments":   i.Comments,
			"created_at": i.Created,
			"updated_at": i.Updated,
		}
		if len(i.Labels) > 0 {
			m["labels"] = slim.LabelNames(i.Labels)
		}
		if i.Repository != nil {
			m["repository"] = i.Repository.FullName
		}
		if i.Ref != "" {
			m["ref"] = i.Ref
		}
		if i.Deadline != nil {
			m["deadline"] = i.Deadline
		}
		if i.PullRequest != nil {
			m["is_pull"] = true
		}
		out = append(out, m)
	}
	return out
}
