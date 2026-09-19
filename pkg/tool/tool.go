package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/log"
	"gitea.com/gitea/gitea-mcp/pkg/to"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Handler func(context.Context, map[string]any) (*mcp.CallToolResult, error)

type AccessClass int

const (
	AccessClassUnset AccessClass = iota
	AccessClassRead
	AccessClassWrite
)

// ScopeKind defines how allowlists and boundaries apply to a tool or method.
type ScopeKind interface {
	isScopeKind()
}

// RepoScope binds a tool or method to repository-level access.
type RepoScope struct {
	OwnerArg string
	RepoArg  string
}

func (RepoScope) isScopeKind() {}

func RepoScoped(ownerArg, repoArg string) RepoScope {
	return RepoScope{OwnerArg: ownerArg, RepoArg: repoArg}
}

// OwnerScope binds a tool or method to organization or user owner-level access.
type OwnerScope struct {
	OwnerArg string
}

func (OwnerScope) isScopeKind() {}

func OwnerScoped(ownerArg string) OwnerScope {
	return OwnerScope{OwnerArg: ownerArg}
}

// GlobalFilterFunc filters items during allowlist enforcement.
type GlobalFilterFunc func(ctx context.Context, item any) bool

// GlobalScope marks a tool or method that spans across repositories or organizations.
type GlobalScope struct {
	Filter GlobalFilterFunc
}

func (GlobalScope) isScopeKind() {}

func GlobalScoped(filter GlobalFilterFunc) GlobalScope {
	return GlobalScope{Filter: filter}
}

// UnscopedScope marks a tool or method that accesses no repository or org data.
type UnscopedScope struct{}

func (UnscopedScope) isScopeKind() {}

func Unscoped() UnscopedScope {
	return UnscopedScope{}
}

// MethodScope defines method-level scope dispatch for compound tools.
type MethodScope struct {
	MethodArg string
	Methods   map[string]ScopeKind
}

func (MethodScope) isScopeKind() {}

func MethodScoped(methods map[string]ScopeKind) MethodScope {
	return MethodScope{
		MethodArg: "method",
		Methods:   methods,
	}
}

type ServerTool struct {
	Tool        *mcp.Tool
	Handler     Handler
	ScopeKind   ScopeKind
	accessClass AccessClass
}

// AccessClass returns the access class (read or write) of the tool.
func (s ServerTool) AccessClass() AccessClass {
	return s.accessClass
}

type Tool struct {
	scope string
	write []ServerTool
	read  []ServerTool
}

func New(scope string) *Tool {
	return &Tool{
		scope: scope,
		write: make([]ServerTool, 0, 100),
		read:  make([]ServerTool, 0, 100),
	}
}

// Scope returns the canonical scope name this domain of tools was registered under.
func (t *Tool) Scope() string {
	return t.scope
}

func (t *Tool) RegisterWrite(s ServerTool) {
	validateRegistration(s, AccessClassWrite)
	s.accessClass = AccessClassWrite
	t.write = append(t.write, s)
}

func (t *Tool) RegisterRead(s ServerTool) {
	validateRegistration(s, AccessClassRead)
	s.accessClass = AccessClassRead
	t.read = append(t.read, s)
}

func validateRegistration(s ServerTool, class AccessClass) {
	if s.Tool == nil {
		panic("tool registration failed: nil Tool")
	}
	if class != AccessClassRead && class != AccessClassWrite {
		panic(fmt.Sprintf("tool %q registration failed: invalid access class %v", s.Tool.Name, class))
	}
	if s.ScopeKind == nil {
		panic(fmt.Sprintf("tool %q registration failed: missing scope kind", s.Tool.Name))
	}

	if methodProp, hasMethod := getMethodProperty(s.Tool); hasMethod {
		ms, ok := s.ScopeKind.(MethodScope)
		if !ok {
			panic(fmt.Sprintf("tool %q has method property in schema but declared %T instead of MethodScope", s.Tool.Name, s.ScopeKind))
		}
		enums := getEnumValues(methodProp)
		for _, m := range enums {
			if _, declared := ms.Methods[m]; !declared {
				panic(fmt.Sprintf("tool %q missing scope kind declaration for method %q", s.Tool.Name, m))
			}
		}
	}
}

func getMethodProperty(toolDef *mcp.Tool) (map[string]any, bool) {
	if toolDef == nil || toolDef.InputSchema == nil {
		return nil, false
	}
	schemaMap, ok := toolDef.InputSchema.(map[string]any)
	if !ok {
		return nil, false
	}
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	methodProp, ok := props["method"].(map[string]any)
	return methodProp, ok
}

func getEnumValues(prop map[string]any) []string {
	enumRaw, ok := prop["enum"]
	if !ok {
		return nil
	}
	switch v := enumRaw.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// ReadTools returns the read-only tools registered on this domain, ignoring
// the read-only and allowlist flags that Tools applies.
func (t *Tool) ReadTools() []ServerTool {
	return t.read
}

// WriteTools returns the write tools registered on this domain, ignoring the
// read-only and allowlist flags that Tools applies.
func (t *Tool) WriteTools() []ServerTool {
	return t.write
}

// Tools returns the tools registered on this domain after applying the
// read-only filter and the scope/tool allowlists (union semantics: a tool is
// kept if its domain's scope is in AllowedScopes OR its name is in
// AllowedTools). With no allowlists set, all tools pass through unchanged.
var oauthDeniedTools = map[string]struct{}{
	"actions_config_read": {},
	"notification_read":   {},
	"package_read":        {},
}

var oauthDeniedMethods = map[string]map[string]struct{}{
	"actions_run_read": {
		"download_job_log":    {},
		"get_job_log_preview": {},
		"download_artifact":   {},
	},
	"attachment_read": {
		"download": {},
	},
}

func cloneToolWithoutMethods(t *mcp.Tool, denied map[string]struct{}) *mcp.Tool {
	if t == nil {
		return nil
	}
	cp := *t
	schemaMap, ok := t.InputSchema.(map[string]any)
	if !ok {
		return &cp
	}
	schemaCopy := maps.Clone(schemaMap)
	props, ok := schemaCopy["properties"].(map[string]any)
	if !ok {
		return &cp
	}
	propsCopy := maps.Clone(props)
	if methodProp, ok := propsCopy["method"].(map[string]any); ok {
		methodPropCopy := maps.Clone(methodProp)
		if enumVals, ok := methodPropCopy["enum"].([]string); ok {
			var filtered []string
			for _, v := range enumVals {
				if _, isDenied := denied[v]; !isDenied {
					filtered = append(filtered, v)
				}
			}
			methodPropCopy["enum"] = filtered
		} else if enumAny, ok := methodPropCopy["enum"].([]any); ok {
			var filtered []any
			for _, v := range enumAny {
				if s, ok := v.(string); ok {
					if _, isDenied := denied[s]; !isDenied {
						filtered = append(filtered, v)
					}
				} else {
					filtered = append(filtered, v)
				}
			}
			methodPropCopy["enum"] = filtered
		}
		propsCopy["method"] = methodPropCopy
	}
	schemaCopy["properties"] = propsCopy
	cp.InputSchema = schemaCopy
	return &cp
}

// Tools returns the tools registered on this domain after applying the
// read-only filter, the OAuth-mode denylist, and the scope/tool allowlists (union semantics: a tool is
// kept if its domain's scope is in AllowedScopes OR its name is in
// AllowedTools). With no allowlists set, all tools pass through unchanged.
func (t *Tool) Tools() []ServerTool {
	all := make([]ServerTool, 0, len(t.write)+len(t.read))
	if !flag.ReadOnly && flag.OAuthClientID == "" {
		all = append(all, t.write...)
	}
	all = append(all, t.read...)

	if flag.ReadOnly || flag.OAuthClientID != "" {
		annotated := make([]ServerTool, len(all))
		f := false
		for i, st := range all {
			toolCopy := *st.Tool
			var ann mcp.ToolAnnotations
			if st.Tool.Annotations != nil {
				ann = *st.Tool.Annotations
			}
			ann.ReadOnlyHint = true
			ann.DestructiveHint = &f
			toolCopy.Annotations = &ann
			stCopy := st
			stCopy.Tool = &toolCopy
			annotated[i] = stCopy
		}
		all = annotated
	}

	if flag.OAuthClientID != "" {
		oauthFiltered := make([]ServerTool, 0, len(all))
		for _, st := range all {
			if _, denied := oauthDeniedTools[st.Tool.Name]; denied {
				continue
			}
			if deniedMethods, ok := oauthDeniedMethods[st.Tool.Name]; ok {
				stCopy := st
				if ms, isMS := st.ScopeKind.(MethodScope); isMS {
					newMethods := make(map[string]ScopeKind, len(ms.Methods))
					for m, k := range ms.Methods {
						if _, isDenied := deniedMethods[m]; !isDenied {
							newMethods[m] = k
						}
					}
					ms.Methods = newMethods
					stCopy.ScopeKind = ms
				}
				stCopy.Tool = cloneToolWithoutMethods(st.Tool, deniedMethods)
				oauthFiltered = append(oauthFiltered, stCopy)
				continue
			}
			oauthFiltered = append(oauthFiltered, st)
		}
		all = oauthFiltered
	}

	if len(flag.AllowedScopes) == 0 && len(flag.AllowedTools) == 0 {
		return all
	}
	_, scopeAllowed := flag.AllowedScopes[t.scope]
	filtered := make([]ServerTool, 0, len(all))
	for _, st := range all {
		_, toolAllowed := flag.AllowedTools[st.Tool.Name]
		if scopeAllowed || toolAllowed {
			filtered = append(filtered, st)
		}
	}
	return filtered
}

// MCPHandler adapts a project handler to the official SDK's low-level handler.
func (s ServerTool) MCPHandler() mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				result, err = to.ErrorResult(fmt.Errorf("panic recovered in %s tool handler: %v", s.Tool.Name, recovered))
			}
		}()

		arguments, err := decodeArguments(req.Params.Arguments)
		if err != nil {
			return nil, err
		}

		if ms, ok := s.ScopeKind.(MethodScope); ok {
			methodArg := ms.MethodArg
			if methodArg == "" {
				methodArg = "method"
			}
			rawMethod, exists := arguments[methodArg]
			if !exists {
				return to.ErrorResult(fmt.Errorf("missing required method argument %q for tool %s", methodArg, s.Tool.Name))
			}
			methodStr, ok := rawMethod.(string)
			if !ok || methodStr == "" {
				return to.ErrorResult(fmt.Errorf("invalid method argument %q for tool %s", methodArg, s.Tool.Name))
			}
			if _, declared := ms.Methods[methodStr]; !declared {
				return to.ErrorResult(fmt.Errorf("unknown method %q for tool %s", methodStr, s.Tool.Name))
			}
		}

		result, err = s.Handler(ctx, arguments)
		if err != nil {
			if _, ok := errors.AsType[*jsonrpc.Error](err); ok {
				return nil, err
			}
			return to.ErrorResult(err)
		}
		return result, nil
	}
}

func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	// An omitted and a null "arguments" both mean the tool was called without any.
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}

	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("invalid tool arguments: %v", err),
		}
	}
	return arguments, nil
}

// warnUnmatched logs the names present in allowlist but absent from known,
// via logUnmatched, so WarnUnmatchedAllowedTools and WarnUnmatchedAllowedScopes
// share the same "collect, sort, no-op when empty" logic and can't drift.
// No-op if allowlist is empty or every name in it is known.
func warnUnmatched(allowlist, known map[string]struct{}, logUnmatched func(unmatched []string)) {
	if len(allowlist) == 0 {
		return
	}
	var unmatched []string
	for name := range allowlist {
		if _, ok := known[name]; !ok {
			unmatched = append(unmatched, name)
		}
	}
	if len(unmatched) == 0 {
		return
	}
	slices.Sort(unmatched)
	logUnmatched(unmatched)
}

// WarnUnmatchedAllowedTools logs any names in flag.AllowedTools that don't
// match a tool registered on any of the given domains. No-op if the allowlist
// is empty.
func WarnUnmatchedAllowedTools(domains ...*Tool) {
	known := map[string]struct{}{}
	for _, d := range domains {
		for _, st := range d.read {
			known[st.Tool.Name] = struct{}{}
		}
		for _, st := range d.write {
			known[st.Tool.Name] = struct{}{}
		}
	}
	warnUnmatched(flag.AllowedTools, known, func(unmatched []string) {
		log.Warnf("Unknown tools in --tools allowlist (ignored): %s", strings.Join(unmatched, ", "))
	})
}

// WarnUnmatchedAllowedScopes logs any names in flag.AllowedScopes that don't
// match the scope of any of the given domains. No-op if the allowlist is
// empty.
func WarnUnmatchedAllowedScopes(domains ...*Tool) {
	knownSet := map[string]struct{}{}
	known := make([]string, 0, len(domains))
	for _, d := range domains {
		if _, ok := knownSet[d.scope]; !ok {
			knownSet[d.scope] = struct{}{}
			known = append(known, d.scope)
		}
	}
	warnUnmatched(flag.AllowedScopes, knownSet, func(unmatched []string) {
		slices.Sort(known)
		log.Warnf("Unknown scopes in --scope allowlist (ignored): %s. Valid scopes: %s", strings.Join(unmatched, ", "), strings.Join(known, ", "))
	})
}
