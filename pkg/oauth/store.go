package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// PendingAuth holds state for an in-flight authorization redirect toward Gitea (UP-1).
type PendingAuth struct {
	UpstreamState       string
	ClientID            string
	RedirectURI         string
	ClientState         string
	CodeChallenge       string
	CodeChallengeMethod string
	UpstreamVerifier    string
	ExpiresAt           time.Time
}

// AuthCode holds state for an issued authorization code awaiting exchange (AS-5).
type AuthCode struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	GrantID       string
	ExpiresAt     time.Time
}

// Grant holds the upstream Gitea credentials and expiry for an authorized user (UP-4).
type Grant struct {
	mu                sync.Mutex
	ID                string
	GiteaAccessToken  string
	GiteaRefreshToken string
	ExpiresAt         time.Time
	UserLogin         string
}

// Lock acquires the grant's mutex for serialized refresh operations (UP-4).
func (g *Grant) Lock() {
	g.mu.Lock()
}

// Unlock releases the grant's mutex.
func (g *Grant) Unlock() {
	g.mu.Unlock()
}

// TokenType distinguishes MCP access tokens from refresh tokens.
type TokenType string

const (
	TokenTypeAccess  TokenType = "access_token"
	TokenTypeRefresh TokenType = "refresh_token"
)

// MCPToken represents an issued MCP token, indexed by SHA-256 hash (AS-7).
type MCPToken struct {
	TokenHash [32]byte
	Type      TokenType
	GrantID   string
	ClientID  string
	ExpiresAt time.Time
	Audience  string
}

// Store provides thread-safe in-memory storage for the OAuth authorization server and resource server.
type Store struct {
	mu           sync.RWMutex
	nowFunc      func() time.Time
	pendingAuths map[string]*PendingAuth // upstreamState -> PendingAuth
	authCodes    map[string]*AuthCode    // code -> AuthCode
	grants       map[string]*Grant       // grantID -> Grant
	tokens       map[[32]byte]*MCPToken  // sha256(token) -> MCPToken
}

// NewStore creates a new in-memory OAuth store.
func NewStore(nowFunc func() time.Time) *Store {
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &Store{
		nowFunc:      nowFunc,
		pendingAuths: make(map[string]*PendingAuth),
		authCodes:    make(map[string]*AuthCode),
		grants:       make(map[string]*Grant),
		tokens:       make(map[[32]byte]*MCPToken),
	}
}

// HashToken computes the SHA-256 hash of an opaque token string.
func HashToken(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

// GenerateRandomString generates an opaque URL-safe string containing n random bytes.
func GenerateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random string: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Maximum in-memory capacities for pending authorizations and authorization codes (AS-5, UP-1).
const (
	MaxPendingAuths = 1000
	MaxAuthCodes    = 1000
)

// PutPendingAuth sweeps expired entries and stores a pending authorization record (UP-1).
// Returns false if the pending authorization store has reached MaxPendingAuths.
func (s *Store) PutPendingAuth(p *PendingAuth) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowFunc()
	for k, v := range s.pendingAuths {
		if now.After(v.ExpiresAt) {
			delete(s.pendingAuths, k)
		}
	}

	if len(s.pendingAuths) >= MaxPendingAuths {
		return false
	}

	s.pendingAuths[p.UpstreamState] = p
	return true
}

// ConsumePendingAuth atomically removes and returns a pending authorization record (UP-2).
// Returns nil, false if unknown or expired.
func (s *Store) ConsumePendingAuth(upstreamState string) (*PendingAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.pendingAuths[upstreamState]
	if !exists {
		return nil, false
	}
	delete(s.pendingAuths, upstreamState)

	if s.nowFunc().After(p.ExpiresAt) {
		return nil, false
	}
	return p, true
}

// PutAuthCode sweeps expired entries and stores an issued authorization code (AS-5).
// Returns false if the auth code store has reached MaxAuthCodes.
func (s *Store) PutAuthCode(c *AuthCode) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowFunc()
	for k, v := range s.authCodes {
		if now.After(v.ExpiresAt) {
			delete(s.authCodes, k)
		}
	}

	if len(s.authCodes) >= MaxAuthCodes {
		return false
	}

	s.authCodes[c.Code] = c
	return true
}

// ConsumeAuthCode atomically removes and returns an authorization code record (AS-5).
// Once consumed, the code cannot be reused (T-TK-1).
// Returns nil, false if not found or expired.
func (s *Store) ConsumeAuthCode(code string) (*AuthCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, exists := s.authCodes[code]
	if !exists {
		return nil, false
	}
	delete(s.authCodes, code)

	if s.nowFunc().After(c.ExpiresAt) {
		return nil, false
	}
	return c, true
}

// PutGrant stores an upstream grant.
func (s *Store) PutGrant(g *Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[g.ID] = g
}

// GetGrant retrieves a grant by ID.
func (s *Store) GetGrant(id string) (*Grant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, exists := s.grants[id]
	return g, exists
}

// GrantCount returns the number of active grants currently stored.
func (s *Store) GrantCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.grants)
}

// DeleteGrant removes a grant and all associated MCP tokens (UP-4).
func (s *Store) DeleteGrant(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.grants, id)
	for hash, tok := range s.tokens {
		if tok.GrantID == id {
			delete(s.tokens, hash)
		}
	}
}

// PutMCPToken stores an MCP access or refresh token keyed by SHA-256 hash.
func (s *Store) PutMCPToken(tok *MCPToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok.TokenHash] = tok
}

// GetMCPToken retrieves an MCP token by its SHA-256 hash.
// If expired, the token is lazily deleted and nil, false is returned.
func (s *Store) GetMCPToken(tokenHash [32]byte) (*MCPToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, exists := s.tokens[tokenHash]
	if !exists {
		return nil, false
	}
	if s.nowFunc().After(tok.ExpiresAt) {
		delete(s.tokens, tokenHash)
		return nil, false
	}
	return tok, true
}

// ConsumeMCPRefreshToken atomically validates, removes and returns an MCP refresh token record (AS-6, T-TK-3).
// Validates token type, client and expiry, deletes the token and returns the record under one lock.
func (s *Store) ConsumeMCPRefreshToken(tokenHash [32]byte, clientID string) (*MCPToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, exists := s.tokens[tokenHash]
	if !exists || tok == nil || tok.Type != TokenTypeRefresh || tok.ClientID != clientID {
		return nil, false
	}
	delete(s.tokens, tokenHash)

	if s.nowFunc().After(tok.ExpiresAt) {
		return nil, false
	}
	return tok, true
}

// RevokeMCPToken removes an individual MCP token (e.g. rotated refresh token, AS-6).
func (s *Store) RevokeMCPToken(tokenHash [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, tokenHash)
}
