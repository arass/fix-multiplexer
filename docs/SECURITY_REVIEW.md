# Security review checkpoint

Review started 2026-11-02 against local tag `security-review-baseline` (commit 67f1284). This is a working review, not an independent audit or a production approval.

## High: upstream bridge trusts all local processes

The upstream transport bridge listens on an ephemeral loopback TCP port and accepts a connection without authenticating its local peer (`internal/transport/bridge.go`). It then opens the configured upstream connection, including using the configured TLS client certificate if present. A local process able to connect to that port can occupy the bridge and send bytes through the gateway's upstream transport. This can bypass downstream allowlisting and, where the venue trusts the gateway's client certificate, provide a path to impersonation. It also permits denial of service by occupying the bridge. The peer must discover the port and meet any separate venue authentication requirements; the issue does not by itself establish credential disclosure.

**Status: open.** Loopback restricts network exposure but does not isolate users on the same host or processes sharing a network namespace. Until fixed, use a dedicated host or an isolated network namespace containing only mutually trusted processes. Do not treat mTLS at the venue as a defense against an untrusted local process using this bridge. Investigate an authenticated engine-to-bridge transport compatible with QuickFIX/Go without changing FIX session semantics; add negative and reconnect regressions before considering it fixed.

## Trust and storage

Downstream authentication is optional; without a configured password or mTLS, the allowlisted CompID is an identifier rather than a secret. All connected clients receive broadcasts. Deploy only among mutually trusted clients and local processes, and explicitly configure appropriate authentication. FIX file stores contain message payloads and must be protected by filesystem permissions and host/container access controls. Existing directories retain their existing permissions; verify them during deployment.

## Dependency scan

On 2026-11-02, `govulncheck v1.1.4 ./...` using Go 1.27.3 reported zero reachable vulnerable symbols. It also reported one vulnerability in an imported package and 19 in required modules whose vulnerable symbols were not found reachable. The imported-package result is [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503), affecting `golang.org/x/net v0.24.0` and fixed in v0.36.0; assess dependency updates and re-run the scan rather than interpreting an unreachable result as proof of safety. The scan covers the Go source and database state at that time, not the Java fixture or container OS packages.

## Remaining review tasks

- Investigate and remediate the upstream bridge trust boundary; validate with local adversarial socket tests, TLS/mTLS and reconnect integration tests.
- Review upstream admin-message persistence and verify secret exposure in actual file stores.
- Review authentication, authorization, TLS, health endpoints, filesystem race/symlink handling, resource-exhaustion paths, logging and error redaction.
- Assess dependency upgrades and scan container/Java dependencies where tooling is available.
- Record verified findings, fixes, tests and residual limitations; update task documents and commit each completed step locally.
