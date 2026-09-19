package params

import (
	"fmt"
	"strconv"
	"time"
)

// Shared parameter description strings used across tools. Extracted to avoid
// repeating the same boilerplate in every tool schema (saves tokens in the
// tool list sent to MCP clients).
const (
	OwnerDesc      = "repo owner"
	RepoDesc       = "repo name"
	PageDesc       = "page"
	PaginationDesc = "results per page"
)

// GetString extracts a required string parameter. Empty strings are treated as missing.
func GetString(args map[string]any, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}

func GetOptionalString(args map[string]any, key, defaultVal string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return defaultVal
}

func GetStringSlice(args map[string]any, key string) []string {
	val, ok := args[key]
	if !ok {
		return nil
	}
	sliceVal, ok := val.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(sliceVal))
	for _, item := range sliceVal {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func GetPagination(args map[string]any, defaultPageSize int64) (page, pageSize int) {
	return int(GetOptionalInt(args, "page", 1)), int(GetOptionalInt(args, "per_page", defaultPageSize))
}

// ToInt64 accepts float64 (JSON number) and string representations.
func ToInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case float64:
		return int64(v), true
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// GetIndex extracts a required integer. Accepts numeric or string forms — LLM callers
// often pass identifiers like issue/PR numbers as strings.
func GetIndex(args map[string]any, key string) (int64, error) {
	val, exists := args[key]
	if !exists {
		return 0, fmt.Errorf("%s is required", key)
	}

	if i, ok := ToInt64(val); ok {
		return i, nil
	}

	if s, ok := val.(string); ok {
		return 0, fmt.Errorf("%s must be a valid integer (got %q)", key, s)
	}

	return 0, fmt.Errorf("%s must be a number or numeric string", key)
}

func GetInt64Slice(args map[string]any, key string) ([]int64, error) {
	raw, ok := args[key].([]any)
	if !ok {
		return nil, fmt.Errorf("%s (array of IDs) is required", key)
	}
	out := make([]int64, 0, len(raw))
	for _, v := range raw {
		id, ok := ToInt64(v)
		if !ok {
			return nil, fmt.Errorf("invalid ID in %s array", key)
		}
		out = append(out, id)
	}
	return out, nil
}

// GetOptionalTime parses RFC3339, returning nil if missing or unparseable.
func GetOptionalTime(args map[string]any, key string) *time.Time {
	val, ok := args[key].(string)
	if !ok {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return &t
	}
	return nil
}

func GetOptionalInt(args map[string]any, key string, defaultVal int64) int64 {
	val, exists := args[key]
	if !exists {
		return defaultVal
	}
	if i, ok := ToInt64(val); ok {
		return i
	}
	return defaultVal
}

// GetOptionalBoolPtr is for SDK fields where nil/false/true are distinct (e.g. "no change" vs "set to false").
func GetOptionalBoolPtr(args map[string]any, key string) *bool {
	if v, ok := args[key].(bool); ok {
		return &v
	}
	return nil
}

// GetOptionalStringPtr returns nil when the key is missing OR the value is an empty string.
// Use this for create/fork-style fields where "" is meaningless (e.g. fork target name).
func GetOptionalStringPtr(args map[string]any, key string) *string {
	if v, ok := args[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

// GetPresentStringPtr returns &v whenever the key is present as a string, including "".
// Use this for PATCH-style fields where the SDK distinguishes "no change" (nil) from
// "set to empty" (&""), e.g. clearing an issue body or label description.
func GetPresentStringPtr(args map[string]any, key string) *string {
	if v, ok := args[key].(string); ok {
		return &v
	}
	return nil
}
