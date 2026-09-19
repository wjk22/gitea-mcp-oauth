package tool

import (
	"context"
	"slices"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func makeTool(name string) ServerTool {
	return ServerTool{
		Tool:      &mcp.Tool{Name: name},
		ScopeKind: Unscoped(),
	}
}

func names(sts []ServerTool) []string {
	out := make([]string, len(sts))
	for i, st := range sts {
		out[i] = st.Tool.Name
	}
	return out
}

func TestTools(t *testing.T) {
	tests := []struct {
		name     string
		readOnly bool
		allowed  map[string]struct{}
		read     []string
		write    []string
		want     []string
	}{
		{
			name:  "no filters returns write then read",
			read:  []string{"r1", "r2"},
			write: []string{"w1", "w2"},
			want:  []string{"w1", "w2", "r1", "r2"},
		},
		{
			name:     "read-only excludes write",
			readOnly: true,
			read:     []string{"r1", "r2"},
			write:    []string{"w1"},
			want:     []string{"r1", "r2"},
		},
		{
			name:    "allowlist keeps only listed",
			allowed: map[string]struct{}{"r1": {}, "w1": {}},
			read:    []string{"r1", "r2"},
			write:   []string{"w1", "w2"},
			want:    []string{"w1", "r1"},
		},
		{
			name:     "allowlist intersected with read-only drops write entries",
			readOnly: true,
			allowed:  map[string]struct{}{"r1": {}, "w1": {}},
			read:     []string{"r1", "r2"},
			write:    []string{"w1", "w2"},
			want:     []string{"r1"},
		},
		{
			name:    "allowlist with only unknown names returns empty",
			allowed: map[string]struct{}{"unknown": {}},
			read:    []string{"r1"},
			write:   []string{"w1"},
			want:    []string{},
		},
		{
			name:    "empty allowlist map passes through",
			allowed: map[string]struct{}{},
			read:    []string{"r1"},
			write:   []string{"w1"},
			want:    []string{"w1", "r1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRO, origAllow := flag.ReadOnly, flag.AllowedTools
			t.Cleanup(func() {
				flag.ReadOnly, flag.AllowedTools = origRO, origAllow
			})
			flag.ReadOnly = tt.readOnly
			flag.AllowedTools = tt.allowed

			tr := New("scope1")
			for _, n := range tt.read {
				tr.RegisterRead(makeTool(n))
			}
			for _, n := range tt.write {
				tr.RegisterWrite(makeTool(n))
			}

			got := names(tr.Tools())
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tools() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolsScopeFiltering(t *testing.T) {
	tests := []struct {
		name          string
		toolScope     string
		readOnly      bool
		allowedScopes map[string]struct{}
		allowedTools  map[string]struct{}
		read          []string
		write         []string
		want          []string
	}{
		{
			name:      "no scope or tool filters returns all",
			toolScope: "repository",
			read:      []string{"r1"},
			write:     []string{"w1"},
			want:      []string{"w1", "r1"},
		},
		{
			name:          "matching scope keeps everything",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"repository": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{"w1", "r1"},
		},
		{
			name:          "non-matching scope drops everything not in allowed tools",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"file": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{},
		},
		{
			name:         "tools-only allowlist behaves as before scopes existed",
			toolScope:    "repository",
			allowedTools: map[string]struct{}{"r1": {}},
			read:         []string{"r1", "r2"},
			write:        []string{"w1"},
			want:         []string{"r1"},
		},
		{
			name:          "scope and tools allowlists are unioned",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"file": {}},
			allowedTools:  map[string]struct{}{"r1": {}},
			read:          []string{"r1", "r2"},
			write:         []string{"w1"},
			want:          []string{"r1"},
		},
		{
			name:          "matching scope combined with read-only drops write entries",
			toolScope:     "repository",
			readOnly:      true,
			allowedScopes: map[string]struct{}{"repository": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{"r1"},
		},
		{
			name:          "unknown scope name matches nothing",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"unknown_scope": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRO, origAllowScopes, origAllowTools := flag.ReadOnly, flag.AllowedScopes, flag.AllowedTools
			t.Cleanup(func() {
				flag.ReadOnly, flag.AllowedScopes, flag.AllowedTools = origRO, origAllowScopes, origAllowTools
			})
			flag.ReadOnly = tt.readOnly
			flag.AllowedScopes = tt.allowedScopes
			flag.AllowedTools = tt.allowedTools

			tr := New(tt.toolScope)
			for _, n := range tt.read {
				tr.RegisterRead(makeTool(n))
			}
			for _, n := range tt.write {
				tr.RegisterWrite(makeTool(n))
			}

			got := names(tr.Tools())
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tools() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScope(t *testing.T) {
	tr := New("repository")
	if got := tr.Scope(); got != "repository" {
		t.Errorf("Scope() = %q, want %q", got, "repository")
	}
}

func TestWarnUnmatchedAllowedScopes(t *testing.T) {
	origAllowScopes := flag.AllowedScopes
	t.Cleanup(func() {
		flag.AllowedScopes = origAllowScopes
	})

	repoTool := New("repository")
	fileTool := New("file")

	t.Run("empty allowlist is a no-op", func(t *testing.T) {
		flag.AllowedScopes = nil
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})

	t.Run("known scopes produce no warning", func(t *testing.T) {
		flag.AllowedScopes = map[string]struct{}{"repository": {}, "file": {}}
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})

	t.Run("unknown scope is tolerated", func(t *testing.T) {
		flag.AllowedScopes = map[string]struct{}{"not_a_real_scope": {}}
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})
}

// T-AL-3: a tool without access class or scope kind fails registration.
func TestT_AL_3_RegistrationValidation(t *testing.T) {
	tr := New("test")

	t.Run("panic on nil Tool", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for nil Tool, got nil")
			}
		}()
		tr.RegisterRead(ServerTool{Tool: nil, ScopeKind: Unscoped()})
	})

	t.Run("panic on nil ScopeKind for RegisterRead", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for nil ScopeKind, got nil")
			}
		}()
		tr.RegisterRead(ServerTool{
			Tool: &mcp.Tool{Name: "read_no_scope"},
		})
	})

	t.Run("panic on nil ScopeKind for RegisterWrite", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for nil ScopeKind, got nil")
			}
		}()
		tr.RegisterWrite(ServerTool{
			Tool: &mcp.Tool{Name: "write_no_scope"},
		})
	})

	t.Run("panic on compound tool without MethodScope", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic when compound tool does not declare MethodScope, got nil")
			}
		}()
		compoundDef := NewDefinition("compound_tool", "desc", nil,
			String("method", Required(), Enum("op1", "op2")),
		)
		tr.RegisterRead(ServerTool{
			Tool:      compoundDef,
			ScopeKind: Unscoped(),
		})
	})

	t.Run("panic on compound tool missing method in MethodScope", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic when method enum value is missing in MethodScope, got nil")
			}
		}()
		compoundDef := NewDefinition("compound_tool_incomplete", "desc", nil,
			String("method", Required(), Enum("op1", "op2")),
		)
		tr.RegisterRead(ServerTool{
			Tool: compoundDef,
			ScopeKind: MethodScoped(map[string]ScopeKind{
				"op1": Unscoped(),
			}),
		})
	})

	t.Run("MCPHandler denies missing method on compound tool", func(t *testing.T) {
		handlerCalled := false
		st := ServerTool{
			Tool: NewDefinition("compound_call", "desc", nil,
				String("method", Required(), Enum("op1")),
			),
			Handler: func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
				handlerCalled = true
				return &mcp.CallToolResult{}, nil
			},
			ScopeKind: MethodScoped(map[string]ScopeKind{
				"op1": Unscoped(),
			}),
		}
		h := st.MCPHandler()
		res, err := h(context.Background(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{
				Name:      "compound_call",
				Arguments: []byte(`{}`),
			},
		})
		if err != nil {
			t.Fatalf("unexpected jsonrpc error: %v", err)
		}
		if res == nil || !res.IsError {
			t.Errorf("expected IsError: true for missing method, got %v", res)
		}
		if handlerCalled {
			t.Errorf("handler should not have been called when method was missing")
		}
	})

	t.Run("MCPHandler denies unknown method on compound tool", func(t *testing.T) {
		handlerCalled := false
		st := ServerTool{
			Tool: NewDefinition("compound_call", "desc", nil,
				String("method", Required(), Enum("op1")),
			),
			Handler: func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
				handlerCalled = true
				return &mcp.CallToolResult{}, nil
			},
			ScopeKind: MethodScoped(map[string]ScopeKind{
				"op1": Unscoped(),
			}),
		}
		h := st.MCPHandler()
		res, err := h(context.Background(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{
				Name:      "compound_call",
				Arguments: []byte(`{"method":"unknown_method"}`),
			},
		})
		if err != nil {
			t.Fatalf("unexpected jsonrpc error: %v", err)
		}
		if res == nil || !res.IsError {
			t.Errorf("expected IsError: true for unknown method, got %v", res)
		}
		if handlerCalled {
			t.Errorf("handler should not have been called for unknown method")
		}
	})
}
