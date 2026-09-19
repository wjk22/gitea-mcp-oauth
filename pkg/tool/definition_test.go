package tool

import (
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewDefinition(t *testing.T) {
	annotations := &mcp.ToolAnnotations{Title: "Example", ReadOnlyHint: true}
	definition := NewDefinition(
		"example",
		"Example tool",
		annotations,
		String("owner", Required(), Description("repository owner"), Enum("one", "two"), Default("one")),
		Number("page", Required(), Default(1), Minimum(1)),
		Boolean("draft"),
		Array("labels", Items(map[string]any{"type": "string"})),
		Object("inputs", Description("workflow inputs")),
	)

	if definition.Name != "example" || definition.Description != "Example tool" {
		t.Fatalf("definition = %#v", definition)
	}
	if definition.Annotations != annotations {
		t.Fatal("NewDefinition did not preserve annotations")
	}

	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner": map[string]any{
				"type":        "string",
				"description": "repository owner",
				"enum":        []string{"one", "two"},
				"default":     "one",
			},
			"page": map[string]any{
				"type":    "number",
				"default": 1,
				"minimum": float64(1),
			},
			"draft": map[string]any{"type": "boolean"},
			"labels": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"inputs": map[string]any{
				"type":        "object",
				"properties":  map[string]any{},
				"description": "workflow inputs",
			},
		},
		"required": []string{"owner", "page"},
	}
	if !reflect.DeepEqual(definition.InputSchema, want) {
		t.Errorf("InputSchema = %#v, want %#v", definition.InputSchema, want)
	}
}

func TestNewDefinitionWithoutRequiredProperties(t *testing.T) {
	definition := NewDefinition("empty", "", nil)
	schema := definition.InputSchema.(map[string]any)
	if _, ok := schema["required"]; ok {
		t.Errorf("InputSchema unexpectedly contains required: %#v", schema)
	}
	if got := schema["properties"]; !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("properties = %#v, want empty map", got)
	}
}
