# Gitea MCP OAuth

**透過 OAuth 将 ChatGPT 和 Claude 連線到自行託管 Gitea —— 并提供强制唯讀存取。**

> [!NOTE]
> 本儲存庫是官方 [Gitea MCP Server](https://gitea.com/gitea/gitea-mcp) 的非官方分支。它为 ChatGPT、Claude 等遠端 Web MCP 用戶端新增了 OAuth 授權伺服器和嚴格的唯讀强制机制。本專案与 Gitea 專案无隶属关系，也未获得其官方背书。
>
> 設計与安全模型：[`docs/oauth/SPEC.md`](docs/oauth/SPEC.md)  
> 部署指南：[`deploy/README.md`](deploy/README.md)

[English](README.md) | [简体中文](README.zh-cn.md)

## 為什麼有這個專案

官方 Gitea MCP Server 很适合本機 MCP 用戶端和個人存取令牌（PAT）。但 ChatGPT、Claude 这类遠端 Web 用戶端需要一个支援 OAuth 的 MCP 端點。

此分支补上了這一層。

它適合希望让 AI 助手檢查儲存庫、Issue、Pull Request、Commit、Release 和其他 Gitea 資料，同时**不授予这些助手寫入權限**的使用者。

## 主要特性

- 支援 **ChatGPT 自定义 MCP Connector**
- 支援 **Claude Web Connector**
- 使用带 **PKCE** 的 OAuth 流程
- 支援 Web MCP 用戶端的动态用戶端注册
- OAuth 模式**始终唯讀**
- OAuth 模式下不會暴露可修改資料的 MCP 工具
- 发往 Gitea 的請求还会经过唯讀 HTTP Transport 保护
- 单使用者允许清單
- Gitea 憑證始终保留在伺服器端
- 無狀態 MCP HTTP Transport
- 包含 Docker 部署設定
- 包含反向代理設定
- OAuth 模式之外仍保留原有 PAT/stdio 行為

## 安全模型

OAuth 模式刻意維持一个很窄的信任邊界。

Web 用戶端取得的是 MCP Access Token，而不是你的 Gitea OAuth Token。

唯讀存取透過多層機制強制執行：

1. Gitea OAuth 唯讀 Scope
2. 排除具備寫入能力的 MCP 工具
3. 发往 Gitea 的 HTTP 請求僅允許安全的唯讀方法
4. OAuth 模式無法切換为写模式

因此，ChatGPT 或 Claude 可以檢查 Gitea 中的資料，但无法透過 MCP Server 修改儲存庫。

> [!IMPORTANT]
> 唯讀存取仍然代表用戶端可以讀取儲存庫內容。不要授權包含你不希望 AI 用戶端讀取的秘密資訊的儲存庫。

## 已測試用戶端

| 用戶端 | 狀態 |
|---|---|
| ChatGPT Web 自定义 MCP Connector | ✅ 可用 |
| Claude Web Connector | ✅ 可用 |
| 本機 MCP 用戶端 / PAT 模式 | ✅ 与上游相容 |

## 快速開始

### 1. 建立 Gitea OAuth 應用程式

在 Gitea 中建立 OAuth2 應用程式，并将 Callback 設為：

```text
https://your-mcp-host.example/oauth/callback
```

### 2. 設定伺服器

複製部署範例：

```bash
cp deploy/.env.example deploy/.env
```

至少設定：

```text
GITEA_HOST=https://git.example.com
GITEA_OAUTH_CLIENT_ID=...
GITEA_OAUTH_PUBLIC_URL=https://mcp.example.com
GITEA_OAUTH_ALLOWED_USER=your-user
```

使用提供的 Docker Secret 檔案儲存 OAuth Client Secret 和 Signing Key。

### 3. 啟動

```bash
cd deploy
docker compose up -d
```

MCP 端點为：

```text
https://mcp.example.com/mcp
```

### 4. 連線 ChatGPT

建立自定义 MCP App/Connector：

```text
Server URL: https://mcp.example.com/mcp
Authentication: OAuth
```

然后完成 Gitea 登入流程。

### 5. 連線 Claude

将同一个 MCP 端點添加为自定义 Connector，并透過 Gitea 完成驗證。

## ChatGPT Callback 說明

較新的 ChatGPT Connector 会使用每个 Connector 獨立的 Callback URI，格式如下：

```text
https://chatgpt.com/connector/oauth/<callback_id>
```

你的部署必須允許 Connector 所需的 Callback URI 格式。目前部署細節请参阅 [`deploy/README.md`](deploy/README.md)。

## 部署

完整的 Docker、反向代理、TLS、OAuth 与疑難排解說明见 [`deploy/README.md`](deploy/README.md)。

## 与上游專案的差異

本儲存庫基於官方 Gitea MCP Server：

https://gitea.com/gitea/gitea-mcp

此分支新增了：

- 面向 Web MCP 用戶端的 OAuth 授權伺服器功能
- PKCE 与动态用戶端注册
- Web 用戶端 Redirect URI 相容
- 单使用者授權
- 强制唯讀 OAuth 模式
- OAuth 專用安全测试
- 用于遠端 MCP 的 Docker / 反向代理部署範例

在可行的情況下，原有 Gitea MCP 的本機/PAT 行為保持不變。

## OAuth 模式

啟用 OAuth 模式后，伺服器会强制唯讀。不存在可以在 OAuth 模式下啟用写工具的設定開關。

支援 OAuth 的伺服器会暴露相容 Web MCP 用戶端所需的 Metadata 与 Authorization Endpoint，同时将上游 Gitea OAuth 憑證保留在伺服器端。

## MCP 協定与 HTTP Transport

伺服器支援最高 `2026-07-28` 版本的 MCP，并会向下協商到用戶端支援的版本，只宣告 `tools` Capability。

HTTP 模式是無狀態的。MCP 端點为：

```text
/mcp
```

在 OAuth 模式下，`/mcp` 由本伺服器签发的 MCP Access Token 保护。

在 OAuth 模式之外，原有的 PAT HTTP 与 stdio 行為仍可使用。

HTTP 模式也提供：

```text
/healthz
```

Docker 映像包含內建 Health Check，会執行：

```text
gitea-mcp -healthcheck
```

并檢查 `http://127.0.0.1:<port>/healthz`。

## 本機 / 上游相容用法

对于本機用戶端和基於 PAT 的工作流程，本專案保留原有 Gitea MCP 行為。

### Claude Code

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

基於 PAT 的上游風格本機 Docker 設定仍可使用：

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

### 其他用戶端

本機 stdio 設定可以類似這樣：

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

非 OAuth HTTP 模式可以这样設定：

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

## 安裝與建置

### OAuth 部署

OAuth 模式需要从本儲存庫建置。請參閱 [`deploy/README.md`](deploy/README.md)。

### 從原始碼建置

需要 Go 1.27 或更高版本：

```bash
git clone https://github.com/wjk22/gitea-mcp-oauth.git
cd gitea-mcp-oauth
make install
```

### 上游二進位与映像

如果你只需要原有的本機/PAT 行為，官方上游專案也提供自己的二進位檔案和 Docker 映像：

https://gitea.com/gitea/gitea-mcp

这些上游建置**不包含**此分支新增的 OAuth 功能。

## 設定

可以透過命令列參數或環境變數传入 Gitea Host 与 Access Token；命令列參數優先。

執行：

```bash
gitea-mcp --help
```

查看完整選項。

記錄寫入：

```text
$HOME/.gitea-mcp/gitea-mcp.log
```

使用 `-d` 啟用 Debug Logging。

## 工具篩選

伺服器執行在唯讀模式（`-r` / `GITEA_READONLY`）时，所有 `Write` 工具都會被隱藏。

也可以依 Scope 过滤暴露的工具：

```bash
gitea-mcp -S issue,pull_request
```

或者按單一工具名稱过滤：

```bash
gitea-mcp --scope repository,branch --tools get_me
```

若兩種篩選皆未設定，則載入目前執行模式允許的所有工具。

## 可用工具

| 工具 | Scope | 權限 | 說明 |
| :--- | :--- | :--- | :--- |
| get_gitea_mcp_server_version | version | 讀取 | 取得 Gitea MCP Server 版本 |
| get_me | user | 讀取 | 取得目前驗證使用者 |
| get_user_orgs | user | 讀取 | 列出目前使用者所属組織 |
| search_users | search | 讀取 | 搜尋使用者 |
| search_org_teams | search | 讀取 | 搜尋組織内的 Team |
| search_repos | search | 讀取 | 搜尋儲存庫 |
| search_issues | search | 讀取 | 跨儲存庫搜尋 Issue 和 Pull Request |
| notification_read | notification | 讀取 | 讀取通知：列出通知或按 ID 取得 Thread |
| notification_write | notification | 寫入 | 将一个或全部通知標記為已讀 |
| label_read | label | 讀取 | 讀取儲存庫或組織 Label |
| label_write | label | 寫入 | 建立、編輯或刪除儲存庫/組織 Label |
| milestone_read | milestone | 讀取 | 取得或列出 Milestone |
| milestone_write | milestone | 寫入 | 建立、更新或刪除 Milestone |
| wiki_read | wiki | 讀取 | 讀取 Wiki 頁面及修订历史 |
| wiki_write | wiki | 寫入 | 建立、更新或刪除 Wiki 頁面 |
| timetracking_read | timetracking | 讀取 | 讀取 Issue/儲存庫時間記錄、Stopwatch 和個人時間記錄 |
| timetracking_write | timetracking | 寫入 | 寫入 Stopwatch 和時間記錄 |
| package_read | packages | 讀取 | 讀取 Package Registry：Package、Version 等 |
| package_write | packages | 寫入 | 刪除 Package Version（不可逆） |
| list_issues | issue | 讀取 | 列出儲存庫 Issue |
| attachment_read | issue | 讀取 | 讀取 Issue/Comment Attachment 的 Metadata 或內容 |
| issue_read | issue | 讀取 | 讀取 Issue、Comment 和 Label |
| issue_write | issue | 寫入 | 建立/更新 Issue、管理 Comment 与 Label |
| list_pull_requests | pull_request | 讀取 | 列出 Pull Request |
| pull_request_read | pull_request | 讀取 | 讀取 PR 詳細資料、Diff、檔案、Status、Review 和 Comment |
| pull_request_write | pull_request | 寫入 | 建立、更新、關閉、重新開啟或合併 PR，并管理 Reviewer |
| pull_request_review_write | pull_request | 寫入 | 建立、Commit、刪除或驳回 PR Review，并处理 Review Comment |
| actions_config_read | actions | 讀取 | 讀取 Actions Secret 和 Variable |
| actions_config_write | actions | 寫入 | 寫入 Actions Secret 和 Variable |
| actions_run_read | actions | 讀取 | 讀取 Workflow、Run、Job、Log 与 Artifact |
| actions_run_write | actions | 寫入 | Dispatch、Cancel 或 Rerun Actions Run |
| create_repo | repository | 寫入 | 建立儲存庫 |
| fork_repo | repository | 寫入 | Fork 儲存庫 |
| list_my_repos | repository | 讀取 | 列出目前使用者拥有的儲存庫 |
| list_org_repos | repository | 讀取 | 列出組織儲存庫 |
| get_repository_tree | repository | 讀取 | 取得儲存庫檔案树 |
| get_file_contents | file | 讀取 | 取得檔案內容与 Metadata |
| get_dir_contents | file | 讀取 | 取得目錄內容 |
| create_or_update_file | file | 寫入 | 建立或更新檔案 |
| delete_file | file | 寫入 | 刪除檔案 |
| create_branch | branch | 寫入 | 建立 Branch |
| delete_branch | branch | 寫入 | 刪除 Branch |
| list_branches | branch | 讀取 | 列出 Branch |
| rename_branch | branch | 寫入 | 重新命名 Branch |
| create_tag | tag | 寫入 | 建立 Tag |
| delete_tag | tag | 寫入 | 刪除 Tag |
| get_tag | tag | 讀取 | 取得 Tag 詳細資料 |
| list_tags | tag | 讀取 | 列出 Tag |
| list_commits | commit | 讀取 | 列出 Commit |
| get_commit | commit | 讀取 | 取得 Commit 詳細資料 |
| create_release | release | 寫入 | 建立 Release |
| delete_release | release | 寫入 | 刪除 Release |
| get_release | release | 讀取 | 按 ID 取得 Release |
| get_latest_release | release | 讀取 | 取得最新 Release |
| list_releases | release | 讀取 | 列出 Release |

> **說明：** 若干工具採用合併后的 Action-Based 設計，一個工具透過 `method` 参数暴露多個操作。伺服器執行在唯讀模式（`-r` / `GITEA_READONLY`）时，带 `Write` 權限的工具会被隐藏。还可以透過 `-S` / `--scope`（`GITEA_SCOPES`）以及 `-O` / `--tools`（`GITEA_TOOLS`）限制暴露的工具集合。

如果两种过滤都未設定，則載入目前模式允许的全部工具。`--scope` 僅載入 Scope 列中符合的工具；`--tools` 僅載入指定名稱的工具；同时使用时会加载两者的聯集。未知 Scope 会在啟動时產生警告并被忽略。

```bash
gitea-mcp -S issue,pull_request
gitea-mcp --scope repository,branch --tools get_me
```

許多工具支援 `page` 和 `per_page` 分頁參數。實際最大 Page Size 受 Gitea Server `[api].MAX_RESPONSE_ITEMS` 設定限制（默认 **50**），更大的值会被靜默截斷。

## 安全說明

主要安全目標：

- Gitea Token 永遠不會離開 MCP Server
- OAuth 模式始终唯讀
- Web 用戶端取得的是 MCP Token，而不是上游 Gitea Credential
- Redirect URI 會被驗證
- 强制使用 PKCE
- 只有設定的 Gitea 使用者可以完成授權
- 在 MCP Tool Layer 之下仍有唯讀保护
- OAuth 与 PAT 模式不會靜默混用

目前設計见 [`docs/oauth/SPEC.md`](docs/oauth/SPEC.md)。

## License

本專案基於官方 Gitea MCP Server，并繼續使用 MIT License。

請參閱 [`LICENSE`](LICENSE)。

## 上游專案

官方專案：

https://gitea.com/gitea/gitea-mcp
