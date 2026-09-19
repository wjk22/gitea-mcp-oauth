package issue

import (
	"context"
	"fmt"
	"net/url"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/slim"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// issueWithAssets / commentWithAssets wrap the SDK types to capture the
// `assets` field that the SDK currently drops on these endpoints.
type issueWithAssets struct {
	gitea_sdk.Issue
	Assets []*gitea_sdk.Attachment `json:"assets"`
}

type commentWithAssets struct {
	gitea_sdk.Comment
	Assets []*gitea_sdk.Attachment `json:"assets"`
}

var Tool = tool.New("issue")

const (
	ListRepoIssuesToolName = "list_issues"
	IssueReadToolName      = "issue_read"
	IssueWriteToolName     = "issue_write"
)

var (
	ListRepoIssuesTool = tool.NewDefinition(
		ListRepoIssuesToolName,
		"List issues in a repository (or pull requests, via the 'type' filter), filterable by state, labels, milestones, and update time range.",
		annotation.ReadOnly("List repository issues"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("state", tool.Default("all")),
		tool.String("type", tool.Description("issues or pulls"), tool.Enum("issues", "pulls")),
		tool.Array("labels", tool.Description("label name filter"), tool.Items(map[string]any{"type": "string"})),
		tool.Array("milestones", tool.Description("milestone name or ID filter"), tool.Items(map[string]any{"type": "string"})),
		tool.String("since", tool.Description("updated after ISO 8601")),
		tool.String("before", tool.Description("updated before ISO 8601")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	IssueReadTool = tool.NewDefinition(
		IssueReadToolName,
		"Read issue: details, comments, or labels.",
		annotation.ReadOnly("Read issue details"),
		tool.String("method", tool.Required(), tool.Enum("get", "get_comments", "get_labels")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("issue_number", tool.Required()),
	)

	IssueWriteTool = tool.NewDefinition(
		IssueWriteToolName,
		"Write issues: create, update, manage comments and labels.",
		annotation.Write("Create or update issues, comments, and labels"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "add_comment", "edit_comment", "add_labels", "remove_label", "replace_labels", "clear_labels")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("issue_number", tool.Description("required except for 'create'")),
		tool.String("title", tool.Description("required for 'create'")),
		tool.String("body", tool.Description("required for 'create'/'add_comment'/'edit_comment'")),
		tool.Array("assignees", tool.Items(map[string]any{"type": "string"})),
		tool.Number("milestone"),
		tool.String("state", tool.Enum("open", "closed", "all")),
		tool.Number("commentID", tool.Description("for 'edit_comment'")),
		tool.Array("labels", tool.Description("label IDs"), tool.Items(map[string]any{"type": "number"})),
		tool.Number("label_id", tool.Description("for 'remove_label'")),
		tool.String("ref", tool.Description("branch to associate")),
		tool.String("deadline", tool.Description("ISO 8601")),
		tool.Boolean("remove_deadline"),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:      ListRepoIssuesTool,
		Handler:   listRepoIssuesFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    IssueReadTool,
		Handler: issueReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"get":          tool.RepoScoped("owner", "repo"),
			"get_comments": tool.RepoScoped("owner", "repo"),
			"get_labels":   tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    IssueWriteTool,
		Handler: issueWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create":         tool.RepoScoped("owner", "repo"),
			"update":         tool.RepoScoped("owner", "repo"),
			"add_comment":    tool.RepoScoped("owner", "repo"),
			"edit_comment":   tool.RepoScoped("owner", "repo"),
			"add_labels":     tool.RepoScoped("owner", "repo"),
			"remove_label":   tool.RepoScoped("owner", "repo"),
			"replace_labels": tool.RepoScoped("owner", "repo"),
			"clear_labels":   tool.RepoScoped("owner", "repo"),
		}),
	})
}

func issueReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "get":
		return getIssueByIndexFn(ctx, args)
	case "get_comments":
		return getIssueCommentsByIndexFn(ctx, args)
	case "get_labels":
		return getIssueLabelsFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func issueWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createIssueFn(ctx, args)
	case "update":
		return editIssueFn(ctx, args)
	case "add_comment":
		return createIssueCommentFn(ctx, args)
	case "edit_comment":
		return editIssueCommentFn(ctx, args)
	case "add_labels":
		return addIssueLabelsFn(ctx, args)
	case "remove_label":
		return removeIssueLabelFn(ctx, args)
	case "replace_labels":
		return replaceIssueLabelsFn(ctx, args)
	case "clear_labels":
		return clearIssueLabelsFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func getIssueByIndexFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	var issue issueWithAssets
	path := fmt.Sprintf("repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "GET", path, nil, nil, &issue); err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	m := slimIssue(&issue.Issue)
	m["body"] = slim.BodyWithAttachments(issue.Body, issue.Assets)
	return to.TextResult(m)
}

func listRepoIssuesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	state, ok := args["state"].(string)
	if !ok {
		state = "all"
	}
	labels := params.GetStringSlice(args, "labels")
	milestones := params.GetStringSlice(args, "milestones")
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListIssueOption{
		State:      gitea_sdk.StateType(state),
		Labels:     labels,
		Milestones: milestones,
		Page:       page,
		PageSize:   pageSize,
	}
	switch args["type"] {
	case "issues":
		opt.Type = gitea_sdk.IssueTypeIssue
	case "pulls":
		opt.Type = gitea_sdk.IssueTypePull
	}
	if t := params.GetOptionalTime(args, "since"); t != nil {
		opt.Since = *t
	}
	if t := params.GetOptionalTime(args, "before"); t != nil {
		opt.Before = *t
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issues, _, err := client.Issues.ListRepoIssues(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues err: %v", owner, repo, err))
	}
	return to.TextResult(slimIssues(issues))
}

func createIssueFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	opt := gitea_sdk.CreateIssueOption{
		Title: title,
		Body:  body,
	}
	opt.Assignees = params.GetStringSlice(args, "assignees")
	if val, exists := args["milestone"]; exists {
		if milestone, ok := params.ToInt64(val); ok {
			opt.Milestone = milestone
		}
	}
	if labelIDs, err := params.GetInt64Slice(args, "labels"); err == nil {
		opt.Labels = labelIDs
	}
	if ref, ok := args["ref"].(string); ok {
		opt.Ref = ref
	}
	opt.Deadline = params.GetOptionalTime(args, "deadline")
	issue, _, err := client.Issues.CreateIssue(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/issue err: %v", owner, repo, err))
	}

	return to.TextResult(slimIssue(issue))
}

func createIssueCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.CreateIssueCommentOption{
		Body: body,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueComment, _, err := client.Issues.CreateIssueComment(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/issue/%v/comment err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimComment(issueComment))
}

func editIssueFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditIssueOption{
		Body:           params.GetPresentStringPtr(args, "body"),
		Ref:            params.GetPresentStringPtr(args, "ref"),
		Assignees:      params.GetStringSlice(args, "assignees"),
		Deadline:       params.GetOptionalTime(args, "deadline"),
		RemoveDeadline: params.GetOptionalBoolPtr(args, "remove_deadline"),
	}
	if title, ok := args["title"].(string); ok {
		opt.Title = title
	}
	if val, exists := args["milestone"]; exists {
		if milestone, ok := params.ToInt64(val); ok {
			opt.Milestone = &milestone
		}
	}
	if state, ok := args["state"].(string); ok {
		s := gitea_sdk.StateType(state)
		opt.State = &s
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issue, _, err := client.Issues.EditIssue(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/issue/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimIssue(issue))
}

func editIssueCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	commentID, err := params.GetIndex(args, "commentID")
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.EditIssueCommentOption{
		Body: body,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueComment, _, err := client.Issues.EditIssueComment(ctx, owner, repo, commentID, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/issues/comments/%v err: %v", owner, repo, commentID, err))
	}

	return to.TextResult(slimComment(issueComment))
}

func getIssueCommentsByIndexFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	var comments []commentWithAssets
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "GET", path, nil, nil, &comments); err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/%v/comments err: %v", owner, repo, index, err))
	}
	out := make([]map[string]any, 0, len(comments))
	for i := range comments {
		m := slimComment(&comments[i].Comment)
		m["body"] = slim.BodyWithAttachments(comments[i].Body, comments[i].Assets)
		out = append(out, m)
	}
	return to.TextResult(out)
}

func getIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Issues.GetIssueLabels(ctx, owner, repo, index, gitea_sdk.ListLabelsOptions{})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/%v/labels err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func addIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labels, err := params.GetInt64Slice(args, "labels")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueLabels, _, err := client.Issues.AddIssueLabels(ctx, owner, repo, index, gitea_sdk.IssueLabelsOption{Labels: labels})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("add labels to %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(issueLabels))
}

func replaceIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labels, err := params.GetInt64Slice(args, "labels")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueLabels, _, err := client.Issues.ReplaceIssueLabels(ctx, owner, repo, index, gitea_sdk.IssueLabelsOption{Labels: labels})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("replace labels on %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(issueLabels))
}

func clearIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.ClearIssueLabels(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("clear labels on %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult("Labels cleared successfully")
}

func removeIssueLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labelID, err := params.GetIndex(args, "label_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.DeleteIssueLabel(ctx, owner, repo, index, labelID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("remove label %v from %v/%v/issue/%v err: %v", labelID, owner, repo, index, err))
	}
	return to.TextResult("Label removed successfully")
}
