package repo

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

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
	)

	GetDirContentTool = tool.NewDefinition(
		GetDirToolName,
		"List the entries (files and subdirectories) in a repository directory at a given ref (branch, tag, or commit SHA).",
		annotation.ReadOnly("Get directory contents"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("ref", tool.Required(), tool.Description("branch, tag, or commit SHA")),
		tool.String("path", tool.Required()),
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

type ContentLine struct {
	LineNumber int    `json:"line"`
	Content    string `json:"content"`
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
	withLines, _ := args["withLines"].(bool)
	if withLines {
		rawContent, err := base64.StdEncoding.DecodeString(*content.Content)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("decode base64 content err: %v", err))
		}

		contentLines := make([]ContentLine, 0)
		line := 0

		scanner := bufio.NewScanner(bytes.NewReader(rawContent))

		for scanner.Scan() {
			line++

			contentLines = append(contentLines, ContentLine{
				LineNumber: line,
				Content:    scanner.Text(),
			})
		}
		if err := scanner.Err(); err != nil {
			return to.ErrorResult(fmt.Errorf("scan content err: %v", err))
		}

		// remove the last blank line if exists
		// git does not consider the last line as a new line
		if len(contentLines) > 0 && contentLines[len(contentLines)-1].Content == "" {
			contentLines = contentLines[:len(contentLines)-1]
		}

		contentBytes, err := json.MarshalIndent(contentLines, "", "  ")
		if err != nil {
			return to.ErrorResult(fmt.Errorf("marshal content lines err: %v", err))
		}
		contentStr := string(contentBytes)
		content.Content = &contentStr
	}
	return to.TextResult(slimContents(content))
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
	filePath, err := params.GetString(args, "path")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	content, _, err := client.Repositories.ListContents(ctx, owner, repo, ref, filePath)
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
