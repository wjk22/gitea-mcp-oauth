package tool

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Property describes one property in a tool's input schema.
type Property struct {
	name     string
	schema   map[string]any
	required bool
}

// PropertyOption configures one property in a tool's input schema.
type PropertyOption func(*Property)

// NewDefinition builds a tool definition without enabling SDK-side validation.
func NewDefinition(name, description string, annotations *mcp.ToolAnnotations, properties ...Property) *mcp.Tool {
	inputProperties := make(map[string]any, len(properties))
	required := make([]string, 0, len(properties))
	for _, property := range properties {
		inputProperties[property.name] = property.schema
		if property.required {
			required = append(required, property.name)
		}
	}

	inputSchema := map[string]any{
		"type":       "object",
		"properties": inputProperties,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: annotations,
		InputSchema: inputSchema,
	}
}

func String(name string, options ...PropertyOption) Property {
	return newProperty(name, map[string]any{"type": "string"}, options...)
}

func Number(name string, options ...PropertyOption) Property {
	return newProperty(name, map[string]any{"type": "number"}, options...)
}

func Boolean(name string, options ...PropertyOption) Property {
	return newProperty(name, map[string]any{"type": "boolean"}, options...)
}

func Array(name string, options ...PropertyOption) Property {
	return newProperty(name, map[string]any{"type": "array"}, options...)
}

func Object(name string, options ...PropertyOption) Property {
	return newProperty(name, map[string]any{"type": "object", "properties": map[string]any{}}, options...)
}

func newProperty(name string, schema map[string]any, options ...PropertyOption) Property {
	property := Property{name: name, schema: schema}
	for _, option := range options {
		option(&property)
	}
	return property
}

// Required marks the property as required on the parent schema. It is not a
// property-level keyword, so it never touches the emitted property schema.
func Required() PropertyOption {
	return func(property *Property) {
		property.required = true
	}
}

func Description(description string) PropertyOption {
	return func(property *Property) {
		property.schema["description"] = description
	}
}

func Enum(values ...string) PropertyOption {
	return func(property *Property) {
		property.schema["enum"] = values
	}
}

func Default(value any) PropertyOption {
	return func(property *Property) {
		property.schema["default"] = value
	}
}

func Minimum(value float64) PropertyOption {
	return func(property *Property) {
		property.schema["minimum"] = value
	}
}

func Items(schema any) PropertyOption {
	return func(property *Property) {
		property.schema["items"] = schema
	}
}
