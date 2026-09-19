package operation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gitea.com/gitea/gitea-mcp/operation/actions"
	"gitea.com/gitea/gitea-mcp/operation/issue"
	"gitea.com/gitea/gitea-mcp/operation/label"
	"gitea.com/gitea/gitea-mcp/operation/milestone"
	"gitea.com/gitea/gitea-mcp/operation/notification"
	"gitea.com/gitea/gitea-mcp/operation/packages"
	"gitea.com/gitea/gitea-mcp/operation/pull"
	"gitea.com/gitea/gitea-mcp/operation/repo"
	"gitea.com/gitea/gitea-mcp/operation/search"
	"gitea.com/gitea/gitea-mcp/operation/timetracking"
	"gitea.com/gitea/gitea-mcp/operation/user"
	"gitea.com/gitea/gitea-mcp/operation/version"
	"gitea.com/gitea/gitea-mcp/operation/wiki"
	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"
	"gitea.com/gitea/gitea-mcp/pkg/log"
	"gitea.com/gitea/gitea-mcp/pkg/oauth"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxRequestBodyBytes raises the SDK's 4 MiB default, which is too tight for the
// base64 file content create_or_update_file accepts.
const maxRequestBodyBytes = 32 << 20

// httpReadHeaderTimeout bounds slow header reads without limiting SSE writes.
const httpReadHeaderTimeout = 10 * time.Second

var (
	mcpServer *mcp.Server

	domainTools = []*tool.Tool{
		user.Tool, actions.Tool, repo.Tool, notification.Tool, issue.Tool,
		label.Tool, milestone.Tool, packages.Tool, pull.Tool, search.Tool,
		version.Tool, wiki.Tool, timetracking.Tool,
		repo.FileTool, repo.BranchTool, repo.TagTool, repo.CommitTool, repo.ReleaseTool,
	}
)

// DomainTools returns all registered domain tool groups.
func DomainTools() []*tool.Tool {
	return domainTools
}

func RegisterTool(s *mcp.Server) {
	for _, t := range domainTools {
		for _, registeredTool := range t.Tools() {
			s.AddTool(registeredTool.Tool, registeredTool.MCPHandler())
		}
	}
	tool.WarnUnmatchedAllowedTools(domainTools...)
	tool.WarnUnmatchedAllowedScopes(domainTools...)
}

// parseAuthToken extracts the token from an Authorization header.
// Supports "Bearer <token>" (case-insensitive per RFC 7235) and
// Gitea-style "token <token>" formats.
// Returns the token and true if valid, empty string and false otherwise.
func parseAuthToken(authHeader string) (string, bool) {
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if token != "" {
			return token, true
		}
	}
	if len(authHeader) > 6 && strings.EqualFold(authHeader[:6], "token ") {
		token := strings.TrimSpace(authHeader[6:])
		if token != "" {
			return token, true
		}
	}
	return "", false
}

func getContextWithToken(ctx context.Context, authHeader string) context.Context {
	if authHeader == "" {
		return ctx
	}

	token, ok := parseAuthToken(authHeader)
	if !ok {
		return ctx
	}

	return context.WithValue(ctx, mcpContext.TokenContextKey, token)
}

func authTokenMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if extra := req.GetExtra(); extra != nil {
			if extra.TokenInfo != nil && extra.TokenInfo.Extra != nil {
				if client := extra.TokenInfo.Extra["oauth_client"]; client != nil {
					ctx = context.WithValue(ctx, mcpContext.OAuthClientContextKey, client)
				}
			}
			ctx = getContextWithToken(ctx, extra.Header.Get("Authorization"))
		}
		return next(ctx, method, req)
	}
}

func protectMCPOrigin(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check exempts safe methods, but MCP requires Origin validation on every request.
		checkRequest := r
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			checkRequest = r.Clone(r.Context())
			checkRequest.Method = http.MethodPost
		}
		if err := protection.Check(checkRequest); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newHTTPServer(addr string, s *mcp.Server) *http.Server {
	return newHTTPServerWithHandler(addr, s, nil)
}

func newHTTPServerWithHandler(addr string, s *mcp.Server, wrapMCP func(http.Handler) http.Handler) *http.Server {
	return newHTTPServerWithStore(addr, s, wrapMCP, nil)
}

func newHTTPServerWithStore(addr string, s *mcp.Server, wrapMCP func(http.Handler) http.Handler, store *oauth.Store) *http.Server {
	mux := http.NewServeMux()

	var mcpHandler http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{
			Logger:                       log.Slog(),
			MaxRequestBodyBytes:          maxRequestBodyBytes,
			Stateless:                    true,
			PropagateRequestCancellation: true,
		},
	)
	if wrapMCP != nil {
		mcpHandler = wrapMCP(mcpHandler)
	}

	if flag.OAuthClientID != "" {
		oauthStore := store
		if oauthStore == nil {
			oauthStore = oauth.NewStore(nil)
		}

		mcpHandler = oauth.RequireOAuthToken(oauth.RequireOAuthTokenConfig{
			PublicURL:         flag.OAuthPublicURL,
			GiteaHost:         flag.Host,
			GiteaClientID:     flag.OAuthClientID,
			GiteaClientSecret: flag.OAuthClientSecret,
			Store:             oauthStore,
		}, mcpHandler)

		// AS-8: metadata discovery endpoints (RFC 8414 and RFC 9728)
		mux.HandleFunc("/.well-known/oauth-protected-resource", oauth.HandleProtectedResourceMetadata(flag.OAuthPublicURL))
		mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", oauth.HandleProtectedResourceMetadata(flag.OAuthPublicURL))
		mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.HandleAuthorizationServerMetadata(flag.OAuthPublicURL))
		mux.HandleFunc("/.well-known/openid-configuration", oauth.HandleOpenIDConfiguration())

		// AS-1: dynamic client registration
		mux.HandleFunc("/oauth/register", oauth.HandleRegister(flag.OAuthSigningKey, flag.OAuthAllowedRedirectURIs, nil))

		// AS-2..7, UP-1..3: authorize, callback, token endpoints
		mux.HandleFunc("/oauth/authorize", oauth.HandleAuthorize(oauth.AuthorizeConfig{
			SigningKey:    flag.OAuthSigningKey,
			PublicURL:     flag.OAuthPublicURL,
			GiteaHost:     flag.Host,
			GiteaClientID: flag.OAuthClientID,
			Store:         oauthStore,
		}))
		mux.HandleFunc("/oauth/callback", oauth.HandleCallback(oauth.CallbackConfig{
			GiteaHost:         flag.Host,
			GiteaClientID:     flag.OAuthClientID,
			GiteaClientSecret: flag.OAuthClientSecret,
			PublicURL:         flag.OAuthPublicURL,
			AllowedUser:       flag.OAuthAllowedUser,
			Store:             oauthStore,
		}))
		mux.HandleFunc("/oauth/token", oauth.HandleToken(oauth.TokenConfig{
			SigningKey: flag.OAuthSigningKey,
			PublicURL:  flag.OAuthPublicURL,
			Store:      oauthStore,
		}))
	}

	mux.Handle("/mcp", protectMCPOrigin(mcpHandler))
	mux.HandleFunc("/healthz", handleHealthz)
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func Run() error {
	mcpServer = newMCPServer(flag.Version)
	RegisterTool(mcpServer)
	switch flag.Mode {
	case "stdio":
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			return err
		}
	case "http":
		addr := net.JoinHostPort(flag.Bind, strconv.Itoa(flag.Port))
		httpServer := newHTTPServer(addr, mcpServer)
		log.Infof("Gitea MCP HTTP server listening on %s (stateless, protocol up to 2026-07-28)", addr)

		// Graceful shutdown setup
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		shutdownDone := make(chan struct{})

		go func() {
			<-sigCh
			log.Infof("Shutdown signal received, gracefully stopping HTTP server...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Errorf("HTTP server shutdown error: %v", err)
			}
			close(shutdownDone)
		}()

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		<-shutdownDone // Wait for shutdown to finish
	default:
		return fmt.Errorf("invalid transport type: %s. Must be 'stdio' or 'http'", flag.Mode)
	}
	return nil
}

func newMCPServer(version string) *mcp.Server {
	// SDK keepalives send MCP ping requests and disconnect clients without a
	// server-to-client channel, so KeepAlive stays disabled.
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "Gitea MCP Server",
			Version: version,
		},
		&mcp.ServerOptions{
			Logger: log.Slog(),
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
		},
	)
	s.AddReceivingMiddleware(authTokenMiddleware)
	return s
}
