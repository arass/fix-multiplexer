# Security review checkpoint

Review started 2026-11-02 against local tag `security-review-baseline` (commit 67f1284). This is a working review, not an independent audit or a production approval.

The owner clarified that developers need to connect over a network and their FIX Logon credentials may differ from the exchange credentials. The current downstream listener is loopback-only and does not yet satisfy remote access. Upstream credentials and per-client downstream passwords are configured independently. Network exposure, TLS, and whether client identity is SenderCompID or a separate FIX username require a security design and end-to-end tests before remote binding is enabled. The intended network and identity details are pending owner clarification.

## High: upstream bridge trusts all local processes

The upstream transport bridge listens on an ephemeral loopback TCP port and accepts a connection without authenticating its local peer (`internal/transport/bridge.go`). It then opens the configured upstream connection, including using the configured TLS client certificate if present. A local process able to connect to that port can occupy the bridge and send bytes through the gateway's upstream transport. This can bypass downstream allowlisting and, where the venue trusts the gateway's client certificate, provide a path to impersonation. It also permits denial of service by occupying the bridge. The peer must discover the port and meet any separate venue authentication requirements; the issue does not by itself establish credential disclosure.

**Status: open.** Loopback restricts network exposure but does not isolate users on the same host or processes sharing a network namespace. Until fixed, use a dedicated host or an isolated network namespace containing only mutually trusted processes. Do not treat mTLS at the venue as a defense against an untrusted local process using this bridge. Investigate an authenticated engine-to-bridge transport compatible with QuickFIX/Go without changing FIX session semantics; add negative and reconnect regressions before considering it fixed.

## Trust and storage

Downstream authentication is optional; without a configured password or mTLS, the allowlisted CompID is an identifier rather than a secret. All connected clients receive broadcasts. Deploy only among mutually trusted clients and local processes, and explicitly configure appropriate authentication. FIX file stores contain message payloads and must be protected by filesystem permissions and host/container access controls. Existing directories retain their existing permissions; verify them during deployment.

### Client certificates are not bound to individual FIX identities

Code inspection shows that downstream mTLS verifies that a certificate is trusted by the configured CA, but does not map its subject or fingerprint to a specific SenderCompID. A holder of another certificate trusted by the same CA can claim an allowlisted CompID; a per-client FIX password, if configured, is checked separately. Duplicate-connection rejection can also let an early connection occupy a session. This is a trust-boundary limitation, not evidence that a particular deployment has been compromised. For mutually untrusted clients, use distinct strong per-client credentials and additional network isolation; assess certificate-to-CompID binding before promising per-client certificate identity. Integration tests for cross-identity attempts are still needed.

### Upstream credentials persist in the FIX store

**Confirmed:** QuickFIX/Go persists outgoing Logon messages before sending them. If an upstream username or password is configured, the corresponding Logon fields are written to the upstream session's durable body file in plaintext. The event and application logs suppress message content, but that does not remove credentials from session stores, snapshots or backups. `TestUpstreamLogonCredentialIsPersisted` verifies this with a synthetic local credential and no raw payload output.

**Status: open.** Restrict store and backup access, use encrypted storage as appropriate, and rotate any credentials if a store is exposed. Existing files and backups remain sensitive even if future code changes reduce persistence. Investigate safely omitting sensitive admin payloads without breaking FIX sequencing, resend, restart or message-store integrity; validate with recovery and engine interoperability tests before changing this behavior.

## Dependency scan

On 2026-11-02, `govulncheck v1.1.4 ./...` using Go 1.27.3 reported zero reachable vulnerable symbols. It also reported one vulnerability in an imported package and 19 in required modules whose vulnerable symbols were not found reachable. The imported-package result is [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503), affecting `golang.org/x/net v0.24.0` and fixed in v0.36.0; assess dependency updates and re-run the scan rather than interpreting an unreachable result as proof of safety. The scan covers the Go source and database state at that time, not the Java fixture or container OS packages.

## Fixed: bounded metadata reads

Session metadata was read entirely into memory before its 4 KiB size check. A locally writable oversized or growing marker could therefore cause excessive allocation during startup or offline maintenance. Reads now stop at 4,097 bytes and reject oversized data. Unit tests cover both paths and the read bound. This does not make store directories safe to share with untrusted users or eliminate filesystem races.

## Remaining review tasks

- Investigate and remediate the upstream bridge trust boundary; validate with local adversarial socket tests, TLS/mTLS and reconnect integration tests.
- Design and validate a safe mitigation for confirmed upstream logon credential persistence, including existing-store handling.
- Review authentication, authorization, TLS, health endpoints, filesystem race/symlink handling, resource-exhaustion paths, logging and error redaction.
- Test cross-identity mTLS attempts and assess per-client certificate identity binding and duplicate-session exhaustion.
- Design authenticated, encrypted remote downstream access after confirming network exposure and client identity requirements; keep management and default listener exposure constrained.
- Assess dependency upgrades and scan container/Java dependencies where tooling is available.
- Record verified findings, fixes, tests and residual limitations; update task documents and commit each completed step locally.
