package repo

import (
	"context"
	"errors"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BranchTool holds the branch-related tools (scope "branch").
var BranchTool = tool.New("branch")

const (
	CreateBranchToolName = "create_branch"
	DeleteBranchToolName = "delete_branch"
	ListBranchesToolName = "list_branches"
	RenameBranchToolName = "rename_branch"
)

var (
	CreateBranchTool = tool.NewDefinition(
		CreateBranchToolName,
		"Create a new branch in a repository, optionally from a specific source branch (defaults to the repository's default branch).",
		annotation.Write("Create a new branch"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("branch", tool.Required()),
		tool.String("old_branch", tool.Description("source branch (default: repo default)")),
	)

	DeleteBranchTool = tool.NewDefinition(
		DeleteBranchToolName,
		"Permanently delete a branch from a repository. This action is destructive and cannot be undone.",
		annotation.Destructive("Delete a branch"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("branch", tool.Required()),
	)

	ListBranchesTool = tool.NewDefinition(
		ListBranchesToolName,
		"List all branches in a repository, paginated.",
		annotation.ReadOnly("List repository branches"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	RenameBranchTool = tool.NewDefinition(
		RenameBranchToolName,
		"Rename an existing branch in a repository.",
		annotation.Write("Rename a branch"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("branch", tool.Required()),
		tool.String("new_name", tool.Required()),
	)
)

func init() {
	BranchTool.RegisterWrite(tool.ServerTool{
		Tool:      CreateBranchTool,
		Handler:   CreateBranchFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	BranchTool.RegisterWrite(tool.ServerTool{
		Tool:      DeleteBranchTool,
		Handler:   DeleteBranchFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	BranchTool.RegisterRead(tool.ServerTool{
		Tool:      ListBranchesTool,
		Handler:   ListBranchesFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	BranchTool.RegisterWrite(tool.ServerTool{
		Tool:      RenameBranchTool,
		Handler:   RenameBranchFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func CreateBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	branch, err := params.GetString(args, "branch")
	if err != nil {
		return to.ErrorResult(err)
	}
	oldBranch, _ := args["old_branch"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.CreateBranch(ctx, owner, repo, gitea_sdk.CreateBranchOption{
		BranchName:    branch,
		OldBranchName: oldBranch,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create branch error: %v", err))
	}

	return to.TextResult("Branch Created")
}

func DeleteBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	branch, err := params.GetString(args, "branch")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.DeleteRepoBranch(ctx, owner, repo, branch)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete branch error: %v", err))
	}

	return to.TextResult("Branch Deleted")
}

func ListBranchesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListRepoBranchesOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	branches, _, err := client.Repositories.ListRepoBranches(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list branches error: %v", err))
	}

	return to.TextResult(slimBranches(branches))
}

func RenameBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	branch, err := params.GetString(args, "branch")
	if err != nil {
		return to.ErrorResult(err)
	}
	newName, err := params.GetString(args, "new_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	successful, _, err := client.Repositories.RenameRepoBranch(ctx, owner, repo, branch, gitea_sdk.RenameRepoBranchOption{Name: newName})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("rename branch error: %v", err))
	}
	if !successful {
		return to.ErrorResult(errors.New("rename branch error: unsuccessful"))
	}

	return to.TextResult("Branch renamed")
}
