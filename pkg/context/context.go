package context

type contextKey string

const (
	TokenContextKey       = contextKey("token")
	OAuthClientContextKey = contextKey("oauthClient")
)
