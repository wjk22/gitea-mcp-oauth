package to

import (
	"encoding/json"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TextResult(v any) (*mcp.CallToolResult, error) {
	resultBytes, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result err: %v", err)
	}
	if flag.Debug {
		log.Debugf("Text Result: %s", string(resultBytes))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(resultBytes)}},
	}, nil
}

func ErrorResult(err error) (*mcp.CallToolResult, error) {
	log.Errorf("%s", err.Error())
	var result mcp.CallToolResult
	result.SetError(err)
	return &result, nil
}
