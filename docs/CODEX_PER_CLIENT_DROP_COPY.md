# Codex task: Per-client read-only FIX access and Drop Copy receive filtering

## Goal
Extend the **existing** `fix-multiplexer` project to allow **mixed downstream clients** on one upstream FIX session: some clients may send permitted application messages upstream, while others are **read-only** recipients of selected upstream messages (e.g., execution reports / Drop Copy). Make the feature broker-neutral and useful beyond trading UAT.

**This task supersedes the prior `CODEX_DROP_COPY_SUPPORT.md` instruction that read-only must be accomplished solely by setting the global `upstream.app_message_types: []`.** Global deny-all must remain available, but the primary new control is **per-client `read_only`**. Do not replace the existing FIX engine or build a new gateway.

Read `AGENTS.md`, existing source, tests, `config/example.yaml`, README, and documentation before changes. Follow actual repository conventions. Preserve backward compatibility and current nonproduction limitations.

## Existing configuration semantics to preserve

- `service.mode: nonproduction` is currently the only supported mode. **Do not broaden production claims.**
- `upstream.app_message_types` is the **global** downstream-to-upstream application MsgType allowlist (currently `D,F,G,H,V,x`).
- `fanout.app_message_types` is the **global** upstream-to-downstream application MsgType allowlist (currently `8,9,j,W,X,Y,y`).
- `fanout.mode: live-only`: no offline application backlog.
- `downstream.clients` is an explicit SenderCompID allowlist. Each client has an independent FIX session and session store.
- Existing FIX administrative/session handling, credentials, TLS, retention, disk limits, metrics and logging remain intact.

## Proposed YAML

Use the repo's exact configuration schema and naming conventions. Add these two optional fields under each downstream client:

```yaml
upstream:
  # Global allowlist. Applies to every downstream client, including writable ones.
  app_message_types: ["D", "F", "G", "H", "V", "x"]

downstream:
  clients:
    - sender_comp_id: TRADING
      read_only: false                  # optional; false when omitted
      receive_message_types: ["8", "9"]
    - sender_comp_id: RISK
      read_only: true                   # may receive, cannot forward application messages upstream
      receive_message_types: ["8"]
    - sender_comp_id: COMPLIANCE
      read_only: true
      receive_message_types: ["8", "9", "AE"]
    - sender_comp_id: OBSERVER
      read_only: true                   # omitted receive list inherits global fanout list
    - sender_comp_id: QUIET
      read_only: true
      receive_message_types: []        # receives no upstream application messages

fanout:
  mode: live-only
  # COMPLIANCE can only subscribe to AE if AE is globally enabled.
  app_message_types: ["8", "9", "AE"]
```

Keep the original `config/example.yaml` a working UAT example; do not make all clients read-only. Add a separate runnable `config/drop-copy.example.yaml` containing mixed writable/read-only sessions and local/mock endpoints, without real secrets.

## Mandatory behavior

### Downstream -> upstream application messages

1. A **missing** `read_only` field preserves previous behavior (`false`).
2. `read_only: true` prohibits **all FIX application messages** from that downstream session from being forwarded upstream, regardless of MsgType. It does **not** close the session or disable administrative traffic.
3. `read_only: false` allows forwarding only for application MsgTypes in the existing **global** `upstream.app_message_types` allowlist. The global list remains authoritative; per-client settings never bypass it.
4. Add and test explicit `upstream.app_message_types: []` as a global application deny-all. The current validator rejects empty allowlists. Omitted must retain the existing defaults.
5. Enforce the policy based on the **authenticated, configured FIX session identity** (e.g., its validated SenderCompID/session key), not untrusted values inside an application message body. Unknown/unconfigured sessions must not gain write access.
6. Decide and implement consistent handling of forbidden application messages, using the project's existing rejection/notice conventions if protocol-correct; do **not** invent FIX session-level Reject messages for policy denials or disconnect users merely for attempting to send an application message. Expose blocked counters and safe metadata logging. Document downstream client's experience.
7. Perform authorization before an application message enters the upstream queue or upstream outgoing store, or is forwarded upstream. Downstream FIX session bookkeeping and the client's own store are separate.

### Upstream -> downstream application messages

8. Add optional `receive_message_types` per client. **Absent** = inherit the global `fanout.app_message_types` list. **Explicit `[]`** = receive no application messages.
9. An explicit nonempty list must be a **subset** of the global `fanout.app_message_types`. Prefer a clear config validation failure for non-subset entries, rather than silently widening or intersecting. Validate exact, case-sensitive FIX MsgType strings; support multi-character values like `AE`.
10. Receive filtering applies to **both writable and read-only clients**. `read_only` affects only client -> upstream application messages; it does not implicitly alter received message types.
11. Filter before sending to that downstream FIX session. Disallowed fanout messages must not consume that downstream session's outgoing sequence number or enter its outgoing resend store.
12. Do not rewrite `ClOrdID` or application fields or maintain order ID mappings. Do not relay original upstream FIX header sequence numbers downstream.

The global `fanout.deny_message_types` remains authoritative even when a client subscribes to a type. Receive filters govern upstream application messages, not gateway-generated operational messages: preserve the existing shared upstream session Reject notices and deliver gateway-generated policy errors to the submitting client, even for an empty receive list. Shared notices may contain counterparty text visible to all connected clients.

Policy changes do not erase messages previously accepted into the upstream FIX session's resend history. Only new submissions denied by the current policy are prevented from entering the upstream queue and store.

### FIX session control and reliability

13. FIX administrative messages (Logon, Logout, Heartbeat, TestRequest, ResendRequest, SequenceReset, etc.) must continue to be handled independently by each FIX engine/session in **both directions** for **all** clients, including read-only ones. Never blindly broadcast or forward raw FIX administrative messages.
14. Preserve session resend recovery for messages that were actually sent and stored on that particular downstream FIX session. **Do not** create an application backlog for reports generated while a client was disconnected; retain `live-only` semantics.
15. Preserve bounded storage, expiry, client lifecycle handling, backpressure, reconnect handling and graceful shutdown. Do not introduce indefinite retention.
16. Preserve existing client authentication and TLS enforcement. Do not treat read-only clients as inherently safe: they still receive potentially sensitive order/execution information.
17. On any existing configuration reload path, policy tightening must not create a temporary forwarding window; invalid reloads must be rejected atomically. If reload is unsupported, do not add it just for this task.

## Intended use and limitations

A read-only client is a **subscriber to the messages available on the gateway's existing upstream session**. This does not magically obtain a venue-wide Drop Copy feed: that is available only if the upstream counterparty actually sends it. Mixed writable + read-only clients may be useful for UAT observability, but are **not equivalent** to a venue-provided consolidated Drop Copy feed.

Use terminology **"best-effort Drop Copy-style fan-out"** or **"read-only FIX subscriber"** unless the upstream really is a Drop Copy feed. Document that this is not production-certified, not an authoritative audit trail, and does not guarantee complete delivery or offline recovery.

## Tests required

Checkpoint: real FIX-session tests now cover read-only and receive filters across disconnect/reconnect and gateway restart; live-only delivery; replay of a previously delivered report; filtered messages excluded from the downstream sequence and resend store; and a multi-character `AE` message with a repeating group delivered only to a matching subscriber. Unit and other socket tests cover basic policy and notice behavior. The broader matrix below still calls for explicit real-session `F`, unlisted-type and admin TestRequest coverage; do not treat these remaining cases as individually verified by the recovery test.

Write focused unit tests of all added branches plus real FIX-session integration tests. At minimum:

### Configuration and authorization unit tests

- Existing configs lacking both fields behave unchanged.
- `read_only` omitted/false/true behavior and YAML parser validation (reject non-boolean, unexpected/duplicate client config as applicable).
- Two simultaneous clients, one writable and one read-only, under a permissive global upstream allowlist: writable sends `D`, `F`, `G` upstream; read-only sends same and **none reach** upstream.
- A read-only client cannot send otherwise globally permitted application types, including query/market-data requests (`H`, `V`, `x`).
- Global `upstream.app_message_types: []` blocks application messages from writable **and** read-only clients; global policy cannot be bypassed.
- Deny unknown clients/unauthenticated sessions; cannot spoof another client's write access by altering tags or session identity.
- Forbidden messages never reach upstream queues, stores or replay paths; count/log denials without leaking bodies or secrets.
- Absent `receive_message_types` inherits global; explicitly empty denies all; explicit subset routes exactly those messages.
- Per-client types outside global fanout fail validation; case sensitivity (`x` vs `X`), multi-character `AE`, malformed/empty/duplicate entries as appropriate.
- Read-only client can still receive reports allowed by receive policy; writable client can be receive-filtered too.
- Filtering before downstream send does not increment downstream outgoing app sequence or append to resend storage.
- Session administrative traffic is never subjected to either application allowlist.
- All pre-existing resource limits, backpressure and retention tests still pass.

### Real FIX integration tests

1. Boot a mock upstream acceptor, gateway, and >=3 real downstream initiators (`TRADING`, `RISK`, `COMPLIANCE`) using the **existing FIX engine**, not a newly introduced one. Test concurrent sessions and differing policies.
2. Send valid `D` and `F` from writable TRADING, assert upstream receives them. Send the same from read-only RISK, assert upstream never receives them, while RISK stays logged on and can receive reports.
3. Send upstream `8`, `9`, `AE` and an unlisted application MsgType. Assert recipient sets and exact application payload preservation while downstream session headers and sequence numbers are independently managed.
4. Verify admin heartbeats, TestRequests, ResendRequests and reconnects work for read-only and writable sessions; forbidden application messages never appear after reconnect.
5. Disconnect RISK, generate upstream execution reports, reconnect RISK: no invented offline application backlog; reports previously sent on its FIX session can be recovered according to existing session store/resend rules.
6. Restart gateway with persistent stores: read-only restrictions remain effective immediately upon reconnect, with no temporary fail-open path.
7. Invalid config fails startup before accepting application traffic. Existing unmodified UAT config continues to behave identically.
8. If runtime reload exists, cover read-only <-> writable transitions, concurrent sends and atomic policy application.

Use deterministic synchronization where possible. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, lint and documented integration tests. Maintain the existing coverage gate, and add unit coverage of all new logic rather than relying solely on integration coverage.

## Documentation and scope

- Update `README.md` with **"Per-client read-only FIX subscribers"** and a concise mixed-client example.
- Update `config/example.yaml` comments and add `config/drop-copy.example.yaml`; validate examples in tests or CI.
- Update `docs/ARCHITECTURE.md`, `docs/TEST_PLAN.md`, and any configuration references that exist.
- Explain why receive filtering does not affect administrative FIX traffic, and why `live-only` does not guarantee replay of disconnected-client messages.
- Keep examples free of actual broker/customer secrets and client-specific references.
- Do not change licensing, commercial positioning or repository ownership in this task.
- Do not add a new FIX engine, order-state database, protocol transformation system, durable event broker, HA design, or production-grade Drop Copy guarantees.

## Completion report

Give a concise list of changed files; explain policy precedence; report exact test commands/results and coverage; list assumptions and known remaining limitations. Do not claim a test passed unless actually run.
