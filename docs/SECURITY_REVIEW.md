# Security review checkpoint

Review started 2026-11-02 against local tag `security-review-baseline` (commit 0e7f4d5 after history cleanup). This is a working review, not an independent audit or a production approval.

## Findings at a glance

| Finding | Status | Practical impact |
|---|---|---|
| Private upstream bridge accepted any local process | Fixed with per-process mutual TLS; local denial of service remains possible | A local process without the ephemeral identity cannot use the gateway's upstream transport, but can attempt to occupy the bounded handshake slot. |
| Outbound Logon credentials written to session stores | Fixed for newly written Logon records | Older stores and backups may still contain plaintext credentials; do not remove replay state without coordination. |
| Oversized store metadata read fully before validation | Fixed with bounded reads | Store roots still need to be protected from untrusted filesystem writes. |
| Repeated generic engine errors could flood logs | Partially mitigated with global error-log rate limiting | Metrics still count every error; authenticated application traffic and other log types remain unthrottled. |
| Remote FIX authentication has no per-IP attempt limit | Open | Connection caps are not a brute-force or denial-of-service defense. Restrict network access and use strong distinct passwords. |
| Per-client source IP restrictions | Implemented as optional `allowed_ips` | The TCP peer IP must match the configured client's IP/CIDR list before session binding; password checks remain separate. This does not rate-limit attempts or distinguish clients sharing an egress IP. |
| Optional plaintext downstream FIX exposes credentials and messages | Accepted configuration option at owner's request | Use an encrypted VPN or enable TLS; a password alone does not protect traffic from interception. |
| Client certificates are not bound to SenderCompIDs | Open design limitation | Any certificate trusted by the configured CA can claim an allowlisted session unless its separately configured FIX password is also required. |
| Separate downstream FIX username in tag 553 | Open requirement | SenderCompID and tag 554 password are checked; tag 553 is not. Confirm whether client software requires an independently verified username. |

The review and tests do not establish public-internet or production readiness. No actual exchange credentials, counterparty connections, cross-host penetration tests or third-party audit were used.

The owner clarified that developers need to connect over a network and their FIX Logon credentials may differ from the exchange credentials. Non-loopback downstream FIX listeners require a password reference for every enabled client; upstream credentials and per-client downstream passwords are configured independently. TLS is optional at the owner's request. `TestDownstreamLogonUsesSeparatePassword` verifies over local FIX sockets that a missing password and the upstream password are rejected, while the configured client password is accepted. `TestNetworkListenerRequiresAuthenticatedLogon` verifies password and source IP outcomes with and without TLS using a loopback test listener and non-loopback configuration; it does not test connectivity across hosts or firewalls. Management remains loopback-only. The owner expects source restrictions to be used for direct internet client connections; whether a separate FIX username should be verified remains an open requirement.

There is currently no per-IP authentication rate limit or lockout. Optional per-client IP/CIDR allowlists reject mismatched socket source addresses before session binding, but cannot prevent connections from consuming bounded pre-session resources or distinguish clients behind the same public IP. No source identity is trusted from FIX fields or proxy metadata. The transport connection cap and deadlines bound concurrent connections, but do not prevent repeated password attempts or deliberate denial of service. Generic engine-error and authentication-failure logs share a global one-per-second limit. IP-denial diagnostics have their own one-per-second limit and include the observed raw source IP, hashed claimed-session label and suppressed count; their metric counts every denial without IP labels. Unknown sessions and TLS failures before session identification are excluded from this IP-policy counter. Other log categories, including application traffic from authenticated clients, are not rate-limited. Use network restrictions, strong distinct client passwords, monitored access and bounded log retention; additional adversarial tests and security review remain required.

## Fixed: upstream bridge accepted unauthenticated local processes

Previously, the upstream transport bridge accepted connections on its ephemeral loopback TCP port without authenticating the local peer. It then opened the configured upstream connection, including using the configured TLS client certificate if present. A local process able to connect to that port could occupy the bridge and send bytes through the gateway's upstream transport, bypassing downstream allowlisting and potentially impersonating the gateway. The peer needed to discover the port and meet any separate venue authentication requirements; the issue did not by itself establish credential disclosure.

**Status: fixed in the working code.** The engine and private bridge now mutually authenticate using a process-specific ephemeral TLS identity generated in memory. The bridge completes the handshake before dialing the configured upstream; a local peer with no certificate or an unrelated certificate is rejected. A local test verifies both rejection and a subsequent successful authenticated connection, and the real upstream TLS, persistent recovery, race-enabled integration, QuickFIX/J interoperability and container smoke checks passed. Local peers can still attempt to occupy the bounded handshake for up to five seconds at a time, so this is not a guarantee against local denial of service or privileged access to process memory. Continue to isolate the gateway from untrusted local processes.

## Trust and storage

Downstream authentication is optional on loopback listeners; without a configured password or mTLS, the allowlisted CompID is an identifier rather than a secret. Non-loopback listeners require a password for every enabled client. TLS is optional; plaintext FIX exposes passwords and messages to network interception or alteration, so use an encrypted VPN or enable TLS. All connected clients receive broadcasts. Deploy only among mutually trusted clients and local processes, and explicitly configure appropriate authentication. FIX file stores contain message payloads and must be protected by filesystem permissions and host/container access controls. Existing directories retain their existing permissions; verify them during deployment.

### Client certificates are not bound to individual FIX identities

Code inspection shows that downstream mTLS verifies that a certificate is trusted by the configured CA, but does not map its subject or fingerprint to a specific SenderCompID. The existing `TestDownstreamMutualTLS` logs on as `CLIENT` with a trusted certificate whose subject is `synthetic-local-test`, confirming that a matching identity is not required. A holder of another certificate trusted by the same CA can claim an allowlisted CompID; a per-client FIX password, if configured, is checked separately. Duplicate-connection rejection can also let an early connection occupy a session. This is a trust-boundary limitation, not evidence that a particular deployment has been compromised. For mutually untrusted clients, use distinct strong per-client credentials and additional network isolation; assess certificate-to-CompID binding before promising per-client certificate identity. Additional cross-client tests remain useful.

### Fixed for new records: upstream Logon credentials in the FIX store

**Confirmed in the previous implementation:** QuickFIX/Go persists outgoing Logon messages before sending them. Configured Logon credentials appeared in the upstream session's durable body file in plaintext despite metadata-only logs.

**Status: fixed for newly written Logons.** The adapter now retains only required session header/sequence metadata in stored Logon records; the original Logon, including any configured credentials, is still sent to the peer. QuickFIX/Go gap-fills administrative messages during resend. A synthetic integration test verifies the venue receives the credential, new store bytes do not contain it, and a real request for the Logon sequence produces a valid gap fill. Application replay records remain unchanged. Existing store files, snapshots and backups are not rewritten and may retain old credentials. Do not delete or reset them without coordinating FIX sequence state; restrict access, use encrypted storage as appropriate, and rotate credentials if exposed. Application messages and other session data remain sensitive.

## Dependency scan

On 2026-11-02, `govulncheck v1.1.4 ./...` using Go 1.27.3 initially reported zero reachable vulnerable symbols, one vulnerability in an imported package and 19 in required modules whose vulnerable symbols were not found reachable. The imported-package result was [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503), affecting `golang.org/x/net v0.24.0`. The dependency was updated to v0.43.0, which retains the project's Go 1.23 minimum. A repeat scan reported zero reachable or imported-package vulnerabilities and 17 module-level advisories whose vulnerable symbols were not found reachable. Later x/net fixes require a newer Go minimum; reassess the tradeoff when updating the toolchain. A scan is not proof of safety and covers only Go source and the database state at that time, not the Java fixture or container OS packages.

## Fixed: bounded metadata reads

Session metadata was read entirely into memory before its 4 KiB size check. A locally writable oversized or growing marker could therefore cause excessive allocation during startup or offline maintenance. Reads now stop at 4,097 bytes and reject oversized data. Unit tests cover both paths and the read bound. This does not make store directories safe to share with untrusted users or eliminate filesystem races.

Environment-referenced credentials previously lacked the 4,096-byte limit already applied to credential files. Both sources now reject oversized credentials before a session starts; boundary and failure cases are tested. These references are controlled by the operator, not by an unauthenticated remote client.

Missing or incorrect configured-session passwords previously did not reliably produce an operator-visible authentication event. They now increment `fix_mux_auth_failures_total` and produce a generic, session-hashed event subject to the shared one-per-second error-log limit. Suppressed authentication and engine events are counted together in the next log; their metrics remain separate. Unknown sessions and TLS handshake failures are not included in the authentication counter. This improves detection but does not limit attempts.

## Remaining review tasks

- Assess residual local denial-of-service exposure and operational isolation of the authenticated upstream bridge.
- Plan coordinated handling of existing stores and backups that may still contain pre-fix Logon credentials; do not silently alter replay history.
- Review authentication, authorization, TLS, health endpoints, filesystem race/symlink handling, resource-exhaustion paths, logging and error redaction.
- Test cross-identity mTLS attempts and assess per-client certificate identity binding and duplicate-session exhaustion.
- Confirm network exposure and whether separate FIX usernames are required; assess rate limiting, remote deployments and adversarial authentication tests before broader exposure.
- Reassess the Go minimum and later dependency upgrades; scan container/Java dependencies where tooling is available.
- Record verified findings, fixes, tests and residual limitations; update task documents and commit each completed step locally.
