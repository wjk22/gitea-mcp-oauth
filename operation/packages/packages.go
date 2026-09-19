package packages

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("packages")

const (
	PackageReadToolName  = "package_read"
	PackageWriteToolName = "package_write"
)

var (
	PackageReadTool = tool.NewDefinition(
		PackageReadToolName,
		"Read package registry: list packages (one entry per version, filter via 'q'/'type'), list versions, or get a version.",
		annotation.ReadOnly("Read package registry"),
		tool.String("method", tool.Required(), tool.Enum("list", "list_versions", "get")),
		tool.String("owner", tool.Required(), tool.Description("user or org")),
		tool.String("type", tool.Description("container/npm/maven/pypi/cargo/generic; required except 'list'")),
		tool.String("name", tool.Description("slashes auto-encoded; required except 'list'")),
		tool.String("version", tool.Description("for 'get'")),
		tool.String("q", tool.Description("search query")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	PackageWriteTool = tool.NewDefinition(
		PackageWriteToolName,
		"Delete a package version (irreversible).",
		annotation.Destructive("Delete a package version"),
		tool.String("method", tool.Required(), tool.Enum("delete")),
		tool.String("owner", tool.Required(), tool.Description("user or org")),
		tool.String("type", tool.Required(), tool.Description("container/npm/maven/pypi/cargo/generic")),
		tool.String("name", tool.Required(), tool.Description("slashes auto-encoded")),
		tool.String("version", tool.Required()),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    PackageReadTool,
		Handler: packageReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list":          tool.OwnerScoped("owner"),
			"list_versions": tool.OwnerScoped("owner"),
			"get":           tool.OwnerScoped("owner"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    PackageWriteTool,
		Handler: packageWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"delete": tool.OwnerScoped("owner"),
		}),
	})
}

func packageReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list":
		return listPackagesFn(ctx, args)
	case "list_versions":
		return listPackageVersionsFn(ctx, args)
	case "get":
		return getPackageFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func packageWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "delete":
		return deletePackageVersionFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

// escapePackageName normalises a package name for use in URL paths. It
// accepts both raw names (my-repo/my-image) and pre-encoded names
// (my-repo%2Fmy-image), decoding first to avoid double-encoding. A literal
// '%' followed by two hex digits in a raw name will be folded into its
// decoded form, but package names typically do not contain '%'.
func escapePackageName(name string) string {
	if strings.Contains(name, "%") {
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
	}
	return url.PathEscape(name)
}

func listPackagesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}

	query := url.Values{}
	if typ, ok := args["type"].(string); ok && typ != "" {
		query.Set("type", typ)
	}
	if q, ok := args["q"].(string); ok && q != "" {
		query.Set("q", q)
	}
	page, pageSize := params.GetPagination(args, 30)
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))

	var result any
	_, err = gitea.DoJSON(ctx, "GET", "packages/"+url.PathEscape(owner), query, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list packages err: %v", err))
	}

	return to.TextResult(slimPackages(result))
}

func listPackageVersionsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	typ, err := params.GetString(args, "type")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}

	query := url.Values{}
	page, pageSize := params.GetPagination(args, 30)
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("packages/%s/%s/%s", url.PathEscape(owner), url.PathEscape(typ), escapePackageName(name)), query, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list package versions err: %v", err))
	}

	return to.TextResult(slimPackages(result))
}

func getPackageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	typ, err := params.GetString(args, "type")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	version, err := params.GetString(args, "version")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("packages/%s/%s/%s/%s", url.PathEscape(owner), url.PathEscape(typ), escapePackageName(name), url.PathEscape(version)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get package err: %v", err))
	}

	return to.TextResult(slimPackage(result))
}

func deletePackageVersionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	typ, err := params.GetString(args, "type")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	version, err := params.GetString(args, "version")
	if err != nil {
		return to.ErrorResult(err)
	}

	_, err = gitea.DoJSON(ctx, "DELETE", fmt.Sprintf("packages/%s/%s/%s/%s", url.PathEscape(owner), url.PathEscape(typ), escapePackageName(name), url.PathEscape(version)), nil, nil, nil)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete package version err: %v", err))
	}

	return to.TextResult("Package version deleted successfully")
}
