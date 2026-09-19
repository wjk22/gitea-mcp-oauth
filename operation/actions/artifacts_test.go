package actions

import "testing"

func TestArtifactFilename(t *testing.T) {
	tests := []struct {
		name       string
		meta       map[string]any
		artifactID int64
		want       string
	}{
		{"uses name", map[string]any{"name": "build-output"}, 7, "build-output.zip"},
		{"trims whitespace", map[string]any{"name": "  logs  "}, 7, "logs.zip"},
		{"missing name falls back to id", map[string]any{}, 7, "7.zip"},
		{"empty name falls back to id", map[string]any{"name": ""}, 7, "7.zip"},
		{"non-string name falls back to id", map[string]any{"name": 42}, 7, "7.zip"},
		{"rejects forward slash traversal", map[string]any{"name": "../etc/passwd"}, 7, "7.zip"},
		{"rejects backslash traversal", map[string]any{"name": `..\win`}, 7, "7.zip"},
		{"rejects dot", map[string]any{"name": "."}, 7, "7.zip"},
		{"rejects dotdot", map[string]any{"name": ".."}, 7, "7.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artifactFilename(tt.meta, tt.artifactID); got != tt.want {
				t.Errorf("artifactFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}
