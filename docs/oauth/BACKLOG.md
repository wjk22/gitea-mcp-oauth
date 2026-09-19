# gitea-mcp OAuth — Backlog

Deliberately **not** built in Revision 4. Each item names what would justify building it. Each item is deliberately not implemented.

| Item | Why deferred | Build it when |
|---|---|---|
| Persistent store (bbolt), encrypted Gitea tokens at rest | Single user; reconnecting after a restart is acceptable; in-memory tokens never touch disk | Restarts become frequent or a second user is added |
| Consent page | Redirect allowlist + single allowed user make code theft impractical | More than one user, or the redirect allowlist is widened |
| Repo/org allowlists (enforcing the declared scope kinds) | The maintainer wants the connector to read all their repos | A repo must be hidden from web chats |
| Rate limiting, client caps | Stateless client IDs store nothing; single user | Abuse shows up in logs |
| Audit log | Single user | Multiple users or incident review needed |
| Revocation endpoint (RFC 7009), refresh-token reuse detection | Restart revokes everything; single user | Multiple users |
| Automated scope regression against real Gitea (T-SCOPE) | Verified manually with the scope probe in `test/scope-probe/` | Gitea is upgraded to a new minor/major version — rerun the manual probe at minimum |
| Multiple allowed users / team-based access | One user | Someone else needs access |
| Trusted-proxy handling for client IPs | Only needed for rate limiting | Rate limiting is built |
