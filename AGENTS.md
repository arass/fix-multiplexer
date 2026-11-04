# Instructions for coding agents

Read `docs/CODEX_TASK.md`, `docs/ARCHITECTURE.md`, `docs/TEST_PLAN.md`, and `README.md` before modifying code. These files are authoritative for scope and semantics; do not silently broaden the project into an order management system.

## Core invariants

1. **Never blindly proxy FIX admin messages.** QuickFIX/Go owns Logon, Logout, Heartbeat, TestRequest, ResendRequest, SequenceReset and session-level sequence numbers separately for each side.
2. **Never forward original session framing.** Forward application-level FIX fields through the engine, reconstructing valid per-session header/trailer and avoiding stale session fields. Preserve repeating groups, field order where required, and extension tags; test these explicitly.
3. **No `ClOrdID` rewriting or order-affinity state in default mode.** Broadcast upstream application messages to all currently logged-on, allowlisted downstream sessions. Collision risks must be documented.
4. **Default to live-only downstream fanout.** No offline application queue. Once a message has been assigned a live downstream session sequence number, its replay history is governed by that session's message store. Never conflate offline delivery with FIX resend.
5. **Never silently drop outbound orders when upstream is unavailable or forwarding fails.** Return an explicit protocol-appropriate or operational error; test the chosen behavior. Avoid falsely signaling order acceptance or guaranteeing exactly-once delivery.
6. **Document capabilities and limits accurately.** Deployment is an operator decision; do not claim production readiness or certification without evidence. Fail closed on unknown clients, invalid configuration, unacceptable resource consumption and incompatible FIX versions.
7. **Secrets and PII.** No real customer data, upstream credentials, private keys, or unredacted message content in tests, examples, issues, or CI logs. Redact FIX credential tags and support metadata-only logging by default.
8. **Don't modify or depend on any private counterparty-specific project.** It may be used for independent compatibility testing only if its owner explicitly provides it and license rights permit.
9. **Minimize hidden global state.** Use dependency injection for FIX session sender, session registry, clock, durable store, config, and shutdown hooks. Prefer deterministic tests.
10. **Keep the project generic.** No vendor-specific CompIDs, endpoints, business tags, credential rules, or integrations hardcoded into the main package.

## Engineering workflow

- Work through the phases in `docs/CODEX_TASK.md`, keeping the repo runnable at each milestone.
- Favor a small, documented core over performance micro-optimizations.
- Write table-driven unit tests before or alongside each behavior. Prefer race-safe and event-driven integration tests; avoid `time.Sleep` as synchronization.
- Unit tests must not need internet, live counterparty credentials, Docker, or developer-specific environments.
- Integration tests must use local FIX engines and mock counterparties. No live counterparty connectivity in CI.
- Run and report: `go fmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, coverage, and container smoke tests. Adapt CI commands to the implementation, and never claim success without execution.
- Target **>=90% statement coverage for project-owned routing/session orchestration/config/retention packages**, excluding generated or third-party engine code. More importantly, cover every test scenario in `docs/TEST_PLAN.md`. Do not inflate coverage with trivial tests.
- Document unresolved questions and unsupported FIX message types, and add regression tests for every bug.
- Do not refactor the underlying FIX engine or implement your own FIX parser, sequencing or resend protocol unless an independently tested defect absolutely requires it.

## Pull request checklist

- [ ] Behavior and operational limitations documented.
- [ ] Tests for happy path, recovery, failures, security boundaries, concurrency and cleanup.
- [ ] No broker-specific data or secrets introduced.
- [ ] Race detector and lint checks pass.
- [ ] Configuration compatibility and retention changes documented.
- [ ] New functionality has minimal user-facing examples.
