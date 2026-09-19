package flag

var (
	Host    string
	Bind    string
	Port    int
	Token   string
	Version string
	Mode    string

	MaxInlineAttachmentBytes int

	Insecure      bool
	ReadOnly      bool
	Debug         bool
	AllowedTools  map[string]struct{}
	AllowedScopes map[string]struct{}

	// OAuth mode configuration
	OAuthClientID            string
	OAuthClientSecretFile    string
	OAuthClientSecret        string
	OAuthPublicURL           string
	OAuthSigningKeyFile      string
	OAuthSigningKey          []byte
	OAuthAllowedUser         string
	OAuthAllowedRedirectURIs []string
)

// DefaultRedirectURIs is the default client redirect URI allowlist.
var DefaultRedirectURIs = []string{
	"https://claude.ai/api/mcp/auth_callback",
	"https://claude.com/api/mcp/auth_callback",
	"https://chatgpt.com/connector_platform_oauth_redirect",
}
