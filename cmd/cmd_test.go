package cmd

import (
	"bytes"
	"errors"
	"flag"
	"maps"
	"slices"
	"strings"
	"testing"

	flagPkg "gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestInitFlagSetBind(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default is empty, meaning all interfaces", args: []string{}},
		{name: "-b sets the address", args: []string{"-b", "127.0.0.1"}, want: "127.0.0.1"},
		{name: "-bind sets an IPv6 literal", args: []string{"-bind", "::1"}, want: "::1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(func() { flagPkg.Bind = "" })
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			initFlagSet(fs, test.args, func(string) string { return "" }, func(string) ([]byte, error) { return nil, nil }, &bytes.Buffer{})
			if flagPkg.Bind != test.want {
				t.Errorf("Bind = %q, want %q", flagPkg.Bind, test.want)
			}
		})
	}
}

func TestInitFlagSetScopes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{
			name: "no scope flag or env leaves AllowedScopes unset",
			args: []string{},
			want: nil,
		},
		{
			name: "-S sets a single scope",
			args: []string{"-S", "repository"},
			want: []string{"repository"},
		},
		{
			name: "-scope sets a comma-separated list",
			args: []string{"-scope", "repository,file"},
			want: []string{"file", "repository"},
		},
		{
			name: "GITEA_SCOPES env sets the default",
			args: []string{},
			env:  map[string]string{"GITEA_SCOPES": "issue,pull_request"},
			want: []string{"issue", "pull_request"},
		},
		{
			name: "-S flag takes precedence over GITEA_SCOPES env",
			args: []string{"-S", "file"},
			env:  map[string]string{"GITEA_SCOPES": "issue"},
			want: []string{"file"},
		},
		{
			name: "normalizes case, whitespace, and hyphens/spaces to underscores",
			args: []string{"-S", " Pull Request , pull-request , PULL_REQUEST "},
			want: []string{"pull_request"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origScopes := flagPkg.AllowedScopes
			t.Cleanup(func() {
				flagPkg.AllowedScopes = origScopes
			})
			flagPkg.AllowedScopes = nil

			getenv := func(key string) string { return tt.env[key] }
			readFile := func(string) ([]byte, error) { return nil, nil }
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var stderr bytes.Buffer

			initFlagSet(fs, tt.args, getenv, readFile, &stderr)

			got := slices.Sorted(maps.Keys(flagPkg.AllowedScopes))
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("AllowedScopes = %v, want %v", got, want)
			}
		})
	}
}

func TestInitFlagSetHealthcheck(t *testing.T) {
	t.Cleanup(func() { healthcheck = false })
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	initFlagSet(fs, []string{"-healthcheck"}, func(string) string { return "" }, func(string) ([]byte, error) { return nil, nil }, &bytes.Buffer{})
	if !healthcheck {
		t.Error("healthcheck = false, want true")
	}
}

func TestStartupRulesTCFG1(t *testing.T) {
	type exitPanic struct{ code int }

	rawKey32 := "01234567890123456789012345678901"
	b64Key32 := "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="

	files := map[string][]byte{
		"/secrets/secret": []byte("valid-secret\n"),
		"/secrets/empty":  []byte("   \n"),
		"/secrets/rawkey": []byte(rawKey32),
		"/secrets/b64key": []byte(b64Key32),
		"/secrets/badkey": []byte("too-short"),
	}

	tests := []struct {
		name            string
		args            []string
		wantExit        bool
		wantErrContains string
	}{
		{
			name: "oauth mode rejected if transport is stdio",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=stdio",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "OAuth mode requires transport 'http'",
		},
		{
			name: "oauth mode rejected if public url missing",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "OAuth mode requires --oauth-public-url",
		},
		{
			name: "oauth mode rejected if public url is not https",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=http://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "must be canonical https:// URL",
		},
		{
			name: "oauth mode rejected if public url has path",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com/oauth",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "must be canonical https:// URL with no path",
		},
		{
			name: "oauth mode rejected if secret file missing",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/nonexistent",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "error reading --oauth-client-secret-file",
		},
		{
			name: "oauth mode rejected if secret file empty",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/empty",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "oauth client secret must not be empty",
		},
		{
			name: "oauth mode rejected if signing key file missing",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/nonexistent",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "error reading --oauth-signing-key-file",
		},
		{
			name: "oauth mode rejected if signing key not 32 bytes",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/badkey",
				"--oauth-allowed-user=alice",
			},
			wantExit:        true,
			wantErrContains: "signing key must be exactly 32 bytes",
		},
		{
			name: "oauth mode rejected if allowed user empty",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=",
			},
			wantExit:        true,
			wantErrContains: "requires non-empty --oauth-allowed-user",
		},
		{
			name: "valid oauth config with raw 32-byte key succeeds and forces read-only",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com/",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/rawkey",
				"--oauth-allowed-user=alice",
			},
			wantExit: false,
		},
		{
			name: "valid oauth config with base64 key succeeds and forces read-only",
			args: []string{
				"--oauth-client-id=client-1",
				"-t=http",
				"--oauth-public-url=https://mcp.example.com",
				"--oauth-client-secret-file=/secrets/secret",
				"--oauth-signing-key-file=/secrets/b64key",
				"--oauth-allowed-user=alice",
			},
			wantExit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				flagPkg.ReadOnly = false
				flagPkg.Mode = "stdio"
				flagPkg.OAuthClientID = ""
				flagPkg.OAuthClientSecretFile = ""
				flagPkg.OAuthClientSecret = ""
				flagPkg.OAuthPublicURL = ""
				flagPkg.OAuthSigningKeyFile = ""
				flagPkg.OAuthSigningKey = nil
				flagPkg.OAuthAllowedUser = ""
				flagPkg.OAuthAllowedRedirectURIs = nil
				oauthClientID = ""
				oauthClientSecretFile = ""
				oauthPublicURL = ""
				oauthSigningKeyFile = ""
				oauthAllowedUser = ""
				oauthAllowedRedirectURIs = ""
			})

			origExit := osExit
			defer func() { osExit = origExit }()
			osExit = func(code int) {
				panic(exitPanic{code: code})
			}

			readFile := func(path string) ([]byte, error) {
				if d, ok := files[path]; ok {
					return d, nil
				}
				return nil, errors.New("file not found")
			}

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var stderr bytes.Buffer

			var panicked bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						if _, ok := r.(exitPanic); ok {
							panicked = true
							return
						}
						panic(r)
					}
				}()
				initFlagSet(fs, tt.args, func(string) string { return "" }, readFile, &stderr)
			}()

			if tt.wantExit {
				if !panicked {
					t.Errorf("T-CFG-1 failed: expected startup exit, but initFlagSet succeeded without exit")
				}
				if tt.wantErrContains != "" && !strings.Contains(stderr.String(), tt.wantErrContains) {
					t.Errorf("T-CFG-1 failed: stderr %q does not contain %q", stderr.String(), tt.wantErrContains)
				}
			} else {
				if panicked {
					t.Fatalf("T-CFG-1 failed: unexpected exit; stderr: %s", stderr.String())
				}
				if !flagPkg.ReadOnly {
					t.Errorf("T-CFG-1 failed: OAuth mode must force ReadOnly=true")
				}
				if len(flagPkg.OAuthSigningKey) != 32 {
					t.Errorf("T-CFG-1 failed: expected 32-byte signing key, got %d", len(flagPkg.OAuthSigningKey))
				}
				if flagPkg.OAuthPublicURL != "https://mcp.example.com" {
					t.Errorf("T-CFG-1 failed: public URL = %q, want https://mcp.example.com", flagPkg.OAuthPublicURL)
				}
			}
		})
	}
}
