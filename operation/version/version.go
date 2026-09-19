package version

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("version")

const (
	GetGiteaMCPServerVersion = "get_gitea_mcp_server_version"
)

var GetGiteaMCPServerVersionTool = tool.NewDefinition(
	GetGiteaMCPServerVersion,
	"Get the running version of the Gitea MCP Server itself (not the Gitea instance it connects to).",
	annotation.ReadOnly("Get server version"),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:      GetGiteaMCPServerVersionTool,
		Handler:   GetGiteaMCPServerVersionFn,
		ScopeKind: tool.Unscoped(),
	})
}

func GetGiteaMCPServerVersionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	version := flag.Version
	if version == "" {
		version = "dev"
	}
	return to.TextResult(fmt.Sprintf("Gitea MCP Server version: %v", version))
}
