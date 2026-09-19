package actions

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ActionsConfigReadToolName  = "actions_config_read"
	ActionsConfigWriteToolName = "actions_config_write"
)

type secretMeta struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
}

func toSecretMetas(secrets []*gitea_sdk.Secret) []secretMeta {
	metas := make([]secretMeta, 0, len(secrets))
	for _, s := range secrets {
		if s == nil {
			continue
		}
		metas = append(metas, secretMeta{
			Name:        s.Name,
			Description: s.Description,
			CreatedAt:   s.Created,
		})
	}
	return metas
}

var (
	ActionsConfigReadTool = tool.NewDefinition(
		ActionsConfigReadToolName,
		"Read Actions secrets and variables.",
		annotation.ReadOnly("Read Actions secrets and variables"),
		tool.String("method", tool.Required(), tool.Enum("list_repo_secrets", "list_org_secrets", "list_repo_variables", "get_repo_variable", "list_org_variables", "get_org_variable")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.String("name", tool.Description("for get methods")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	ActionsConfigWriteTool = tool.NewDefinition(
		ActionsConfigWriteToolName,
		"Write Actions secrets and variables: upsert, create, update, delete.",
		annotation.Destructive("Manage Actions secrets and variables"),
		tool.String("method", tool.Required(), tool.Enum("upsert_repo_secret", "delete_repo_secret", "upsert_org_secret", "delete_org_secret", "create_repo_variable", "update_repo_variable", "delete_repo_variable", "create_org_variable", "update_org_variable", "delete_org_variable")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.String("name", tool.Description("secret or variable name")),
		tool.String("data", tool.Description("secret value (upsert)")),
		tool.String("value", tool.Description("variable value")),
		tool.String("description"),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    ActionsConfigReadTool,
		Handler: configReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list_repo_secrets":   tool.RepoScoped("owner", "repo"),
			"list_org_secrets":    tool.OwnerScoped("org"),
			"list_repo_variables": tool.RepoScoped("owner", "repo"),
			"get_repo_variable":   tool.RepoScoped("owner", "repo"),
			"list_org_variables":  tool.OwnerScoped("org"),
			"get_org_variable":    tool.OwnerScoped("org"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    ActionsConfigWriteTool,
		Handler: configWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"upsert_repo_secret":   tool.RepoScoped("owner", "repo"),
			"delete_repo_secret":   tool.RepoScoped("owner", "repo"),
			"upsert_org_secret":    tool.OwnerScoped("org"),
			"delete_org_secret":    tool.OwnerScoped("org"),
			"create_repo_variable": tool.RepoScoped("owner", "repo"),
			"update_repo_variable": tool.RepoScoped("owner", "repo"),
			"delete_repo_variable": tool.RepoScoped("owner", "repo"),
			"create_org_variable":  tool.OwnerScoped("org"),
			"update_org_variable":  tool.OwnerScoped("org"),
			"delete_org_variable":  tool.OwnerScoped("org"),
		}),
	})
}

func configReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list_repo_secrets":
		return listRepoActionSecretsFn(ctx, args)
	case "list_org_secrets":
		return listOrgActionSecretsFn(ctx, args)
	case "list_repo_variables":
		return listRepoActionVariablesFn(ctx, args)
	case "get_repo_variable":
		return getRepoActionVariableFn(ctx, args)
	case "list_org_variables":
		return listOrgActionVariablesFn(ctx, args)
	case "get_org_variable":
		return getOrgActionVariableFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func configWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "upsert_repo_secret":
		return upsertRepoActionSecretFn(ctx, args)
	case "delete_repo_secret":
		return deleteRepoActionSecretFn(ctx, args)
	case "upsert_org_secret":
		return upsertOrgActionSecretFn(ctx, args)
	case "delete_org_secret":
		return deleteOrgActionSecretFn(ctx, args)
	case "create_repo_variable":
		return createRepoActionVariableFn(ctx, args)
	case "update_repo_variable":
		return updateRepoActionVariableFn(ctx, args)
	case "delete_repo_variable":
		return deleteRepoActionVariableFn(ctx, args)
	case "create_org_variable":
		return createOrgActionVariableFn(ctx, args)
	case "update_org_variable":
		return updateOrgActionVariableFn(ctx, args)
	case "delete_org_variable":
		return deleteOrgActionVariableFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func listRepoActionSecretsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	secrets, _, err := client.Actions.ListRepoSecrets(ctx, owner, repo, gitea_sdk.ListRepoActionsSecretOption{
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list repo action secrets err: %v", err))
	}

	return to.TextResult(toSecretMetas(secrets))
}

func upsertRepoActionSecretFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	data, err := params.GetString(args, "data")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.CreateRepoSecret(ctx, owner, repo, name, gitea_sdk.CreateOrUpdateSecretOption{
		Data:        data,
		Description: description,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("upsert repo action secret err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "secret upserted", "status": resp.StatusCode})
}

func deleteRepoActionSecretFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.DeleteRepoSecret(ctx, owner, repo, name)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete repo action secret err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "secret deleted", "status": resp.StatusCode})
}

func listOrgActionSecretsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	secrets, _, err := client.Actions.ListOrgSecrets(ctx, org, gitea_sdk.ListOrgActionsSecretOption{
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list org action secrets err: %v", err))
	}

	return to.TextResult(toSecretMetas(secrets))
}

func upsertOrgActionSecretFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	data, err := params.GetString(args, "data")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.CreateOrgSecret(ctx, org, name, gitea_sdk.CreateOrUpdateSecretOption{
		Data:        data,
		Description: description,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("upsert org action secret err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "secret upserted", "status": resp.StatusCode})
}

func deleteOrgActionSecretFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}

	escapedOrg := url.PathEscape(org)
	escapedSecret := url.PathEscape(name)
	_, err = gitea.DoJSON(ctx, "DELETE", fmt.Sprintf("orgs/%s/actions/secrets/%s", escapedOrg, escapedSecret), nil, nil, nil)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete org action secret err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "secret deleted"})
}

func listRepoActionVariablesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/actions/variables", url.PathEscape(owner), url.PathEscape(repo)), query, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list repo action variables err: %v", err))
	}
	return to.TextResult(result)
}

func getRepoActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	variable, _, err := client.Actions.GetRepoVariable(ctx, owner, repo, name)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get repo action variable err: %v", err))
	}
	return to.TextResult(variable)
}

func createRepoActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	value, err := params.GetString(args, "value")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.CreateRepoVariable(ctx, owner, repo, name, value)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create repo action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable created", "status": resp.StatusCode})
}

func updateRepoActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	value, err := params.GetString(args, "value")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.UpdateRepoVariable(ctx, owner, repo, name, value)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("update repo action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable updated", "status": resp.StatusCode})
}

func deleteRepoActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.DeleteRepoVariable(ctx, owner, repo, name)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete repo action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable deleted", "status": resp.StatusCode})
}

func listOrgActionVariablesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	variables, _, err := client.Actions.ListOrgVariables(ctx, org, gitea_sdk.ListOrgActionsVariableOption{
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list org action variables err: %v", err))
	}
	return to.TextResult(variables)
}

func getOrgActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	variable, _, err := client.Actions.GetOrgVariable(ctx, org, name)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get org action variable err: %v", err))
	}
	return to.TextResult(variable)
}

func createOrgActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	value, err := params.GetString(args, "value")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.CreateOrgVariable(ctx, org, name, gitea_sdk.CreateActionsVariableOption{
		Value:       value,
		Description: description,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create org action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable created", "status": resp.StatusCode})
}

func updateOrgActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	value, err := params.GetString(args, "value")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	resp, err := client.Actions.UpdateOrgVariable(ctx, org, name, gitea_sdk.UpdateActionsVariableOption{
		Name:        name,
		Value:       value,
		Description: description,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("update org action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable updated", "status": resp.StatusCode})
}

func deleteOrgActionVariableFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}

	_, err = gitea.DoJSON(ctx, "DELETE", fmt.Sprintf("orgs/%s/actions/variables/%s", url.PathEscape(org), url.PathEscape(name)), nil, nil, nil)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete org action variable err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "variable deleted"})
}
