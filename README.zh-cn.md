# Gitea MCP OAuth

**通过 OAuth 将 ChatGPT 和 Claude 连接到自托管 Gitea —— 并提供强制只读访问。**

> [!NOTE]
> 本仓库是官方 [Gitea MCP Server](https://gitea.com/gitea/gitea-mcp) 的非官方分支。它为 ChatGPT、Claude 等远程 Web MCP 客户端增加了 OAuth 授权服务器和严格的只读强制机制。本项目与 Gitea 项目无隶属关系，也未获得其官方背书。
>
> 设计与安全模型：[`docs/oauth/SPEC.md`](docs/oauth/SPEC.md)  
> 部署指南：[`deploy/README.md`](deploy/README.md)

[English](README.md) | [繁體中文](README.zh-tw.md)

## 为什么有这个项目

官方 Gitea MCP Server 很适合本地 MCP 客户端和个人访问令牌（PAT）。但 ChatGPT、Claude 这类远程 Web 客户端需要一个支持 OAuth 的 MCP 端点。

这个分支补上了这一层。

它适合希望让 AI 助手检查仓库、Issue、Pull Request、提交、Release 和其他 Gitea 数据，同时**不授予这些助手写权限**的用户。

## 主要特性

- 支持 **ChatGPT 自定义 MCP Connector**
- 支持 **Claude Web Connector**
- 使用带 **PKCE** 的 OAuth 流程
- 支持 Web MCP 客户端的动态客户端注册
- OAuth 模式**始终只读**
- OAuth 模式下不会暴露可修改数据的 MCP 工具
- 发往 Gitea 的请求还会经过只读 HTTP Transport 保护
- 单用户允许列表
- Gitea 凭据始终保留在服务器端
- 无状态 MCP HTTP Transport
- 包含 Docker 部署配置
- 包含反向代理配置
- OAuth 模式之外仍保留原有 PAT/stdio 行为

## 安全模型

OAuth 模式有意保持一个很窄的信任边界。

Web 客户端拿到的是 MCP Access Token，而不是你的 Gitea OAuth Token。

只读访问通过多层机制强制执行：

1. Gitea OAuth 只读 Scope
2. 排除具备写能力的 MCP 工具
3. 发往 Gitea 的 HTTP 请求仅允许安全的只读方法
4. OAuth 模式无法切换为写模式

因此，ChatGPT 或 Claude 可以检查 Gitea 中的数据，但无法通过 MCP Server 修改仓库。

> [!IMPORTANT]
> 只读访问仍然意味着客户端可以读取仓库内容。不要授权包含你不希望 AI 客户端读取的秘密信息的仓库。

## 已测试客户端

| 客户端 | 状态 |
|---|---|
| ChatGPT Web 自定义 MCP Connector | ✅ 可用 |
| Claude Web Connector | ✅ 可用 |
| 本地 MCP 客户端 / PAT 模式 | ✅ 与上游兼容 |

## 快速开始

### 1. 创建 Gitea OAuth 应用

在 Gitea 中创建 OAuth2 应用，并将 Callback 设置为：

```text
https://your-mcp-host.example/oauth/callback
```

### 2. 配置服务器

复制部署示例：

```bash
cp deploy/.env.example deploy/.env
```

至少设置：

```text
GITEA_HOST=https://git.example.com
GITEA_OAUTH_CLIENT_ID=...
GITEA_OAUTH_PUBLIC_URL=https://mcp.example.com
GITEA_OAUTH_ALLOWED_USER=your-user
```

使用提供的 Docker Secret 文件保存 OAuth Client Secret 和 Signing Key。

### 3. 启动

```bash
cd deploy
docker compose up -d
```

MCP 端点为：

```text
https://mcp.example.com/mcp
```

### 4. 连接 ChatGPT

创建自定义 MCP App/Connector：

```text
Server URL: https://mcp.example.com/mcp
Authentication: OAuth
```

然后完成 Gitea 登录流程。

### 5. 连接 Claude

将同一个 MCP 端点添加为自定义 Connector，并通过 Gitea 完成认证。

## ChatGPT Callback 说明

较新的 ChatGPT Connector 会使用每个 Connector 独立的 Callback URI，格式如下：

```text
https://chatgpt.com/connector/oauth/<callback_id>
```

你的部署必须允许 Connector 所需的 Callback URI 格式。当前部署细节请参阅 [`deploy/README.md`](deploy/README.md)。

## 部署

完整的 Docker、反向代理、TLS、OAuth 与故障排查说明见 [`deploy/README.md`](deploy/README.md)。

## 与上游项目的区别

本仓库基于官方 Gitea MCP Server：

https://gitea.com/gitea/gitea-mcp

本分支增加了：

- 面向 Web MCP 客户端的 OAuth 授权服务器功能
- PKCE 与动态客户端注册
- Web 客户端 Redirect URI 兼容
- 单用户授权
- 强制只读 OAuth 模式
- OAuth 专用安全测试
- 用于远程 MCP 的 Docker / 反向代理部署示例

在可行的情况下，原有 Gitea MCP 的本地/PAT 行为保持不变。

## OAuth 模式

启用 OAuth 模式后，服务器会强制只读。不存在可以在 OAuth 模式下启用写工具的配置开关。

支持 OAuth 的服务器会暴露兼容 Web MCP 客户端所需的 Metadata 与 Authorization Endpoint，同时将上游 Gitea OAuth 凭据保留在服务器端。

## MCP 协议与 HTTP Transport

服务器支持最高 `2026-07-28` 版本的 MCP，并会向下协商到客户端支持的版本，只声明 `tools` Capability。

HTTP 模式是无状态的。MCP 端点为：

```text
/mcp
```

在 OAuth 模式下，`/mcp` 由本服务器签发的 MCP Access Token 保护。

在 OAuth 模式之外，原有的 PAT HTTP 与 stdio 行为仍然可用。

HTTP 模式还提供：

```text
/healthz
```

Docker 镜像包含内置 Health Check，会运行：

```text
gitea-mcp -healthcheck
```

并检查 `http://127.0.0.1:<port>/healthz`。

## 本地 / 上游兼容用法

对于本地客户端和基于 PAT 的工作流，本项目保留原有 Gitea MCP 行为。

### Claude Code

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

基于 PAT 的上游风格本地 Docker 配置仍然可用：

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

### 其他客户端

本地 stdio 配置可以类似这样：

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

非 OAuth HTTP 模式可以这样配置：

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

## 安装与构建

### OAuth 部署

OAuth 模式需要从本仓库构建。参见 [`deploy/README.md`](deploy/README.md)。

### 从源码构建

需要 Go 1.27 或更高版本：

```bash
git clone https://github.com/wjk22/gitea-mcp-oauth.git
cd gitea-mcp-oauth
make install
```

### 上游二进制与镜像

如果你只需要原有的本地/PAT 行为，官方上游项目也提供自己的二进制文件和 Docker 镜像：

https://gitea.com/gitea/gitea-mcp

这些上游构建**不包含**本分支新增的 OAuth 功能。

## 配置

可以通过命令行参数或环境变量传入 Gitea Host 与 Access Token；命令行参数优先。

运行：

```bash
gitea-mcp --help
```

查看完整选项。

日志写入：

```text
$HOME/.gitea-mcp/gitea-mcp.log
```

使用 `-d` 启用 Debug Logging。

## 工具过滤

服务器运行在只读模式（`-r` / `GITEA_READONLY`）时，所有 `Write` 工具都会被隐藏。

也可以按 Scope 过滤暴露的工具：

```bash
gitea-mcp -S issue,pull_request
```

或者按单个工具名过滤：

```bash
gitea-mcp --scope repository,branch --tools get_me
```

如果两种过滤都没有设置，则加载当前运行模式允许的所有工具。

## 可用工具

| 工具 | Scope | 权限 | 说明 |
| :--- | :--- | :--- | :--- |
| get_gitea_mcp_server_version | version | 读取 | 获取 Gitea MCP Server 版本 |
| get_me | user | 读取 | 获取当前认证用户 |
| get_user_orgs | user | 读取 | 列出当前用户所属组织 |
| search_users | search | 读取 | 搜索用户 |
| search_org_teams | search | 读取 | 搜索组织内的 Team |
| search_repos | search | 读取 | 搜索仓库 |
| search_issues | search | 读取 | 跨仓库搜索 Issue 和 Pull Request |
| notification_read | notification | 读取 | 读取通知：列出通知或按 ID 获取 Thread |
| notification_write | notification | 写入 | 将一个或全部通知标记为已读 |
| label_read | label | 读取 | 读取仓库或组织 Label |
| label_write | label | 写入 | 创建、编辑或删除仓库/组织 Label |
| milestone_read | milestone | 读取 | 获取或列出 Milestone |
| milestone_write | milestone | 写入 | 创建、更新或删除 Milestone |
| wiki_read | wiki | 读取 | 读取 Wiki 页面及修订历史 |
| wiki_write | wiki | 写入 | 创建、更新或删除 Wiki 页面 |
| timetracking_read | timetracking | 读取 | 读取 Issue/仓库时间记录、Stopwatch 和个人时间记录 |
| timetracking_write | timetracking | 写入 | 写入 Stopwatch 和时间记录 |
| package_read | packages | 读取 | 读取 Package Registry：Package、Version 等 |
| package_write | packages | 写入 | 删除 Package Version（不可逆） |
| list_issues | issue | 读取 | 列出仓库 Issue |
| attachment_read | issue | 读取 | 读取 Issue/Comment Attachment 的 Metadata 或内容 |
| issue_read | issue | 读取 | 读取 Issue、Comment 和 Label |
| issue_write | issue | 写入 | 创建/更新 Issue、管理 Comment 与 Label |
| list_pull_requests | pull_request | 读取 | 列出 Pull Request |
| pull_request_read | pull_request | 读取 | 读取 PR 详情、Diff、文件、Status、Review 和 Comment |
| pull_request_write | pull_request | 写入 | 创建、更新、关闭、重开或合并 PR，并管理 Reviewer |
| pull_request_review_write | pull_request | 写入 | 创建、提交、删除或驳回 PR Review，并处理 Review Comment |
| actions_config_read | actions | 读取 | 读取 Actions Secret 和 Variable |
| actions_config_write | actions | 写入 | 写入 Actions Secret 和 Variable |
| actions_run_read | actions | 读取 | 读取 Workflow、Run、Job、Log 与 Artifact |
| actions_run_write | actions | 写入 | Dispatch、Cancel 或 Rerun Actions Run |
| create_repo | repository | 写入 | 创建仓库 |
| fork_repo | repository | 写入 | Fork 仓库 |
| list_my_repos | repository | 读取 | 列出当前用户拥有的仓库 |
| list_org_repos | repository | 读取 | 列出组织仓库 |
| get_repository_tree | repository | 读取 | 获取仓库文件树 |
| get_file_contents | file | 读取 | 获取文件内容与 Metadata |
| get_dir_contents | file | 读取 | 获取目录内容 |
| create_or_update_file | file | 写入 | 创建或更新文件 |
| delete_file | file | 写入 | 删除文件 |
| create_branch | branch | 写入 | 创建 Branch |
| delete_branch | branch | 写入 | 删除 Branch |
| list_branches | branch | 读取 | 列出 Branch |
| rename_branch | branch | 写入 | 重命名 Branch |
| create_tag | tag | 写入 | 创建 Tag |
| delete_tag | tag | 写入 | 删除 Tag |
| get_tag | tag | 读取 | 获取 Tag 详情 |
| list_tags | tag | 读取 | 列出 Tag |
| list_commits | commit | 读取 | 列出 Commit |
| get_commit | commit | 读取 | 获取 Commit 详情 |
| create_release | release | 写入 | 创建 Release |
| delete_release | release | 写入 | 删除 Release |
| get_release | release | 读取 | 按 ID 获取 Release |
| get_latest_release | release | 读取 | 获取最新 Release |
| list_releases | release | 读取 | 列出 Release |

> **说明：** 若干工具采用合并后的 Action-Based 设计，一个工具通过 `method` 参数暴露多个操作。服务器运行在只读模式（`-r` / `GITEA_READONLY`）时，带 `Write` 权限的工具会被隐藏。还可以通过 `-S` / `--scope`（`GITEA_SCOPES`）以及 `-O` / `--tools`（`GITEA_TOOLS`）限制暴露的工具集合。

如果两种过滤都未设置，则加载当前模式允许的全部工具。`--scope` 仅加载 Scope 列中匹配的工具；`--tools` 仅加载指定名称的工具；同时使用时会加载两者的并集。未知 Scope 会在启动时产生警告并被忽略。

```bash
gitea-mcp -S issue,pull_request
gitea-mcp --scope repository,branch --tools get_me
```

很多工具支持 `page` 和 `per_page` 分页参数。实际最大 Page Size 受 Gitea Server `[api].MAX_RESPONSE_ITEMS` 设置限制（默认 **50**），更大的值会被静默截断。

## 安全说明

主要安全目标：

- Gitea Token 永远不会离开 MCP Server
- OAuth 模式始终只读
- Web 客户端拿到的是 MCP Token，而不是上游 Gitea Credential
- Redirect URI 会被验证
- 强制使用 PKCE
- 只有配置的 Gitea 用户可以完成授权
- 在 MCP Tool Layer 之下仍有只读保护
- OAuth 与 PAT 模式不会静默混用

当前设计见 [`docs/oauth/SPEC.md`](docs/oauth/SPEC.md)。

## License

本项目基于官方 Gitea MCP Server，并继续使用 MIT License。

参见 [`LICENSE`](LICENSE)。

## 上游项目

官方项目：

https://gitea.com/gitea/gitea-mcp
