# Local development operations

The gateway is a nonproduction tool for mutually trusted participants. Listeners currently require loopback. All configured clients see upstream application broadcasts. Local engine acceptance is not venue acceptance, and no exactly-once guarantee is made.

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

Age limits govern replay eligibility, not rolling deletion from active stores. Engine files can retain older bytes until offline cleanup, while `session_limit_bytes` and aggregate `disk_limit_bytes` bound growth. Store writes include an overhead reservation. A durability/budget failure is latched and stops further application forwarding until the issue is resolved and the daemon restarted. No required history is silently erased. Slow disk writes may still delay shutdown; full bounded transport and shutdown hardening remains a Phase 4 task.

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

## Hard crash recovery

A crash leaves `.gateway-lock` in each configured store root. This intentionally blocks accidental concurrent ownership. Verify the previous process has terminated, then remove only those two empty lock directories before restart. Do not remove message or sequence files. A hard-kill integration test executes this operator recovery step and verifies increasing persisted sequences after restart.

The lock is conservative: there is no automatic PID-based stale-lock deletion. If connection metadata was left active by a crash, stale pruning skips it; inspect the state and use explicit coordinated reprovisioning when needed. Fsync reduces durability uncertainty but cannot provide atomic end-to-end delivery.

## Current protocol limitations

FIX 4.4 only; identical XML dictionaries on both boundaries. Standard and customized dictionary-defined groups are rebuilt through engine APIs. Scalar extension tags survive. Body-level ordering follows the engine; custom ordering requirements need explicit compatibility tests. Original header/trailer fields other than MsgType are discarded, including routing delegation and replay indicators. Embedded SOH in EncodedText is unsupported by engine v0.9.12 and tested as a reconstruction error. No custom parser or engine fork is used.

Health/metrics, TLS failure-injection, pre-parser transport resource bounds, full shutdown bounds, soak and independent-engine interoperability are still being implemented. See IMPLEMENTATION_STATUS.md for the current checkpoint rather than treating this document as a release guarantee.
