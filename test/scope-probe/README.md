# Gitea OAuth2 Scope Probe (T-SCOPE-0)

This directory contains the reproducible manual scope probe for verifying OAuth2 read scopes against Gitea.

## Purpose
Layer 1 of read-only enforcement relies on Gitea strictly enforcing read scopes on OAuth2 access tokens. This probe tests whether an OAuth2 token issued with only read scopes (`read:repository read:issue read:user read:organization`) allows or blocks write API requests against Gitea.

---

## Prerequisites
- `bash`, `curl`, and `jq`
- `openssl` (for PKCE S256 code verifier & challenge generation)
- Network access to the target Gitea instance (e.g. `http://127.0.0.1:3000` or your remote instance)

---

## Running Against a Local Docker Instance (Optional)

If running locally with Docker Compose:
```bash
cd test/scope-probe
docker compose up -d
```
1. Open `http://127.0.0.1:3000` in your browser.
2. Complete initial installation (SQLite database, default settings).
3. Register an administrator account.
4. Create a test repository (e.g. `testuser/probe-repo`).

---

## Step-by-Step Probe Instructions

### 1. Register an OAuth2 Application in Gitea
1. Log in to your Gitea account (`http://127.0.0.1:3000` or your remote instance).
2. Go to **Settings** (top-right avatar -> Settings) -> **Applications**.
3. Under **Manage OAuth2 Applications**, enter:
   - **Application Name**: `Scope Probe Test`
   - **Redirect URI**: `http://127.0.0.1:9999/callback`
   - **Confidential Client**: Yes (or leave unchecked for public client)
4. Click **Create Application**.
5. Copy the generated **Client ID** and **Client Secret**.

### 2. Run the Probe Script
From the repository root:
```bash
./test/scope-probe/probe.sh
```

You will be prompted for:
- **Gitea URL**: (e.g. `http://127.0.0.1:3000` or `https://git.example.com`)
- **OAuth2 Client ID**: paste the Client ID from step 1
- **OAuth2 Client Secret**: paste the Client Secret (or leave blank if public)
- **Redirect URI**: `http://127.0.0.1:9999/callback` (or custom redirect URI)

### 3. Authorize in Browser
1. Copy the authorization URL printed by `probe.sh`.
2. Open it in your web browser.
3. Review the permissions requested: `read:repository read:issue read:user read:organization`.
4. Click **Authorize Application**.
5. When your browser redirects to `http://127.0.0.1:9999/callback?code=...&state=...`:
   - Copy the value of the `code` query parameter (or copy the entire address bar URL).
   - Paste it into the `probe.sh` prompt and press Enter.

### 4. Automatic Verification & Results
`probe.sh` automatically:
1. Exchanges the code using PKCE `S256` for an access token.
2. Checks the `scope` field returned in the token response.
3. Calls `GET /api/v1/user` (verifies read access; must return HTTP 200).
4. Attempts `POST /api/v1/user/repos` (attempt to create a repo; must return HTTP 403).
5. Attempts `PATCH /api/v1/repos/{owner}/{repo}` on an existing repo (must return HTTP 403).

### Pass/Fail Criteria
- **PASS**: `GET` returns 200, and both `POST` and `PATCH` return 403 Forbidden.
- **CRITICAL FAILURE**: If `POST` or `PATCH` returns 2xx, the probe exits with status 2 and prints a loud warning. Layer 1 scope enforcement is ineffective on that Gitea version.