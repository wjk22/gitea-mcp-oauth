# Gitea MCP OAuth

**Connect ChatGPT and Claude to self-hosted Gitea through OAuth — with hard read-only access.**

> [!NOTE]
> This repository is an unofficial fork of the official [Gitea MCP Server](https://gitea.com/gitea/gitea-mcp). It adds an OAuth authorization server and strict read-only enforcement for remote web MCP clients such as ChatGPT and Claude. It is not affiliated with or endorsed by the Gitea project.
>
> Design and security model: [`docs/oauth/SPEC.md`](docs/oauth/SPEC.md)  
> Deployment guide: [`deploy/README.md`](deploy/README.md)

[繁體中文](README.zh-tw.md) | [简体中文](README.zh-cn.md)

## Why this exists

The official Gitea MCP server works well with local MCP clients and personal access tokens. Remote web clients such as ChatGPT and Claude, however, need an OAuth-capable MCP endpoint.

This fork adds that missing layer.

It is for people who want AI assistants to inspect repositories, issues, pull requests, commits, releases and other Gitea data **without giving those assistants write access**.

## Highlights

- Works with **ChatGPT custom MCP connectors**
- Works with **Claude web connectors**
- OAuth flow with **PKCE**
- Dynamic client registration for web MCP clients
- OAuth mode is **always read-only**
- Mutating MCP tools are hidden in OAuth mode
- Outbound Gitea requests are guarded by a read-only HTTP transport
- Single-user allowlist
- Gitea credentials stay server-side
- Stateless MCP HTTP transport
- Docker deployment included
- Reverse-proxy configuration included
- Existing PAT/stdio behavior remains available outside OAuth mode

## Security model

OAuth mode intentionally has a narrow trust boundary.

The web client receives an MCP access token, not your Gitea OAuth token.

Read-only access is enforced in multiple layers:

1. Gitea OAuth read scopes
2. write-capable MCP tools are excluded
3. outbound Gitea HTTP requests are restricted to read-safe methods
4. OAuth mode cannot be switched into write mode

The result is a connector that allows ChatGPT or Claude to inspect Gitea while preventing repository modification through the MCP server.

> [!IMPORTANT]
> Read-only access still exposes repository contents. Do not authorize repositories containing secrets you would not want an AI client to read.

## Tested clients

| Client | Status |
|---|---|
| ChatGPT web custom MCP connector | ✅ Working |
| Claude web connector | ✅ Working |
| Local MCP clients / PAT mode | ✅ Upstream-compatible |

## Quick start

### 1. Create a Gitea OAuth application

Create an OAuth2 application in Gitea with callback:

```text
https://your-mcp-host.example/oauth/callback
```

### 2. Configure the server

Copy the deployment example:

```bash
cp deploy/.env.example deploy/.env
```

Set at minimum:

```text
GITEA_HOST=https://git.example.com
GITEA_OAUTH_CLIENT_ID=...
GITEA_OAUTH_PUBLIC_URL=https://mcp.example.com
GITEA_OAUTH_ALLOWED_USER=your-user
```

Store the OAuth client secret and signing key using the provided Docker secret files.

### 3. Start it

```bash
cd deploy
docker compose up -d
```

The MCP endpoint is:

```text
https://mcp.example.com/mcp
```

### 4. Connect ChatGPT

Create a custom MCP app/connector with:

```text
Server URL: https://mcp.example.com/mcp
Authentication: OAuth
```

Complete the Gitea login flow.

### 5. Connect Claude

Add the same MCP endpoint as a custom connector and authenticate through Gitea.

## ChatGPT callback note

Recent ChatGPT connectors use a per-connector callback URI of the form:

```text
https://chatgpt.com/connector/oauth/<callback_id>
```

Your deployment must allow the callback URI pattern expected by the connector. See [`deploy/README.md`](deploy/README.md) for the current deployment details.

## Deployment

See [`deploy/README.md`](deploy/README.md) for the complete Docker, reverse-proxy, TLS, OAuth and troubleshooting guide.

## How this differs from upstream

This repository is based on the official Gitea MCP server:

https://gitea.com/gitea/gitea-mcp

The fork adds:

- OAuth authorization-server functionality for web MCP clients
- PKCE and dynamic client registration
- web-client redirect compatibility
- single-user authorization
- hard read-only OAuth mode
- OAuth-specific security tests
- Docker/reverse-proxy deployment examples for remote MCP use

The original Gitea MCP behavior is retained for local/PAT use where possible.

## OAuth mode

When OAuth mode is enabled, the server forces read-only operation. There is no configuration switch that enables write tools in OAuth mode.

The OAuth-enabled server exposes the metadata and authorization endpoints needed by compatible web MCP clients while keeping upstream Gitea OAuth credentials on the server side.

## MCP protocol and HTTP transport

The server supports MCP up to `2026-07-28` and negotiates down to the client's version, advertising only the `tools` capability.

HTTP is stateless. The MCP endpoint is:

```text
/mcp
```

In OAuth mode, `/mcp` is protected by MCP access tokens issued by this server.

Outside OAuth mode, the original PAT-based HTTP and stdio behavior remains available.

HTTP mode also serves:

```text
/healthz
```

The Docker image includes a built-in health check that runs:

```text
gitea-mcp -healthcheck
```

and verifies `http://127.0.0.1:<port>/healthz`.

## Local / upstream-compatible usage

For local clients and PAT-based workflows, the project retains the original Gitea MCP behavior.

### Claude Code

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

The upstream-style local Docker setup remains available for PAT-based usage:

```json
{
  "mcp": {
    "inputs": [
      {
        "type": "promptString",
        "id": "gitea_token",
        "description": "Gitea Personal Access Token",
        "password": true
      }
    ],
    "servers": {
      "gitea-mcp": {
        "command": "docker",
        "args": ["run", "-i", "--rm", "-e", "GITEA_ACCESS_TOKEN", "docker.gitea.com/gitea-mcp-server"],
        "env": {
          "GITEA_ACCESS_TOKEN": "${input:gitea_token}"
        }
      }
    }
  }
}
```

### OpenCode

```json
"gitea-mcp": {
  "enabled": true,
  "type": "local",
  "command": [
    "gitea-mcp",
    "-t", "stdio",
    "-H", "https://gitea.com",
    "-T", "<your personal access token>"
  ]
}
```

### Other clients

A local stdio configuration can look like:

```json
{
  "mcpServers": {
    "gitea": {
      "command": "gitea-mcp",
      "args": ["-t", "stdio", "--host", "https://gitea.com"],
      "env": {
        "GITEA_ACCESS_TOKEN": "<your personal access token>"
      }
    }
  }
}
```

Or for non-OAuth HTTP mode:

```json
{
  "mcpServers": {
    "gitea": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer <your personal access token>"
      }
    }
  }
}
```

## Installation and building

### OAuth deployment

OAuth mode requires building from this repository. See [`deploy/README.md`](deploy/README.md).

### Build from source

Requires Go 1.27 or later:

```bash
git clone https://github.com/wjk22/gitea-mcp-oauth.git
cd gitea-mcp-oauth
make install
```

### Upstream binaries and images

If you only need the original local/PAT behavior, the official upstream project also publishes its own binaries and Docker image:

https://gitea.com/gitea/gitea-mcp

Those upstream artifacts do **not** include this fork's OAuth additions.

## Configuration

Pass the Gitea host and access token as command-line flags or environment variables; flags take precedence.

Run:

```bash
gitea-mcp --help
```

for the full list of options.

Logs are written to:

```text
$HOME/.gitea-mcp/gitea-mcp.log
```

Use `-d` for debug logging.

## Tool filtering

Tools with `Write` access are hidden when the server runs in read-only mode (`-r` / `GITEA_READONLY`).

The exposed tool set can also be filtered by scope:

```bash
gitea-mcp -S issue,pull_request
```

or by individual tool name:

```bash
gitea-mcp --scope repository,branch --tools get_me
```

With neither filter set, every tool allowed by the active mode loads.


## Available Tools

| Tool                         | Scope        | Access | Description |
| :--------------------------- | :----------- | :----- | :---------- |
| get_gitea_mcp_server_version | version      | Read   | Get the Gitea MCP server version |
| get_me                       | user         | Read   | Get the current authenticated user |
| get_user_orgs                | user         | Read   | List the current user's organizations |
| search_users                 | search       | Read   | Search for users |
| search_org_teams             | search       | Read   | Search teams within an organization |
| search_repos                 | search       | Read   | Search for repositories |
| search_issues                | search       | Read   | Search issues and pull requests across repositories |
| notification_read            | notification | Read   | Read notifications: list (optionally scoped to a repo) or get a thread by ID |
| notification_write           | notification | Write  | Mark a notification or all notifications as read |
| label_read                   | label        | Read   | Read repository or organization labels |
| label_write                  | label        | Write  | Write labels (repo or org): create, edit, delete |
| milestone_read               | milestone    | Read   | Read milestones: get one or list |
| milestone_write              | milestone    | Write  | Write milestones: create, update, delete |
| wiki_read                    | wiki         | Read   | Read wiki: list pages, get content, revision history |
| wiki_write                   | wiki         | Write  | Write wiki pages: create, update, delete |
| timetracking_read            | timetracking | Read   | Read time tracking: issue/repo times, active stopwatches, your tracked times |
| timetracking_write           | timetracking | Write  | Write time tracking: stopwatches and entries |
| package_read                 | packages     | Read   | Read package registry: list packages, list versions, or get a version |
| package_write                | packages     | Write  | Delete a package version (irreversible) |
| list_issues                  | issue        | Read   | List repository issues |
| attachment_read              | issue        | Read   | Read issue/comment attachments: list metadata, get metadata, or download content |
| issue_read                   | issue        | Read   | Read issue: details, comments, or labels |
| issue_write                  | issue        | Write  | Write issues: create, update, manage comments and labels |
| list_pull_requests           | pull_request | Read   | List repository pull requests |
| pull_request_read            | pull_request | Read   | Read pull request: details, diff, files, status, reviews, review comments |
| pull_request_write           | pull_request | Write  | Write pull requests: create, update, close, reopen, merge, update branch, manage reviewers |
| pull_request_review_write    | pull_request | Write  | Write PR reviews: create, submit, delete, dismiss, reply to and resolve review comments |
| actions_config_read          | actions      | Read   | Read Actions secrets and variables |
| actions_config_write         | actions      | Write  | Write Actions secrets and variables: upsert, create, update, delete |
| actions_run_read             | actions      | Read   | Read Actions workflows, runs, jobs, logs, and artifacts |
| actions_run_write            | actions      | Write  | Write Actions runs: dispatch, cancel, rerun |
| create_repo                  | repository   | Write  | Create a new repository |
| fork_repo                    | repository   | Write  | Fork a repository |
| list_my_repos                | repository   | Read   | List repositories owned by the current user |
| list_org_repos               | repository   | Read   | List repositories in an organization |
| get_repository_tree          | repository   | Read   | Get the repository file tree |
| get_file_contents            | file         | Read   | Get file content and metadata |
| get_dir_contents             | file         | Read   | Get the entries in a directory |
| create_or_update_file        | file         | Write  | Create or update a file (provide sha to update an existing file) |
| delete_file                  | file         | Write  | Delete a file |
| create_branch                | branch       | Write  | Create a new branch |
| delete_branch                | branch       | Write  | Delete a branch |
| list_branches                | branch       | Read   | List repository branches |
| rename_branch                | branch       | Write  | Rename a branch |
| create_tag                   | tag          | Write  | Create a tag |
| delete_tag                   | tag          | Write  | Delete a tag |
| get_tag                      | tag          | Read   | Get tag details |
| list_tags                    | tag          | Read   | List repository tags |
| list_commits                 | commit       | Read   | List repository commits |
| get_commit                   | commit       | Read   | Get commit details |
| create_release               | release      | Write  | Create a release |
| delete_release               | release      | Write  | Delete a release |
| get_release                  | release      | Read   | Get a release by ID |
| get_latest_release           | release      | Read   | Get the latest release |
| list_releases                | release      | Read   | List repository releases |

> **Note:** Several tools are consolidated, action-based tools, a single tool exposes multiple operations through a `method` parameter. Tools with `Write` access are hidden when the server runs in read-only mode (`-r` / `GITEA_READONLY`), and the exposed tool set can be filtered by scope with `-S` / `--scope` (`GITEA_SCOPES`) and/or by individual tool name with `-O` / `--tools` (`GITEA_TOOLS`).

With neither flag set, every tool loads. `--scope` limits loading to tools whose Scope column value is in the given list; `--tools` limits loading to the named tools; setting both loads the union of the selected scopes and the individually named tools. Unknown scope names are ignored with a startup warning.

```bash
gitea-mcp -S issue,pull_request
gitea-mcp --scope repository,branch --tools get_me
```

Many tools accept `page` and `per_page` for pagination. The maximum effective page size is the Gitea server's `[api].MAX_RESPONSE_ITEMS` setting (default **50**), larger values are silently capped.

## Security notes

The main security goals are:

- Gitea tokens never leave the MCP server
- OAuth mode is always read-only
- web clients receive MCP tokens rather than upstream Gitea credentials
- redirect URIs are validated
- PKCE is required
- only the configured Gitea user may authorize
- read-only enforcement exists below the MCP tool layer
- OAuth/PAT modes do not silently mix

See [`docs/oauth/SPEC.md`](docs/oauth/SPEC.md) for the current design.

## License

This project is based on the official Gitea MCP server and remains licensed under the MIT License.

See [`LICENSE`](LICENSE).

## Upstream

Official project:

https://gitea.com/gitea/gitea-mcp
