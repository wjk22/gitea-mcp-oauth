package notification

import (
	"context"
	"fmt"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("notification")

const (
	NotificationReadToolName  = "notification_read"
	NotificationWriteToolName = "notification_write"
)

var (
	NotificationReadTool = tool.NewDefinition(
		NotificationReadToolName,
		"Read notifications: list (optionally scoped to a repo) or get a thread by ID.",
		annotation.ReadOnly("Read notifications"),
		tool.String("method", tool.Required(), tool.Enum("list", "get")),
		tool.String("owner", tool.Description("scope 'list' to a repo")),
		tool.String("repo", tool.Description("scope 'list' to a repo")),
		tool.Number("id", tool.Description("thread ID (for 'get')")),
		tool.String("status", tool.Enum("unread", "read", "pinned")),
		tool.String("subject_type", tool.Enum("Issue", "Pull", "Commit", "Repository")),
		tool.String("since", tool.Description("updated after ISO 8601")),
		tool.String("before", tool.Description("updated before ISO 8601")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	NotificationWriteTool = tool.NewDefinition(
		NotificationWriteToolName,
		"Mark a notification or all notifications as read.",
		annotation.Write("Manage notifications"),
		tool.String("method", tool.Required(), tool.Enum("mark_read", "mark_all_read")),
		tool.Number("id", tool.Description("thread ID (for 'mark_read')")),
		tool.String("owner", tool.Description("scope 'mark_all_read' to a repo")),
		tool.String("repo", tool.Description("scope 'mark_all_read' to a repo")),
		tool.String("last_read_at", tool.Description("ISO 8601; defaults to now")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    NotificationReadTool,
		Handler: notificationReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list": tool.GlobalScoped(nil),
			"get":  tool.GlobalScoped(nil),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    NotificationWriteTool,
		Handler: notificationWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"mark_read":     tool.GlobalScoped(nil),
			"mark_all_read": tool.RepoScoped("owner", "repo"),
		}),
	})
}

func notificationReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list":
		return listNotificationsFn(ctx, args)
	case "get":
		return getNotificationFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func notificationWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "mark_read":
		return markNotificationReadFn(ctx, args)
	case "mark_all_read":
		return markAllNotificationsReadFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func listNotificationsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListNotificationOptions{
		Page:     page,
		PageSize: pageSize,
	}
	if status, ok := args["status"].(string); ok {
		opt.Status = []gitea_sdk.NotificationStatus{gitea_sdk.NotificationStatus(status)}
	}
	if subjectType, ok := args["subject_type"].(string); ok {
		opt.SubjectTypes = []gitea_sdk.NotificationSubjectType{gitea_sdk.NotificationSubjectType(subjectType)}
	}
	if t := params.GetOptionalTime(args, "since"); t != nil {
		opt.Since = *t
	}
	if t := params.GetOptionalTime(args, "before"); t != nil {
		opt.Before = *t
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	owner := params.GetOptionalString(args, "owner", "")
	repo := params.GetOptionalString(args, "repo", "")
	if owner != "" && repo != "" {
		threads, _, err := client.Notifications.ListByRepo(ctx, owner, repo, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("list %v/%v/notifications err: %v", owner, repo, err))
		}
		return to.TextResult(slimThreads(threads))
	}

	threads, _, err := client.Notifications.List(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list notifications err: %v", err))
	}
	return to.TextResult(slimThreads(threads))
}

func getNotificationFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	thread, _, err := client.Notifications.GetByID(ctx, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get notification/%v err: %v", id, err))
	}
	return to.TextResult(slimThread(thread))
}

func markNotificationReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	thread, _, err := client.Notifications.MarkReadByID(ctx, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("mark notification/%v read err: %v", id, err))
	}
	if thread != nil {
		return to.TextResult(slimThread(thread))
	}
	return to.TextResult("Notification marked as read")
}

func markAllNotificationsReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	lastReadAt := time.Now()
	if t := params.GetOptionalTime(args, "last_read_at"); t != nil {
		lastReadAt = *t
	}
	opt := gitea_sdk.MarkNotificationOptions{
		LastReadAt: lastReadAt,
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	owner := params.GetOptionalString(args, "owner", "")
	repo := params.GetOptionalString(args, "repo", "")
	if owner != "" && repo != "" {
		threads, _, err := client.Notifications.MarkReadByRepo(ctx, owner, repo, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("mark %v/%v/notifications read err: %v", owner, repo, err))
		}
		if threads != nil {
			return to.TextResult(slimThreads(threads))
		}
		return to.TextResult("All repository notifications marked as read")
	}

	threads, _, err := client.Notifications.MarkRead(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("mark all notifications read err: %v", err))
	}
	if threads != nil {
		return to.TextResult(slimThreads(threads))
	}
	return to.TextResult("All notifications marked as read")
}
