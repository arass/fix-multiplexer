# Security review checkpoint

Review started 2026-11-02 against local tag `security-review-baseline` (commit 67f1284). This is a working review, not an independent audit or a production approval.

The owner clarified that developers need to connect over a network and their FIX Logon credentials may differ from the exchange credentials. Non-loopback downstream FIX listeners now require TLS and a password reference for every enabled client; upstream credentials and per-client downstream passwords are configured independently. `TestDownstreamLogonUsesSeparatePassword` verifies over local FIX sockets that a missing password and the upstream password are rejected, while the configured client password is accepted. `TestNetworkListenerRequiresAuthenticatedTLSLogon` verifies password outcomes over TLS using a loopback test listener and non-loopback configuration; it does not test connectivity across hosts or firewalls. Management remains loopback-only. The intended network and whether a separate FIX username should be verified remain pending owner clarification.

There is currently no per-IP authentication rate limit or lockout. The transport connection cap and deadlines bound concurrent connections, but do not prevent repeated password attempts or deliberate denial of service. Use network restrictions such as a VPN or firewall for a pilot, strong distinct client passwords, and monitored access; do not treat this as public-internet security certification. Additional adversarial tests and security review remain required.

## Fixed: upstream bridge accepted unauthenticated local processes

The upstream transport bridge listens on an ephemeral loopback TCP port and accepts a connection without authenticating its local peer (`internal/transport/bridge.go`). It then opens the configured upstream connection, including using the configured TLS client certificate if present. A local process able to connect to that port can occupy the bridge and send bytes through the gateway's upstream transport. This can bypass downstream allowlisting and, where the venue trusts the gateway's client certificate, provide a path to impersonation. It also permits denial of service by occupying the bridge. The peer must discover the port and meet any separate venue authentication requirements; the issue does not by itself establish credential disclosure.

**Status: fixed in the working code.** The engine and private bridge now mutually authenticate using a process-specific ephemeral TLS identity generated in memory. The bridge completes the handshake before dialing the configured upstream; a local peer with no certificate or an unrelated certificate is rejected. A local test verifies both rejection and a subsequent successful authenticated connection, and the real upstream TLS, persistent recovery, race-enabled integration, QuickFIX/J interoperability and container smoke checks passed. Local peers can still attempt to occupy the bounded handshake for up to five seconds at a time, so this is not a guarantee against local denial of service or privileged access to process memory. Continue to isolate the gateway from untrusted local processes.

## Trust and storage

Downstream authentication is optional; without a configured password or mTLS, the allowlisted CompID is an identifier rather than a secret. All connected clients receive broadcasts. Deploy only among mutually trusted clients and local processes, and explicitly configure appropriate authentication. FIX file stores contain message payloads and must be protected by filesystem permissions and host/container access controls. Existing directories retain their existing permissions; verify them during deployment.

### Client certificates are not bound to individual FIX identities

Code inspection shows that downstream mTLS verifies that a certificate is trusted by the configured CA, but does not map its subject or fingerprint to a specific SenderCompID. The existing `TestDownstreamMutualTLS` logs on as `CLIENT` with a trusted certificate whose subject is `synthetic-local-test`, confirming that a matching identity is not required. A holder of another certificate trusted by the same CA can claim an allowlisted CompID; a per-client FIX password, if configured, is checked separately. Duplicate-connection rejection can also let an early connection occupy a session. This is a trust-boundary limitation, not evidence that a particular deployment has been compromised. For mutually untrusted clients, use distinct strong per-client credentials and additional network isolation; assess certificate-to-CompID binding before promising per-client certificate identity. Additional cross-client tests remain useful.

### Fixed for new records: upstream Logon credentials in the FIX store

**Confirmed in the previous implementation:** QuickFIX/Go persists outgoing Logon messages before sending them. Configured Logon credentials appeared in the upstream session's durable body file in plaintext despite metadata-only logs.

**Status: fixed for newly written Logons.** The adapter now retains only required session header/sequence metadata in stored Logon records; the original Logon, including any configured credentials, is still sent to the peer. QuickFIX/Go gap-fills administrative messages during resend. A synthetic integration test verifies the venue receives the credential, new store bytes do not contain it, and a real request for the Logon sequence produces a valid gap fill. Application replay records remain unchanged. Existing store files, snapshots and backups are not rewritten and may retain old credentials. Do not delete or reset them without coordinating FIX sequence state; restrict access, use encrypted storage as appropriate, and rotate credentials if exposed. Application messages and other session data remain sensitive.

## Dependency scan

On 2026-11-02, `govulncheck v1.1.4 ./...` using Go 1.27.3 initially reported zero reachable vulnerable symbols, one vulnerability in an imported package and 19 in required modules whose vulnerable symbols were not found reachable. The imported-package result was [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503), affecting `golang.org/x/net v0.24.0`. The dependency was updated to v0.43.0, which retains the project's Go 1.23 minimum. A repeat scan reported zero reachable or imported-package vulnerabilities and 17 module-level advisories whose vulnerable symbols were not found reachable. Later x/net fixes require a newer Go minimum; reassess the tradeoff when updating the toolchain. A scan is not proof of safety and covers only Go source and the database state at that time, not the Java fixture or container OS packages.

## Fixed: bounded metadata reads

Session metadata was read entirely into memory before its 4 KiB size check. A locally writable oversized or growing marker could therefore cause excessive allocation during startup or offline maintenance. Reads now stop at 4,097 bytes and reject oversized data. Unit tests cover both paths and the read bound. This does not make store directories safe to share with untrusted users or eliminate filesystem races.

## Remaining review tasks

- Assess residual local denial-of-service exposure and operational isolation of the authenticated upstream bridge.
- Plan coordinated handling of existing stores and backups that may still contain pre-fix Logon credentials; do not silently alter replay history.
- Review authentication, authorization, TLS, health endpoints, filesystem race/symlink handling, resource-exhaustion paths, logging and error redaction.
- Test cross-identity mTLS attempts and assess per-client certificate identity binding and duplicate-session exhaustion.
- Confirm network exposure and whether separate FIX usernames are required; assess rate limiting, remote deployments and adversarial authentication tests before broader exposure.
- Reassess the Go minimum and later dependency upgrades; scan container/Java dependencies where tooling is available.
- Record verified findings, fixes, tests and residual limitations; update task documents and commit each completed step locally.
