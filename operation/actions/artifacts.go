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
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Artifact endpoints require Gitea 1.25+. Older servers answer 404/405, which is
// surfaced as a clear "not supported" message rather than a raw HTTP error.
func artifactNotSupportedErr(err error) error {
	var httpErr *gitea.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed) {
		return fmt.Errorf("actions artifacts not supported on this Gitea version (endpoint returned %d, requires Gitea 1.25+). Check https://docs.gitea.com/api/1.25/ for available Actions endpoints", httpErr.StatusCode)
	}
	return err
}

func listRepoActionArtifactsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	if name := params.GetOptionalString(args, "artifact_name", ""); name != "" {
		query.Set("name", name)
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/artifacts", url.PathEscape(owner), url.PathEscape(repo)),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action artifacts err: %v", artifactNotSupportedErr(err)))
	}
	return to.TextResult(slimActionArtifacts(result))
}

func listRepoActionRunArtifactsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	if name := params.GetOptionalString(args, "artifact_name", ""); name != "" {
		query.Set("name", name)
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/runs/%d/artifacts", url.PathEscape(owner), url.PathEscape(repo), runID),
		},
		query, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list action run artifacts err: %v", artifactNotSupportedErr(err)))
	}
	return to.TextResult(slimActionArtifacts(result))
}

func getRepoActionArtifactFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	artifactID, err := params.GetIndex(args, "artifact_id")
	if err != nil || artifactID <= 0 {
		return to.ErrorResult(errors.New("artifact_id is required"))
	}

	var result any
	err = doJSONWithFallback(ctx, "GET",
		[]string{
			fmt.Sprintf("repos/%s/%s/actions/artifacts/%d", url.PathEscape(owner), url.PathEscape(repo), artifactID),
		},
		nil, nil, &result,
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get action artifact err: %v", artifactNotSupportedErr(err)))
	}
	return to.TextResult(slimActionArtifact(result))
}

func downloadRepoActionArtifactFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	artifactID, err := params.GetIndex(args, "artifact_id")
	if err != nil || artifactID <= 0 {
		return to.ErrorResult(errors.New("artifact_id is required"))
	}
	outputPath, _ := args["output_path"].(string)

	// Best-effort metadata lookup: gives a friendly filename and lets us fail
	// early with a clear message when the artifact has expired.
	var meta map[string]any
	_ = doJSONWithFallback(ctx, "GET",
		[]string{fmt.Sprintf("repos/%s/%s/actions/artifacts/%d", url.PathEscape(owner), url.PathEscape(repo), artifactID)},
		nil, nil, &meta,
	)
	if expired, ok := meta["expired"].(bool); ok && expired {
		return to.ErrorResult(fmt.Errorf("artifact %d has expired and is no longer downloadable", artifactID))
	}

	// The zip endpoint answers with a 302 redirect to signed blob storage;
	// DoBytes follows GET redirects and returns the archive bytes.
	raw, _, err := gitea.DoBytes(ctx, "GET",
		fmt.Sprintf("repos/%s/%s/actions/artifacts/%d/zip", url.PathEscape(owner), url.PathEscape(repo), artifactID),
		nil, nil, "application/zip",
	)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("download action artifact err: %v", artifactNotSupportedErr(err)))
	}

	if outputPath == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = os.TempDir()
		}
		outputPath = filepath.Join(home, ".gitea-mcp", "artifacts", "actions-artifacts", owner, repo, artifactFilename(meta, artifactID))
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return to.ErrorResult(fmt.Errorf("create output dir err: %v", err))
	}
	if err := os.WriteFile(outputPath, raw, 0o600); err != nil {
		return to.ErrorResult(fmt.Errorf("write artifact file err: %v", err))
	}

	res := map[string]any{
		"artifact_id": artifactID,
		"path":        outputPath,
		"bytes":       len(raw),
	}
	if name, ok := meta["name"].(string); ok && name != "" {
		res["name"] = name
	}
	return to.TextResult(res)
}

// artifactFilename derives a safe "<name>.zip" filename from artifact metadata,
// falling back to the artifact ID when the name is missing or path-unsafe.
func artifactFilename(meta map[string]any, artifactID int64) string {
	name, _ := meta["name"].(string)
	name = strings.TrimSpace(name)
	if name != "" && !strings.ContainsAny(name, `/\`) && name != "." && name != ".." {
		return name + ".zip"
	}
	return fmt.Sprintf("%d.zip", artifactID)
}
