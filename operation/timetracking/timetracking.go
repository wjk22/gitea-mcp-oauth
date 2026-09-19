// Package timetracking provides MCP tools for Gitea time tracking operations
package timetracking

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("timetracking")

const (
	TimetrackingReadToolName  = "timetracking_read"
	TimetrackingWriteToolName = "timetracking_write"
)

var (
	TimetrackingReadTool = tool.NewDefinition(
		TimetrackingReadToolName,
		"Read time tracking: issue times, repo times, active stopwatches, your tracked times.",
		annotation.ReadOnly("Read tracked time"),
		tool.String("method", tool.Required(), tool.Enum("list_issue_times", "list_repo_times", "get_my_stopwatches", "get_my_times")),
		tool.String("owner", tool.Description("for list_* methods")),
		tool.String("repo", tool.Description("for list_* methods")),
		tool.Number("issue_number", tool.Description("for 'list_issue_times'")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	TimetrackingWriteTool = tool.NewDefinition(
		TimetrackingWriteToolName,
		"Write time tracking: stopwatches and entries.",
		annotation.Write("Add or manage tracked time"),
		tool.String("method", tool.Required(), tool.Enum("start_stopwatch", "stop_stopwatch", "delete_stopwatch", "add_time", "delete_time")),
		tool.String("owner", tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Description(params.RepoDesc)),
		tool.Number("issue_number"),
		tool.Number("time", tool.Description("seconds (for 'add_time')")),
		tool.Number("id", tool.Description("entry ID (for 'delete_time')")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    TimetrackingReadTool,
		Handler: readFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list_issue_times":   tool.RepoScoped("owner", "repo"),
			"list_repo_times":    tool.RepoScoped("owner", "repo"),
			"get_my_stopwatches": tool.GlobalScoped(nil),
			"get_my_times":       tool.GlobalScoped(nil),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    TimetrackingWriteTool,
		Handler: writeFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"start_stopwatch":  tool.RepoScoped("owner", "repo"),
			"stop_stopwatch":   tool.RepoScoped("owner", "repo"),
			"delete_stopwatch": tool.RepoScoped("owner", "repo"),
			"add_time":         tool.RepoScoped("owner", "repo"),
			"delete_time":      tool.RepoScoped("owner", "repo"),
		}),
	})
}

func readFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list_issue_times":
		return listTrackedTimesFn(ctx, args)
	case "list_repo_times":
		return listRepoTimesFn(ctx, args)
	case "get_my_stopwatches":
		return getMyStopwatchesFn(ctx, args)
	case "get_my_times":
		return getMyTimesFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func writeFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "start_stopwatch":
		return startStopwatchFn(ctx, args)
	case "stop_stopwatch":
		return stopStopwatchFn(ctx, args)
	case "delete_stopwatch":
		return deleteStopwatchFn(ctx, args)
	case "add_time":
		return addTrackedTimeFn(ctx, args)
	case "delete_time":
		return deleteTrackedTimeFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func startStopwatchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.StartIssueStopWatch(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("start stopwatch on %s/%s#%d err: %v", owner, repo, index, err))
	}
	return to.TextResult(fmt.Sprintf("Stopwatch started on issue %s/%s#%d", owner, repo, index))
}

func stopStopwatchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.StopIssueStopWatch(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("stop stopwatch on %s/%s#%d err: %v", owner, repo, index, err))
	}
	return to.TextResult(fmt.Sprintf("Stopwatch stopped on issue %s/%s#%d - time recorded", owner, repo, index))
}

func deleteStopwatchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.DeleteIssueStopwatch(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete stopwatch on %s/%s#%d err: %v", owner, repo, index, err))
	}
	return to.TextResult(fmt.Sprintf("Stopwatch deleted/cancelled on issue %s/%s#%d", owner, repo, index))
}

func getMyStopwatchesFn(ctx context.Context, _ map[string]any) (*mcp.CallToolResult, error) {
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	stopwatches, _, err := client.Issues.ListMyStopwatches(ctx, gitea_sdk.ListStopwatchesOptions{})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get stopwatches err: %v", err))
	}
	if len(stopwatches) == 0 {
		return to.TextResult("No active stopwatches")
	}
	return to.TextResult(slimStopWatches(stopwatches))
}

func listTrackedTimesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	times, _, err := client.Issues.ListIssueTrackedTimes(ctx, owner, repo, index, gitea_sdk.ListTrackedTimesOptions{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list tracked times for %s/%s#%d err: %v", owner, repo, index, err))
	}
	if len(times) == 0 {
		return to.TextResult(fmt.Sprintf("No tracked times for issue %s/%s#%d", owner, repo, index))
	}
	return to.TextResult(slimTrackedTimes(times))
}

func addTrackedTimeFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	timeSeconds, err := params.GetIndex(args, "time")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	trackedTime, _, err := client.Issues.AddTime(ctx, owner, repo, index, gitea_sdk.AddTimeOption{
		Time: timeSeconds,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("add tracked time to %s/%s#%d err: %v", owner, repo, index, err))
	}
	return to.TextResult(slimTrackedTime(trackedTime))
}

func deleteTrackedTimeFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}

	index, err := params.GetIndex(args, "issue_number")
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
	_, err = client.Issues.DeleteTime(ctx, owner, repo, index, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete tracked time %d from %s/%s#%d err: %v", id, owner, repo, index, err))
	}
	return to.TextResult(fmt.Sprintf("Tracked time entry %d deleted from issue %s/%s#%d", id, owner, repo, index))
}

func listRepoTimesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	times, _, err := client.Issues.ListRepoTrackedTimes(ctx, owner, repo, gitea_sdk.ListTrackedTimesOptions{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list repo tracked times for %s/%s err: %v", owner, repo, err))
	}
	if len(times) == 0 {
		return to.TextResult(fmt.Sprintf("No tracked times for repository %s/%s", owner, repo))
	}
	return to.TextResult(slimTrackedTimes(times))
}

func getMyTimesFn(ctx context.Context, _ map[string]any) (*mcp.CallToolResult, error) {
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	times, _, err := client.Issues.ListMyTrackedTimes(ctx, gitea_sdk.ListTrackedTimesOptions{})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get tracked times err: %v", err))
	}
	if len(times) == 0 {
		return to.TextResult("No tracked times found")
	}
	return to.TextResult(slimTrackedTimes(times))
}
