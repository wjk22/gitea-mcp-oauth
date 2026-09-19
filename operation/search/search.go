package search

import (
	"context"
	"fmt"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/slim"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("search")

const (
	SearchUsersToolName    = "search_users"
	SearchOrgTeamsToolName = "search_org_teams"
	SearchReposToolName    = "search_repos"
	SearchIssuesToolName   = "search_issues"
)

var (
	SearchUsersTool = tool.NewDefinition(
		SearchUsersToolName,
		"Search for Gitea users by username or full name.",
		annotation.ReadOnly("Search users"),
		tool.String("query", tool.Required()),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	SearOrgTeamsTool = tool.NewDefinition(
		SearchOrgTeamsToolName,
		"Search for teams within an organization by name, optionally including each team's description in the results.",
		annotation.ReadOnly("Search organization teams"),
		tool.String("org", tool.Required()),
		tool.String("query", tool.Required()),
		tool.Boolean("includeDescription"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	SearchReposTool = tool.NewDefinition(
		SearchReposToolName,
		"Search for repositories by keyword, with filters for topic/description matching, owner, visibility, archived status, and sort order.",
		annotation.ReadOnly("Search repositories"),
		tool.String("query", tool.Required()),
		tool.Boolean("keywordIsTopic"),
		tool.Boolean("keywordInDescription"),
		tool.Number("ownerID"),
		tool.Boolean("isPrivate"),
		tool.Boolean("isArchived"),
		tool.String("sort"),
		tool.String("order"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	SearchIssuesTool = tool.NewDefinition(
		SearchIssuesToolName,
		"Search issues and PRs across repositories",
		annotation.ReadOnly("Search issues"),
		tool.String("query", tool.Required()),
		tool.String("state", tool.Enum("open", "closed", "all")),
		tool.String("type", tool.Enum("issues", "pulls")),
		tool.String("labels", tool.Description("comma-separated")),
		tool.String("owner", tool.Description("filter by owner")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:      SearchUsersTool,
		Handler:   UsersFn,
		ScopeKind: tool.GlobalScoped(nil),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:      SearOrgTeamsTool,
		Handler:   OrgTeamsFn,
		ScopeKind: tool.OwnerScoped("org"),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:      SearchReposTool,
		Handler:   ReposFn,
		ScopeKind: tool.GlobalScoped(nil),
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:      SearchIssuesTool,
		Handler:   IssuesFn,
		ScopeKind: tool.GlobalScoped(nil),
	})
}

func UsersFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	keyword, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchUsersOption{
		KeyWord:  keyword,
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	users, _, err := client.Users.SearchUsers(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search users err: %v", err))
	}
	return to.TextResult(slimUserDetails(users))
}

func OrgTeamsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	query, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	includeDescription, _ := args["includeDescription"].(bool)
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchTeamsOptions{
		Query:              query,
		IncludeDescription: includeDescription,
		Page:               page,
		PageSize:           pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	teams, _, err := client.Organizations.SearchOrgTeams(ctx, org, &opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search organization teams error: %v", err))
	}
	return to.TextResult(slimTeams(teams))
}

func ReposFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	keyword, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	keywordIsTopic, _ := args["keywordIsTopic"].(bool)
	keywordInDescription, _ := args["keywordInDescription"].(bool)
	sort, _ := args["sort"].(string)
	order, _ := args["order"].(string)
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchRepoOptions{
		Keyword:              keyword,
		KeywordIsTopic:       keywordIsTopic,
		KeywordInDescription: keywordInDescription,
		OwnerID:              params.GetOptionalInt(args, "ownerID", 0),
		IsPrivate:            params.GetOptionalBoolPtr(args, "isPrivate"),
		IsArchived:           params.GetOptionalBoolPtr(args, "isArchived"),
		Sort:                 sort,
		Order:                order,
		Page:                 page,
		PageSize:             pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	repos, _, err := client.Repositories.SearchRepos(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search repos error: %v", err))
	}
	return to.TextResult(slim.Repos(repos))
}

func IssuesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListIssueOption{
		KeyWord:  query,
		Page:     page,
		PageSize: pageSize,
	}
	if state, ok := args["state"].(string); ok {
		opt.State = gitea_sdk.StateType(state)
	}
	if issueType, ok := args["type"].(string); ok {
		opt.Type = gitea_sdk.IssueType(issueType)
	}
	if labels, ok := args["labels"].(string); ok && labels != "" {
		opt.Labels = strings.Split(labels, ",")
	}
	if owner, ok := args["owner"].(string); ok {
		opt.Owner = owner
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issues, _, err := client.Issues.ListIssues(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search issues err: %v", err))
	}
	return to.TextResult(slimIssues(issues))
}
