# FIX Multiplexer

**Open-source FIX protocol proxy and gateway for sharing one upstream FIX session across multiple clients.**

FIX Multiplexer is a standalone **FIX-to-FIX session multiplexer** for trading-system development, QA, certification rehearsals, and shared UAT environments. It lets multiple independent FIX clients connect to a single FIX counterparty session without changing those clients to use REST, WebSockets, or a proprietary SDK.

> **Project status:** Implementation specification / starter repository. The gateway and Docker demo described below are planned, not yet shipped. See [CODEX_TASK.md](CODEX_TASK.md) for the implementation brief.

```text
Dev A ── FIX ──┐
Dev B ── FIX ──┤     ┌────────────────────────────┐
QA    ── FIX ──┼────►│ FIX Multiplexer            │──── FIX ───► Counterparty UAT
CI    ── FIX ──┘     │ downstream: FIX acceptor   │   one upstream FIX session
                     │ upstream:   FIX initiator  │
                     └────────────────────────────┘
                       Orders/requests: many → one
                       Reports/events:  one → many
```

## Why use a FIX multiplexer?

Brokers, exchanges, and trading venues often grant only one or a few FIX sessions for development and testing. That creates a bottleneck when several developers, automated test suites, and environments need to send FIX messages concurrently.

FIX Multiplexer is designed to act as a **FIX protocol proxy server**: downstream clients retain their existing FIX implementations and connect to the proxy, which manages one upstream session. Unlike a TCP proxy, the gateway terminates each FIX session and maintains independent FIX sequencing and recovery state.

### Planned features

- **Many-to-one FIX connectivity:** multiple downstream FIX clients, one upstream FIX session.
- **Native FIX on both sides:** no REST or WebSocket migration required.
- **Message fanout:** broadcast upstream application messages to connected downstream clients.
- **Independent sessions:** logon, heartbeats, sequence numbers, resend requests, and resets per FIX session.
- **Minimal business interference:** no order-owner database, `ClOrdID (11)` rewriting, or order-state tracking in the default mode.
- **Bounded resource use:** configurable session limits, retention policies, and cleanup for retired clients.
- **Broker-neutral configuration:** no hardcoded counterparty, proprietary data dictionary, or customer-specific logic.
- **Docker-first testing:** a planned self-contained demo with a simulated FIX counterparty and multiple clients.

The proposed implementation uses [QuickFIX/Go](https://github.com/quickfixgo/quickfix), but the published service is intended to be usable by clients written in **any language** with a compatible FIX engine.

## Example use cases

| Use case | Benefit |
|---|---|
| Shared UAT or certification FIX session | Multiple developers can test through one allocated session. |
| Parallel trading-system integration tests | Distinct test clients can submit messages concurrently. |
| QA environments with limited venue connectivity | Reuse the same upstream connection without changing client application code. |
| FIX gateway and session-behavior experiments | Test downstream logon, reconnection, and message handling independently. |

## How message forwarding works

| Traffic | Planned behavior |
|---|---|
| Downstream → upstream application messages | Forward supported application messages through the single upstream FIX session, retaining business fields where possible while regenerating session headers and trailer. |
| Upstream → downstream application messages | Broadcast to all currently connected, authorized downstream sessions; each receives its own sequence numbers. |
| FIX administrative messages | Handled by each FIX session independently; **never** blindly forwarded between sessions. |
| Downstream disconnected | Default **live-only** mode does not accumulate new application messages for offline clients. |
| Upstream unavailable | Fail forwarding visibly; never claim successful upstream business delivery. |

### Recovery and retention

FIX session recovery is **not the same as end-to-end message recovery**. Each session's engine may resend messages already entered into its outgoing sequence stream. However, a client that is disconnected during a broadcast will not receive messages that were never sent to its session.

To prevent abandoned development sessions from consuming disk indefinitely, the design requires configurable retention and safe cleanup of inactive session stores. Expired state may require an explicitly coordinated FIX sequence reset on the next connection.

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the proposed failure semantics and [docs/TEST_PLAN.md](docs/TEST_PLAN.md) for recovery and cleanup testing.

## Nonproduction use only

**FIX Multiplexer is intended for development and testing, not production trading.** All clients share the counterparty's business state and potentially its order-ID uniqueness scope and rate limits. All connected downstream clients may receive each other's execution reports or other application messages.

- Use only with **trusted, authorized participants** and synthetic or otherwise permitted test data.
- Applications should tolerate unrelated application messages and use appropriately unique `ClOrdID` values.
- Do not assume this is a transparent substitute for a dedicated FIX session when testing counterparty-specific session recovery or certification behavior.
- Do not use for production order routing without substantially more isolation, delivery guarantees, security controls, and validation.

## Getting started

**No runnable release exists in this starter repository yet.** The implementation task calls for a reproducible Docker Compose demo containing:

1. A mock FIX acceptor acting as the upstream counterparty.
2. The FIX multiplexer with one upstream initiator and multiple downstream acceptor sessions.
3. Three independent FIX test clients.
4. Automated smoke tests proving that orders reach the upstream counterparty and application-level reports reach connected clients.

Once implemented, this section should be replaced with **verified** build, configuration, and startup commands, including the exact Docker Compose invocation and sample FIX session identifiers.

For contributors implementing the project, start with [CODEX_TASK.md](CODEX_TASK.md), [AGENTS.md](AGENTS.md), and [config/example.yaml](config/example.yaml).

## Architecture and project documentation

- [Architecture and failure semantics](docs/ARCHITECTURE.md)
- [Comprehensive test plan](docs/TEST_PLAN.md)
- [Implementation task for Codex](CODEX_TASK.md)
- [Development instructions](AGENTS.md)
- [Related FIX engines and prior art](docs/PRIOR_ART.md)
- [Contributing](CONTRIBUTING.md)

## Contributing

Contributions are welcome, especially interoperability tests with different FIX engines, recovery edge cases, documentation improvements, and broker-neutral enhancements. Counterparty-specific behavior should live in configuration or isolated adapters rather than the core routing layer.

See [CONTRIBUTING.md](CONTRIBUTING.md). Please do not submit proprietary venue configuration, production credentials, or customer data.

## License

**Apache License 2.0 is proposed.** An actual `LICENSE` file must be added and third-party dependency obligations reviewed before public release. Until then, do not assume an open-source license has been granted.

---

**Search terms:** FIX protocol proxy, FIX gateway, FIX session multiplexer, FIX session sharing, QuickFIX/Go, FIX acceptor-to-initiator bridge, FIX message fanout, FIX testing, FIX UAT.
