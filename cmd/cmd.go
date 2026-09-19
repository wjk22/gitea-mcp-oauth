package cmd

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"gitea.com/gitea/gitea-mcp/operation"
	flagPkg "gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/log"
)

var (
	host                            string
	bind                            string
	port                            int
	token                           string
	tools                           string
	scopes                          string
	version                         bool
	healthcheck                     bool
	maxInlineAttachmentBytes        int
	maxInlineAttachmentBytesFlagSet bool
	osExit                          = os.Exit
	healthcheckClient               = http.DefaultClient

	oauthClientID            string
	oauthClientSecretFile    string
	oauthPublicURL           string
	oauthSigningKeyFile      string
	oauthAllowedUser         string
	oauthAllowedRedirectURIs string
)

func initFlagSet(fs *flag.FlagSet, args []string, getenv func(string) string, readFile func(string) ([]byte, error), stderr io.Writer) {
	fs.StringVar(&flagPkg.Mode, "t", "stdio", "")
	fs.StringVar(&flagPkg.Mode, "transport", "stdio", "")
	fs.StringVar(&host, "H", getenv("GITEA_HOST"), "")
	fs.StringVar(&host, "host", getenv("GITEA_HOST"), "")
	fs.StringVar(&bind, "b", "", "")
	fs.StringVar(&bind, "bind", "", "")
	fs.IntVar(&port, "p", 8080, "")
	fs.IntVar(&port, "port", 8080, "")
	fs.StringVar(&token, "T", "", "")
	fs.StringVar(&token, "token", "", "")
	fs.BoolVar(&flagPkg.ReadOnly, "r", false, "")
	fs.BoolVar(&flagPkg.ReadOnly, "read-only", false, "")
	defaultTools := getenv("GITEA_TOOLS")
	fs.StringVar(&tools, "O", defaultTools, "")
	fs.StringVar(&tools, "tools", defaultTools, "")
	defaultScopes := getenv("GITEA_SCOPES")
	fs.StringVar(&scopes, "S", defaultScopes, "")
	fs.StringVar(&scopes, "scope", defaultScopes, "")
	fs.BoolVar(&flagPkg.Debug, "d", false, "")
	fs.BoolVar(&flagPkg.Debug, "debug", false, "")
	fs.BoolVar(&flagPkg.Insecure, "k", false, "")
	fs.BoolVar(&flagPkg.Insecure, "insecure", false, "")
	fs.BoolVar(&version, "v", false, "")
	fs.BoolVar(&version, "version", false, "")
	fs.BoolVar(&healthcheck, "healthcheck", false, "")
	maxInlineAttachmentBytes = 5 * 1024 * 1024
	fs.Func("max-inline-attachment-bytes", "", func(val string) error {
		parsed, err := strconv.Atoi(val)
		if err != nil || parsed < 0 {
			return fmt.Errorf("invalid value %q", val)
		}
		maxInlineAttachmentBytes = parsed
		maxInlineAttachmentBytesFlagSet = true
		return nil
	})
	fs.StringVar(&oauthClientID, "oauth-client-id", getenv("GITEA_OAUTH_CLIENT_ID"), "")
	fs.StringVar(&oauthClientSecretFile, "oauth-client-secret-file", getenv("GITEA_OAUTH_CLIENT_SECRET_FILE"), "")
	fs.StringVar(&oauthPublicURL, "oauth-public-url", getenv("GITEA_OAUTH_PUBLIC_URL"), "")
	fs.StringVar(&oauthSigningKeyFile, "oauth-signing-key-file", getenv("GITEA_OAUTH_SIGNING_KEY_FILE"), "")
	fs.StringVar(&oauthAllowedUser, "oauth-allowed-user", getenv("GITEA_OAUTH_ALLOWED_USER"), "")
	fs.StringVar(&oauthAllowedRedirectURIs, "oauth-allowed-redirect-uris", getenv("GITEA_OAUTH_ALLOWED_REDIRECT_URIS"), "")

	fs.Usage = func() {
		w := tabwriter.NewWriter(stderr, 0, 0, 3, ' ', 0)
		fmt.Fprintln(stderr, "Usage: gitea-mcp [options]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		fmt.Fprintf(w, "  -t, -transport <type>\tTransport type: stdio or http (default: stdio)\n")
		fmt.Fprintf(w, "  -H, -host <url>\tGitea host URL (default: https://gitea.com)\n")
		fmt.Fprintf(w, "  -b, -bind <address>\tHTTP listen address, e.g. 127.0.0.1 (default: all interfaces)\n")
		fmt.Fprintf(w, "  -p, -port <number>\tHTTP server port (default: 8080)\n")
		fmt.Fprintf(w, "  -T, -token <token>\tPersonal access token\n")
		fmt.Fprintf(w, "  -r, -read-only\tExpose only read-only tools\n")
		fmt.Fprintf(w, "  -O, -tools <names>\tComma-separated list of tool names to expose\n")
		fmt.Fprintf(w, "  -S, -scope <names>\tComma-separated list of tool scopes to expose\n")
		fmt.Fprintf(w, "  -d, -debug\tEnable debug mode\n")
		fmt.Fprintf(w, "  -k, -insecure\tIgnore TLS certificate errors\n")
		fmt.Fprintf(w, "  -max-inline-attachment-bytes <bytes>\tInline image attachments up to this size (default: 5242880)\n")
		fmt.Fprintf(w, "  -v, -version\tPrint version and exit\n")
		fmt.Fprintf(w, "  -healthcheck\tCheck a running HTTP server's /healthz endpoint and exit\n")
		fmt.Fprintf(w, "  --oauth-client-id <id>\tGitea OAuth2 app client ID (enables OAuth mode)\n")
		fmt.Fprintf(w, "  --oauth-client-secret-file <path>\tPath to file containing Gitea app client secret\n")
		fmt.Fprintf(w, "  --oauth-public-url <url>\tCanonical https:// base URL of gitea-mcp\n")
		fmt.Fprintf(w, "  --oauth-signing-key-file <path>\tPath to file containing 32-byte signing key\n")
		fmt.Fprintf(w, "  --oauth-allowed-user <username>\tThe single Gitea login allowed to authorize\n")
		fmt.Fprintf(w, "  --oauth-allowed-redirect-uris <urls>\tComma-separated client redirect URI allowlist override\n")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Environment variables:")
		fmt.Fprintf(w, "  GITEA_ACCESS_TOKEN\tProvide access token\n")
		fmt.Fprintf(w, "  GITEA_ACCESS_TOKEN_FILE\tPath to a file containing the access token (e.g. a Docker secret)\n")
		fmt.Fprintf(w, "  GITEA_DEBUG\tSet to 'true' for debug mode\n")
		fmt.Fprintf(w, "  GITEA_HOST\tOverride Gitea host URL\n")
		fmt.Fprintf(w, "  GITEA_INSECURE\tSet to 'true' to ignore TLS errors\n")
		fmt.Fprintf(w, "  GITEA_MAX_INLINE_ATTACHMENT_BYTES\tOverride inline image attachment size limit in bytes\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_ALLOWED_REDIRECT_URIS\tComma-separated client redirect URI allowlist override\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_ALLOWED_USER\tThe single Gitea login allowed to authorize\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_CLIENT_ID\tGitea OAuth2 app client ID (enables OAuth mode)\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_CLIENT_SECRET_FILE\tPath to file containing Gitea app client secret\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_PUBLIC_URL\tCanonical https:// base URL of gitea-mcp\n")
		fmt.Fprintf(w, "  GITEA_OAUTH_SIGNING_KEY_FILE\tPath to file containing 32-byte signing key\n")
		fmt.Fprintf(w, "  GITEA_READONLY\tSet to 'true' for read-only mode\n")
		fmt.Fprintf(w, "  GITEA_SCOPES\tComma-separated list of tool scopes to expose\n")
		fmt.Fprintf(w, "  GITEA_TOOLS\tComma-separated list of tool names to expose\n")
		fmt.Fprintf(w, "  MCP_MODE\tOverride transport mode\n")
		_ = w.Flush()
	}

	_ = fs.Parse(args)

	flagPkg.Host = host
	if flagPkg.Host == "" {
		flagPkg.Host = "https://gitea.com"
	}

	flagPkg.Bind = bind
	flagPkg.Port = port
	flagPkg.MaxInlineAttachmentBytes = maxInlineAttachmentBytes

	flagPkg.Token = token
	if flagPkg.Token == "" {
		flagPkg.Token = getenv("GITEA_ACCESS_TOKEN")
	}
	if flagPkg.Token == "" {
		if tokenFile := getenv("GITEA_ACCESS_TOKEN_FILE"); tokenFile != "" {
			data, err := readFile(tokenFile)
			if err != nil {
				fmt.Fprintf(stderr, "error reading GITEA_ACCESS_TOKEN_FILE: %v\n", err)
				osExit(1)
			}
			flagPkg.Token = strings.TrimRight(string(data), "\r\n")
		}
	}

	if getenv("MCP_MODE") != "" {
		flagPkg.Mode = getenv("MCP_MODE")
	}
	if getenv("GITEA_READONLY") == "true" {
		flagPkg.ReadOnly = true
	}

	allowed := map[string]struct{}{}
	for t := range strings.SplitSeq(tools, ",") {
		if t = strings.TrimSpace(t); t != "" {
			allowed[t] = struct{}{}
		}
	}
	if len(allowed) > 0 {
		flagPkg.AllowedTools = allowed
	}

	allowedScopes := map[string]struct{}{}
	for s := range strings.SplitSeq(scopes, ",") {
		if s = normalizeScope(s); s != "" {
			allowedScopes[s] = struct{}{}
		}
	}
	if len(allowedScopes) > 0 {
		flagPkg.AllowedScopes = allowedScopes
	}
	if getenv("GITEA_DEBUG") == "true" {
		flagPkg.Debug = true
	}
	if getenv("GITEA_INSECURE") == "true" {
		flagPkg.Insecure = true
	}
	if !maxInlineAttachmentBytesFlagSet {
		if val := getenv("GITEA_MAX_INLINE_ATTACHMENT_BYTES"); val != "" {
			parsed, err := strconv.Atoi(val)
			if err != nil || parsed < 0 {
				fmt.Fprintf(stderr, "invalid GITEA_MAX_INLINE_ATTACHMENT_BYTES: %q\n", val)
				osExit(1)
			}
			flagPkg.MaxInlineAttachmentBytes = parsed
		}
	}

	flagPkg.OAuthClientID = oauthClientID
	if flagPkg.OAuthClientID != "" {
		if flagPkg.Mode != "http" {
			fmt.Fprintf(stderr, "error: OAuth mode requires transport 'http', got %q\n", flagPkg.Mode)
			osExit(1)
			return
		}

		if oauthPublicURL == "" {
			fmt.Fprintf(stderr, "error: OAuth mode requires --oauth-public-url\n")
			osExit(1)
			return
		}
		u, err := url.Parse(oauthPublicURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") {
			fmt.Fprintf(stderr, "error: invalid --oauth-public-url %q: must be canonical https:// URL with no path\n", oauthPublicURL)
			osExit(1)
			return
		}
		if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			fmt.Fprintf(stderr, "error: invalid --oauth-public-url %q: must not contain query, fragment, or user info\n", oauthPublicURL)
			osExit(1)
			return
		}
		flagPkg.OAuthPublicURL = strings.TrimRight(oauthPublicURL, "/")

		if oauthClientSecretFile == "" {
			fmt.Fprintf(stderr, "error: OAuth mode requires --oauth-client-secret-file\n")
			osExit(1)
			return
		}
		secretData, err := readFile(oauthClientSecretFile)
		if err != nil {
			fmt.Fprintf(stderr, "error reading --oauth-client-secret-file: %v\n", err)
			osExit(1)
			return
		}
		trimmedSecret := strings.TrimSpace(string(secretData))
		if trimmedSecret == "" {
			fmt.Fprintf(stderr, "error: oauth client secret must not be empty\n")
			osExit(1)
			return
		}
		flagPkg.OAuthClientSecretFile = oauthClientSecretFile
		flagPkg.OAuthClientSecret = trimmedSecret

		if oauthSigningKeyFile == "" {
			fmt.Fprintf(stderr, "error: OAuth mode requires --oauth-signing-key-file\n")
			osExit(1)
			return
		}
		keyData, err := readFile(oauthSigningKeyFile)
		if err != nil {
			fmt.Fprintf(stderr, "error reading --oauth-signing-key-file: %v\n", err)
			osExit(1)
			return
		}
		signingKey, err := parseSigningKey(keyData)
		if err != nil {
			fmt.Fprintf(stderr, "error validating --oauth-signing-key-file: %v\n", err)
			osExit(1)
			return
		}
		flagPkg.OAuthSigningKeyFile = oauthSigningKeyFile
		flagPkg.OAuthSigningKey = signingKey

		trimmedUser := strings.TrimSpace(oauthAllowedUser)
		if trimmedUser == "" {
			fmt.Fprintf(stderr, "error: OAuth mode requires non-empty --oauth-allowed-user\n")
			osExit(1)
			return
		}
		flagPkg.OAuthAllowedUser = trimmedUser

		if oauthAllowedRedirectURIs != "" {
			var uris []string
			for uri := range strings.SplitSeq(oauthAllowedRedirectURIs, ",") {
				if uri = strings.TrimSpace(uri); uri != "" {
					uris = append(uris, uri)
				}
			}
			flagPkg.OAuthAllowedRedirectURIs = uris
		} else {
			flagPkg.OAuthAllowedRedirectURIs = flagPkg.DefaultRedirectURIs
		}

		// OAuth mode forces read-only; there is no override.
		flagPkg.ReadOnly = true
	}
}

func parseSigningKey(data []byte) ([]byte, error) {
	if len(data) == 32 {
		return data, nil
	}
	trimmed := strings.TrimSpace(string(data))
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if decoded, err := enc.DecodeString(trimmed); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if len(trimmed) == 32 {
		return []byte(trimmed), nil
	}
	return nil, fmt.Errorf("signing key must be exactly 32 bytes (got %d bytes)", len(data))
}

// normalizeScope trims whitespace, lowercases, and converts internal spaces
// and hyphens to underscores, so "Pull Request", "pull-request", and
// "PULL_REQUEST" all normalize to "pull_request".
func normalizeScope(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func Execute() {
	initFlagSet(flag.CommandLine, os.Args[1:], os.Getenv, os.ReadFile, os.Stderr)
	if version {
		fmt.Fprintln(os.Stdout, flagPkg.Version)
		return
	}
	if healthcheck {
		if runHealthcheck(healthcheckClient, flagPkg.Port, os.Stdout, os.Stderr) {
			osExit(0)
		} else {
			osExit(1)
		}
		return
	}
	if err := operation.Run(); err != nil {
		if err == context.Canceled {
			log.Info("Server shutdown due to context cancellation")
			_ = log.Default().Sync() // best-effort flush
			return
		}
		_ = log.Default().Sync() // best-effort flush
		log.Fatalf("Run Gitea MCP Server Error: %v", err)
	}
	_ = log.Default().Sync() // best-effort flush
}
