package pull

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/log"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/slim"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("pull_request")

const (
	ListRepoPullRequestsToolName   = "list_pull_requests"
	PullRequestReadToolName        = "pull_request_read"
	PullRequestWriteToolName       = "pull_request_write"
	PullRequestReviewWriteToolName = "pull_request_review_write"
)

var (
	ListRepoPullRequestsTool = tool.NewDefinition(
		ListRepoPullRequestsToolName,
		"List pull requests in a repository, filterable by state and milestone, with configurable sort order (e.g. recently updated, most commented).",
		annotation.ReadOnly("List pull requests"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("state", tool.Enum("open", "closed", "all"), tool.Default("all")),
		tool.String("sort", tool.Enum("oldest", "recentupdate", "leastupdate", "mostcomment", "leastcomment", "priority"), tool.Default("recentupdate")),
		tool.Number("milestone"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	PullRequestReadTool = tool.NewDefinition(
		PullRequestReadToolName,
		"Read pull request: details, diff, changed files, head commit status, reviews, review comments.",
		annotation.ReadOnly("Read pull request details"),
		tool.String("method", tool.Required(), tool.Enum("get", "get_diff", "get_files", "get_status", "get_reviews", "get_review", "get_review_comments")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("pull_number", tool.Required()),
		tool.Number("review_id", tool.Description("for 'get_review'; optional for 'get_review_comments', omit to list all")),
		tool.Boolean("binary", tool.Description("include binary diff")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	PullRequestWriteTool = tool.NewDefinition(
		PullRequestWriteToolName,
		"Write pull requests: create, update, close, reopen, merge, update branch from base, manage reviewers.",
		annotation.Write("Create, update, close, reopen, or merge pull requests"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "close", "reopen", "merge", "update_branch", "add_reviewers", "remove_reviewers")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("pull_number", tool.Description("required except for 'create'")),
		tool.String("title", tool.Description("required for 'create'; optional for 'update'/'merge'")),
		tool.String("body", tool.Description("required for 'create'; optional for 'update'")),
		tool.String("head", tool.Description("head branch (required for 'create')")),
		tool.String("base", tool.Description("base branch (required for 'create')")),
		tool.String("assignee", tool.Description("for 'update'")),
		tool.Array("assignees", tool.Description("for 'update'"), tool.Items(map[string]any{"type": "string"})),
		tool.Number("milestone", tool.Description("for 'update'")),
		tool.String("state", tool.Description("for 'update'"), tool.Enum("open", "closed")),
		tool.Boolean("allow_maintainer_edit", tool.Description("for 'update'")),
		tool.Array("labels", tool.Description("label IDs"), tool.Items(map[string]any{"type": "number"})),
		tool.String("deadline", tool.Description("ISO 8601")),
		tool.Boolean("remove_deadline", tool.Description("for 'update'")),
		tool.String("merge_style", tool.Description("for 'merge'"), tool.Enum("merge", "rebase", "rebase-merge", "squash", "fast-forward-only"), tool.Default("merge")),
		tool.String("message", tool.Description("merge commit message or dismissal reason")),
		tool.Boolean("delete_branch", tool.Description("for 'merge'")),
		tool.Boolean("force_merge", tool.Description("merge even if checks fail")),
		tool.Boolean("merge_when_checks_succeed", tool.Description("for 'merge'")),
		tool.String("head_commit_id", tool.Description("expected head SHA for conflict detection")),
		tool.Array("reviewers", tool.Description("for 'add_reviewers'/'remove_reviewers'"), tool.Items(map[string]any{"type": "string"})),
		tool.Array("team_reviewers", tool.Description("for 'add_reviewers'/'remove_reviewers'"), tool.Items(map[string]any{"type": "string"})),
		tool.Boolean("draft", tool.Description("uses 'WIP: ' title prefix")),
	)

	PullRequestReviewWriteTool = tool.NewDefinition(
		PullRequestReviewWriteToolName,
		"Write PR reviews: create, submit, delete, dismiss, reply to and resolve review comments.",
		annotation.Write("Write pull request reviews"),
		tool.String("method", tool.Required(), tool.Enum("create", "submit", "delete", "dismiss", "reply_comment", "resolve_thread", "unresolve_thread")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("pull_number", tool.Description("required except for 'resolve_thread'/'unresolve_thread'")),
		tool.Number("review_id", tool.Description("for 'submit'/'delete'/'dismiss'")),
		tool.Number("comment_id", tool.Description("comment ID from 'get_review_comments'; resolve takes the thread's first")),
		tool.String("state", tool.Enum("APPROVED", "REQUEST_CHANGES", "COMMENT", "PENDING")),
		tool.String("body", tool.Description("review body, or reply text for 'reply_comment'")),
		tool.String("commit_id", tool.Description("for 'create'")),
		tool.String("message", tool.Description("dismissal reason")),
		tool.Array("comments", tool.Description("inline comments (for 'create')"), tool.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":         map[string]any{"type": "string"},
				"body":         map[string]any{"type": "string"},
				"old_line_num": map[string]any{"type": "number", "description": "old-file line (deletions)"},
				"new_line_num": map[string]any{"type": "number", "description": "new-file line (additions)"},
			},
		})),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:      ListRepoPullRequestsTool,
		Handler:   listRepoPullRequestsFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    PullRequestReadTool,
		Handler: pullRequestReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"get":                 tool.RepoScoped("owner", "repo"),
			"get_diff":            tool.RepoScoped("owner", "repo"),
			"get_files":           tool.RepoScoped("owner", "repo"),
			"get_status":          tool.RepoScoped("owner", "repo"),
			"get_reviews":         tool.RepoScoped("owner", "repo"),
			"get_review":          tool.RepoScoped("owner", "repo"),
			"get_review_comments": tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    PullRequestWriteTool,
		Handler: pullRequestWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create":           tool.RepoScoped("owner", "repo"),
			"update":           tool.RepoScoped("owner", "repo"),
			"close":            tool.RepoScoped("owner", "repo"),
			"reopen":           tool.RepoScoped("owner", "repo"),
			"merge":            tool.RepoScoped("owner", "repo"),
			"update_branch":    tool.RepoScoped("owner", "repo"),
			"add_reviewers":    tool.RepoScoped("owner", "repo"),
			"remove_reviewers": tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    PullRequestReviewWriteTool,
		Handler: pullRequestReviewWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create":           tool.RepoScoped("owner", "repo"),
			"submit":           tool.RepoScoped("owner", "repo"),
			"delete":           tool.RepoScoped("owner", "repo"),
			"dismiss":          tool.RepoScoped("owner", "repo"),
			"reply_comment":    tool.RepoScoped("owner", "repo"),
			"resolve_thread":   tool.RepoScoped("owner", "repo"),
			"unresolve_thread": tool.RepoScoped("owner", "repo"),
		}),
	})
}

func pullRequestReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "get":
		return getPullRequestByIndexFn(ctx, args)
	case "get_diff":
		return getPullRequestDiffFn(ctx, args)
	case "get_files":
		return getPullRequestFilesFn(ctx, args)
	case "get_status":
		return getPullRequestStatusFn(ctx, args)
	case "get_reviews":
		return listPullRequestReviewsFn(ctx, args)
	case "get_review":
		return getPullRequestReviewFn(ctx, args)
	case "get_review_comments":
		return listPullRequestReviewCommentsFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func pullRequestWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createPullRequestFn(ctx, args)
	case "update":
		return editPullRequestFn(ctx, args)
	case "close":
		return closePullRequestFn(ctx, args)
	case "reopen":
		return reopenPullRequestFn(ctx, args)
	case "merge":
		return mergePullRequestFn(ctx, args)
	case "update_branch":
		return updatePullRequestBranchFn(ctx, args)
	case "add_reviewers":
		return createPullRequestReviewerFn(ctx, args)
	case "remove_reviewers":
		return deletePullRequestReviewerFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func closePullRequestFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	state := gitea_sdk.StateClosed
	pr, _, err := client.PullRequests.EditPullRequest(ctx, owner, repo, index, gitea_sdk.EditPullRequestOption{
		State: &state,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("close %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimPullRequest(pr))
}

func reopenPullRequestFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	state := gitea_sdk.StateOpen
	pr, _, err := client.PullRequests.EditPullRequest(ctx, owner, repo, index, gitea_sdk.EditPullRequestOption{
		State: &state,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("reopen %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimPullRequest(pr))
}

func pullRequestReviewWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createPullRequestReviewFn(ctx, args)
	case "submit":
		return submitPullRequestReviewFn(ctx, args)
	case "delete":
		return deletePullRequestReviewFn(ctx, args)
	case "dismiss":
		return dismissPullRequestReviewFn(ctx, args)
	case "reply_comment":
		return replyPullRequestReviewCommentFn(ctx, args)
	case "resolve_thread":
		return resolveReviewThreadFn(ctx, args)
	case "unresolve_thread":
		return unresolveReviewThreadFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func getPullRequestByIndexFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	pr, _, err := client.PullRequests.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	// /pulls/{n} omits `assets`; PRs are issues internally, so the issue
	// assets endpoint surfaces description attachments.
	var assets []*gitea_sdk.Attachment
	assetsPath := fmt.Sprintf("repos/%s/%s/issues/%d/assets", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "GET", assetsPath, nil, nil, &assets); err != nil {
		log.Debugf("fetch %v/%v/issues/%v/assets err: %v", owner, repo, index, err)
	}

	m := slimPullRequest(pr)
	m["body"] = slim.BodyWithAttachments(pr.Body, assets)
	return to.TextResult(m)
}

func getPullRequestDiffFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	binary, _ := args["binary"].(bool)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	diffBytes, _, err := client.PullRequests.GetPullRequestDiff(ctx, owner, repo, index, gitea_sdk.PullRequestDiffOptions{
		Binary: binary,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v diff err: %v", owner, repo, index, err))
	}

	return to.TextResult(string(diffBytes))
}

func listRepoPullRequestsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	state, _ := args["state"].(string)
	sort := params.GetOptionalString(args, "sort", "recentupdate")
	milestone := params.GetOptionalInt(args, "milestone", 0)
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListPullRequestsOptions{
		State:     gitea_sdk.StateType(state),
		Sort:      sort,
		Milestone: milestone,
		Page:      page,
		PageSize:  pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	pullRequests, _, err := client.PullRequests.ListRepoPullRequests(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list %v/%v/pull_requests err: %v", owner, repo, err))
	}

	return to.TextResult(slimPullRequests(pullRequests))
}

// defaultWIPPrefixes are the default Gitea title prefixes that mark a PR as
// work-in-progress / draft. Gitea matches these case-insensitively.
var defaultWIPPrefixes = []string{"WIP:", "[WIP]"}

// applyDraftPrefix adds or removes a WIP title prefix that Gitea uses to mark
// pull requests as drafts. When the title already carries a recognized prefix
// and isDraft is true, the title is returned unchanged to avoid normalization.
func applyDraftPrefix(title string, isDraft bool) string {
	for _, prefix := range defaultWIPPrefixes {
		if len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			if isDraft {
				return title
			}
			return strings.TrimLeft(title[len(prefix):], " ")
		}
	}
	if isDraft {
		return "WIP: " + title
	}
	return title
}

func createPullRequestFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	title, err := params.GetString(args, "title")
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}
	head, err := params.GetString(args, "head")
	if err != nil {
		return to.ErrorResult(err)
	}
	base, err := params.GetString(args, "base")
	if err != nil {
		return to.ErrorResult(err)
	}

	if draft, ok := args["draft"].(bool); ok {
		title = applyDraftPrefix(title, draft)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	opt := gitea_sdk.CreatePullRequestOption{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
	}
	if labelIDs, err := params.GetInt64Slice(args, "labels"); err == nil {
		opt.Labels = labelIDs
	}
	opt.Deadline = params.GetOptionalTime(args, "deadline")
	pr, _, err := client.PullRequests.CreatePullRequest(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/pull_request err: %v", owner, repo, err))
	}

	return to.TextResult(slimPullRequest(pr))
}

type reviewerOp func(client *gitea_sdk.PullRequestsService, ctx context.Context, owner, repo string, index int64, opt gitea_sdk.PullReviewRequestOptions) (*gitea_sdk.Response, error)

func pullRequestReviewerFn(ctx context.Context, args map[string]any, verb string, op reviewerOp) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	reviewers := params.GetStringSlice(args, "reviewers")
	teamReviewers := params.GetStringSlice(args, "team_reviewers")

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	if _, err := op(client.PullRequests, ctx, owner, repo, index, gitea_sdk.PullReviewRequestOptions{
		Reviewers:     reviewers,
		TeamReviewers: teamReviewers,
	}); err != nil {
		return to.ErrorResult(fmt.Errorf("%s review requests for %v/%v/pr/%v err: %v", verb, owner, repo, index, err))
	}

	return to.TextResult(map[string]any{
		"message":        fmt.Sprintf("Successfully %sd review requests", verb),
		"reviewers":      reviewers,
		"team_reviewers": teamReviewers,
		"pr_index":       index,
		"repository":     fmt.Sprintf("%s/%s", owner, repo),
	})
}

func createPullRequestReviewerFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return pullRequestReviewerFn(ctx, args, "create", (*gitea_sdk.PullRequestsService).CreateReviewRequests)
}

func deletePullRequestReviewerFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return pullRequestReviewerFn(ctx, args, "delete", (*gitea_sdk.PullRequestsService).DeleteReviewRequests)
}

func listPullRequestReviewsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	reviews, _, err := client.PullRequests.ListPullReviews(ctx, owner, repo, index, gitea_sdk.ListPullReviewsOptions{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list reviews for %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimReviews(reviews))
}

func getPullRequestReviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	reviewID, err := params.GetIndex(args, "review_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	review, _, err := client.PullRequests.GetPullReview(ctx, owner, repo, index, reviewID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get review %v for %v/%v/pr/%v err: %v", reviewID, owner, repo, index, err))
	}

	return to.TextResult(slimReview(review))
}

func listPullRequestReviewCommentsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	// review comments hang off reviews, so without a review_id walk a page of
	// reviews, keeping each thread and its replies together
	var reviewIDs []int64
	if reviewID := params.GetOptionalInt(args, "review_id", 0); reviewID != 0 {
		reviewIDs = append(reviewIDs, reviewID)
	} else {
		page, pageSize := params.GetPagination(args, 30)
		reviews, _, err := client.PullRequests.ListPullReviews(ctx, owner, repo, index, gitea_sdk.ListPullReviewsOptions{
			Page: page, PageSize: pageSize,
		})
		if err != nil {
			return to.ErrorResult(fmt.Errorf("list reviews for %v/%v/pr/%v err: %v", owner, repo, index, err))
		}
		reviewIDs = make([]int64, 0, len(reviews))
		for _, review := range reviews {
			if review.CodeCommentsCount > 0 {
				reviewIDs = append(reviewIDs, review.ID)
			}
		}
	}

	var comments []*gitea_sdk.PullReviewComment
	for _, reviewID := range reviewIDs {
		reviewComments, _, err := client.PullRequests.ListPullReviewComments(ctx, owner, repo, index, reviewID)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("list review comments for review %v on %v/%v/pr/%v err: %v", reviewID, owner, repo, index, err))
		}
		comments = append(comments, reviewComments...)
	}

	return to.TextResult(slimReviewComments(comments))
}

func createPullRequestReviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.CreatePullReviewOptions{}

	if state, ok := args["state"].(string); ok {
		opt.State = gitea_sdk.ReviewStateType(state)
	}
	if body, ok := args["body"].(string); ok {
		opt.Body = body
	}
	if commitID, ok := args["commit_id"].(string); ok {
		opt.CommitID = commitID
	}

	// Parse inline comments
	if commentsArg, exists := args["comments"]; exists {
		if commentsSlice, ok := commentsArg.([]any); ok {
			for _, comment := range commentsSlice {
				if commentMap, ok := comment.(map[string]any); ok {
					reviewComment := gitea_sdk.CreatePullReviewComment{}
					if path, ok := commentMap["path"].(string); ok {
						reviewComment.Path = path
					}
					if body, ok := commentMap["body"].(string); ok {
						reviewComment.Body = body
					}
					if oldLineNum, ok := params.ToInt64(commentMap["old_line_num"]); ok {
						reviewComment.OldLineNum = oldLineNum
					}
					if newLineNum, ok := params.ToInt64(commentMap["new_line_num"]); ok {
						reviewComment.NewLineNum = newLineNum
					}
					opt.Comments = append(opt.Comments, reviewComment)
				}
			}
		}
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	review, _, err := client.PullRequests.CreatePullReview(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create review for %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimReview(review))
}

func submitPullRequestReviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	reviewID, err := params.GetIndex(args, "review_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	state, err := params.GetString(args, "state")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.SubmitPullReviewOptions{
		State: gitea_sdk.ReviewStateType(state),
	}
	if body, ok := args["body"].(string); ok {
		opt.Body = body
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	review, _, err := client.PullRequests.SubmitPullReview(ctx, owner, repo, index, reviewID, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("submit review %v for %v/%v/pr/%v err: %v", reviewID, owner, repo, index, err))
	}

	return to.TextResult(slimReview(review))
}

func deletePullRequestReviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	reviewID, err := params.GetIndex(args, "review_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	_, err = client.PullRequests.DeletePullReview(ctx, owner, repo, index, reviewID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete review %v for %v/%v/pr/%v err: %v", reviewID, owner, repo, index, err))
	}

	successMsg := map[string]any{
		"message":    "Successfully deleted review",
		"review_id":  reviewID,
		"pr_index":   index,
		"repository": fmt.Sprintf("%s/%s", owner, repo),
	}

	return to.TextResult(successMsg)
}

func dismissPullRequestReviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	reviewID, err := params.GetIndex(args, "review_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.DismissPullReviewOptions{}
	if message, ok := args["message"].(string); ok {
		opt.Message = message
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	_, err = client.PullRequests.DismissPullReview(ctx, owner, repo, index, reviewID, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("dismiss review %v for %v/%v/pr/%v err: %v", reviewID, owner, repo, index, err))
	}

	successMsg := map[string]any{
		"message":    "Successfully dismissed review",
		"review_id":  reviewID,
		"pr_index":   index,
		"repository": fmt.Sprintf("%s/%s", owner, repo),
	}

	return to.TextResult(successMsg)
}

func replyPullRequestReviewCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	commentID, err := params.GetIndex(args, "comment_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	comment, _, err := client.PullRequests.CreatePullReviewCommentReply(ctx, owner, repo, index, commentID, gitea_sdk.CreatePullReviewCommentReplyOptions{
		Body: body,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("reply to review comment %v on %v/%v/pr/%v err: %v", commentID, owner, repo, index, err))
	}

	return to.TextResult(slimReviewComment(comment))
}

func resolveReviewThreadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return setReviewThreadResolvedFn(ctx, args, true)
}

func unresolveReviewThreadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return setReviewThreadResolvedFn(ctx, args, false)
}

func setReviewThreadResolvedFn(ctx context.Context, args map[string]any, resolved bool) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	commentID, err := params.GetIndex(args, "comment_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	if resolved {
		_, err = client.PullRequests.ResolvePullReviewComment(ctx, owner, repo, commentID)
	} else {
		_, err = client.PullRequests.UnresolvePullReviewComment(ctx, owner, repo, commentID)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("set resolved=%v on review comment %v in %v/%v err: %v", resolved, commentID, owner, repo, err))
	}

	successMsg := map[string]any{
		"message":    "Successfully updated review thread",
		"comment_id": commentID,
		"resolved":   resolved,
		"repository": fmt.Sprintf("%s/%s", owner, repo),
	}

	return to.TextResult(successMsg)
}

func mergePullRequestFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	mergeStyle := params.GetOptionalString(args, "merge_style", "merge")
	title, _ := args["title"].(string)
	message, _ := args["message"].(string)
	deleteBranch, _ := args["delete_branch"].(bool)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	forceMerge, _ := args["force_merge"].(bool)
	mergeWhenChecksSucceed, _ := args["merge_when_checks_succeed"].(bool)
	headCommitID, _ := args["head_commit_id"].(string)
	deleteBranchAfterMerge := &deleteBranch

	opt := gitea_sdk.MergePullRequestOption{
		Style:                  gitea_sdk.MergeStyle(mergeStyle),
		Title:                  title,
		Message:                message,
		DeleteBranchAfterMerge: deleteBranchAfterMerge,
		ForceMerge:             forceMerge,
		MergeWhenChecksSucceed: mergeWhenChecksSucceed,
		HeadCommitId:           headCommitID,
	}

	merged, resp, err := client.PullRequests.MergePullRequest(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("merge %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	if !merged && resp != nil && resp.StatusCode >= 400 {
		return to.ErrorResult(fmt.Errorf("merge %v/%v/pr/%v failed: HTTP %d %s", owner, repo, index, resp.StatusCode, resp.Status))
	}

	if !merged {
		return to.ErrorResult(fmt.Errorf("merge %v/%v/pr/%v returned merged=false", owner, repo, index))
	}

	successMsg := map[string]any{
		"merged":         merged,
		"pr_index":       index,
		"repository":     fmt.Sprintf("%s/%s", owner, repo),
		"merge_style":    mergeStyle,
		"branch_deleted": deleteBranch,
	}

	return to.TextResult(successMsg)
}

func editPullRequestFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditPullRequestOption{}

	if title, ok := args["title"].(string); ok {
		opt.Title = title
	}
	if draft, ok := args["draft"].(bool); ok {
		if opt.Title == "" {
			// Fetch current title so the caller doesn't have to provide it
			// just to toggle draft status.
			client, err := gitea.ClientFromContext(ctx)
			if err != nil {
				return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
			}
			pr, _, err := client.PullRequests.GetPullRequest(ctx, owner, repo, index)
			if err != nil {
				return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v err: %v", owner, repo, index, err))
			}
			opt.Title = pr.Title
		}
		opt.Title = applyDraftPrefix(opt.Title, draft)
	}
	opt.Body = params.GetPresentStringPtr(args, "body")
	opt.AllowMaintainerEdit = params.GetOptionalBoolPtr(args, "allow_maintainer_edit")
	opt.RemoveDeadline = params.GetOptionalBoolPtr(args, "remove_deadline")
	opt.Deadline = params.GetOptionalTime(args, "deadline")
	if base, ok := args["base"].(string); ok {
		opt.Base = base
	}
	if assignee, ok := args["assignee"].(string); ok {
		opt.Assignee = assignee
	}
	if assignees := params.GetStringSlice(args, "assignees"); assignees != nil {
		opt.Assignees = assignees
	}
	if val, exists := args["milestone"]; exists {
		if milestone, ok := params.ToInt64(val); ok {
			opt.Milestone = milestone
		}
	}
	if state, ok := args["state"].(string); ok {
		s := gitea_sdk.StateType(state)
		opt.State = &s
	}
	if labelIDs, err := params.GetInt64Slice(args, "labels"); err == nil {
		opt.Labels = labelIDs
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	pr, _, err := client.PullRequests.EditPullRequest(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/pr/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimPullRequest(pr))
}

func updatePullRequestBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	path := fmt.Sprintf("repos/%s/%s/pulls/%d/update", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "POST", path, nil, nil, nil); err != nil {
		return to.ErrorResult(fmt.Errorf("update %v/%v/pr/%v branch err: %v", owner, repo, index, err))
	}
	return to.TextResult(map[string]any{"message": "branch updated from base"})
}

func getPullRequestFilesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	files, _, err := client.PullRequests.ListPullRequestFiles(ctx, owner, repo, index, gitea_sdk.ListPullRequestFilesOptions{
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v files err: %v", owner, repo, index, err))
	}
	return to.TextResult(files)
}

func getPullRequestStatusFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "pull_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	pr, _, err := client.PullRequests.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v err: %v", owner, repo, index, err))
	}
	if pr.Head == nil || pr.Head.Sha == "" {
		return to.ErrorResult(fmt.Errorf("pr %v/%v/%v has no head SHA", owner, repo, index))
	}

	status, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, pr.Head.Sha)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/pr/%v status err: %v", owner, repo, index, err))
	}
	return to.TextResult(status)
}
