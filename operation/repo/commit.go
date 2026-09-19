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

// CommitTool holds the commit-related tools (scope "commit").
var CommitTool = tool.New("commit")

const (
	ListRepoCommitsToolName = "list_commits"
	GetCommitToolName       = "get_commit"
)

var (
	ListRepoCommitsTool = tool.NewDefinition(
		ListRepoCommitsToolName,
		"List commits in a repository, optionally starting from a specific branch or SHA and filtered to commits touching a given file path.",
		annotation.ReadOnly("List repository commits"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("sha", tool.Description("starting SHA or branch")),
		tool.String("path", tool.Description("only commits touching this path")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	GetCommitTool = tool.NewDefinition(
		GetCommitToolName,
		"Get details for a single commit in a repository by its SHA.",
		annotation.ReadOnly("Get commit details"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("sha", tool.Required()),
	)
)

func init() {
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:      ListRepoCommitsTool,
		Handler:   ListRepoCommitsFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:      GetCommitTool,
		Handler:   GetCommitFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func ListRepoCommitsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	sha, _ := args["sha"].(string)
	path, _ := args["path"].(string)
	opt := gitea_sdk.ListCommitOptions{
		Page:     page,
		PageSize: pageSize,
		SHA:      sha,
		Path:     path,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	commits, _, err := client.Repositories.ListRepoCommits(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list repo commits err: %v", err))
	}
	return to.TextResult(slimCommits(commits))
}

func GetCommitFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	sha, err := params.GetString(args, "sha")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	commit, _, err := client.Repositories.GetSingleCommit(ctx, owner, repo, sha)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get commit %v err: %v", sha, err))
	}
	return to.TextResult(slimCommit(commit))
}
