# Gitea MCP 服务器

[English](README.md) | [繁體中文](README.zh-tw.md)

**Gitea MCP 服务器** 将 [Gitea](https://about.gitea.com) 实例接入 [Model Context Protocol](https://modelcontextprotocol.io) 客户端，让仓库、问题、拉取请求等都能在兼容 MCP 的聊天界面中浏览和管理。

[![在 VS Code 中使用 Docker 安装](https://img.shields.io/badge/VS_Code-Install_Server-0098FF?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}) [![在 VS Code Insiders 中使用 Docker 安装](https://img.shields.io/badge/VS_Code_Insiders-Install_Server-24bfa5?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}&quality=insiders)

## 安装

可从 [发布页面](https://gitea.com/gitea/gitea-mcp/releases) 下载二进制文件并放入 `PATH`，或使用 `docker.gitea.com/gitea-mcp-server` 镜像，也可用 `make` 和 Go 1.27 及以上从源码构建到 `$GOPATH/bin`：

```bash
git clone https://gitea.com/gitea/gitea-mcp.git
cd gitea-mcp
make install
```

## 配置

Gitea 主机和访问令牌可通过命令行参数或环境变量提供，命令行参数优先。运行 `gitea-mcp --help` 可查看完整的参数与环境变量列表。日志写入 `$HOME/.gitea-mcp/gitea-mcp.log`，加上 `-d` 可启用调试日志。

### MCP 协议与 HTTP 传输

服务器支持最高至 `2026-07-28` 的 MCP 协议，并向下协商到客户端的版本，仅声明 `tools` 能力。工具和 Gitea 执行失败会在 `tools/call` 结果中返回并设置 `result.isError: true`，格式错误的请求和服务器故障仍返回 JSON-RPC 错误。

HTTP 传输固定为无状态：`/mcp` 仅接受 POST，没有 `Mcp-Session-Id`、独立 SSE 和 `Last-Event-ID` 断点续传。服务器会验证来源，反向代理必须原样转发 `Mcp-Protocol-Version`、`Mcp-Method` 和 `Mcp-Name`。`Authorization: Bearer <令牌>` 和 `Authorization: token <令牌>` 会在每个请求中传递 Gitea 凭据，这是凭据透传，而不是 MCP OAuth。

HTTP 模式还提供 `/healthz` 端点，服务器正常运行时返回 `200 OK`。Docker 镜像内置的 `HEALTHCHECK` 会运行 `gitea-mcp -healthcheck`，它使用与 `-p`/`-port` 相同的端口（默认 `8080`）请求 `http://127.0.0.1:<端口>/healthz`，成功时退出码为 `0`，失败时为 `1`。stdio 部署不提供 `/healthz`，因此在 stdio 模式下运行时应覆盖或禁用镜像自带的 `HEALTHCHECK`。

### Claude Code

通过 `go run` 运行服务器，需要安装 [Go](https://go.dev)：

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

可使用本 README 顶部的安装按钮，或将下面的内容加入用户设置 (JSON)，按 `Ctrl + Shift + P` 并输入 `Preferences: Open User Settings (JSON)` 即可打开。也可放在工作区的 `.vscode/mcp.json` 中，此时无需 `mcp` 键。

```json
{
  "mcp": {
    "inputs": [
      {
        "type": "promptString",
        "id": "gitea_token",
        "description": "Gitea 个人访问令牌",
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

将下面的内容加入 [OpenCode](https://opencode.ai) 配置的顶层 `mcp` 对象：

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

将下面的内容加入 `~/.vibe/config.toml`：

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

### 其他客户端

Cursor 等客户端可使用 stdio 命令：

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

或使用 http 端点，对应以 `gitea-mcp -t http --port 8080` 启动的服务器：

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

配置完成后，可在聊天框输入 `列出我所有的仓库` 试试。

## 可用工具

| 工具                         | 范围         | 访问 | 描述 |
| :--------------------------- | :----------- | :--- | :--- |
| get_gitea_mcp_server_version | version      | 读取 | 获取 Gitea MCP 服务器版本 |
| get_me                       | user         | 读取 | 获取当前已认证用户 |
| get_user_orgs                | user         | 读取 | 列出当前用户的组织 |
| search_users                 | search       | 读取 | 搜索用户 |
| search_org_teams             | search       | 读取 | 搜索组织中的团队 |
| search_repos                 | search       | 读取 | 搜索仓库 |
| search_issues                | search       | 读取 | 跨仓库搜索问题和拉取请求 |
| notification_read            | notification | 读取 | 读取通知：列出（可限定仓库）或按 ID 获取会话 |
| notification_write           | notification | 写入 | 将某条或全部通知标记为已读 |
| label_read                   | label        | 读取 | 读取仓库或组织标签 |
| label_write                  | label        | 写入 | 写入标签（仓库或组织）：创建、编辑、删除 |
| milestone_read               | milestone    | 读取 | 读取里程碑：获取单个或列出 |
| milestone_write              | milestone    | 写入 | 写入里程碑：创建、更新、删除 |
| wiki_read                    | wiki         | 读取 | 读取 Wiki：列出页面、获取内容、修订历史 |
| wiki_write                   | wiki         | 写入 | 写入 Wiki 页面：创建、更新、删除 |
| timetracking_read            | timetracking | 读取 | 读取时间跟踪：问题/仓库耗时、活动计时器、我的跟踪记录 |
| timetracking_write           | timetracking | 写入 | 写入时间跟踪：计时器和记录条目 |
| package_read                 | packages     | 读取 | 读取软件包注册表：列出软件包、列出版本或获取某个版本 |
| package_write                | packages     | 写入 | 删除软件包版本（不可恢复） |
| list_issues                  | issue        | 读取 | 列出仓库问题 |
| attachment_read              | issue        | 读取 | 读取问题/评论附件：列出元数据、获取元数据或下载内容 |
| issue_read                   | issue        | 读取 | 读取问题：详情、评论或标签 |
| issue_write                  | issue        | 写入 | 写入问题：创建、更新、管理评论和标签 |
| list_pull_requests           | pull_request | 读取 | 列出仓库拉取请求 |
| pull_request_read            | pull_request | 读取 | 读取拉取请求：详情、差异、变更文件、头部提交状态、审查、审查评论 |
| pull_request_write           | pull_request | 写入 | 写入拉取请求：创建、更新、关闭、重新打开、合并、更新分支、管理审查者 |
| pull_request_review_write    | pull_request | 写入 | 写入 PR 审查：创建、提交、删除、驳回、回复和解决审查评论 |
| actions_config_read          | actions      | 读取 | 读取 Actions 密钥和变量 |
| actions_config_write         | actions      | 写入 | 写入 Actions 密钥和变量：更新插入、创建、更新、删除 |
| actions_run_read             | actions      | 读取 | 读取 Actions 工作流、运行、作业、日志和构件 |
| actions_run_write            | actions      | 写入 | 写入 Actions 运行：触发、取消、重新运行 |
| create_repo                  | repository   | 写入 | 创建新仓库 |
| fork_repo                    | repository   | 写入 | 复刻仓库 |
| list_my_repos                | repository   | 读取 | 列出当前用户拥有的仓库 |
| list_org_repos               | repository   | 读取 | 列出组织中的仓库 |
| get_repository_tree          | repository   | 读取 | 获取仓库文件树 |
| get_file_contents            | file         | 读取 | 获取文件内容和元数据 |
| get_dir_contents             | file         | 读取 | 获取目录中的条目 |
| create_or_update_file        | file         | 写入 | 创建或更新文件（提供 sha 以更新现有文件） |
| delete_file                  | file         | 写入 | 删除文件 |
| create_branch                | branch       | 写入 | 创建新分支 |
| delete_branch                | branch       | 写入 | 删除分支 |
| list_branches                | branch       | 读取 | 列出仓库分支 |
| rename_branch                | branch       | 写入 | 重命名分支 |
| create_tag                   | tag          | 写入 | 创建标签 |
| delete_tag                   | tag          | 写入 | 删除标签 |
| get_tag                      | tag          | 读取 | 获取标签详情 |
| list_tags                    | tag          | 读取 | 列出仓库标签 |
| list_commits                 | commit       | 读取 | 列出仓库提交 |
| get_commit                   | commit       | 读取 | 获取提交详情 |
| create_release               | release      | 写入 | 创建版本发布 |
| delete_release               | release      | 写入 | 删除版本发布 |
| get_release                  | release      | 读取 | 按 ID 获取版本发布 |
| get_latest_release           | release      | 读取 | 获取最新版本发布 |
| list_releases                | release      | 读取 | 列出仓库版本发布 |

> **说明：** 部分工具是聚合的、基于操作的工具，单个工具通过 `method` 参数暴露多个操作。当服务器以只读模式运行时（`-r` / `GITEA_READONLY`），访问为「写入」的工具会被隐藏；可通过 `-S` / `--scope`（`GITEA_SCOPES`）按范围过滤，或通过 `-O` / `--tools`（`GITEA_TOOLS`）按工具名称过滤对外暴露的工具集合。

未设置任一参数时，会加载所有工具；仅设置 `--scope` 时，会加载这些范围内的所有工具；仅设置 `--tools` 时，只会加载指定名称的工具；两者都设置时，会加载所选范围的工具与指定工具名称的并集。范围名称即上表「范围」列中的值，未知的范围名称仅会在启动时产生警告并被忽略。

```bash
gitea-mcp -S issue,pull_request
gitea-mcp --scope repository,branch --tools get_me
```

许多工具支持 `page` 和 `per_page` 分页参数。最大有效页面大小由 Gitea 服务器的 `[api].MAX_RESPONSE_ITEMS` 设置决定（默认 **50**），超出的值会被静默截断。
