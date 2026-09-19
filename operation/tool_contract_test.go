package operation

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolContract checks the properties every exposed tool must hold, rather
// than a snapshot of the current surface, so adding a tool needs no fixture
// update and a malformed schema fails here instead of panicking in AddTool.
func TestToolContract(t *testing.T) {
	scopeByName := map[string]string{}
	seenScopes := map[string]struct{}{}
	for _, domain := range domainTools {
		scope := domain.Scope()
		if scope == "" {
			t.Error("domainTools contains a domain with an empty scope")
		}
		// Tools() filters one domain by exactly one scope name, so a shared
		// scope would make --scope select more than the caller asked for.
		if _, duplicate := seenScopes[scope]; duplicate {
			t.Errorf("domainTools contains a duplicate scope %q", scope)
		}
		seenScopes[scope] = struct{}{}

		for _, registered := range domain.ReadTools() {
			assertToolContract(t, scope, registered.Tool, true, scopeByName)
		}
		for _, registered := range domain.WriteTools() {
			assertToolContract(t, scope, registered.Tool, false, scopeByName)
		}
	}
	if len(scopeByName) == 0 {
		t.Fatal("no tools are registered")
	}
}

func assertToolContract(t *testing.T, scope string, definition *mcp.Tool, readOnly bool, scopeByName map[string]string) {
	t.Helper()

	t.Run(definition.Name, func(t *testing.T) {
		if previous, duplicate := scopeByName[definition.Name]; duplicate {
			t.Errorf("tool name is already registered in scope %q; AddTool would silently replace it", previous)
		}
		scopeByName[definition.Name] = scope

		// Strict MCP clients reject a tools/list entry without a description.
		if definition.Description == "" {
			t.Error("tool has no description")
		}

		// A write tool registered as read stays exposed under --read-only.
		if definition.Annotations == nil || definition.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("annotations = %+v, want readOnlyHint %v", definition.Annotations, readOnly)
		}

		schema := decodeJSON(t, definition.InputSchema)
		if schema["type"] != "object" {
			t.Fatalf("input schema type = %v, want object", schema["type"])
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("input schema properties = %T, want a JSON object", schema["properties"])
		}

		for name, raw := range properties {
			property, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("property %q = %T, want a JSON object", name, raw)
				continue
			}
			assertPropertyContract(t, name, property)
		}
	})
}

func assertPropertyContract(t *testing.T, name string, property map[string]any) {
	t.Helper()

	propertyType, ok := property["type"].(string)
	if !ok {
		t.Errorf("property %q has no type", name)
		return
	}

	enum, hasEnum := property["enum"].([]any)
	if _, declared := property["enum"]; declared && len(enum) == 0 {
		t.Errorf("property %q has an empty enum", name)
	}

	defaultValue, hasDefault := property["default"]
	if !hasDefault {
		return
	}
	if !matchesJSONType(defaultValue, propertyType) {
		t.Errorf("property %q default %#v is not a %s", name, defaultValue, propertyType)
	}
	if hasEnum && !slices.Contains(enum, defaultValue) {
		t.Errorf("property %q default %#v is not one of its enum values %#v", name, defaultValue, enum)
	}
}

func matchesJSONType(value any, propertyType string) bool {
	switch propertyType {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return false
	}
}

// decodeJSON round-trips through JSON so the assertions see what an MCP client
// receives rather than the Go values behind it.
func decodeJSON(t *testing.T, value any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}
