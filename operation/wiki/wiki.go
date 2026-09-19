package wiki

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("wiki")

const (
	WikiReadToolName  = "wiki_read"
	WikiWriteToolName = "wiki_write"
)

var (
	WikiReadTool = tool.NewDefinition(
		WikiReadToolName,
		"Read wiki: list pages, get content, revision history.",
		annotation.ReadOnly("Read wiki pages"),
		tool.String("method", tool.Required(), tool.Enum("list", "get", "get_revisions")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("pageName", tool.Description("for 'get'/'get_revisions'")),
	)

	WikiWriteTool = tool.NewDefinition(
		WikiWriteToolName,
		"Write wiki pages: create, update, delete.",
		annotation.Destructive("Create, update, or delete wiki pages"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "delete")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("pageName", tool.Description("for 'update'/'delete'")),
		tool.String("title", tool.Description("for 'create'")),
		tool.String("content", tool.Description("for 'create'/'update'")),
		tool.String("message", tool.Description("commit message")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    WikiReadTool,
		Handler: wikiReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list":          tool.RepoScoped("owner", "repo"),
			"get":           tool.RepoScoped("owner", "repo"),
			"get_revisions": tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    WikiWriteTool,
		Handler: wikiWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create": tool.RepoScoped("owner", "repo"),
			"update": tool.RepoScoped("owner", "repo"),
			"delete": tool.RepoScoped("owner", "repo"),
		}),
	})
}

func wikiReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list":
		return listWikiPagesFn(ctx, args)
	case "get":
		return getWikiPageFn(ctx, args)
	case "get_revisions":
		return getWikiRevisionsFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func wikiWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createWikiPageFn(ctx, args)
	case "update":
		return updateWikiPageFn(ctx, args)
	case "delete":
		return deleteWikiPageFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func listWikiPagesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/pages", url.PathEscape(owner), url.PathEscape(repo)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list wiki pages err: %v", err))
	}

	return to.TextResult(result)
}

func getWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "pageName")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func getWikiRevisionsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "pageName")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/revisions/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get wiki revisions err: %v", err))
	}

	return to.TextResult(result)
}

func createWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	content, err := params.GetString(args, "content")
	if err != nil {
		return to.ErrorResult(err)
	}

	message, _ := args["message"].(string)
	if message == "" {
		message = fmt.Sprintf("Create wiki page '%s'", title)
	}

	requestBody := map[string]string{
		"title":          title,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(content)),
		"message":        message,
	}

	var result any
	_, err = gitea.DoJSON(ctx, "POST", fmt.Sprintf("repos/%s/%s/wiki/new", url.PathEscape(owner), url.PathEscape(repo)), nil, requestBody, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func updateWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "pageName")
	if err != nil {
		return to.ErrorResult(err)
	}
	content, err := params.GetString(args, "content")
	if err != nil {
		return to.ErrorResult(err)
	}

	requestBody := map[string]string{
		"content_base64": base64.StdEncoding.EncodeToString([]byte(content)),
	}

	// If title is given, use it. Otherwise, keep current page name
	if title, ok := args["title"].(string); ok && title != "" {
		requestBody["title"] = title
	} else {
		requestBody["title"] = pageName
	}

	if message, ok := args["message"].(string); ok && message != "" {
		requestBody["message"] = message
	} else {
		requestBody["message"] = fmt.Sprintf("Update wiki page '%s'", pageName)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "PATCH", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, requestBody, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("update wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func deleteWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "pageName")
	if err != nil {
		return to.ErrorResult(err)
	}

	_, err = gitea.DoJSON(ctx, "DELETE", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, nil)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete wiki page err: %v", err))
	}

	return to.TextResult(map[string]string{"message": "Wiki page deleted successfully"})
}
