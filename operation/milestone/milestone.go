package milestone

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("milestone")

const (
	MilestoneReadToolName  = "milestone_read"
	MilestoneWriteToolName = "milestone_write"
)

var (
	MilestoneReadTool = tool.NewDefinition(
		MilestoneReadToolName,
		"Read milestones: get one or list.",
		annotation.ReadOnly("Read milestones"),
		tool.String("method", tool.Required(), tool.Enum("get", "list")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("id", tool.Description("for 'get'")),
		tool.String("state", tool.Default("all")),
		tool.String("name", tool.Description("name filter (for 'list')")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	MilestoneWriteTool = tool.NewDefinition(
		MilestoneWriteToolName,
		"Write milestones: create, update, delete.",
		annotation.Destructive("Create, update, or delete milestones"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "edit", "delete")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("id", tool.Description("for 'update'/'delete'")),
		tool.String("title", tool.Description("for 'create'")),
		tool.String("description"),
		tool.String("due_on", tool.Description("due date")),
		tool.String("state", tool.Enum("open", "closed")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    MilestoneReadTool,
		Handler: milestoneReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"get":  tool.RepoScoped("owner", "repo"),
			"list": tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    MilestoneWriteTool,
		Handler: milestoneWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create": tool.RepoScoped("owner", "repo"),
			"update": tool.RepoScoped("owner", "repo"),
			"edit":   tool.RepoScoped("owner", "repo"),
			"delete": tool.RepoScoped("owner", "repo"),
		}),
	})
}

func milestoneReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "get":
		return getMilestoneFn(ctx, args)
	case "list":
		return listMilestonesFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func milestoneWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createMilestoneFn(ctx, args)
	case "update":
		return editMilestoneFn(ctx, args)
	case "edit":
		return editMilestoneFn(ctx, args)
	case "delete":
		return deleteMilestoneFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func getMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.GetMilestone(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func listMilestonesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	state := params.GetOptionalString(args, "state", "all")
	name := params.GetOptionalString(args, "name", "")
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListMilestoneOption{
		State:    gitea_sdk.StateType(state),
		Name:     name,
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestones, _, err := client.Repositories.ListMilestones(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/milestones err: %v", owner, repo, err))
	}
	return to.TextResult(slimMilestones(milestones))
}

func createMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	opt := gitea_sdk.CreateMilestoneOption{
		Title: title,
	}

	description, ok := args["description"].(string)
	if ok {
		opt.Description = description
	}
	opt.Deadline = params.GetOptionalTime(args, "due_on")

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.CreateMilestone(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/milestone err: %v", owner, repo, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func editMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditMilestoneOption{
		Description: params.GetPresentStringPtr(args, "description"),
		Deadline:    params.GetOptionalTime(args, "due_on"),
	}
	if title, ok := args["title"].(string); ok {
		opt.Title = title
	}
	if state, ok := args["state"].(string); ok {
		s := gitea_sdk.StateType(state)
		opt.State = &s
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.EditMilestone(ctx, owner, repo, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func deleteMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteMilestone(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult("Milestone deleted successfully")
}
