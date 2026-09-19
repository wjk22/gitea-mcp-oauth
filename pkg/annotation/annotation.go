package annotation

import "github.com/modelcontextprotocol/go-sdk/mcp"

func ReadOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true}
}

func Write(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title}
}

func Destructive(title string) *mcp.ToolAnnotations {
	t := true
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &t}
}
