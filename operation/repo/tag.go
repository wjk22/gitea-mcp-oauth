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

// TagTool holds the tag-related tools (scope "tag").
var TagTool = tool.New("tag")

const (
	CreateTagToolName = "create_tag"
	DeleteTagToolName = "delete_tag"
	GetTagToolName    = "get_tag"
	ListTagsToolName  = "list_tags"
)

var (
	CreateTagTool = tool.NewDefinition(
		CreateTagToolName,
		"Create a new Git tag in a repository at a target commit, branch, or existing tag, with an optional annotation message.",
		annotation.Write("Create a tag"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("tag_name", tool.Required()),
		tool.String("target", tool.Description("commitish")),
		tool.String("message", tool.Description("tag message")),
	)

	DeleteTagTool = tool.NewDefinition(
		DeleteTagToolName,
		"Permanently delete a tag from a repository. This action is destructive and cannot be undone.",
		annotation.Destructive("Delete a tag"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("tag_name", tool.Required()),
	)

	GetTagTool = tool.NewDefinition(
		GetTagToolName,
		"Get details for a single tag in a repository by name.",
		annotation.ReadOnly("Get tag details"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("tag_name", tool.Required()),
	)

	ListTagsTool = tool.NewDefinition(
		ListTagsToolName,
		"List all tags in a repository, paginated.",
		annotation.ReadOnly("List tags"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(20), tool.Minimum(1)),
	)
)

func init() {
	TagTool.RegisterWrite(tool.ServerTool{
		Tool:      CreateTagTool,
		Handler:   CreateTagFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	TagTool.RegisterWrite(tool.ServerTool{
		Tool:      DeleteTagTool,
		Handler:   DeleteTagFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	TagTool.RegisterRead(tool.ServerTool{
		Tool:      GetTagTool,
		Handler:   GetTagFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	TagTool.RegisterRead(tool.ServerTool{
		Tool:      ListTagsTool,
		Handler:   ListTagsFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func CreateTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	target, _ := args["target"].(string)
	message, _ := args["message"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.CreateTag(ctx, owner, repo, gitea_sdk.CreateTagOption{
		TagName: tagName,
		Target:  target,
		Message: message,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create tag error: %v", err))
	}

	return to.TextResult("Tag Created")
}

func DeleteTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteTag(ctx, owner, repo, tagName)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete tag error: %v", err))
	}

	return to.TextResult("Tag deleted")
}

func GetTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	tag, _, err := client.Repositories.GetTag(ctx, owner, repo, tagName)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get tag error: %v", err))
	}

	return to.TextResult(slimTag(tag))
}

func ListTagsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page := params.GetOptionalInt(args, "page", 1)
	pageSize := params.GetOptionalInt(args, "per_page", 20)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	tags, _, err := client.Repositories.ListRepoTags(ctx, owner, repo, gitea_sdk.ListRepoTagsOptions{
		Page:     int(page),
		PageSize: int(pageSize),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list tags error: %v", err))
	}

	return to.TextResult(slimTags(tags))
}
