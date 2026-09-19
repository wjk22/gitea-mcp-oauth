package actions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ActionsRunReadToolName  = "actions_run_read"
	ActionsRunWriteToolName = "actions_run_write"
)

var (
	ActionsRunReadTool = tool.NewDefinition(
		ActionsRunReadToolName,
		"Read Actions workflows, runs, jobs, logs, and artifacts.",
		annotation.ReadOnly("Read Actions workflow, run, job, and artifact data"),
		tool.String("method", tool.Required(), tool.Enum("list_workflows", "get_workflow", "list_runs", "get_run", "list_jobs", "list_run_jobs", "get_job", "get_job_log_preview", "download_job_log", "list_artifacts", "list_run_artifacts", "get_artifact", "download_artifact")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("workflow_id", tool.Description("ID or filename (for 'get_workflow')")),
		tool.Number("run_id", tool.Description("for 'get_run'/'list_run_jobs'/'list_run_artifacts'")),
		tool.Number("job_id", tool.Description("for 'get_job'/log methods")),
		tool.Number("artifact_id", tool.Description("for 'get_artifact'/'download_artifact'")),
		tool.String("artifact_name", tool.Description("name filter for 'list_artifacts'/'list_run_artifacts'")),
		tool.String("status", tool.Description("filter for 'list_runs'/'list_jobs'")),
		tool.Number("tail_lines", tool.Description("log tail lines"), tool.Default(200), tool.Minimum(1)),
		tool.Number("max_bytes", tool.Description("max log bytes"), tool.Default(65536), tool.Minimum(1024)),
		tool.String("output_path", tool.Description("for 'download_job_log'/'download_artifact'")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	ActionsRunWriteTool = tool.NewDefinition(
		ActionsRunWriteToolName,
		"Write Actions runs: dispatch, cancel, rerun.",
		annotation.Write("Trigger, cancel, or rerun Actions workflows"),
		tool.String("method", tool.Required(), tool.Enum("dispatch_workflow", "cancel_run", "rerun_run")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("workflow_id", tool.Description("ID or filename (for 'dispatch_workflow')")),
		tool.String("ref", tool.Description("branch or tag (for 'dispatch_workflow')")),
		tool.Object("inputs", tool.Description("for 'dispatch_workflow'")),
		tool.Number("run_id", tool.Description("for 'cancel_run'/'rerun_run'")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    ActionsRunReadTool,
		Handler: runReadFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"list_workflows":      tool.RepoScoped("owner", "repo"),
			"get_workflow":        tool.RepoScoped("owner", "repo"),
			"list_runs":           tool.RepoScoped("owner", "repo"),
			"get_run":             tool.RepoScoped("owner", "repo"),
			"list_jobs":           tool.RepoScoped("owner", "repo"),
			"list_run_jobs":       tool.RepoScoped("owner", "repo"),
			"get_job":             tool.RepoScoped("owner", "repo"),
			"get_job_log_preview": tool.RepoScoped("owner", "repo"),
			"download_job_log":    tool.RepoScoped("owner", "repo"),
			"list_artifacts":      tool.RepoScoped("owner", "repo"),
			"list_run_artifacts":  tool.RepoScoped("owner", "repo"),
			"get_artifact":        tool.RepoScoped("owner", "repo"),
			"download_artifact":   tool.RepoScoped("owner", "repo"),
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    ActionsRunWriteTool,
		Handler: runWriteFn,
		ScopeKind: tool.MethodScoped(map[string]tool.ScopeKind{
			"dispatch_workflow": tool.RepoScoped("owner", "repo"),
			"cancel_run":        tool.RepoScoped("owner", "repo"),
			"rerun_run":         tool.RepoScoped("owner", "repo"),
		}),
	})
}

func runReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "list_workflows":
		return listRepoActionWorkflowsFn(ctx, args)
	case "get_workflow":
		return getRepoActionWorkflowFn(ctx, args)
	case "list_runs":
		return listRepoActionRunsFn(ctx, args)
	case "get_run":
		return getRepoActionRunFn(ctx, args)
	case "list_jobs":
		return listRepoActionJobsFn(ctx, args)
	case "list_run_jobs":
		return listRepoActionRunJobsFn(ctx, args)
	case "get_job":
		return getRepoActionJobFn(ctx, args)
	case "get_job_log_preview":
		return getRepoActionJobLogPreviewFn(ctx, args)
	case "download_job_log":
		return downloadRepoActionJobLogFn(ctx, args)
	case "list_artifacts":
		return listRepoActionArtifactsFn(ctx, args)
	case "list_run_artifacts":
		return listRepoActionRunArtifactsFn(ctx, args)
	case "get_artifact":
		return getRepoActionArtifactFn(ctx, args)
	case "download_artifact":
		return downloadRepoActionArtifactFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func runWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "dispatch_workflow":
		return dispatchRepoActionWorkflowFn(ctx, args)
	case "cancel_run":
		return cancelRepoActionRunFn(ctx, args)
	case "rerun_run":
		return rerunRepoActionRunFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func doJSONWithFallback(ctx context.Context, method string, paths []string, query url.Values, body, respOut any) error {
	var lastErr error
	for _, p := range paths {
		_, err := gitea.DoJSON(ctx, method, p, query, body, respOut)
		if err == nil {
			return nil
		}
		lastErr = err
		var httpErr *gitea.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
			continue
		}
		return err
	}
	return lastErr
}

func listRepoActionWorkflowsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/workflows", url.PathEscape(owner), url.PathEscape(repo)),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action workflows err: %v", err))
	}
	return to.TextResult(slimActionWorkflows(result))
}

func getRepoActionWorkflowFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	workflowID, err := params.GetString(args, "workflow_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/workflows/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowID)),
		},
		nil, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get action workflow err: %v", err))
	}
	return to.TextResult(slimActionWorkflow(result))
}

func dispatchRepoActionWorkflowFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	workflowID, err := params.GetString(args, "workflow_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	ref, err := params.GetString(args, "ref")
	if err != nil {
		return to.ErrorResult(err)
	}

	var inputs map[string]any
	if raw, exists := args["inputs"]; exists {
		if m, ok := raw.(map[string]any); ok {
			inputs = m
		}
	}

	body := map[string]any{
		"ref": ref,
	}
	if inputs != nil {
		body["inputs"] = inputs
	}

	err = doJSONWithFallback(ctx, "POST",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/workflows/%s/dispatches", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowID)),
			fmt.Sprintf("repos/%s/%s/actions/workflows/%s/dispatch", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowID)),
		},
		nil, body, nil,
	)
	if err != nil {
		var httpErr *gitea.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
			return to.ErrorResult(fmt.Errorf("workflow dispatch not supported on this Gitea version (endpoint returned %d). Check https://docs.gitea.com/api/1.24/ for available Actions endpoints", httpErr.StatusCode))
		}
		return to.ErrorResult(fmt.Errorf("dispatch action workflow err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "workflow dispatched"})
}

func listRepoActionRunsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	statusFilter, _ := args["status"].(string)

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))
	if statusFilter != "" {
		query.Set("status", statusFilter)
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs", url.PathEscape(owner), url.PathEscape(repo)),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action runs err: %v", err))
	}
	return to.TextResult(slimActionRuns(result))
}

func getRepoActionRunFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	runID, err := params.GetIndex(args, "run_id")
	if err != nil || runID <= 0 {
		return to.ErrorResult(errors.New("run_id is required"))
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs/%d", url.PathEscape(owner), url.PathEscape(repo), runID),
		},
		nil, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get action run err: %v", err))
	}
	return to.TextResult(slimActionRun(result))
}

func cancelRepoActionRunFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	runID, err := params.GetIndex(args, "run_id")
	if err != nil || runID <= 0 {
		return to.ErrorResult(errors.New("run_id is required"))
	}

	err = doJSONWithFallback(ctx, "POST",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs/%d/cancel", url.PathEscape(owner), url.PathEscape(repo), runID),
		},
		nil, nil, nil,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("cancel action run err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "run cancellation requested"})
}

func rerunRepoActionRunFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	runID, err := params.GetIndex(args, "run_id")
	if err != nil || runID <= 0 {
		return to.ErrorResult(errors.New("run_id is required"))
	}

	err = doJSONWithFallback(ctx, "POST",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs/%d/rerun", url.PathEscape(owner), url.PathEscape(repo), runID),
			fmt.Sprintf("repos/%s/%s/actions/runs/%d/rerun-failed-jobs", url.PathEscape(owner), url.PathEscape(repo), runID),
		},
		nil, nil, nil,
	)
	if err != nil {
		var httpErr *gitea.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
			return to.ErrorResult(fmt.Errorf("workflow rerun not supported on this Gitea version (endpoint returned %d). Check https://docs.gitea.com/api/1.24/ for available Actions endpoints", httpErr.StatusCode))
		}
		return to.ErrorResult(fmt.Errorf("rerun action run err: %v", err))
	}
	return to.TextResult(map[string]any{"message": "run rerun requested"})
}

func listRepoActionJobsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	statusFilter, _ := args["status"].(string)

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))
	if statusFilter != "" {
		query.Set("status", statusFilter)
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/jobs", url.PathEscape(owner), url.PathEscape(repo)),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action jobs err: %v", err))
	}
	return to.TextResult(slimActionJobs(result))
}

func listRepoActionRunJobsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	runID, err := params.GetIndex(args, "run_id")
	if err != nil || runID <= 0 {
		return to.ErrorResult(errors.New("run_id is required"))
	}
	page, pageSize := params.GetPagination(args, 30)

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(pageSize))

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs/%d/jobs", url.PathEscape(owner), url.PathEscape(repo), runID),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action run jobs err: %v", err))
	}
	return to.TextResult(slimActionJobs(result))
}

func getRepoActionJobFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	jobID, err := params.GetIndex(args, "job_id")
	if err != nil || jobID <= 0 {
		return to.ErrorResult(errors.New("job_id is required"))
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/jobs/%d", url.PathEscape(owner), url.PathEscape(repo), jobID),
		},
		nil, nil, &result,
	)
	if err != nil {
		var httpErr *gitea.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
			return to.ErrorResult(fmt.Errorf("get action job not supported on this Gitea version (endpoint returned %d, requires Gitea 1.26+). Check https://docs.gitea.com/api/1.26/ for available Actions endpoints", httpErr.StatusCode))
		}
		return to.ErrorResult(fmt.Errorf("get action job err: %v", err))
	}
	return to.TextResult(slimActionJob(result))
}

func logPaths(owner, repo string, jobID int64) []string {
	return []string{
		fmt.Sprintf("repos/%s/%s/actions/jobs/%d/logs", url.PathEscape(owner), url.PathEscape(repo), jobID),
		fmt.Sprintf("repos/%s/%s/actions/jobs/%d/log", url.PathEscape(owner), url.PathEscape(repo), jobID),
		fmt.Sprintf("repos/%s/%s/actions/tasks/%d/log", url.PathEscape(owner), url.PathEscape(repo), jobID),
		fmt.Sprintf("repos/%s/%s/actions/task/%d/log", url.PathEscape(owner), url.PathEscape(repo), jobID),
	}
}

func fetchJobLogBytes(ctx context.Context, owner, repo string, jobID int64) ([]byte, string, error) {
	var lastErr error
	for _, p := range logPaths(owner, repo, jobID) {
		b, _, err := gitea.DoBytes(ctx, "GET", p, nil, nil, "text/plain")
		if err == nil {
			return b, p, nil
		}
		lastErr = err
		var httpErr *gitea.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
			continue
		}
		return nil, p, err
	}
	return nil, "", lastErr
}

func tailByLines(data []byte, tailLines int) []byte {
	if tailLines <= 0 || len(data) == 0 {
		return data
	}
	lines := 0
	i := len(data) - 1
	for i >= 0 {
		if data[i] == '\n' {
			lines++
			if lines > tailLines {
				return data[i+1:]
			}
		}
		i--
	}
	return data
}

func limitBytes(data []byte, maxBytes int) ([]byte, bool) {
	if maxBytes <= 0 {
		return data, false
	}
	if len(data) <= maxBytes {
		return data, false
	}
	return data[len(data)-maxBytes:], true
}

func getRepoActionJobLogPreviewFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	jobID, err := params.GetIndex(args, "job_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	tailLines := int(params.GetOptionalInt(args, "tail_lines", 200))
	maxBytes := int(params.GetOptionalInt(args, "max_bytes", 65536))
	raw, usedPath, err := fetchJobLogBytes(ctx, owner, repo, jobID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get job log err: %v", err))
	}

	tailed := tailByLines(raw, tailLines)
	limited, truncated := limitBytes(tailed, maxBytes)

	return to.TextResult(map[string]any{
		"endpoint":   usedPath,
		"job_id":     jobID,
		"bytes":      len(raw),
		"tail_lines": tailLines,
		"max_bytes":  maxBytes,
		"truncated":  truncated,
		"log":        string(limited),
	})
}

func downloadRepoActionJobLogFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	jobID, err := params.GetIndex(args, "job_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	outputPath, _ := args["output_path"].(string)

	raw, usedPath, err := fetchJobLogBytes(ctx, owner, repo, jobID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("download job log err: %v", err))
	}

	if outputPath == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = os.TempDir()
		}
		outputPath = filepath.Join(home, ".gitea-mcp", "artifacts", "actions-logs", owner, repo, fmt.Sprintf("%d.log", jobID))
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return to.ErrorResult(fmt.Errorf("create output dir err: %v", err))
	}
	if err := os.WriteFile(outputPath, raw, 0o600); err != nil {
		return to.ErrorResult(fmt.Errorf("write log file err: %v", err))
	}

	return to.TextResult(map[string]any{
		"endpoint": usedPath,
		"job_id":   jobID,
		"path":     outputPath,
		"bytes":    len(raw),
	})
}
