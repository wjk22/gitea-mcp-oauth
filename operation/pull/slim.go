package pull

import (
	"gitea.com/gitea/gitea-mcp/pkg/slim"

	gitea_sdk "gitea.dev/sdk"
)

func repoRef(r *gitea_sdk.Repository) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"full_name":   r.FullName,
		"description": r.Description,
	}
}

func slimPullRequest(pr *gitea_sdk.PullRequest) map[string]any {
	if pr == nil {
		return nil
	}
	m := map[string]any{
		"number":     pr.Index,
		"title":      pr.Title,
		"body":       pr.Body,
		"state":      pr.State,
		"draft":      pr.Draft,
		"merged":     pr.HasMerged,
		"mergeable":  pr.Mergeable,
		"html_url":   pr.HTMLURL,
		"user":       slim.UserLogin(pr.Poster),
		"labels":     slim.LabelNames(pr.Labels),
		"comments":   pr.Comments,
		"created_at": pr.Created,
		"updated_at": pr.Updated,
		"closed_at":  pr.Closed,
	}
	if pr.HasMerged {
		m["merged_at"] = pr.Merged
		m["merge_commit_sha"] = pr.MergedCommitID
		m["merged_by"] = slim.UserLogin(pr.MergedBy)
	}
	if pr.Head != nil {
		head := map[string]any{"ref": pr.Head.Ref, "sha": pr.Head.Sha}
		if pr.Head.Repository != nil {
			head["repo"] = repoRef(pr.Head.Repository)
		}
		m["head"] = head
	}
	if pr.Base != nil {
		base := map[string]any{"ref": pr.Base.Ref, "sha": pr.Base.Sha}
		if pr.Base.Repository != nil {
			base["repo"] = repoRef(pr.Base.Repository)
		}
		m["base"] = base
	}
	if pr.Additions != nil {
		m["additions"] = *pr.Additions
	}
	if pr.Deletions != nil {
		m["deletions"] = *pr.Deletions
	}
	if pr.ChangedFiles != nil {
		m["changed_files"] = *pr.ChangedFiles
	}
	if len(pr.Assignees) > 0 {
		m["assignees"] = slim.UserLogins(pr.Assignees)
	}
	if pr.Milestone != nil {
		m["milestone"] = pr.Milestone.Title
	}
	if pr.ReviewComments > 0 {
		m["review_comments"] = pr.ReviewComments
	}
	return m
}

func slimPullRequests(prs []*gitea_sdk.PullRequest) []map[string]any {
	out := make([]map[string]any, 0, len(prs))
	for _, pr := range prs {
		if pr == nil {
			continue
		}
		m := map[string]any{
			"number":     pr.Index,
			"title":      pr.Title,
			"state":      pr.State,
			"draft":      pr.Draft,
			"merged":     pr.HasMerged,
			"html_url":   pr.HTMLURL,
			"user":       slim.UserLogin(pr.Poster),
			"created_at": pr.Created,
			"updated_at": pr.Updated,
		}
		if pr.Head != nil {
			m["head"] = pr.Head.Ref
		}
		if pr.Base != nil {
			m["base"] = pr.Base.Ref
		}
		if len(pr.Labels) > 0 {
			m["labels"] = slim.LabelNames(pr.Labels)
		}
		out = append(out, m)
	}
	return out
}

func slimReview(r *gitea_sdk.PullReview) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"id":             r.ID,
		"state":          r.State,
		"body":           r.Body,
		"user":           slim.UserLogin(r.Reviewer),
		"comments_count": r.CodeCommentsCount,
		"submitted_at":   r.Submitted,
		"html_url":       r.HTMLURL,
		"stale":          r.Stale,
		"official":       r.Official,
		"dismissed":      r.Dismissed,
	}
}

func slimReviews(reviews []*gitea_sdk.PullReview) []map[string]any {
	out := make([]map[string]any, 0, len(reviews))
	for _, r := range reviews {
		out = append(out, slimReview(r))
	}
	return out
}

func slimReviewComment(c *gitea_sdk.PullReviewComment) map[string]any {
	if c == nil {
		return nil
	}
	m := map[string]any{
		"id":           c.ID,
		"review_id":    c.ReviewID,
		"body":         c.Body,
		"path":         c.Path,
		"position":     c.LineNum,
		"old_position": c.OldLineNum,
		"diff_hunk":    c.DiffHunk,
		"user":         slim.UserLogin(c.Reviewer),
		"html_url":     c.HTMLURL,
		"created_at":   c.Created,
		"updated_at":   c.Updated,
	}
	// the thread's first comment carries the resolver
	if c.Resolver != nil {
		m["resolved_by"] = slim.UserLogin(c.Resolver)
	}
	return m
}

func slimReviewComments(comments []*gitea_sdk.PullReviewComment) []map[string]any {
	out := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		out = append(out, slimReviewComment(c))
	}
	return out
}
