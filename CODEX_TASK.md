# Codex implementation brief: FIX Multiplexer

## Mission

## Confirmed implementation decisions (2026-11-02)

- Use Go and QuickFIX/Go, subject to verifying engine behavior and licensing.
- When upstream is unavailable or forwarding fails, return a clearly identified gateway-generated standard FIX error. Prefer BusinessMessageReject (35=j) where valid, with references to the failed request and a safe reason; never fabricate venue execution reports or imply venue acceptance. Verify callback behavior and failure delivery with integration tests.
- Do not discard inbound application messages merely because PossDupFlag is set. Preserve valid replayed application traffic through routing; do not route outbound engine resend callbacks as new inbound traffic. Exactly-once delivery is not promised.
- Default downstream replay history window: configurable 8h. This does not create offline fanout delivery. History expiry requires an explicit session reset/reprovision policy, rather than pretending older resends remain available. Keep upstream retention independently configured. Never delete active replay history silently.
- Keep task documents and the implementation checkpoint current. Commit locally after each completed, verified step; record incomplete checks and resume instructions honestly. No push is requested.
- Use three downstream clients in the demo and acceptance tests.

See docs/IMPLEMENTATION_STATUS.md for completed work, pending tasks, and environment blockers. These decisions supersede conflicting earlier suggestions in the starter specification.

Implement a ready-to-run, open-source, **generic standalone FIX session multiplexer** for nonproduction use. Multiple downstream FIX initiators connect to our FIX acceptor; one upstream FIX initiator connects to a venue's FIX acceptor. Forward downstream application messages upstream and broadcast upstream application messages to currently connected, logged-on downstream FIX sessions. No order ownership database, no `ClOrdID` translation, no WebSocket API, and no dependency on any private counterparty code.

Use **Go + QuickFIX/Go** by default. Although the daemon is language-agnostic to its users, Go makes reuse and review by the existing development team easy. Build a CLI binary and container. Keep business logic separate from the engine adapter so an alternative engine remains possible.

### Prior art to inspect first

- https://github.com/quickfixgo/quickfix (engine; sessions, stores, TLS, FIX dictionaries)
- https://github.com/quickfixgo/examples (initiator and acceptor examples; mock executor)
- https://github.com/sotarak/nest-quickfix (multi-session broadcast ideas, bounded resource use, authentication, logging, recovery testing; **do not** port its engine)
- https://github.com/artiofix/artio (separation of engine/session infrastructure and routing; not a turnkey mux)
- https://github.com/quickfix-j/quickfixj (alternate engine and interoperability test partner)

Read current upstream documentation and source before choosing APIs; verify licenses. Do not infer that prior art implements our specific functionality. Identify any ready-made product that actually does so, and record findings in `docs/PRIOR_ART.md`.

## Mandatory behavior

### Session architecture

- One separately configured upstream FIX initiator session per process in v1.
- Multiple distinct downstream FIX acceptor sessions. Allowlisted clients only; unique stable IDs; configurable maximum count and duplicate connection policy.
- Support an initial tested FIX version, preferably FIX 4.4 end-to-end. **Explicitly reject unsupported cross-version forwarding**; advertise additional versions only with tests. FIXT 1.1 / FIX 5 and version translation are out of v1 scope unless trivial and verified.
- An adapter must take an inbound *application* message and create a valid new outbound application message without copying upstream session-level sequencing, time or identity headers. Preserve business body values, repeating groups, group ordering, extension tags and relevant permitted application headers. Establish an explicit and tested policy for fields like `OnBehalfOfCompID`, `DeliverToCompID`, `PossDupFlag`, and `OrigSendingTime`: never blindly copy session semantics across boundaries.
- `ToAdmin`/`FromAdmin` are local to each session. `ToApp`/`FromApp` used for delivery/routing according to QuickFIX/Go's actual callback behavior. Avoid double-routing on replay, and distinguish application callbacks from resend/replay callbacks.

### Routing

- Downstream application message → upstream, subject to connection readiness, supported MsgType policy and bounded work queue.
- Upstream application message → fanout to all currently logged-on, authorized downstream sessions; clone message per target to avoid shared mutable message corruption.
- Default fanout includes execution reports and other configured application message types, not just `35=8`. Have safe configurable allow/deny lists and document dropped/unsupported types. Never forward administration messages across boundaries.
- Do not rewrite `ClOrdID (11)`, `OrigClOrdID (41)`, `OrderID (37)` or `ExecID (17)`.
- No per-order routing map, no external database, no mandatory message broker.
- If upstream is unavailable, ensure downstream submissions are **not silently lost**. Specify, implement and test a consistent failure mode. Consider a temporary local reject for orders only if it can be distinguished from upstream broker rejection and does not masquerade as an upstream execution report. Prefer a generic visible error/status for unsupported message types. Note that a protocol engine can advance its incoming seq counter even if application forwarding fails; document limitations. In no circumstances promise exactly-once delivery or broker-side acknowledgment merely from a successful local send.
- If a downstream recipient fails during fanout, continue to other recipients; count/report partial failures. Delivery is best effort across distinct sessions; never claim atomic broadcast.

### Session recovery, offline clients, retention

- Use engine-supported durable session store for sequence state and resend-eligible **outbound** messages, both upstream and downstream. Inspect actual QuickFIX/Go persistence support and do not assume its default store is automatically size/age bounded.
- FIX resend only replays messages previously emitted/assigned to a specific session. Disconnected clients have **no offline application queue by default** and miss new upstream reports until reconnection. This must be visibly documented and tested.
- Reconnect an active client's existing session without unnecessary sequence resets; test gaps, resends, duplicate flags and no duplicate business forwarding. On a stale session whose store has been pruned, require coordinated clean sequence reset/reprovisioning. Never quietly reuse deleted state with the old sequence expectation.
- Support configurable `client_idle_ttl` and an admin cleanup mechanism that removes only **disconnected** sessions that meet the TTL. Never delete an active session's store or reset sequence numbers behind an active FIX peer.
- Avoid unbounded disk growth. Define and implement a documented retention budget: overall disk alert/limit, per-client max message store size and/or time, and an honest policy when replay history must be discarded. **Do not silently delete replay-required FIX messages and then promise correct resends.** Prefer retire/disable/reset/reprovision stale clients and fail closed for a live session whose durability cannot be maintained.
- Address session schedules, daily resets, and `ResetSeqNumFlag (141)` per endpoint; defaults must preserve recovery within a test session and not force reset on every restart.
- Explicitly handle the difference between upstream disconnect (messages not received at the gateway), downstream disconnect (live-only fanout missed), and gateway crash (messages emitted or queued before crash). Document inevitable delivery uncertainty windows.

### Configuration, security and operations

- YAML or TOML top-level config, validated on startup, wrapping actual FIX engine configuration. Config examples contain mock credentials, not real counterparties.
- Environment variables / mounted secret files for credentials; never print secrets, private keys or sensitive tag values. Default logs contain timestamps, direction, sanitized session IDs, MsgType, sequence number and outcomes, not entire FIX payloads.
- Downstream session allowlist, bind address, optional TLS/mTLS where the engine supports it, TLS certificate verification for upstream, session count limit, maximum message size, capped routing queues, startup/shutdown timeouts, graceful termination. Loopback-only default example; no unauthenticated public listener.
- Health endpoints: `/live` for process, `/ready` true only if upstream logged on and internal routing is healthy, `/metrics` with per-session status, forwarded count, failed forwards, fanout recipients, drops, resend events where observable, queue depth, store usage, cleanup counts. Restrict or disable management HTTP exposure by default.
- Prefer a single process and simple Dockerfile/docker-compose setup. No Kubernetes or cloud dependency for v1.
- Provide both `live-only` fanout semantics and upstream-unavailable policy prominently in runtime logs and README.

## Proposed implementation layout (adjust if justified)

```text
cmd/fix-multiplexer/main.go
internal/config/             # parse and validate settings
internal/gateway/            # routing policy, fanout, health
internal/fixadapter/         # QuickFIX/Go Application and message translation
internal/sessions/           # allowlist and session registry
internal/retention/          # closed-session cleanup and store inventory
internal/observability/      # redacted logs, metrics, health
internal/testfix/            # test mock broker and clients
config/example.yaml
config/quickfix-upstream.cfg
config/quickfix-downstream.cfg
examples/docker-compose.yml
Dockerfile
Makefile
.github/workflows/ci.yml
README.md
AGENTS.md
CODEX_TASK.md
docs/ARCHITECTURE.md
docs/TEST_PLAN.md
docs/PRIOR_ART.md
CONTRIBUTING.md
LICENSE
```

## Implement in reviewable phases

**Phase 1: skeleton and semantics.** Verify QuickFIX/Go APIs and licensing; config validation; local mock upstream and downstream clients; core routing interfaces and in-memory test fakes; CI and README. Prove two simultaneous downstream clients and one upstream FIX session.

Progress: Phase 1 implemented in milestones 1a/1b: strict config, CLI, engine-independent core/fakes, local engine peers and three-client proof. See docs/IMPLEMENTATION_STATUS.md for executed checks and remaining limitations.

**Phase 2: forwarding and fanout.** Forward supported app messages and broadcast reports; correctly rebuild FIX sessions; preserve groups and custom tags; enforce allowlist and connection-state behavior; add unit and real-socket integration tests.

Progress: initial implementation and regressions pass for scalar/custom fields, dictionary-defined nested groups, fresh session headers, supported-type policy, queue saturation, slow-client isolation, live-only disconnection and upstream FIX errors. Socket demo includes valid inbound PossDup orders and grouped broadcast reports. Full acceptance matrix remains open across subsequent phases; no release claim.

**Phase 3: reconnect/recovery.** Durable session stores; resend behavior; logon/logoff and reconnect; upstream outage; no offline application queue; restarts; duplicate avoidance; configurable session reset strategy. Add recovery integration tests and failure-injection tests.

Progress: initial implementation covers durable restart, downstream stored replay, genuine upstream gap replay, hard-kill recovery, configurable 8h downstream history, independent upstream policy, fail-closed disk budgets and explicit offline prune/reset with persistent retirement. Fault injection uses synchronized mock stores; no blanket PossDup filtering. Operational metrics and additional failure/interop/soak coverage remain for Phases 4/5.

**Phase 4: safety and maintainability.** Bounded resources and retention, client deprovisioning, credentials/TLS, metrics, health checks, graceful shutdown, container build, documentation and contribution guide. Add race, fuzz/property and soak tests.

Progress: implemented metadata-only logs, health/metrics, TLS/mTLS, bounded transports, engine queue credit, startup/shutdown deadlines, root-level legacy-state rejection, safer cleanup, coverage gate, network-isolated Compose demo and core soak. TLS, resource, restart and race regressions pass. Extended socket stress and remaining acceptance variants are tracked in docs/TEST_STATUS.md for final auditing. Licensing explicitly remains undecided; do not add LICENSE or tag a release without new owner direction.

**Phase 5: interoperability and release.** Test against a *different* FIX implementation (e.g. QuickFIX/J) in CI or reproducibly locally; verify README demo and deployment from a clean checkout; check software bill of materials/licenses; tag only after acceptance criteria pass.

Progress: independent QuickFIX/J 2.3.2 peers passed groups/custom fields, admin isolation, both resend directions, durable reconnect and live-only exclusion. Extended race-enabled socket soak passed 50,000 orders/reports, ten clients, 100 reconnects and 499,900 report deliveries; offline downstream cleanup returned 103,501,141 bytes to 820 bytes of metadata. Added CI interop/manual socket-soak jobs and linked-binary module/license inventory. The documented Compose demo passed from a clean local clone at 6039d6f; its executable SHA256 matches the recorded inventory. Release/tag and project licensing remain deferred by owner choice; outstanding stress variants remain explicit in docs/TEST_STATUS.md.

## Acceptance criteria

1. Local automated end-to-end test: 3 distinct downstream FIX initiators successfully log on and send orders through **one** upstream FIX initiator. Mock upstream sees correct business tags and one session.
2. One mock upstream execution report is delivered to all 3 active downstream clients, with correct **independent** target session headers and sequence numbers, unchanged business IDs.
3. Disconnect one downstream client; other clients continue; disconnected client does not accumulate messages generated while offline. Upon reconnect it can resend previously assigned but missing messages as FIX allows.
4. Upstream disconnect: new downstream messages are not silently discarded, health becomes not-ready, reconnection restores eligible service. No duplicate application forwarding caused by FIX resend callback handling.
5. Restart with persistent stores: expected session sequence numbers recover, resends work within retained history. Stale retired client cleanup has bounded storage and a documented reset/reprovision step.
6. Invalid credentials/session ID, disabled clients, oversize messages, queue saturation and store failures fail visibly and safely.
7. Race detector passes, unit tests cover every row of `docs/TEST_PLAN.md`, and project-owned logic reaches >=90% statement coverage (excluding third-party/generated code). Include an isolated integration-test target; do not include timing-sensitive socket tests in unit coverage claims.
8. Docs clearly distinguish nonproduction fanout from order routing, FIX replay from offline delivery, and local gateway acceptance from upstream business acceptance.
9. Reproduction from a clean checkout: documented one-command smoke demo, build and container commands; no proprietary network or credentials needed.

## Do not implement in v1

- WebSocket/REST trading API; order ID translation; per-order response routing; automatic exchange certification; FIX version conversion; production-grade active-active HA; database-backed order ledger; private counterparty SDK; guaranteed end-to-end delivery; unlimited offline spooling.

## If constraints conflict

Do not silently choose data loss or unbounded resource usage. Implement a small, explicit, safe default; document the tradeoff; write a failing test for any unimplemented guarantee; call out limitations clearly in the PR summary.
