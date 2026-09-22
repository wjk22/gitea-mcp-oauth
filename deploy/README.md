# Deploying gitea-mcp-oauth

Reference deployment for running gitea-mcp in OAuth mode (read-only) behind an Apache2 reverse proxy that terminates TLS.

## Architecture

- **Host:** Linux server with Docker and Apache2.
- **Reverse proxy:** Apache2 serves `https://mcp.example.com` (Let's Encrypt) and proxies to `http://127.0.0.1:3040/`.
- **Container `gitea-mcp-oauth`:** listens on `127.0.0.1:3040` only; never exposed publicly.
- **Secrets:** the Gitea client secret and the signing key are files mounted as Docker secrets. No secret value appears in `.env`, the compose file or environment variables.
- **Authentication:** stateless client registration (AS-1), PKCE S256 (AS-3), login federated to Gitea with fixed read scopes, a single allowed Gitea user.

Deployment directory in this guide: `/opt/gitea-mcp-oauth` (adjust as needed). It holds `docker-compose.yml`, `.env`, `client.secret` and `signing.key`.

---

## Deployment Steps

### 1. Build and push the image

From the repository root, tag with a version or commit SHA (never `latest`) and push to your registry, e.g. your Gitea container registry:

```bash
VERSION="v0.1.0"
docker build -t git.example.com/username/gitea-mcp-oauth:${VERSION} --build-arg VERSION=${VERSION} .
docker login git.example.com
docker push git.example.com/username/gitea-mcp-oauth:${VERSION}
```

### 2. Create the Gitea OAuth2 app

In Gitea: **Settings → Applications → Manage OAuth2 Applications**.

1. **Application name:** `gitea-mcp-oauth`.
2. **Redirect URIs:** exactly one line, `https://mcp.example.com/oauth/callback`. The Claude and ChatGPT callbacks do **not** go here; they belong in `GITEA_OAUTH_ALLOWED_REDIRECT_URIS` (step 4).
3. **Confidential client:** checked.
4. Create the application. Record the **Client ID** (for `.env`). The **Client Secret** is shown only once; put it straight into `client.secret` in step 3.

Scopes are not configured in Gitea; gitea-mcp-oauth requests `read:repository read:issue read:user read:organization` at login.

### 3. Create the secret files

On the server, in the deployment directory:

```bash
cd /opt/gitea-mcp-oauth

# Signing key: 32 random bytes, base64. Keep it stable (see Operational Notes).
head -c 32 /dev/urandom | base64 > signing.key

# Client secret: paste it at the silent prompt, then press Enter.
# Nothing is echoed, nothing lands in shell history, no editor swap files.
read -rs s && printf '%s' "$s" > client.secret && unset s
```

**Permissions.** Compose (without Swarm) mounts secrets as bind mounts with the host owner and mode. The files must be readable by the user the container runs as. Find it:

```bash
docker image inspect -f '{{.Config.User}}' "$(grep '^GITEA_MCP_IMAGE=' .env | cut -d= -f2)"
```

(Run this after step 4, or substitute the image name.) If it prints a UID such as `65532` (or `65532:65532`), give the files to that UID; if it prints nothing, the container runs as root:

```bash
chown 65532:65532 client.secret signing.key   # use the UID printed above; skip if root
chmod 0400 client.secret signing.key
```

If the permissions are wrong, the container exits at startup with an error that a secret file is missing or unreadable.

### 4. Configure `.env`

```bash
cp .env.example .env
chmod 600 .env
```

Set:

- `GITEA_MCP_IMAGE` — the tag pushed in step 1.
- `GITEA_HOST` — public Gitea URL, e.g. `https://git.example.com`.
- `GITEA_OAUTH_CLIENT_ID` — from step 2.
- `GITEA_OAUTH_PUBLIC_URL` — e.g. `https://mcp.example.com` (https, no path).
- `GITEA_OAUTH_ALLOWED_USER` — your Gitea login.
- `GITEA_OAUTH_ALLOWED_REDIRECT_URIS` — required, comma-separated exact values. Start with the two Claude URIs from `.env.example`; add ChatGPT's per-connector URI when you add that connector (see Connector Setup).
- `GITEA_OAUTH_CLIENT_SECRET_FILE`, `GITEA_OAUTH_SIGNING_KEY_FILE` — keep the defaults `./client.secret`, `./signing.key`.

### 5. Apache2 reverse proxy

1. Copy `apache-mcp.conf` to `/etc/apache2/sites-available/`, replace `mcp.example.com`, check the certificate paths. The vhost must keep: `flushpackets=on timeout=300` on `ProxyPass`, `SetEnv no-gzip 1`, and the access log format without query strings or Referer.
2. Enable:

```bash
sudo a2enmod proxy proxy_http ssl
sudo a2ensite apache-mcp.conf
sudo apache2ctl configtest
sudo systemctl reload apache2
```

### 6. Start and check

```bash
docker compose up -d
docker compose logs -f gitea-mcp-oauth

curl -fsS http://127.0.0.1:3040/healthz
# ok

curl -fsS https://mcp.example.com/.well-known/oauth-authorization-server
curl -fsS https://mcp.example.com/.well-known/oauth-protected-resource/mcp

curl -i https://mcp.example.com/mcp
# HTTP 401 with a WWW-Authenticate header
```

---

## Connector Setup

### Claude (claude.ai / Claude Desktop)

1. **Settings → Connectors → Add custom connector**, URL `https://mcp.example.com/mcp`.
2. Log in to Gitea when prompted and authorize.
3. Claude uses `https://claude.ai/api/mcp/auth_callback` or `https://claude.com/api/mcp/auth_callback`; both must be in `GITEA_OAUTH_ALLOWED_REDIRECT_URIS`.

### ChatGPT

Works end to end on ChatGPT Plus with developer mode enabled (Settings → Apps & Connectors → Advanced → Developer mode; ChatGPT may move this location).

1. Create a connector with URL `https://mcp.example.com/mcp` and OAuth authentication.
2. The first attempt fails with `invalid_redirect_uri` and shows the exact callback URI `https://chatgpt.com/connector/oauth/<callback_id>`. Append it to `GITEA_OAUTH_ALLOWED_REDIRECT_URIS` in `.env`:
   ```bash
   GITEA_OAUTH_ALLOWED_REDIRECT_URIS=https://claude.ai/api/mcp/auth_callback,https://claude.com/api/mcp/auth_callback,https://chatgpt.com/connector/oauth/<callback_id>
   ```
3. Apply the change: `docker compose up -d`.
4. Retry the connector; complete the authorization in ChatGPT.

The callback ID belongs to that connector. A newly created connector gets a new ID that must be added the same way; IDs of deleted connectors can be removed from the list later.

Recreating the container clears all sessions, so other connected clients (e.g. Claude) must reconnect.

ChatGPT used Dynamic Client Registration in this test; Client ID Metadata Documents (CIMD) are not supported, see Troubleshooting.

---

## Operational Notes

### Restarts

Grants, authorization codes and MCP tokens live in memory by design. After a restart or recreate, all sessions are gone; click **Reconnect** in claude.ai or ChatGPT. Client registrations survive because they are signed with `signing.key`.

### The signing key

`signing.key` signs the client IDs gitea-mcp-oauth issues to Claude and ChatGPT. Keep it stable: if it changes or is lost, all client IDs become invalid and the connectors must be removed and re-added. Treat it as a secret: anyone holding it can forge client IDs with arbitrary redirect URIs, bypassing the allowlist. Back it up only to protected storage.

### Upgrading Gitea

Read scopes were verified on Gitea 1.27.3 with the scope probe in `test/scope-probe/`. Before upgrading production Gitea, run the scope probe in `test/scope-probe/` against a local container of the new version and confirm writes are rejected.

---

## Troubleshooting

- **Container exits at startup:** check `docker compose logs gitea-mcp-oauth`. Usual causes: a secret file unreadable by the container user (step 3), a missing `.env` value, or a signing key that is not 32 bytes after base64 decoding.
- **`invalid_redirect_uri` (HTTP 400) at registration:** the client presented a redirect URI not in `GITEA_OAUTH_ALLOWED_REDIRECT_URIS`. The logs show the exact URI; add it exactly (no trailing slash, no wildcard) and recreate the container.
- **ChatGPT: invalid client ID at authorize:** ChatGPT may use a Client ID Metadata Document (CIMD, a URL as `client_id`) instead of Dynamic Client Registration. Our metadata advertises DCR only, so DCR is expected; if the logs show a URL-shaped client ID, this is the cause.
- **HTTP 503 on `/oauth/authorize`:** pending authorizations are capped at 1000 in memory. If someone floods the endpoint, legitimate logins get 503 until entries expire (10 minutes). Per-IP rate limiting can be added in Apache (e.g. `mod_qos`).
- **Connector hangs on tool calls:** check that Apache is not buffering (`flushpackets=on`, `SetEnv no-gzip 1`).

---

## End-to-End Checklist

- [ ] **claude.ai:** add the connector, log in to Gitea, tools are listed, read a file from a repository.
- [ ] **ChatGPT:** same, after adding its callback URI.
- [ ] **Denylist:** denied tools/methods are not offered (e.g. `actions_run_read` has no `download_job_log`).
- [ ] **Restart:** `docker compose restart`; the next tool call fails with an auth error; reconnect works.
- [ ] **Other user:** authorizing while logged in to Gitea as a different account is rejected.
- [ ] **Logs:** `/var/log/apache2/gitmcp_access.log` shows no query strings (`?code=`, `?state=`) and no token values; `docker compose logs` shows no tokens, codes or secrets.
- [ ] **File read range:** read a large file with `start_line=1`, `end_line=40`: exactly 40 lines, `total_lines` present.
- [ ] **File read cap:** read the same large file with no range: `truncated: true` and a `next_start_line` that continues correctly.
- [ ] **File read text:** read a small file: plain text, no `encoding`, no `html_url`, no `download_url`.
- [ ] **Reconnect after redeploy:** reconnect the connector in claude.ai and ChatGPT so they pick up the new `get_dir_contents` schema.
- [ ] **Directory root:** `get_dir_contents` on a repo with no `path`: the top-level entries are listed.
- [ ] **Directory subdirectory:** `get_dir_contents` with `path` set to a subdirectory: unchanged behaviour.
