package actions

import "testing"

func TestSlimActionJobKeepsExpectedKeys(t *testing.T) {
	raw := map[string]any{
		"id":            float64(42),
		"run_id":        float64(7),
		"name":          "build",
		"workflow_name": "CI",
		"status":        "completed",
		"conclusion":    "success",
		"html_url":      "https://gitea.example/x",
		"started_at":    "2026-01-01T00:00:00Z",
		"completed_at":  "2026-01-01T00:01:00Z",
		"steps": []any{
			map[string]any{"name": "checkout", "number": float64(1), "status": "completed", "conclusion": "success", "extra": "drop me"},
		},
		// fields that must be dropped
		"head_sha":    "deadbeef",
		"runner_name": "runner-1",
	}

	out, ok := slimActionJob(raw).(map[string]any)
	if !ok {
		t.Fatalf("slimActionJob did not return a map, got %T", slimActionJob(raw))
	}

	for _, k := range []string{"id", "run_id", "name", "workflow_name", "status", "conclusion", "html_url", "started_at", "completed_at", "steps"} {
		if _, present := out[k]; !present {
			t.Errorf("expected key %q to be kept", k)
		}
	}
	for _, k := range []string{"head_sha", "runner_name"} {
		if _, present := out[k]; present {
			t.Errorf("expected key %q to be dropped", k)
		}
	}

	steps, ok := out["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("expected 1 slimmed step, got %v", out["steps"])
	}
	step := steps[0].(map[string]any)
	if _, present := step["extra"]; present {
		t.Errorf("expected step field 'extra' to be dropped")
	}
	if step["name"] != "checkout" {
		t.Errorf("expected step name 'checkout', got %v", step["name"])
	}
}

func TestSlimActionJobPassesThroughNonMap(t *testing.T) {
	if got := slimActionJob("not-a-map"); got != "not-a-map" {
		t.Errorf("expected passthrough, got %v", got)
	}
}
