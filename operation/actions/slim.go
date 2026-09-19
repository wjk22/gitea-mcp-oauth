package actions

func pick(m map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func slimPaginated(raw any, itemFn func(map[string]any) map[string]any) any {
	m, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	result := make(map[string]any)
	if tc, ok := m["total_count"]; ok {
		result["total_count"] = tc
	}
	for key, val := range m {
		if key == "total_count" {
			continue
		}
		arr, ok := val.([]any)
		if !ok {
			continue
		}
		slimmed := make([]any, 0, len(arr))
		for _, item := range arr {
			if im, ok := item.(map[string]any); ok {
				slimmed = append(slimmed, itemFn(im))
			}
		}
		result[key] = slimmed
		break
	}
	return result
}

func slimRun(m map[string]any) map[string]any {
	return pick(m, "id", "name", "head_branch", "head_sha", "run_number",
		"event", "status", "conclusion", "workflow_id",
		"html_url", "created_at", "updated_at")
}

func slimJob(m map[string]any) map[string]any {
	out := pick(m, "id", "run_id", "name", "workflow_name",
		"status", "conclusion", "html_url",
		"started_at", "completed_at")
	if steps, ok := m["steps"].([]any); ok {
		slim := make([]any, 0, len(steps))
		for _, s := range steps {
			if sm, ok := s.(map[string]any); ok {
				slim = append(slim, pick(sm, "name", "number", "status", "conclusion"))
			}
		}
		out["steps"] = slim
	}
	return out
}

func slimWorkflow(m map[string]any) map[string]any {
	return pick(m, "id", "name", "path", "state", "html_url", "created_at", "updated_at")
}

func slimActionRun(raw any) any {
	if m, ok := raw.(map[string]any); ok {
		return slimRun(m)
	}
	return raw
}

func slimActionRuns(raw any) any {
	return slimPaginated(raw, slimRun)
}

func slimActionJob(raw any) any {
	if m, ok := raw.(map[string]any); ok {
		return slimJob(m)
	}
	return raw
}

func slimActionJobs(raw any) any {
	return slimPaginated(raw, slimJob)
}

func slimActionWorkflow(raw any) any {
	if m, ok := raw.(map[string]any); ok {
		return slimWorkflow(m)
	}
	return raw
}

func slimActionWorkflows(raw any) any {
	return slimPaginated(raw, slimWorkflow)
}

func slimArtifact(m map[string]any) map[string]any {
	return pick(m, "id", "name", "size_in_bytes", "expired",
		"created_at", "updated_at", "expires_at")
}

func slimActionArtifact(raw any) any {
	if m, ok := raw.(map[string]any); ok {
		return slimArtifact(m)
	}
	return raw
}

func slimActionArtifacts(raw any) any {
	return slimPaginated(raw, slimArtifact)
}
