package operation

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// toolTableRow matches a row of the "Available Tools" table in the README
// files, capturing the tool name, the scope cell and the access cell, e.g.
// "| get_me | user | Read | Get the current authenticated user |".
var toolTableRow = regexp.MustCompile(`^\|\s*([a-z_]+)\s*\|\s*([a-z_]+)\s*\|\s*(\S+)\s*\|`)

// readmeAccessLabels maps each README to the access-column labels it uses.
var readmeAccessLabels = map[string]map[string]string{
	"../README.md":       {"Read": "read", "Write": "write"},
	"../README.zh-cn.md": {"读取": "read", "写入": "write"},
	"../README.zh-tw.md": {"讀取": "read", "寫入": "write"},
}

// toolInfo is what TestReadmeToolTables tracks per tool, both as registered
// in code and as documented in a README, so the two can be compared.
type toolInfo struct {
	scope  string
	access string
}

// TestReadmeToolTables ensures the tool tables in the README files stay in sync
// with the registered tools, in both directions and for every translation.
// The tables listed tools that no longer existed for several releases before
// anyone noticed.
// The scope names in the README are the canonical, lowercase snake_case names
// returned by (*tool.Tool).Scope(), so no translation is needed to compare them.
func TestReadmeToolTables(t *testing.T) {
	registered := map[string]toolInfo{}
	for _, d := range domainTools {
		scope := d.Scope()
		for _, st := range d.ReadTools() {
			registered[st.Tool.Name] = toolInfo{scope: scope, access: "read"}
		}
		for _, st := range d.WriteTools() {
			registered[st.Tool.Name] = toolInfo{scope: scope, access: "write"}
		}
	}

	for path, labels := range readmeAccessLabels {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			documented := map[string]toolInfo{}
			for line := range strings.SplitSeq(string(content), "\n") {
				if match := toolTableRow.FindStringSubmatch(line); match != nil {
					documented[match[1]] = toolInfo{scope: match[2], access: labels[match[3]]}
				}
			}

			for _, name := range slices.Sorted(maps.Keys(registered)) {
				got, ok := documented[name]
				want := registered[name]
				switch {
				case !ok:
					t.Errorf("tool %q is registered but missing from the tool table", name)
				case got.access != want.access:
					t.Errorf("tool %q is documented with %q access, want %q", name, got.access, want.access)
				case got.scope != want.scope:
					t.Errorf("tool %q is documented with scope %q, want %q", name, got.scope, want.scope)
				}
			}
			for _, name := range slices.Sorted(maps.Keys(documented)) {
				if _, ok := registered[name]; !ok {
					t.Errorf("tool %q is in the tool table but is not registered", name)
				}
			}
		})
	}
}
