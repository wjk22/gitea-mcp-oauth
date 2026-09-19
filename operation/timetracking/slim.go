package timetracking

import (
	gitea_sdk "gitea.dev/sdk"
)

func slimStopWatch(s *gitea_sdk.StopWatch) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"issue_index": s.IssueIndex,
		"issue_title": s.IssueTitle,
		"repo_name":   s.RepoName,
		"repo_owner":  s.RepoOwnerName,
		"created":     s.Created,
		"seconds":     s.Seconds,
	}
}

func slimStopWatches(watches []*gitea_sdk.StopWatch) []map[string]any {
	out := make([]map[string]any, 0, len(watches))
	for _, s := range watches {
		out = append(out, slimStopWatch(s))
	}
	return out
}

func slimTrackedTime(t *gitea_sdk.TrackedTime) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"id":        t.ID,
		"time":      t.Time,
		"user_name": t.UserName,
		"created":   t.Created,
	}
}

func slimTrackedTimes(times []*gitea_sdk.TrackedTime) []map[string]any {
	out := make([]map[string]any, 0, len(times))
	for _, t := range times {
		out = append(out, slimTrackedTime(t))
	}
	return out
}
