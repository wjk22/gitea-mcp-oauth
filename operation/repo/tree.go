package repo

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

const (
	GetRepoTreeToolName = "get_repository_tree"
)

var GetRepoTreeTool = tool.NewDefinition(
	GetRepoTreeToolName,
	"Get the file tree of a repository at a given ref (SHA, branch, or tag), optionally recursively.",
	annotation.ReadOnly("Get repository file tree"),
	tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
	tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
	tool.String("tree_sha", tool.Required(), tool.Description("SHA, branch, or tag")),
	tool.Boolean("recursive"),
	tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
	tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:      GetRepoTreeTool,
		Handler:   GetRepoTreeFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func GetRepoTreeFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	treeSHA, err := params.GetString(args, "tree_sha")
	if err != nil {
		return to.ErrorResult(err)
	}
	recursive, _ := args["recursive"].(bool)
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListTreeOptions{
		Page:      page,
		PageSize:  pageSize,
		Ref:       treeSHA,
		Recursive: recursive,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	tree, _, err := client.Git.GetTrees(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get repository tree err: %v", err))
	}
	return to.TextResult(slimTree(tree))
}
