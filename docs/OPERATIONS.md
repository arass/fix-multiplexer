# Local development operations

The gateway is a nonproduction tool for mutually trusted participants. The management listener requires loopback. A non-loopback downstream FIX listener requires a password for every enabled client; TLS is optional and the example remains loopback-only. Without TLS, FIX passwords and messages are exposed to the network path; use an encrypted VPN and restrict access. All configured clients see upstream application broadcasts. Local engine acceptance is not venue acceptance, and no exactly-once guarantee is made.

For network access use an encrypted VPN restricted to trusted developers and a distinct strong FIX Logon password per client. If TLS is enabled, provision a server certificate clients verify. SenderCompID identifies the session; tag 554 is checked against that client's password. A separate tag 553 is not currently checked. No per-IP authentication rate limiting is implemented, and security review is incomplete. Generic engine-error and failed-password logs share a global limit of one per second; the next emitted error includes the combined `suppressed_errors` count. Their separate metrics count every event. Other log types are not rate-limited. Set log retention and storage limits in the log collector. Docker port publication and the listener bind address must both be configured for the intended network; keep management private.

## Start and verify

```sh
go run ./cmd/fix-multiplexer -config config/example.yaml -check-config
go run ./cmd/fix-multiplexer -demo
go run ./cmd/fix-multiplexer -config config/example.yaml
```

The demo uses synthetic data and temporary stores. The daemon uses configured durable stores. Current default MsgTypes toward upstream: D, F, G, H, V, x; downstream defaults include 8, 9, j, W, X, Y, y. The example narrows downstream to 8, 9, j. Deny rules override allow rules; administrative types cannot enter either application path. Unsupported downstream submissions receive a gateway BusinessMessageReject. Upstream types outside fanout policy are counted as drops.

## Recovery and eight-hour history

Each FIX session owns its sequences and outbound history. Valid inbound PossDup application messages are forwarded, with new session framing. Outbound replay callbacks never create new routing. Clients must handle duplicate business messages.

`retention.downstream_replay_window` defaults to 8h. If a downstream resend asks for an application message older than that window, the gateway fails recovery and persists a retired marker. It does not silently gap-fill away expired business history or delete an open store. A retired session requires an explicit coordinated reset, or a new allowlisted SessionID. Offline-period fanout is never accumulated.

`retention.upstream_replay_window` defaults to 0s (no age cutoff), independently of downstream policy. Both sides remain subject to disk limits. This does not recover upstream traffic the venue never supplied.

Age limits govern replay eligibility, not rolling deletion from active stores. Engine files can retain older bytes until offline cleanup, while `session_limit_bytes` and aggregate `disk_limit_bytes` bound growth. Store writes include an overhead reservation. A durability/budget failure is latched and stops further application forwarding until the issue is resolved and the daemon restarted. No required history is silently erased. The CLI enforces overall startup/shutdown deadlines. If filesystem I/O prevents graceful shutdown, it exits with an explicit operational error and leaves leases for operator recovery.

Live disk accounting conservatively reserves body/index/sequence overhead before engine writes, plus metadata headroom. Reservations are not returned for overwrites or failed writes. Actual files are reconciled at most once per second of write activity; explicit maintenance inventory also scans disk. Reconciliation never reduces live reservations. The store-byte metric therefore includes headroom and can exceed actual disk usage, and a busy session can hit its budget early. Restart inventories preserved files without deleting history. Do not edit store directories while the daemon owns them.

## Offline client maintenance

Stop the gateway first. Maintenance acquires the same exclusive leases as the daemon, and refuses concurrent operation. Default maintenance is a dry run.

```sh
go run ./cmd/fix-multiplexer -list-clients
go run ./cmd/fix-multiplexer -prune-stale
go run ./cmd/fix-multiplexer -prune-stale -dry-run=false
go run ./cmd/fix-multiplexer -reset-client DEV_A
# Only after the client agrees to reset to sequence 1:
go run ./cmd/fix-multiplexer -reset-client DEV_A -dry-run=false
```

Prune eligibility requires an inactive client whose last disconnect is at least `inactive_client_ttl` ago. Active/unknown sessions are untouched. Retirement is persisted outside the removed per-session directory; restart cannot resurrect a pruned session. Disabled clients are not provisioned by the engine. Only configured downstream clients are selected; upstream state is never pruned by this command.

Store directories are hash-named beneath the configured roots. Metadata contains connection/retirement state, not FIX payloads. File-store bodies contain business messages for FIX resend and should be protected as sensitive test data. Root ownership is restricted when creating new directories; do not reuse an insecure shared directory.

New outgoing FIX Logon records retain only session header and sequence metadata; the original Logon and its credentials are still sent to the peer. Stores and backups created by older versions may still contain Logon credentials in plaintext. Do not delete or reset those stores without coordinating session state. Restrict access to all stores and backups and use encrypted storage as appropriate: application messages and other session data remain sensitive. Metadata-only logging does not sanitize durable FIX stores. See SECURITY_REVIEW.md.

Checkpoints before the Phase 4 store-path regression fix could create engine files directly in a root. Such legacy files now block startup explicitly. Do not reset or delete them to get past the check: preserve their sequence state and migrate them offline into the correct session hash directory, or deliberately coordinate fresh session provisioning. There is no automatic legacy migration. Configuration-relative paths are resolved from the process working directory; run documented commands from the repository root.

## Hard crash recovery

A crash leaves `.gateway-lock` in each configured store root. This intentionally blocks accidental concurrent ownership. Verify the previous process has terminated, then remove only those two empty lock directories before restart. Do not remove message or sequence files. A hard-kill integration test executes this operator recovery step and verifies increasing persisted sequences after restart.

The lock is conservative: there is no automatic PID-based stale-lock deletion. If connection metadata was left active by a crash, stale pruning skips it; inspect the state and use explicit coordinated reprovisioning when needed. Fsync reduces durability uncertainty but cannot provide atomic end-to-end delivery.

## Current protocol limitations

FIX 4.4 only; identical XML dictionaries on both boundaries. Standard and customized dictionary-defined groups are rebuilt through engine APIs. Scalar extension tags survive. Body-level ordering follows the engine; custom ordering requirements need explicit compatibility tests. Original header/trailer fields other than MsgType are discarded, including routing delegation and replay indicators. Embedded SOH in EncodedText is unsupported by engine v0.9.12 and tested as a reconstruction error. No custom parser or engine fork is used.

## Health, transport bounds and logging

`/live` checks process serving; `/ready` requires the upstream logged on, routing active and required store health. Disconnected downstream clients do not make the entire service unready. `/metrics` reports routing outcomes, queue bytes/depth, engine queue credit, cached store usage, disk warning threshold, replay observations and hashed per-session labels. Disable HTTP with `service.health_bind: ""`. The CLI `-probe` flag performs a bounded local HTTP check.

`fix_mux_auth_failures_total` counts missing or invalid passwords for configured downstream sessions. It does not count unknown sessions or TLS handshake failures. The rate-limited `authentication_failed` event contains a hashed session label and a generic reason, never the password. These observations do not limit login attempts.

Both network boundaries cap unread bytes before engine parsing. That cap also bounds read-ahead bursts; excessive bursts may cause disconnect even when individual messages are smaller than the limit. Downstream transport connections are capped at twice the number of enabled clients, with a minimum of two slots and a five-second initial read deadline. Established read inactivity has a 65-second bound and writes a five-second deadline, alongside FIX heartbeat handling.

The allowlist supports at most 128 configured clients, including disabled entries. Older configuration files containing `service.max_clients` must remove that field; strict validation rejects it. The connection cap is calculated automatically.

The upstream engine connects through a private loopback transport bridge because its API has no custom dial/read hook. The engine and bridge authenticate each other using an ephemeral in-memory TLS identity; the bridge then performs separately configured, certificate-verified TLS against the venue when enabled. The bridge copies opaque bytes for that same FIX session. It does not interpret FIX, tunnel admin messages between independent sessions, or replace the engine. Independent logon/admin/sequence behavior remains at the two FIX boundaries.

An unauthenticated local peer can still consume the bounded bridge handshake slot temporarily. Run the gateway with trusted local processes and protect its process memory and stores. See SECURITY_REVIEW.md.

Downstream mTLS checks that the client certificate chains to the configured CA; it does not bind a certificate identity to one SenderCompID. Configure distinct per-client FIX passwords and use network isolation where individual client identities must be protected. All logged-on clients still receive the same permitted broadcast messages.

Application sends admitted to the engine's internal queues are capped at 2MiB per session and 64MiB globally, in addition to routing queues and the routing 64MiB budget. Queue credit releases when the engine hands bytes to its writer, not when a peer accepts an order. Resends use the engine's synchronous writer path and are not refused for queue credit (that would risk gap-filling away business history). After a failed/disconnected send, conservative outstanding credit can remain until emission or daemon restart. Treat a full queue as a visible forwarding error, never as acceptance.

Default logs emit JSON timestamps, hashed session identity, direction, MsgType, sequence and outcome. They omit all body values, credential/custom sensitive tags, raw frames and untrusted engine error interpolation. Store payloads remain sensitive even though logs are metadata only. The built-in lint gate is go vet with the pinned CI Go version, plus gofmt; no external linter is claimed.

See TEST_STATUS.md and IMPLEMENTATION_STATUS.md for current acceptance gaps rather than treating these operations as a production or release guarantee.
