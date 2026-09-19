package params

import (
	"strings"
	"testing"
)

func TestGetPagination(t *testing.T) {
	page, perPage := GetPagination(map[string]any{"page": float64(2), "per_page": float64(40)}, 30)
	if page != 2 || perPage != 40 {
		t.Errorf("GetPagination = (%d, %d), want (2, 40)", page, perPage)
	}
	page, perPage = GetPagination(map[string]any{}, 30)
	if page != 1 || perPage != 30 {
		t.Errorf("GetPagination defaults = (%d, %d), want (1, 30)", page, perPage)
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int64
		ok   bool
	}{
		{"float64", float64(42), 42, true},
		{"float64 zero", float64(0), 0, true},
		{"float64 negative", float64(-5), -5, true},
		{"string", "123", 123, true},
		{"string zero", "0", 0, true},
		{"string negative", "-10", -10, true},
		{"invalid string", "abc", 0, false},
		{"decimal string", "1.5", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ToInt64(tt.val)
			if ok != tt.ok {
				t.Errorf("ToInt64() ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ToInt64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetOptionalInt(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		key        string
		defaultVal int64
		want       int64
	}{
		{"present float64", map[string]any{"page": float64(3)}, "page", 1, 3},
		{"present string", map[string]any{"page": "5"}, "page", 1, 5},
		{"missing key", map[string]any{}, "page", 1, 1},
		{"invalid string", map[string]any{"page": "abc"}, "page", 1, 1},
		{"invalid type", map[string]any{"page": true}, "page", 1, 1},
		{"zero value", map[string]any{"id": float64(0)}, "id", 99, 0},
		{"string zero", map[string]any{"id": "0"}, "id", 99, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetOptionalInt(tt.args, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("GetOptionalInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetOptionalStringPtr(t *testing.T) {
	if p := GetOptionalStringPtr(map[string]any{}, "k"); p != nil {
		t.Errorf("missing key: got %v, want nil", p)
	}
	if p := GetOptionalStringPtr(map[string]any{"k": ""}, "k"); p != nil {
		t.Errorf("empty string: got %v, want nil", p)
	}
	if p := GetOptionalStringPtr(map[string]any{"k": 42}, "k"); p != nil {
		t.Errorf("non-string: got %v, want nil", p)
	}
	if p := GetOptionalStringPtr(map[string]any{"k": nil}, "k"); p != nil {
		t.Errorf("nil value (JSON null): got %v, want nil", p)
	}
	if p := GetOptionalStringPtr(map[string]any{"k": "x"}, "k"); p == nil || *p != "x" {
		t.Errorf("non-empty: got %v, want &\"x\"", p)
	}
}

func TestGetPresentStringPtr(t *testing.T) {
	if p := GetPresentStringPtr(map[string]any{}, "k"); p != nil {
		t.Errorf("missing key: got %v, want nil", p)
	}
	if p := GetPresentStringPtr(map[string]any{"k": 42}, "k"); p != nil {
		t.Errorf("non-string: got %v, want nil", p)
	}
	if p := GetPresentStringPtr(map[string]any{"k": nil}, "k"); p != nil {
		t.Errorf("nil value (JSON null): got %v, want nil", p)
	}
	if p := GetPresentStringPtr(map[string]any{"k": ""}, "k"); p == nil || *p != "" {
		t.Errorf("empty string: got %v, want &\"\"", p)
	}
	if p := GetPresentStringPtr(map[string]any{"k": "x"}, "k"); p == nil || *p != "x" {
		t.Errorf("non-empty: got %v, want &\"x\"", p)
	}
}

func TestGetIndex(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		key       string
		wantIndex int64
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid float64",
			args:      map[string]any{"index": float64(123)},
			key:       "index",
			wantIndex: 123,
			wantErr:   false,
		},
		{
			name:      "valid string",
			args:      map[string]any{"index": "456"},
			key:       "index",
			wantIndex: 456,
			wantErr:   false,
		},
		{
			name:      "valid string with large number",
			args:      map[string]any{"index": "999999"},
			key:       "index",
			wantIndex: 999999,
			wantErr:   false,
		},
		{
			name:    "missing parameter",
			args:    map[string]any{},
			key:     "index",
			wantErr: true,
			errMsg:  "index is required",
		},
		{
			name:    "invalid string (not a number)",
			args:    map[string]any{"index": "abc"},
			key:     "index",
			wantErr: true,
			errMsg:  "must be a valid integer",
		},
		{
			name:    "invalid string (decimal)",
			args:    map[string]any{"index": "12.34"},
			key:     "index",
			wantErr: true,
			errMsg:  "must be a valid integer",
		},
		{
			name:    "invalid type (bool)",
			args:    map[string]any{"index": true},
			key:     "index",
			wantErr: true,
			errMsg:  "must be a number or numeric string",
		},
		{
			name:    "invalid type (map)",
			args:    map[string]any{"index": map[string]string{"foo": "bar"}},
			key:     "index",
			wantErr: true,
			errMsg:  "must be a number or numeric string",
		},
		{
			name:      "custom key name",
			args:      map[string]any{"pr_index": "789"},
			key:       "pr_index",
			wantIndex: 789,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, err := GetIndex(tt.args, tt.key)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetIndex() expected error but got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("GetIndex() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("GetIndex() unexpected error = %v", err)
				return
			}
			if gotIndex != tt.wantIndex {
				t.Errorf("GetIndex() = %v, want %v", gotIndex, tt.wantIndex)
			}
		})
	}
}
