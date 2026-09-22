package repo

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FileTool holds the file-related tools (scope "file").
var FileTool = tool.New("file")

const (
	GetFileToolName            = "get_file_contents"
	GetDirToolName             = "get_dir_contents"
	CreateOrUpdateFileToolName = "create_or_update_file"
	DeleteFileToolName         = "delete_file"
)

var (
	GetFileContentTool = tool.NewDefinition(
		GetFileToolName,
		"Get file content and metadata",
		annotation.ReadOnly("Get file content"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("ref", tool.Required(), tool.Description("branch, tag, or commit SHA")),
		tool.String("path", tool.Required()),
		tool.Boolean("withLines", tool.Description("return numbered lines")),
		tool.Number("start_line", tool.Description("1-based start line whole number (inclusive, default 1)")),
		tool.Number("end_line", tool.Description("1-based end line whole number (inclusive, default last line). Clamped if beyond file")),
		tool.Number("max_bytes", tool.Description("maximum content bytes to return as a whole number (default 32768, maximum 262144, larger clamped). Truncates after last complete line that fits with truncated: true and next_start_line; cuts at UTF-8 boundary if first line exceeds")),
	)

	GetDirContentTool = tool.NewDefinition(
		GetDirToolName,
		"List the entries (files and subdirectories) in a repository directory at a given ref (branch, tag, or commit SHA).",
		annotation.ReadOnly("Get directory contents"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("ref", tool.Required(), tool.Description("branch, tag, or commit SHA")),
		tool.String("path", tool.Description("directory path; omit for the repository root")),
	)

	CreateOrUpdateFileTool = tool.NewDefinition(
		CreateOrUpdateFileToolName,
		"Create or update a file (provide sha to update an existing file).",
		annotation.Write("Create or update a file"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("path", tool.Required()),
		tool.String("content", tool.Required()),
		tool.String("message", tool.Required(), tool.Description("commit message")),
		tool.String("branch_name", tool.Required()),
		tool.String("sha", tool.Description("existing file SHA (omit to create)")),
		tool.String("new_branch_name", tool.Description("branch to create from branch_name and commit to")),
	)

	DeleteFileTool = tool.NewDefinition(
		DeleteFileToolName,
		"Delete a file from a repository by committing the removal to a branch. Requires the file's current SHA and a commit message.",
		annotation.Destructive("Delete a file"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("path", tool.Required()),
		tool.String("message", tool.Required(), tool.Description("commit message")),
		tool.String("branch_name", tool.Required()),
		tool.String("sha", tool.Required()),
	)
)

func init() {
	FileTool.RegisterRead(tool.ServerTool{
		Tool:      GetFileContentTool,
		Handler:   GetFileContentFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	FileTool.RegisterRead(tool.ServerTool{
		Tool:      GetDirContentTool,
		Handler:   GetDirContentFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	FileTool.RegisterWrite(tool.ServerTool{
		Tool:      CreateOrUpdateFileTool,
		Handler:   CreateOrUpdateFileFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
	FileTool.RegisterWrite(tool.ServerTool{
		Tool:      DeleteFileTool,
		Handler:   DeleteFileFn,
		ScopeKind: tool.RepoScoped("owner", "repo"),
	})
}

func GetFileContentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	ref, _ := args["ref"].(string)
	filePath, err := params.GetString(args, "path")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	content, _, err := client.Repositories.GetContents(ctx, owner, repo, ref, filePath)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get file err: %v", err))
	}
	if content == nil {
		return to.ErrorResult(errors.New("file not found"))
	}
	if content.Content == nil {
		// FR-6: nil-content response yields metadata only
		return to.TextResult(map[string]any{
			"name": content.Name,
			"path": content.Path,
			"sha":  content.SHA,
			"type": content.Type,
			"size": content.Size,
		})
	}

	withLines, _ := args["withLines"].(bool)
	opts := ShapeOptions{
		WithLines: withLines,
	}

	startLine, err := getOptionalFileArg(args, "start_line")
	if err != nil {
		return to.ErrorResult(err)
	}
	opts.StartLine = startLine

	endLine, err := getOptionalFileArg(args, "end_line")
	if err != nil {
		return to.ErrorResult(err)
	}
	opts.EndLine = endLine

	maxBytes, err := getOptionalFileArg(args, "max_bytes")
	if err != nil {
		return to.ErrorResult(err)
	}
	opts.MaxBytes = maxBytes

	shaped, err := ShapeFileContent([]byte(*content.Content), opts)
	if err != nil {
		return to.ErrorResult(err)
	}

	return to.TextResult(FormatFileContentResult(content.Name, content.Path, content.SHA, content.Type, content.Size, shaped))
}

// getOptionalFileArg reads one optional integer argument from args for GetFileContentFn.
// Absent key or JSON null → nil (FR-7: treated as not given).
// float64 with no fractional part, or numeric string → *int.
// float64 with fractional part, NaN, Inf, magnitude at or above 2^63, or any other type → error naming the parameter.
func getOptionalFileArg(args map[string]any, key string) (*int, error) {
	val, exists := args[key]
	if !exists || val == nil { // absent or explicit null
		return nil, nil //nolint:nilnil // nil pointer = "not given", nil error = no error: intentional contract
	}
	switch v := val.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return nil, fmt.Errorf("%s must be a whole number (got %v)", key, v)
		}
		const limit = float64(1 << (strconv.IntSize - 1)) // exact power of two on 32 and 64 bit; float64(math.MaxInt) is not
		if v < -limit || v >= limit {
			return nil, fmt.Errorf("%s value out of range (got %v)", key, v)
		}
		i := int(v)
		return &i, nil
	case string:
		i, err := strconv.ParseInt(v, 10, strconv.IntSize)
		if errors.Is(err, strconv.ErrRange) {
			return nil, fmt.Errorf("%s value out of range (got %q)", key, v)
		}
		if err != nil {
			return nil, fmt.Errorf("%s must be a whole number (got %q)", key, v)
		}
		intVal := int(i)
		return &intVal, nil
	default:
		return nil, fmt.Errorf("%s must be a whole number, got %T", key, val)
	}
}

func GetDirContentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	ref, _ := args["ref"].(string)
	dirPath := params.GetOptionalString(args, "path", "")
	if dirPath == "/" || dirPath == "." {
		dirPath = "" // Gitea rejects "contents/." with 400; "" lists the root
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	content, _, err := client.Repositories.ListContents(ctx, owner, repo, ref, dirPath)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get dir content err: %v", err))
	}
	return to.TextResult(slimDirEntries(content))
}

func CreateOrUpdateFileFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	filePath, err := params.GetString(args, "path")
	if err != nil {
		return to.ErrorResult(err)
	}
	content, _ := args["content"].(string)
	message, _ := args["message"].(string)
	branchName, _ := args["branch_name"].(string)
	newBranchName, _ := args["new_branch_name"].(string)
	sha, _ := args["sha"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	fileOpt := gitea_sdk.FileOptions{
		Message:       message,
		BranchName:    branchName,
		NewBranchName: newBranchName,
	}
	targetBranch := cmp.Or(newBranchName, branchName)

	if sha != "" {
		// Update existing file
		opt := gitea_sdk.UpdateFileOptions{
			SHA:         sha,
			Content:     base64.StdEncoding.EncodeToString([]byte(content)),
			FileOptions: fileOpt,
		}
		_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, filePath, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("update file err: %v", err))
		}
		return to.TextResult("Update file success on branch " + targetBranch)
	}

	// Create new file
	opt := gitea_sdk.CreateFileOptions{
		Content:     base64.StdEncoding.EncodeToString([]byte(content)),
		FileOptions: fileOpt,
	}
	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, filePath, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create file err: %v", err))
	}
	return to.TextResult("Create file success on branch " + targetBranch)
}

func DeleteFileFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	filePath, err := params.GetString(args, "path")
	if err != nil {
		return to.ErrorResult(err)
	}
	message, _ := args["message"].(string)
	branchName, _ := args["branch_name"].(string)
	sha, err := params.GetString(args, "sha")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.DeleteFileOptions{
		Message:    message,
		BranchName: branchName,
		SHA:        sha,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteFile(ctx, owner, repo, filePath, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete file err: %v", err))
	}
	return to.TextResult("Delete file success")
}
