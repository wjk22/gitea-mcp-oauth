package slim

import (
	"fmt"
	"strings"

	gitea_sdk "gitea.dev/sdk"
)

func UserLogin(u *gitea_sdk.User) string {
	if u == nil {
		return ""
	}
	return u.UserName
}

func UserLogins(users []*gitea_sdk.User) []string {
	if len(users) == 0 {
		return nil
	}
	out := make([]string, 0, len(users))
	for _, u := range users {
		if u != nil {
			out = append(out, u.UserName)
		}
	}
	return out
}

func LabelNames(labels []*gitea_sdk.Label) []string {
	if len(labels) == 0 {
		return nil
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l != nil {
			out = append(out, l.Name)
		}
	}
	return out
}

func BodyWithAttachments(body string, atts []*gitea_sdk.Attachment) string {
	links := make([]string, 0, len(atts))
	for _, a := range atts {
		if a == nil || a.DownloadURL == "" {
			continue
		}
		links = append(links, fmt.Sprintf("[%s](%s)", a.Name, a.DownloadURL))
	}
	if len(links) == 0 {
		return body
	}
	joined := strings.Join(links, "\n")
	if body == "" {
		return joined
	}
	return body + "\n\n" + joined
}

func UserDetail(u *gitea_sdk.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"id":         u.ID,
		"login":      u.UserName,
		"full_name":  u.FullName,
		"email":      u.Email,
		"avatar_url": u.AvatarURL,
		"html_url":   u.HTMLURL,
		"is_admin":   u.IsAdmin,
	}
}

func Repo(r *gitea_sdk.Repository) map[string]any {
	if r == nil {
		return nil
	}
	m := map[string]any{
		"id":                r.ID,
		"full_name":         r.FullName,
		"description":       r.Description,
		"html_url":          r.HTMLURL,
		"clone_url":         r.CloneURL,
		"ssh_url":           r.SSHURL,
		"default_branch":    r.DefaultBranch,
		"private":           r.Private,
		"fork":              r.Fork,
		"archived":          r.Archived,
		"language":          r.Language,
		"stars_count":       r.Stars,
		"forks_count":       r.Forks,
		"open_issues_count": r.OpenIssues,
		"open_pr_counter":   r.OpenPulls,
		"created_at":        r.Created,
		"updated_at":        r.Updated,
	}
	if r.Owner != nil {
		m["owner"] = r.Owner.UserName
	}
	if len(r.Topics) > 0 {
		m["topics"] = r.Topics
	}
	return m
}

func Repos(repos []*gitea_sdk.Repository) []map[string]any {
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		out = append(out, Repo(r))
	}
	return out
}

func Label(l *gitea_sdk.Label) map[string]any {
	if l == nil {
		return nil
	}
	return map[string]any{
		"id":          l.ID,
		"name":        l.Name,
		"color":       l.Color,
		"description": l.Description,
		"exclusive":   l.Exclusive,
	}
}

func Labels(labels []*gitea_sdk.Label) []map[string]any {
	out := make([]map[string]any, 0, len(labels))
	for _, l := range labels {
		out = append(out, Label(l))
	}
	return out
}
