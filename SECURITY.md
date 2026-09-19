# Security Policy

## Reporting Vulnerabilities

If you discover a security vulnerability in this repository, please report it by email to:

`dev@sn-s.net`

Security vulnerability reports are handled on a best-effort basis by the maintainer. Please do not open public issues for suspected vulnerabilities.

## Scope

### In Scope
- The OAuth 2.1 authorization server implementation in this fork (`pkg/oauth/`).
- Hard read-only enforcement and proxy/transport protections in this fork (`pkg/gitea/readonly_transport.go`, tool filtering).
- Deployment configurations and reference setups provided in this repository (`deploy/`).

### Out of Scope (Upstream)
- Vulnerabilities in upstream [Gitea MCP](https://gitea.com/gitea/gitea-mcp) itself (outside the OAuth and read-only modifications in this fork) should be reported directly to the upstream Gitea MCP maintainers.
- Vulnerabilities in Gitea itself should be reported following the [Gitea Security Process](https://about.gitea.com).
