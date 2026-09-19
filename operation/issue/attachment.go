package issue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const AttachmentReadToolName = "attachment_read"

var AttachmentReadTool = tool.NewDefinition(
	AttachmentReadToolName,
	"Read issue/comment attachments: list metadata, get metadata, or download content.",
	annotation.ReadOnly("Read issue or comment attachments"),
	tool.String("method", tool.Required(), tool.Enum("list", "get", "download")),
	tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
	tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
	tool.Number("issue_number", tool.Description("required for issue attachment list/get or issue-scoped metadata lookup")),
	tool.Number("comment_id", tool.Description("required for comment attachment list/get or comment-scoped metadata lookup")),
	tool.Number("attachment_id", tool.Description("required for get and for download when attachment_uuid is not provided")),
	tool.String("attachment_uuid", tool.Description("attachment UUID for direct download path lookup")),
	tool.String("output_path", tool.Description("write the attachment to this exact path")),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    AttachmentReadTool,
		Handler: attachmentReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list":     tool.RepoScoped("owner", "repo"),
			"get":      tool.RepoScoped("owner", "repo"),
			"download": tool.RepoScoped("owner", "repo"),
		}),
	})
}

func attachmentReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list":
		return listAttachmentsFn(ctx, args)
	case "get":
		return getAttachmentFn(ctx, args)
	case "download":
		return downloadAttachmentFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func listAttachmentsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, issueNumber, commentID, err := attachmentScopeArgs(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	var attachments []*gitea_sdk.Attachment
	if issueNumber > 0 {
		attachments, _, err = client.Issues.ListIssueAttachments(ctx, owner, repo, issueNumber)
	} else {
		attachments, _, err = client.Issues.ListIssueCommentAttachments(ctx, owner, repo, commentID)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list attachments err: %v", err))
	}
	return to.TextResult(slimAttachments(attachments))
}

func getAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	att, err := lookupAttachment(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	return to.TextResult(slimAttachment(att))
}

func downloadAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	explicitOutputPath := params.GetOptionalString(args, "output_path", "")
	attachmentUUID := strings.TrimSpace(params.GetOptionalString(args, "attachment_uuid", ""))

	var att *gitea_sdk.Attachment
	if attachmentUUID == "" {
		att, err = lookupAttachment(ctx, args)
		if err != nil {
			return to.ErrorResult(err)
		}
		attachmentUUID = strings.TrimSpace(att.UUID)
	}
	if attachmentUUID == "" {
		return to.ErrorResult(errors.New("attachment_uuid or attachment metadata with uuid is required"))
	}

	name := attachmentUUID
	if att != nil && strings.TrimSpace(att.Name) != "" {
		name = strings.TrimSpace(att.Name)
	}

	resp, err := gitea.OpenAttachment(ctx, "/attachments/"+attachmentUUID, "*/*")
	if err != nil {
		return to.ErrorResult(fmt.Errorf("download attachment err: %v", err))
	}
	defer resp.Body.Close()

	mimeType := normalizeAttachmentContentType(resp.ContentType, name)
	if explicitOutputPath == "" && shouldInlineAttachment(att, mimeType) {
		limited, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(flag.MaxInlineAttachmentBytes)+1))
		if readErr != nil {
			return to.ErrorResult(fmt.Errorf("read attachment err: %v", readErr))
		}
		if len(limited) <= flag.MaxInlineAttachmentBytes {
			text := fmt.Sprintf("attachment %s (%s, %d bytes, %s)", name, attachmentUUID, len(limited), mimeType)
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: text},
				&mcp.ImageContent{Data: limited, MIMEType: mimeType},
			}}, nil
		}
		outputPath := defaultAttachmentPath(owner, repo, name, attachmentUUID)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
			return to.ErrorResult(fmt.Errorf("create output dir err: %v", err))
		}
		reader := io.MultiReader(bytes.NewReader(limited), resp.Body)
		written, err := gitea.WriteAttachment(reader, outputPath)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("write attachment file err: %v", err))
		}
		return attachmentFileResult(att, outputPath, written, name, attachmentUUID, mimeType)
	}

	outputPath := explicitOutputPath
	if outputPath == "" {
		outputPath = defaultAttachmentPath(owner, repo, name, attachmentUUID)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return to.ErrorResult(fmt.Errorf("create output dir err: %v", err))
	}
	written, err := gitea.WriteAttachment(resp.Body, outputPath)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("write attachment file err: %v", err))
	}
	return attachmentFileResult(att, outputPath, written, name, attachmentUUID, mimeType)
}

func shouldInlineAttachment(att *gitea_sdk.Attachment, mimeType string) bool {
	if !strings.HasPrefix(mimeType, "image/") || flag.MaxInlineAttachmentBytes <= 0 {
		return false
	}
	if att == nil || att.Size <= 0 {
		return true
	}
	return att.Size <= int64(flag.MaxInlineAttachmentBytes)
}

func attachmentFileResult(att *gitea_sdk.Attachment, outputPath string, written int64, name, attachmentUUID, mimeType string) (*mcp.CallToolResult, error) {
	res := map[string]any{
		"path":         outputPath,
		"bytes":        written,
		"name":         name,
		"uuid":         attachmentUUID,
		"mime_type":    mimeType,
		"content_type": mimeType,
	}
	if att != nil {
		res["attachment_id"] = att.ID
	}
	return to.TextResult(res)
}

func attachmentScopeArgs(args map[string]any) (owner, repo string, issueNumber, commentID int64, err error) {
	owner, err = params.GetString(args, "owner")
	if err != nil {
		return "", "", 0, 0, err
	}
	repo, err = params.GetString(args, "repo")
	if err != nil {
		return "", "", 0, 0, err
	}
	issueNumber = params.GetOptionalInt(args, "issue_number", 0)
	commentID = params.GetOptionalInt(args, "comment_id", 0)
	if (issueNumber > 0) == (commentID > 0) {
		return "", "", 0, 0, errors.New("exactly one of issue_number or comment_id is required")
	}
	return owner, repo, issueNumber, commentID, nil
}

func lookupAttachment(ctx context.Context, args map[string]any) (*gitea_sdk.Attachment, error) {
	owner, repo, issueNumber, commentID, err := attachmentScopeArgs(args)
	if err != nil {
		return nil, err
	}
	attachmentID := params.GetOptionalInt(args, "attachment_id", 0)
	if attachmentID <= 0 {
		return nil, errors.New("attachment_id is required")
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get gitea client err: %v", err)
	}
	if issueNumber > 0 {
		att, _, err := client.Issues.GetIssueAttachment(ctx, owner, repo, issueNumber, attachmentID)
		if err != nil {
			return nil, fmt.Errorf("get issue attachment err: %v", err)
		}
		return att, nil
	}
	att, _, err := client.Issues.GetIssueCommentAttachment(ctx, owner, repo, commentID, attachmentID)
	if err != nil {
		return nil, fmt.Errorf("get issue comment attachment err: %v", err)
	}
	return att, nil
}

func slimAttachments(atts []*gitea_sdk.Attachment) []map[string]any {
	out := make([]map[string]any, 0, len(atts))
	for _, att := range atts {
		out = append(out, slimAttachment(att))
	}
	return out
}

func slimAttachment(att *gitea_sdk.Attachment) map[string]any {
	if att == nil {
		return nil
	}
	m := map[string]any{
		"id":             att.ID,
		"name":           att.Name,
		"uuid":           att.UUID,
		"size":           att.Size,
		"download_count": att.DownloadCount,
		"created_at":     att.Created,
		"mime_type":      inferAttachmentMimeType(att.Name),
	}
	if att.DownloadURL != "" {
		m["browser_download_url"] = att.DownloadURL
	}
	return m
}

func inferAttachmentMimeType(name string) string {
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name))); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return strings.Split(mimeType, ";")[0]
		}
	}
	return "application/octet-stream"
}

func normalizeAttachmentContentType(contentType, name string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType != "" && mediaType != "application/octet-stream" {
		return mediaType
	}
	return inferAttachmentMimeType(name)
}

func defaultAttachmentPath(owner, repo, name, uuid string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	filename := attachmentFilename(name, uuid)
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if uuid != "" {
		filename = uuid
		if base != "" && base != "attachment" {
			filename = base + "-" + uuid
		}
		filename += ext
	}
	return filepath.Join(home, ".gitea-mcp", "attachments", safePathPart(owner), safePathPart(repo), filename)
}

func attachmentFilename(name, uuid string) string {
	name = strings.TrimSpace(name)
	if name != "" && !strings.ContainsAny(name, `/\\`) && name != "." && name != ".." {
		return name
	}
	if uuid != "" {
		return uuid + ".bin"
	}
	return "attachment.bin"
}

func safePathPart(name string) string {
	name = strings.TrimSpace(name)
	if name != "" && !strings.ContainsAny(name, `/\\`) && name != "." && name != ".." {
		return name
	}
	return "unknown"
}
