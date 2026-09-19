package label

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

var Tool = tool.New("label")

const (
	LabelReadToolName  = "label_read"
	LabelWriteToolName = "label_write"
)

var (
	LabelReadTool = tool.NewDefinition(
		LabelReadToolName,
		"Read repo or org labels.",
		annotation.ReadOnly("Read labels"),
		tool.String("method", tool.Required(), tool.Enum("list_repo_labels", "get_repo_label", "list_org_labels")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.Number("id", tool.Description("label ID (for 'get_repo_label')")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	LabelWriteTool = tool.NewDefinition(
		LabelWriteToolName,
		"Write labels (repo or org): create, edit, delete.",
		annotation.Destructive("Create, update, or delete labels"),
		tool.String("method", tool.Required(), tool.Enum("create_repo_label", "edit_repo_label", "delete_repo_label", "create_org_label", "edit_org_label", "delete_org_label")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.Number("id", tool.Description("for edit/delete")),
		tool.String("name", tool.Description("required for create")),
		tool.String("color", tool.Description("hex (#RRGGBB); required for create")),
		tool.String("description"),
		tool.Boolean("exclusive", tool.Description("exclusive (org only)")),
		tool.Boolean("is_archived", tool.Description("archived (repo only)")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    LabelReadTool,
		Handler: labelReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list_repo_labels": tool.RepoScoped("owner", "repo"),
			"get_repo_label":   tool.RepoScoped("owner", "repo"),
			"list_org_labels":  tool.OwnerScoped("org"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    LabelWriteTool,
		Handler: labelWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"create_repo_label": tool.RepoScoped("owner", "repo"),
			"edit_repo_label":   tool.RepoScoped("owner", "repo"),
			"delete_repo_label": tool.RepoScoped("owner", "repo"),
			"create_org_label":  tool.OwnerScoped("org"),
			"edit_org_label":    tool.OwnerScoped("org"),
			"delete_org_label":  tool.OwnerScoped("org"),
		}),
	})
}

func labelReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list_repo_labels":
		return listRepoLabelsFn(ctx, args)
	case "get_repo_label":
		return getRepoLabelFn(ctx, args)
	case "list_org_labels":
		return listOrgLabelsFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func labelWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create_repo_label":
		return createRepoLabelFn(ctx, args)
	case "edit_repo_label":
		return editRepoLabelFn(ctx, args)
	case "delete_repo_label":
		return deleteRepoLabelFn(ctx, args)
	case "create_org_label":
		return createOrgLabelFn(ctx, args)
	case "edit_org_label":
		return editOrgLabelFn(ctx, args)
	case "delete_org_label":
		return deleteOrgLabelFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func listRepoLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListLabelsOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Repositories.ListRepoLabels(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list %v/%v/labels err: %v", owner, repo, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func getRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	label, _, err := client.Repositories.GetRepoLabel(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func createRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	color, err := params.GetString(args, "color")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string) // Optional

	isArchived, _ := args["is_archived"].(bool)

	opt := gitea_sdk.CreateLabelOption{
		Name:        name,
		Color:       color,
		Description: description,
		IsArchived:  isArchived,
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Repositories.CreateLabel(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/label err: %v", owner, repo, err))
	}
	return to.TextResult(slim.Label(label))
}

func editRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	opt := gitea_sdk.EditLabelOption{
		Name:        params.GetOptionalStringPtr(args, "name"),
		Color:       params.GetOptionalStringPtr(args, "color"),
		Description: params.GetPresentStringPtr(args, "description"),
		IsArchived:  params.GetOptionalBoolPtr(args, "is_archived"),
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Repositories.EditLabel(ctx, owner, repo, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func deleteRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	_, err = client.Repositories.DeleteLabel(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult("Label deleted successfully")
}

func listOrgLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListOrgLabelsOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Organizations.ListOrgLabels(ctx, org, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list %v/labels err: %v", org, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func createOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	color, err := params.GetString(args, "color")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)
	exclusive, _ := args["exclusive"].(bool)

	opt := gitea_sdk.CreateOrgLabelOption{
		Name:        name,
		Color:       color,
		Description: description,
		Exclusive:   exclusive,
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Organizations.CreateOrgLabel(ctx, org, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/labels err: %v", org, err))
	}
	return to.TextResult(slim.Label(label))
}

func editOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditOrgLabelOption{
		Name:        params.GetOptionalStringPtr(args, "name"),
		Color:       params.GetOptionalStringPtr(args, "color"),
		Description: params.GetPresentStringPtr(args, "description"),
		Exclusive:   params.GetOptionalBoolPtr(args, "exclusive"),
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Organizations.EditOrgLabel(ctx, org, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/labels/%v err: %v", org, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func deleteOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
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
	_, err = client.Organizations.DeleteOrgLabel(ctx, org, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/labels/%v err: %v", org, id, err))
	}
	return to.TextResult("Label deleted successfully")
}
