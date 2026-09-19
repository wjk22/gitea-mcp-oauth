package notification

import (
	gitea_sdk "gitea.dev/sdk"
)

func slimThread(t *gitea_sdk.NotificationThread) map[string]any {
	if t == nil {
		return nil
	}
	m := map[string]any{
		"id":         t.ID,
		"unread":     t.Unread,
		"updated_at": t.UpdatedAt,
	}
	if t.Pinned {
		m["pinned"] = true
	}
	if t.Repository != nil {
		m["repository"] = t.Repository.FullName
	}
	if t.Subject != nil {
		subject := map[string]any{
			"title": t.Subject.Title,
			"type":  t.Subject.Type,
			"state": t.Subject.State,
		}
		if t.Subject.HTMLURL != "" {
			subject["html_url"] = t.Subject.HTMLURL
		}
		if t.Subject.LatestCommentHTMLURL != "" {
			subject["latest_comment_html_url"] = t.Subject.LatestCommentHTMLURL
		}
		m["subject"] = subject
	}
	return m
}

func slimThreads(threads []*gitea_sdk.NotificationThread) []map[string]any {
	out := make([]map[string]any, 0, len(threads))
	for _, t := range threads {
		if t == nil {
			continue
		}
		m := map[string]any{
			"id":         t.ID,
			"unread":     t.Unread,
			"updated_at": t.UpdatedAt,
		}
		if t.Pinned {
			m["pinned"] = true
		}
		if t.Repository != nil {
			m["repository"] = t.Repository.FullName
		}
		if t.Subject != nil {
			m["subject"] = map[string]any{
				"title": t.Subject.Title,
				"type":  t.Subject.Type,
				"state": t.Subject.State,
			}
		}
		out = append(out, m)
	}
	return out
}
