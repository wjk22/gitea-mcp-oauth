package repo

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/slim"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("repository")

const (
	CreateRepoToolName   = "create_repo"
	ForkRepoToolName     = "fork_repo"
	ListMyReposToolName  = "list_my_repos"
	ListOrgReposToolName = "list_org_repos"
)

var (
	CreateRepoTool = tool.NewDefinition(
		CreateRepoToolName,
		"Create a new Git repository, optionally under an organization (defaults to the authenticated user's account), with options for visibility, template, license, .gitignore, and initial README.",
		annotation.Write("Create a new repository"),
		tool.String("name", tool.Required()),
		tool.String("description"),
		tool.Boolean("private"),
		tool.String("issue_labels"),
		tool.Boolean("auto_init"),
		tool.Boolean("template"),
		tool.String("gitignores"),
		tool.String("license"),
		tool.String("readme"),
		tool.String("default_branch"),
		tool.String("trust_model", tool.Enum("default", "collaborator", "committer", "collaboratorcommitter")),
		tool.String("object_format_name", tool.Enum("sha1", "sha256")),
		tool.String("organization", tool.Description("defaults to personal account")),
	)

	ForkRepoTool = tool.NewDefinition(
		ForkRepoToolName,
		"Fork an existing repository into the authenticated user's account or a target organization, optionally under a new name.",
		annotation.Write("Fork a repository"),
		tool.String("user", tool.Required(), tool.Description("owner of source repo")),
		tool.String("repo", tool.Required()),
		tool.String("organization", tool.Description("target org")),
		tool.String("name", tool.Description("fork name")),
	)

	ListMyReposTool = tool.NewDefinition(
		ListMyReposToolName,
		"List repositories owned by the authenticated user.",
		annotation.ReadOnly("List my repositories"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	ListOrgReposTool = tool.NewDefinition(
		ListOrgReposToolName,
		"List repositories belonging to an organization.",
		annotation.ReadOnly("List organization repositories"),
		tool.String("org", tool.Required()),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(100), tool.Minimum(1)),
	)
)

func init() {
	Tool.RegisterWrite(tool.ServerTool{
		Tool:      CreateRepoTool,
		Handler:   CreateRepoFn,
		ScopeKind: tool.OwnerScoped("organization"),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:      ForkRepoTool,
		Handler:   ForkRepoFn,
		ScopeKind: tool.RepoScoped("user", "repo"),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:      ListMyReposTool,
		Handler:   ListMyReposFn,
		ScopeKind: tool.GlobalScoped(nil),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:      ListOrgReposTool,
		Handler:   ListOrgReposFn,
		ScopeKind: tool.OwnerScoped("org"),
	})
}

func CreateRepoFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)
	private, _ := args["private"].(bool)
	issueLabels, _ := args["issue_labels"].(string)
	autoInit, _ := args["auto_init"].(bool)
	template, _ := args["template"].(bool)
	gitignores, _ := args["gitignores"].(string)
	license, _ := args["license"].(string)
	readme, _ := args["readme"].(string)
	defaultBranch, _ := args["default_branch"].(string)
	trustModel, _ := args["trust_model"].(string)
	objectFormatName, _ := args["object_format_name"].(string)
	organization, _ := args["organization"].(string)

	opt := gitea_sdk.CreateRepoOption{
		Name:             name,
		Description:      description,
		Private:          private,
		IssueLabels:      issueLabels,
		AutoInit:         autoInit,
		Template:         template,
		Gitignores:       gitignores,
		License:          license,
		Readme:           readme,
		DefaultBranch:    defaultBranch,
		TrustModel:       gitea_sdk.TrustModel(trustModel),
		ObjectFormatName: objectFormatName,
	}

	var repo *gitea_sdk.Repository
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	if organization != "" {
		repo, _, err = client.Repositories.CreateOrgRepo(ctx, organization, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("create organization repository '%s' in '%s' err: %v", name, organization, err))
		}
	} else {
		repo, _, err = client.Repositories.CreateRepo(ctx, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("create repository '%s' err: %v", name, err))
		}
	}
	return to.TextResult(slim.Repo(repo))
}

func ForkRepoFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	user, err := params.GetString(args, "user")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.CreateForkOption{
		Organization: params.GetOptionalStringPtr(args, "organization"),
		Name:         params.GetOptionalStringPtr(args, "name"),
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.CreateFork(ctx, user, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("fork repository error: %v", err))
	}
	return to.TextResult("Fork success")
}

func ListMyReposFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListReposOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	repos, _, err := client.Repositories.ListMyRepos(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list my repositories error: %v", err))
	}

	return to.TextResult(slim.Repos(repos))
}

func ListOrgReposFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 100)
	opt := gitea_sdk.ListOrgReposOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	repos, _, err := client.Repositories.ListOrgRepos(ctx, org, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list organization '%s' repositories error: %v", org, err))
	}
	return to.TextResult(repos)
}
