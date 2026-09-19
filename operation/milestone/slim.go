package milestone

import (
	gitea_sdk "gitea.dev/sdk"
)

func slimMilestone(m *gitea_sdk.Milestone) map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{
		"id":            m.ID,
		"title":         m.Title,
		"description":   m.Description,
		"state":         m.State,
		"open_issues":   m.OpenIssues,
		"closed_issues": m.ClosedIssues,
		"due_on":        m.Deadline,
	}
}

func slimMilestones(milestones []*gitea_sdk.Milestone) []map[string]any {
	out := make([]map[string]any, 0, len(milestones))
	for _, m := range milestones {
		out = append(out, slimMilestone(m))
	}
	return out
}
