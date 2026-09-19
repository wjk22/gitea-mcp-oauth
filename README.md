# Gitea MCP Server

> [!NOTE]
> This repository is an unofficial fork of [gitea-mcp](https://gitea.com/gitea/gitea-mcp) adding an OAuth 2.1 authorization server and strict read-only enforcement designed for remote web MCP clients (e.g. Claude, ChatGPT). It is not affiliated with or endorsed by the Gitea project. Design and security model: [docs/oauth/SPEC.md](docs/oauth/SPEC.md). See [deploy/README.md](deploy/README.md) for production deployment instructions and [SECURITY.md](SECURITY.md) for vulnerability reporting.
>
> **ChatGPT support status:** ChatGPT support is untested end to end (Claude at claude.ai is tested). Custom remote MCP connectors require supported ChatGPT plans (reported by ChatGPT itself as Pro/Business rather than Plus; not verified against official documentation) and a per-connector redirect URI (`https://chatgpt.com/connector/oauth/{callback_id}`, per [OpenAI documentation](https://developers.openai.com/apps-sdk/build/auth)) which must be added to `GITEA_OAUTH_ALLOWED_REDIRECT_URIS`. In addition, ChatGPT may attempt to use Client ID Metadata Documents rather than DCR; this server supports DCR only. See [deploy/README.md](deploy/README.md#chatgpt-support) for details.

[繁體中文](README.zh-tw.md) | [简体中文](README.zh-cn.md)

**Gitea MCP Server** connects a [Gitea](https://about.gitea.com) instance to [Model Context Protocol](https://modelcontextprotocol.io) clients, so repositories, issues, pull requests and more can be browsed and managed from an MCP-compatible chat interface.

[![Install with Docker in VS Code](https://img.shields.io/badge/VS_Code-Install_Server-0098FF?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}) [![Install with Docker in VS Code Insiders](https://img.shields.io/badge/VS_Code_Insiders-Install_Server-24bfa5?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}&quality=insiders)

## Installation

OAuth mode requires building from this repository; see [deploy/README.md](deploy/README.md).

Download a binary from the [releases page](https://gitea.com/gitea/gitea-mcp/releases) and put it in your `PATH`, use the `docker.gitea.com/gitea-mcp-server` image, or build from source into `$GOPATH/bin` with `make` and Go 1.27 or later:

```bash
git clone https://gitea.com/gitea/gitea-mcp.git
cd gitea-mcp
make install
```

## Configuration

Pass the Gitea host and access token as command-line flags or environment variables, flags take precedence. Run `gitea-mcp --help` for the full list of flags and environment variables. Logs are written to `$HOME/.gitea-mcp/gitea-mcp.log`, add `-d` for debug logging.

### MCP protocol and HTTP transport

The server supports MCP up to `2026-07-28` and negotiates down to the client's version, advertising only the `tools` capability. Tool and Gitea failures return a `tools/call` result with `result.isError: true`, while malformed requests and server faults stay JSON-RPC errors.

HTTP is always stateless: `/mcp` accepts POST only, without `Mcp-Session-Id`, standalone SSE or `Last-Event-ID` resumability. Origins are validated, and reverse proxies must forward `Mcp-Protocol-Version`, `Mcp-Method` and `Mcp-Name` unchanged. `Authorization: Bearer <token>` and `Authorization: token <token>` pass a Gitea credential per request, which is credential passthrough rather than MCP OAuth.

HTTP mode also serves `/healthz`, which returns `200 OK` when the server is up. The Docker image's built-in `HEALTHCHECK` runs `gitea-mcp -healthcheck`, which dials `http://127.0.0.1:<port>/healthz` using the same `-p`/`-port` value (or `8080` by default) and exits `0` on success or `1` on failure. Stdio deployments do not serve `/healthz`, so override or disable the image's `HEALTHCHECK` when running in stdio mode.

### Claude Code

Runs the server through `go run` and requires [Go](https://go.dev):

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

Use the install buttons at the top of this README, or add the block below to your User Settings (JSON), reachable via `Ctrl + Shift + P` and `Preferences: Open User Settings (JSON)`. It also works in a workspace `.vscode/mcp.json`, where the `mcp` key is omitted.

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

Add the following to the top-level `mcp` object of your [OpenCode](https://opencode.ai) config:

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

### Mistral Vibe

Add the following to `~/.vibe/config.toml`:

```toml
[[mcp_servers]]
name = "gitea"
transport = "stdio"
command = "docker"
args = ["run", "--rm", "-i", "-e", "GITEA_ACCESS_TOKEN", "-e", "GITEA_HOST", "docker.gitea.com/gitea-mcp-server"]

[mcp_servers.env]
GITEA_ACCESS_TOKEN = "TOKEN"
GITEA_HOST = "https://gitea.com"
```

### Other clients

Clients such as Cursor take either a stdio command:

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

or an http endpoint, for a server started with `gitea-mcp -t http --port 8080`:

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

Once configured, try `list all my repositories` in the chat box.

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
