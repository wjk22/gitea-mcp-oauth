package issue

import (
	"gitea.com/gitea/gitea-mcp/pkg/slim"

	gitea_sdk "gitea.dev/sdk"
)

func slimIssue(i *gitea_sdk.Issue) map[string]any {
	if i == nil {
		return nil
	}
	m := map[string]any{
		"number":     i.Index,
		"title":      i.Title,
		"body":       i.Body,
		"state":      i.State,
		"html_url":   i.HTMLURL,
		"user":       slim.UserLogin(i.Poster),
		"labels":     slim.LabelNames(i.Labels),
		"comments":   i.Comments,
		"created_at": i.Created,
		"updated_at": i.Updated,
		"closed_at":  i.Closed,
	}
	if len(i.Assignees) > 0 {
		m["assignees"] = slim.UserLogins(i.Assignees)
	}
	if i.Milestone != nil {
		m["milestone"] = map[string]any{
			"id":    i.Milestone.ID,
			"title": i.Milestone.Title,
		}
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
	return m
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
		if i.Milestone != nil {
			m["milestone"] = map[string]any{
				"id":    i.Milestone.ID,
				"title": i.Milestone.Title,
			}
		}
		if i.Ref != "" {
			m["ref"] = i.Ref
		}
		if i.Deadline != nil {
			m["deadline"] = i.Deadline
		}
		out = append(out, m)
	}
	return out
}

func slimComment(c *gitea_sdk.Comment) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"id":         c.ID,
		"body":       c.Body,
		"user":       slim.UserLogin(c.Poster),
		"html_url":   c.HTMLURL,
		"created_at": c.Created,
		"updated_at": c.Updated,
	}
}
