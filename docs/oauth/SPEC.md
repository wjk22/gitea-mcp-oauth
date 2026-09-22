# gitea-mcp OAuth — Specification (Revision 7)

This document describes what the OAuth mode guarantees and what it deliberately does not do. Hardening that is not required here is listed in `docs/oauth/BACKLOG.md` and is deliberately not implemented.

**Revision 5** adds shaped file reads (FR-1..FR-6) to reduce cost for AI clients. **Revision 6** adds handling of null and fractional numeric arguments (FR-7, FR-7a, FR-7b) and strengthens related tests (T-FR-1, T-FR-4, T-FR-5, T-FR-8, T-FR-9). **Revision 7** adds directory listing of the repository root (DR-1..DR-3, T-DR-1..T-DR-3).

## Goal

Let claude.ai and ChatGPT web connectors read a single user's Gitea through gitea-mcp over HTTPS, read-only, authorized via OAuth. Gitea has no Dynamic Client Registration, so gitea-mcp acts as a minimal OAuth 2.1 authorization server: it accepts DCR, federates login to Gitea through one pre-registered Gitea OAuth2 app, and issues its own tokens. Gitea tokens never leave gitea-mcp.

## Non-goals (Revision 4)

Persistence across restarts (a restart means reconnecting the connector), consent page, repo/org allowlists, rate limiting, audit log, revocation endpoint, multiple users, horizontal scaling, OIDC. See `BACKLOG.md`.

## Configuration

| Flag | Env | Description |
|---|---|---|
| `--oauth-client-id` | `GITEA_OAUTH_CLIENT_ID` | Gitea OAuth2 app client ID. Setting it enables OAuth mode. |
| `--oauth-client-secret-file` | `GITEA_OAUTH_CLIENT_SECRET_FILE` | File containing the Gitea app client secret. |
| `--oauth-public-url` | `GITEA_OAUTH_PUBLIC_URL` | Canonical `https://` base URL of gitea-mcp, no path. Issuer and audience derive from it; never from request headers. |
| `--oauth-signing-key-file` | `GITEA_OAUTH_SIGNING_KEY_FILE` | 32 random bytes (raw or base64). Signs client IDs (see AS-1). Must survive restarts. |
| `--oauth-allowed-user` | `GITEA_OAUTH_ALLOWED_USER` | The single Gitea login allowed to authorize. |
| `--oauth-allowed-redirect-uris` | `GITEA_OAUTH_ALLOWED_REDIRECT_URIS` | Overrides the default client redirect URI allowlist. |

**Startup fails** in OAuth mode if: transport mode is not `http`; public URL missing, not `https://`, or has a path; secret file or signing key missing or unreadable; key not 32 bytes; allowed user empty. OAuth mode forces read-only; there is no override.

**Upstream Gitea scopes** (fixed, not configurable): `read:repository read:issue read:user read:organization`. Verified enforced on Gitea 1.27.3 with the scope probe in `test/scope-probe/`.

**Default client redirect URI allowlist**:
`https://claude.ai/api/mcp/auth_callback`, `https://claude.com/api/mcp/auth_callback`, `https://chatgpt.com/connector_platform_oauth_redirect`. New ChatGPT connectors use a per-connector URI `https://chatgpt.com/connector/oauth/<callback_id>`, which must be configured explicitly; the legacy URI in the defaults applies to older connectors only.

## Flow

1. Client calls `/mcp` without token → **401** with `WWW-Authenticate: Bearer resource_metadata="<public>/.well-known/oauth-protected-resource/mcp"`.
2. Client reads protected resource metadata (RFC 9728) and authorization server metadata (RFC 8414).
3. Client registers at `POST /oauth/register` → receives a signed client ID.
4. Client sends the user to `/oauth/authorize` with PKCE → gitea-mcp redirects to Gitea with its own `state` and PKCE.
5. Gitea redirects to `/oauth/callback` → gitea-mcp exchanges the code, checks the user, issues its own authorization code, redirects to the client.
6. Client exchanges the code at `/oauth/token` → MCP access + refresh token.
7. Client calls `/mcp` with the MCP token → gitea-mcp calls Gitea with the stored Gitea token, read-only.

## Requirements

### Authorization server

- **AS-1 Stateless client registration with redirect allowlist.** `POST /oauth/register` accepts RFC 7591 JSON. Every `redirect_uri` must exactly match an entry on the allowlist (HTTPS, no wildcard, no prefix match, no mode that relaxes this); otherwise `400 invalid_redirect_uri`. The client ID is `base64url(JSON{redirect_uris, token_endpoint_auth_method, iat})` plus an HMAC-SHA256 over it with the signing key; nothing is stored, so registrations survive restarts. For confidential methods (`client_secret_post`, `client_secret_basic`) the client secret is `base64url(HMAC-SHA256(key, "secret:" + client_id))`. Supported methods: `none`, `client_secret_post`, `client_secret_basic`.
- **AS-2 Authorize validation.** Invalid signature on `client_id`, or a `redirect_uri` not contained in the signed client ID → HTML error, **no redirect**. After the redirect URI is trusted, other errors redirect back with an RFC 6749 error and the client's `state`.
- **AS-3 PKCE.** `code_challenge` required, `code_challenge_method` must be `S256`.
- **AS-4 Resource.** If `resource` is present at authorize or token, it must equal `<public>/mcp`; otherwise `invalid_target`. Tokens are always issued for audience `<public>/mcp`.
- **AS-5 Authorization codes.** 32 random bytes, single use, 60 seconds, bound to client ID, redirect URI, code challenge and the grant. Consumed atomically. Capped at 1000 in memory; expired entries are removed on every insert; when the cap is reached, authorization code creation fails with 503.
- **AS-6 Token endpoint.** `authorization_code`: verify client authentication per the method in the client ID, code unused and unexpired, exact redirect URI, PKCE verifier. `refresh_token`: verify client, issue a new pair, invalidate the old refresh token. Errors: `invalid_client` (401), `invalid_grant` (400). Response `Cache-Control: no-store`; never contains a Gitea token.
- **AS-7 Tokens.** Opaque, 32 random bytes, base64url. Stored in memory keyed by SHA-256 hash. Access token 1 hour, refresh token 30 days. All lost on restart by design.
- **AS-8 Issuer and metadata.** Issuer equals `--oauth-public-url`. AS metadata: `issuer`, `authorization_endpoint`, `token_endpoint`, `registration_endpoint`, `response_types_supported: ["code"]`, `grant_types_supported: ["authorization_code","refresh_token"]`, `code_challenge_methods_supported: ["S256"]`, `token_endpoint_auth_methods_supported: ["none","client_secret_post","client_secret_basic"]`. Protected resource metadata: `resource: <public>/mcp`, `authorization_servers: [<public>]`, `bearer_methods_supported: ["header"]`. `/.well-known/openid-configuration` returns 404.

### Upstream (Gitea)

- **UP-1 Separate state and PKCE.** Fresh random `state` and PKCE S256 verifier toward Gitea, stored in a pending authorization (in memory, 10 minutes, single use). The client's `state` is never sent to Gitea. Capped at 1000 in memory; expired entries are removed on every insert; when the cap is reached, `/oauth/authorize` returns 503.
- **UP-2 Callback.** Unknown, expired or reused upstream `state` → HTML error, no redirect. Gitea `error=access_denied` → redirect to the client with `access_denied` and the client's `state`. Code exchange with explicit 10-second timeout.
- **UP-3 User check.** After exchange, `GET /api/v1/user`; the login must equal `--oauth-allowed-user` (case-insensitive). Otherwise error page and the Gitea tokens are discarded.
- **UP-4 Refresh.** Before a tool call, if the Gitea access token expires within 5 minutes, refresh it. One refresh at a time per grant (mutex). If Gitea rejects the refresh, delete the grant and all MCP tokens for it; the client gets 401 and reconnects.

### Resource server (`/mcp`)

- **RS-1** Missing, unknown, expired or wrong-audience token → 401 with the `WWW-Authenticate` header from the Flow section, plus `error="invalid_token"` when a token was sent.
- **RS-2** Token only from the `Authorization: Bearer` header.
- **RS-3** OAuth mode never accepts PAT authentication and never falls through to an unauthenticated handler. Until token validation exists, every `/mcp` request in OAuth mode returns 401.
- **RS-4** Each request gets its own Gitea client built from the grant's Gitea token, with the read-only transport fixed at construction (not dependent on the global `flag.ReadOnly`). No per-user token in any global variable.

### Logging

- **LOG-1** Never log `Authorization`, `Cookie`, `Set-Cookie`, request bodies of `/oauth/*`, or query strings of `/oauth/*`.

### File reading (Revision 5)

- **FR-1 Decoded text.** For a text file, `content` is UTF-8 text, never base64. The response contains `name`, `path`, `sha`, `type`, `size`, `content` and the range fields of FR-3. It contains no `encoding`, `html_url` or `download_url`.
- **FR-2 Binary.** A file is binary if the first 8000 bytes contain a NUL byte or the content is not valid UTF-8. For a binary file the response has no `content`, has `binary: true`, keeps `name`, `path`, `sha`, `type`, `size`, and is not an error.
- **FR-3 Line range.** `start_line` (default 1) and `end_line` (default last line) are 1-based and inclusive. The response reports `total_lines` and the range actually returned as `start_line` and `end_line`. A trailing newline does not add an extra line. `end_line` beyond the file is clamped. These are tool errors with a clear message: `start_line` < 1; `end_line` < `start_line`; `start_line` > `total_lines` (the message names `total_lines`). An empty file returns `total_lines: 0` and empty `content` without error.
- **FR-4 Size cap.** `max_bytes` (default 32768, maximum 262144, larger values are clamped, values < 1 are an error) limits the returned `content` after range selection. If the selection is larger, cut after the last complete line that fits and set `truncated: true` and `next_start_line` to the first line not returned; `end_line` then reports the last line returned. If the first line alone exceeds the cap, cut at a valid UTF-8 boundary, set `truncated: true`, and omit `next_start_line`. Never cut inside a UTF-8 character.
- **FR-5 Line numbers.** With `withLines=true` each returned line is prefixed with its original line number and a tab (`N<TAB>text`). Numbers refer to the original file even when a range is requested. The JSON-array-of-objects format is removed. The cap of FR-4 applies to the emitted content including prefixes.
- **FR-6 Nothing else changes.** Tool name, required parameters, read-only classification and the output of every other tool stay as they are. If Gitea returns no content for the path (directory, submodule, symlink, oversized blob), the response is metadata only, as today, and not an error.
- **FR-7 Optional numeric arguments.** For `start_line`, `end_line` and `max_bytes`: an absent key or a JSON `null` means "not given" and behaves exactly as if the key were absent. A whole number is accepted, including a `float64` with no fractional part (`40`, `40.0`). A numeric string is accepted as today. A number with a fractional part (`1.5`), `NaN`, an infinity, or a value outside the range of `int` is an error that names the parameter. Any other type is an error that names the parameter. Range rules of FR-3 and FR-4 are unchanged.
- **FR-7a Upper bound.** A numeric argument given as a `float64` at or above 2^63 is an error that names the parameter and says `out of range`, exactly like other out-of-range values. `float64(math.MaxInt)` rounds up to 2^63, so the bound is compared with `>=`.
- **FR-7b Native int bounds.** A numeric argument (`float64` or numeric string) must fit in the platform's `int`. A whole number in `[-2^(IntSize-1), 2^(IntSize-1)-1]` is accepted; a whole number outside that range is an error that names the parameter and says `out of range`. This holds on every architecture (`strconv.IntSize` gives the width). Fractions, NaN, infinities and non-numeric strings keep their errors from FR-7.

### Directory listing (Revision 7)

- **DR-1 Root listing.** For `get_dir_contents`, `path` is optional. Omitted, empty, `/` and `.` all mean the repository root. Any other value is used as today.
- **DR-2 Same output.** A root listing has the same shape as any other directory listing (`name`, `path`, `type`, `size` per entry, as produced by `slimDirEntries`).
- **DR-3 Nothing else changes.** `get_file_contents` still requires a non-empty `path` and still fails with the current error when it is missing. The read-only classification, all other tools and `params.GetString` are unchanged.

## Read-Only Enforcement

OAuth mode forces read-only.

1. **Layer 1: Upstream scopes** as listed under Configuration. Gitea returns no `scope` field in the token response, so scope enforcement is verified by the manual probe in `test/scope-probe/`, not at runtime.
2. **Layer 2: Tool registration.** Every tool declares its access class (`RegisterRead`/`RegisterWrite`) and scope kind. For compound tools with a `method` parameter, every method declares its own scope kind; unknown or missing method calls are denied; a test enumerates methods from each tool's schema enum and fails on any undeclared method. Write tools are not registered in read-only mode. The read-only set is pinned by `pkg/tool/testdata/readonly_tools.golden` (sorted tool names and `tool:method` pairs).
   - **OAuth denylist.** In OAuth mode these entries are additionally not exposed: `actions_run_read:download_job_log`, `actions_run_read:get_job_log_preview`, `actions_run_read:download_artifact`, `attachment_read:download`, all methods of `actions_config_read`, all methods of `notification_read`, all methods of `package_read`. The OAuth-mode set is pinned by `pkg/tool/testdata/oauth_tools.golden`.
   - Scope kinds are declared but not enforced in Revision 4 (repo/org allowlists are in `BACKLOG.md`).
3. **Layer 3: HTTP transport.** `readOnlyTransport` rejects every method other than `GET` and `HEAD` before dispatch; redirects to other hosts are refused. All Gitea calls go through it.
4. **Layer 4: Annotations.** All exposed tools carry `ReadOnlyHint: true`, `DestructiveHint: false`.

## Tests (required)

| ID | Test | Expected |
|---|---|---|
| T-CFG-1 | Each startup rule under Configuration violated | Startup fails |
| T-RS-3 | OAuth mode: `/mcp` with no token, a random token, and a valid Gitea PAT | 401, handler never invoked |
| T-META | Metadata fields; issuer unaffected by spoofed `Host` / `X-Forwarded-Host` | Exact values |
| T-DCR-1 | Register with non-allowlisted or non-HTTPS redirect URI | 400 |
| T-DCR-2 | Client ID with tampered payload or signature | Rejected at authorize and token |
| T-AZ-1 | Authorize with unregistered redirect URI (incl. trailing slash / case / query variants) | Error page, no `Location` header |
| T-AZ-2 | Missing PKCE or `plain` method | Rejected |
| T-CB-1 | Callback with unknown, expired or reused state | Error page, no redirect |
| T-CB-2 | Gitea user ≠ allowed user | Rejected, tokens discarded |
| T-TK-1 | Code reused | `invalid_grant` |
| T-TK-2 | Code with wrong client, wrong redirect URI, wrong verifier, or expired | `invalid_grant` / `invalid_client` |
| T-TK-3 | Refresh: new pair issued; old refresh token rejected | As stated |
| T-TK-4 | Token response contains no substring of the Gitea tokens | Pass |
| T-RS-1 | Expired, unknown, wrong-audience token | 401 `invalid_token` |
| T-RS-4 | Two concurrent requests for the same grant near expiry | Exactly one upstream refresh |
| T-RO-OAUTH | OAuth-mode tool set equals `oauth_tools.golden` | Pass |
| T-LOG-1 | Full flow with debug logging | No token, code, secret or verifier in logs |
| T-E2E | Happy path through a fake Gitea: register → authorize → callback → token → tool call | Tool result returned |
| T-FR-1 | Multi-line UTF-8 fixture round-trips through `ShapeFileContent`: base64 in, identical text out; `FormatFileContentResult` map has no `encoding`, `html_url`, `download_url` (handler-level assertions in T-FR-8) | Pass |
| T-FR-2 | Fixture with NUL byte and fixture with invalid UTF-8 | `binary: true`, no `content`, no error |
| T-FR-3 | Whole file, middle range, clamped end, start beyond end, start < 1, end < start, newlines, empty file | As stated |
| T-FR-4 | File larger than `max_bytes` pages correctly with round-trip byte-equality for several cap values (37, 100, 150, 400); multi-byte boundary cut; defaults, maximum, cap < 1 | As stated |
| T-FR-5 | Line prefix format `N<TAB>text`; range numbering; cap counts prefixes (proven: `max_bytes=20` on two `12345678\n` lines truncates with prefixes but not without) | As stated |
| T-FR-6 | `get_dir_contents` byte-identical; nil-content yields metadata only; read-only tests pass | Pass |
| T-FR-7 | Tool schema differs only by the three new optional parameters, not in `required` | Pass |
| T-FR-8 | FR-7 and FR-7a, handler level: `float64` args accepted; `null` args treated as absent; output has no `encoding`, `html_url`, `download_url`; a numeric string gives the same result as the float form. Every rejected value (fractional `start_line`/`end_line`/`max_bytes`, `NaN`, infinity, `1e30`, `float64(1<<63)`, a boolean, a non-numeric string) is asserted to set `IsError`, to name the parameter, to say `whole number` or `out of range`, and not to return a shaped file response | Pass |
| T-FR-9 | FR-7b at helper level (`getOptionalFileArg`), table-driven, expectations per `strconv.IntSize`; run under `GOARCH=386` and native | Accepted values returned, out-of-range values error naming the parameter |
| T-DR-1 | `get_dir_contents` with `path` omitted, `""`, `"/"`, `.`, handler level against a stub | Exactly one request to the root contents endpoint each (path asserted); slim listing |
| T-DR-2 | `get_dir_contents` with `docs/oauth` | Same request as before (nested contents endpoint) |
| T-DR-3 | `get_dir_contents` schema; `get_file_contents` with empty `path` | Only `path` no longer required; "path is required" error kept |

## Deployment

Behind a reverse proxy with TLS. A restart invalidates all sessions; reconnect the connector in claude.ai/ChatGPT.
