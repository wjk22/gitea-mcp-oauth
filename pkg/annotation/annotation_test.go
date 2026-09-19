package annotation

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The hints are what clients use to decide whether a tool needs confirmation, so
// assert the encoded form: an omitted readOnlyHint reads as false either way, but
// only the explicit form survives a client that checks for the key.
func TestAnnotations(t *testing.T) {
	for _, test := range []struct {
		name        string
		annotations *mcp.ToolAnnotations
		want        map[string]any
	}{
		{
			name:        "ReadOnly",
			annotations: ReadOnly("Read"),
			want:        map[string]any{"title": "Read", "readOnlyHint": true, "idempotentHint": false},
		},
		{
			name:        "Write",
			annotations: Write("Write"),
			want:        map[string]any{"title": "Write", "readOnlyHint": false, "idempotentHint": false},
		},
		{
			name:        "Destructive",
			annotations: Destructive("Delete"),
			want:        map[string]any{"title": "Delete", "readOnlyHint": false, "idempotentHint": false, "destructiveHint": true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.annotations)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if !maps.Equal(got, test.want) {
				t.Errorf("annotations = %s, want %v", encoded, test.want)
			}
		})
	}
}
