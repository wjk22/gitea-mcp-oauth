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

// ReleaseTool holds the release-related tools (scope "release").
var ReleaseTool = tool.New("release")

const (
	CreateReleaseToolName    = "create_release"
	DeleteReleaseToolName    = "delete_release"
	GetReleaseToolName       = "get_release"
	GetLatestReleaseToolName = "get_latest_release"
	ListReleasesToolName     = "list_releases"
)

var (
	CreateReleaseTool = tool.NewDefinition(
		CreateReleaseToolName,
		"Create a new release in a repository from a tag, optionally marking it as a draft or pre-release.",
		annotation.Write("Create a release"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("tag_name", tool.Required()),
		tool.String("target", tool.Required(), tool.Description("commitish")),
		tool.String("title", tool.Required()),
		tool.Boolean("is_draft"),
		tool.Boolean("is_pre_release"),
		tool.String("body"),
	)

	DeleteReleaseTool = tool.NewDefinition(
		DeleteReleaseToolName,
		"Delete a release from a repository by its numeric ID. This action is destructive and cannot be undone.",
		annotation.Destructive("Delete a release"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("id", tool.Required()),
	)

	GetReleaseTool = tool.NewDefinition(
		GetReleaseToolName,
		"Get a release by ID",
		annotation.ReadOnly("Get release details"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("id", tool.Required()),
	)

	GetLatestReleaseTool = tool.NewDefinition(
		GetLatestReleaseToolName,
		"Get the most recent published (non-draft) release in a repository.",
		annotation.ReadOnly("Get latest release"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
	)

	ListReleasesTool = tool.NewDefinition(
		ListReleasesToolName,
		"List releases in a repository, optionally filtered to drafts or pre-releases.",
		annotation.ReadOnly("List releases"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Boolean("is_draft"),
		tool.Boolean("is_pre_release"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(20), tool.Minimum(1)),
	)
)

func init() {
	ReleaseTool.RegisterWrite(tool.ServerTool{
		Tool:      CreateReleaseTool,
		Handler:   CreateReleaseFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	ReleaseTool.RegisterWrite(tool.ServerTool{
		Tool:      DeleteReleaseTool,
		Handler:   DeleteReleaseFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	ReleaseTool.RegisterRead(tool.ServerTool{
		Tool:      GetReleaseTool,
		Handler:   GetReleaseFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	ReleaseTool.RegisterRead(tool.ServerTool{
		Tool:      GetLatestReleaseTool,
		Handler:   GetLatestReleaseFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	ReleaseTool.RegisterRead(tool.ServerTool{
		Tool:      ListReleasesTool,
		Handler:   ListReleasesFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func CreateReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	tagName, err := params.GetString(args, "tag_name")
	if err != nil {
		return to.ErrorResult(err)
	}
	target, err := params.GetString(args, "target")
	if err != nil {
		return to.ErrorResult(err)
	}
	title, err := params.GetString(args, "title")
	if err != nil {
		return to.ErrorResult(err)
	}
	isDraft, _ := args["is_draft"].(bool)
	isPreRelease, _ := args["is_pre_release"].(bool)
	body, _ := args["body"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Releases.CreateRelease(ctx, owner, repo, gitea_sdk.CreateReleaseOption{
		TagName:      tagName,
		Target:       target,
		Title:        title,
		Note:         body,
		IsDraft:      isDraft,
		IsPrerelease: isPreRelease,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create release error: %v", err))
	}

	return to.TextResult("Release Created")
}

func DeleteReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	_, err = client.Releases.DeleteRelease(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete release error: %v", err))
	}

	return to.TextResult("Release deleted successfully")
}

func GetReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	release, _, err := client.Releases.GetRelease(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get release error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func GetLatestReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	release, _, err := client.Releases.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get latest release error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func ListReleasesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 20)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	releases, _, err := client.Releases.ListReleases(ctx, owner, repo, gitea_sdk.ListReleasesOptions{
		Page:         page,
		PageSize:     pageSize,
		IsDraft:      params.GetOptionalBoolPtr(args, "is_draft"),
		IsPreRelease: params.GetOptionalBoolPtr(args, "is_pre_release"),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list releases error: %v", err))
	}

	return to.TextResult(slimReleases(releases))
}
