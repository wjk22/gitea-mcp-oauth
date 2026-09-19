package oauth_test

import (
	"fmt"
	"testing"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/oauth"
)

func TestStore_PendingAuth(t *testing.T) {
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	p := &oauth.PendingAuth{
		UpstreamState:    "state-1",
		ClientID:         "client-1",
		RedirectURI:      "https://example.com/cb",
		ClientState:      "client-state-1",
		CodeChallenge:    "challenge-1",
		UpstreamVerifier: "verifier-1",
		ExpiresAt:        now.Add(10 * time.Minute),
	}
	store.PutPendingAuth(p)

	// First consume succeeds
	got, ok := store.ConsumePendingAuth("state-1")
	if !ok || got == nil || got.ClientID != "client-1" {
		t.Fatalf("expected to consume pending auth, got ok=%v, p=%v", ok, got)
	}

	// Second consume fails (single-use)
	_, ok = store.ConsumePendingAuth("state-1")
	if ok {
		t.Fatal("expected pending auth to be consumed only once")
	}

	// Expired pending auth fails
	pExpired := &oauth.PendingAuth{
		UpstreamState: "state-expired",
		ExpiresAt:     now.Add(-1 * time.Second),
	}
	store.PutPendingAuth(pExpired)
	_, ok = store.ConsumePendingAuth("state-expired")
	if ok {
		t.Fatal("expected expired pending auth to fail consumption")
	}
}

func TestStore_AuthCode(t *testing.T) {
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	c := &oauth.AuthCode{
		Code:          "code-1",
		ClientID:      "client-1",
		RedirectURI:   "https://example.com/cb",
		CodeChallenge: "challenge-1",
		GrantID:       "grant-1",
		ExpiresAt:     now.Add(60 * time.Second),
	}
	store.PutAuthCode(c)

	// First consume succeeds
	got, ok := store.ConsumeAuthCode("code-1")
	if !ok || got == nil || got.GrantID != "grant-1" {
		t.Fatalf("expected to consume auth code, got ok=%v, c=%v", ok, got)
	}

	// Second consume fails (atomic single-use)
	_, ok = store.ConsumeAuthCode("code-1")
	if ok {
		t.Fatal("expected auth code to be consumed only once")
	}

	// Expired code fails
	cExpired := &oauth.AuthCode{
		Code:      "code-expired",
		ExpiresAt: now.Add(-1 * time.Second),
	}
	store.PutAuthCode(cExpired)
	_, ok = store.ConsumeAuthCode("code-expired")
	if ok {
		t.Fatal("expected expired auth code to fail consumption")
	}
}

func TestStore_GrantsAndTokenCascade(t *testing.T) {
	now := time.Now()
	store := oauth.NewStore(func() time.Time { return now })

	grant := &oauth.Grant{
		ID:                "grant-1",
		GiteaAccessToken:  "gitea-access",
		GiteaRefreshToken: "gitea-refresh",
		ExpiresAt:         now.Add(1 * time.Hour),
		UserLogin:         "alice",
	}
	store.PutGrant(grant)

	g, ok := store.GetGrant("grant-1")
	if !ok || g.UserLogin != "alice" {
		t.Fatalf("expected to get grant, got ok=%v, g=%v", ok, g)
	}

	rawToken := "access-token-123"
	hash := oauth.HashToken(rawToken)
	tok := &oauth.MCPToken{
		TokenHash: hash,
		Type:      oauth.TokenTypeAccess,
		GrantID:   "grant-1",
		ClientID:  "client-1",
		ExpiresAt: now.Add(1 * time.Hour),
		Audience:  "https://test/mcp",
	}
	store.PutMCPToken(tok)

	retrieved, ok := store.GetMCPToken(hash)
	if !ok || retrieved == nil {
		t.Fatal("expected to retrieve MCP token")
	}

	// Delete grant should cascade delete associated tokens
	store.DeleteGrant("grant-1")

	_, ok = store.GetGrant("grant-1")
	if ok {
		t.Fatal("expected grant to be deleted")
	}

	_, ok = store.GetMCPToken(hash)
	if ok {
		t.Fatal("expected MCP token to be deleted on grant cascade deletion")
	}
}

func TestStore_TokenLazyExpiryAndRevocation(t *testing.T) {
	now := time.Now()
	currentTime := now
	store := oauth.NewStore(func() time.Time { return currentTime })

	hash := oauth.HashToken("refresh-tok")
	tok := &oauth.MCPToken{
		TokenHash: hash,
		Type:      oauth.TokenTypeRefresh,
		GrantID:   "grant-2",
		ClientID:  "client-2",
		ExpiresAt: now.Add(30 * time.Minute),
	}
	store.PutMCPToken(tok)

	// Advance time past expiry
	currentTime = now.Add(31 * time.Minute)
	_, ok := store.GetMCPToken(hash)
	if ok {
		t.Fatal("expected token to be expired and removed")
	}

	// Revoke token directly
	hash2 := oauth.HashToken("active-tok")
	tok2 := &oauth.MCPToken{
		TokenHash: hash2,
		Type:      oauth.TokenTypeAccess,
		ExpiresAt: now.Add(10 * time.Hour),
	}
	store.PutMCPToken(tok2)
	store.RevokeMCPToken(hash2)
	_, ok = store.GetMCPToken(hash2)
	if ok {
		t.Fatal("expected revoked token to be gone")
	}
}

func TestStore_PendingAuth_CapAndSweep(t *testing.T) {
	now := time.Now()
	currentTime := now
	store := oauth.NewStore(func() time.Time { return currentTime })

	// Insert 1000 items (MaxPendingAuths)
	for i := range oauth.MaxPendingAuths {
		ok := store.PutPendingAuth(&oauth.PendingAuth{
			UpstreamState: fmt.Sprintf("state-%d", i),
			ClientID:      "client-1",
			ExpiresAt:     now.Add(10 * time.Minute),
		})
		if !ok {
			t.Fatalf("expected insert %d to succeed, got false", i)
		}
	}

	// 1001st insert should fail (cap reached)
	ok := store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState: "state-overflow",
		ClientID:      "client-1",
		ExpiresAt:     now.Add(10 * time.Minute),
	})
	if ok {
		t.Fatal("expected 1001st insert to fail when cap is reached, got true")
	}

	// Advance time past expiry
	currentTime = now.Add(11 * time.Minute)

	// Next insert should sweep all 1000 expired entries and succeed
	ok = store.PutPendingAuth(&oauth.PendingAuth{
		UpstreamState: "state-after-sweep",
		ClientID:      "client-1",
		ExpiresAt:     currentTime.Add(10 * time.Minute),
	})
	if !ok {
		t.Fatal("expected insert after sweep to succeed, got false")
	}

	// Verify the new entry can be consumed
	p, ok := store.ConsumePendingAuth("state-after-sweep")
	if !ok || p == nil {
		t.Fatal("expected to consume entry inserted after sweep")
	}
}

func TestStore_AuthCode_CapAndSweep(t *testing.T) {
	now := time.Now()
	currentTime := now
	store := oauth.NewStore(func() time.Time { return currentTime })

	// Insert 1000 items (MaxAuthCodes)
	for i := range oauth.MaxAuthCodes {
		ok := store.PutAuthCode(&oauth.AuthCode{
			Code:      fmt.Sprintf("code-%d", i),
			ClientID:  "client-1",
			ExpiresAt: now.Add(60 * time.Second),
		})
		if !ok {
			t.Fatalf("expected insert %d to succeed, got false", i)
		}
	}

	// 1001st insert should fail (cap reached)
	ok := store.PutAuthCode(&oauth.AuthCode{
		Code:      "code-overflow",
		ClientID:  "client-1",
		ExpiresAt: now.Add(60 * time.Second),
	})
	if ok {
		t.Fatal("expected 1001st insert to fail when cap is reached, got true")
	}

	// Advance time past expiry
	currentTime = now.Add(61 * time.Second)

	// Next insert should sweep all 1000 expired entries and succeed
	ok = store.PutAuthCode(&oauth.AuthCode{
		Code:      "code-after-sweep",
		ClientID:  "client-1",
		ExpiresAt: currentTime.Add(60 * time.Second),
	})
	if !ok {
		t.Fatal("expected insert after sweep to succeed, got false")
	}

	// Verify the new entry can be consumed
	c, ok := store.ConsumeAuthCode("code-after-sweep")
	if !ok || c == nil {
		t.Fatal("expected to consume entry inserted after sweep")
	}
}
