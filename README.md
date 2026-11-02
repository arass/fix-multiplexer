# FIX Multiplexer

**FIX protocol proxy and gateway for sharing one upstream FIX session across multiple clients.**

FIX Multiplexer is a standalone **FIX-to-FIX session multiplexer** for trading-system development, QA, certification rehearsals, and shared UAT environments. It lets multiple independent FIX clients connect to a single FIX counterparty session without changing those clients to use REST, WebSockets, or a proprietary SDK.

> **Project status:** Phases 1–5 have working implementations and local regression evidence, including independent QuickFIX/J peers and an extended socket soak. This is a nonproduction development checkpoint, with licensing undecided; no release is published. See [implementation checkpoint](docs/IMPLEMENTATION_STATUS.md) and [acceptance evidence and remaining variants](docs/TEST_STATUS.md).

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

### Features

- **Many-to-one FIX connectivity:** multiple downstream FIX clients, one upstream FIX session.
- **Native FIX on both sides:** no REST or WebSocket migration required.
- **Message fanout:** broadcast upstream application messages to connected downstream clients.
- **Independent sessions:** logon, heartbeats, sequence numbers, resend requests, and resets per FIX session.
- **Minimal business interference:** no order-owner database, `ClOrdID (11)` rewriting, or order-state tracking in the default mode.
- **Bounded resource use:** configurable session limits, retention policies, and cleanup for retired clients.
- **Broker-neutral configuration:** no hardcoded counterparty, proprietary data dictionary, or customer-specific logic.
- **Docker-first testing:** a self-contained demo with a simulated FIX counterparty and multiple clients.

The implementation uses [QuickFIX/Go](https://github.com/quickfixgo/quickfix) and accepts clients written in **any language** with a compatible FIX 4.4 engine. The independent Java fixture uses QuickFIX/J.

## Example use cases

| Use case | Benefit |
|---|---|
| Shared UAT or certification FIX session | Multiple developers can test through one allocated session. |
| Parallel trading-system integration tests | Distinct test clients can submit messages concurrently. |
| QA environments with limited venue connectivity | Reuse the same upstream connection without changing client application code. |
| FIX gateway and session-behavior experiments | Test downstream logon, reconnection, and message handling independently. |

## How message forwarding works

| Traffic | Behavior |
|---|---|
| Downstream → upstream application messages | Forward supported application messages through the single upstream FIX session, retaining business fields where possible while regenerating session headers and trailer. |
| Upstream → downstream application messages | Broadcast to all currently connected, authorized downstream sessions; each receives its own sequence numbers. |
| FIX administrative messages | Handled by each FIX session independently; **never** blindly forwarded between sessions. |
| Downstream disconnected | Default **live-only** mode does not accumulate new application messages for offline clients. |
| Upstream unavailable | Fail forwarding visibly; never claim successful upstream business delivery. |

### Recovery and retention

FIX session recovery is **not the same as end-to-end message recovery**. Each session's engine may resend messages already entered into its outgoing sequence stream. However, a client that is disconnected during a broadcast will not receive messages that were never sent to its session.

The downstream replay-history window defaults to a configurable **8 hours**, separately from upstream retention. Older history is not guaranteed; explicit reset/reprovisioning is required when retained history cannot satisfy recovery. Valid inbound `PossDup` application messages are forwarded, and downstream applications must tolerate duplicates. During upstream outages, the gateway returns an identified, standard FIX error rather than a venue execution report.

To prevent abandoned development sessions from consuming disk indefinitely, the design requires configurable retention and safe cleanup of inactive session stores. Expired state may require an explicitly coordinated FIX sequence reset on the next connection.

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the proposed failure semantics and [docs/TEST_PLAN.md](docs/TEST_PLAN.md) for recovery and cleanup testing.

## Nonproduction use only

**FIX Multiplexer is intended for development and testing, not production trading.** All clients share the counterparty's business state and potentially its order-ID uniqueness scope and rate limits. All connected downstream clients may receive each other's execution reports or other application messages.

- Use only with **trusted, authorized participants** and synthetic or otherwise permitted test data.
- Applications should tolerate unrelated application messages and use appropriately unique `ClOrdID` values.
- Do not assume this is a transparent substitute for a dedicated FIX session when testing counterparty-specific session recovery or certification behavior.
- Do not use for production order routing without substantially more isolation, delivery guarantees, security controls, and validation.

## Getting started

Go 1.23 or newer is required (development checks currently use 1.27.3).

```sh
go run ./cmd/fix-multiplexer -config config/example.yaml -check-config
go run ./cmd/fix-multiplexer -demo
go vet ./...
go test -short ./...
go test -tags=integration ./...
go test -race -tags=soak ./internal/gateway -run TestRoutingSoak -timeout=60s
```

On this Windows workspace, the verified local toolchain is `.tools/go/bin/go.exe`; it is not added to PATH. Linux race checks can run in the official Go container. Configuration currently requires loopback listeners; unknown fields and unsupported FIX versions fail startup. Credential values belong in environment/file references. Defaults and the sample are validated by tests.

The `-demo` command starts a local mock venue, the gateway and three clients, verifies orders (including inbound PossDup), grouped fanout, live-only disconnection/reconnection behavior and a standard gateway error on upstream outage, then exits. It uses temporary session stores and synthetic data. No venue credentials are required. The same scenario runs in a nonroot, network-isolated container:

```sh
docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke
```

The demo contains:

1. A mock FIX acceptor acting as the upstream counterparty.
2. The FIX multiplexer with one upstream initiator and multiple downstream acceptor sessions.
3. Three independent FIX test clients.
4. Automated smoke tests proving that orders reach the upstream counterparty and application-level reports reach connected clients.

Independent-engine checks are reproducible with [QuickFIX/J](interop/quickfixj/README.md). The extended socket soak runs separately from fast tests:

```sh
docker build --target build -t fix-multiplexer-build .
docker run --rm --network none --tmpfs /tmp:exec,size=512m --memory 1g fix-multiplexer-build go test -race -tags=integration,socketsoak ./internal/fixadapter -run TestSocketSoak -timeout=21m -v
docker run --rm --network none fix-multiplexer-build go run ./scripts/sbom /fix-multiplexer
```

The final command emits a linked Go module inventory, including executable SHA256; the verified Linux build inventory is in [docs/sbom-linux-amd64.json](docs/sbom-linux-amd64.json). It does not inventory OS packages or imply a vulnerability audit. Container build dependencies require public downloads; tests use synthetic local peers.

To run the gateway against separately started local peers, use `go run ./cmd/fix-multiplexer -config config/example.yaml`. Its file stores persist independently. Eight-hour downstream replay eligibility, disk budgets and explicit offline retirement/reset are enforced. Read [operations and crash recovery](docs/OPERATIONS.md) before using persistent stores. `/live`, `/ready` and `/metrics` bind to loopback by default; `-probe live`, `-probe ready` and `-probe metrics` query them. TLS/mTLS, transport limits, metadata-only logs and startup/shutdown deadlines are implemented. Full acceptance evidence and remaining gaps are tracked in [test status](docs/TEST_STATUS.md).

The adapter reconstructs business fields and dictionary-defined nested groups. All original header/trailer fields are discarded except MsgType, including OnBehalfOf/DeliverTo and PossDup/OrigSendingTime; inbound PossDup application messages are routed as fresh messages on the other session. Unknown scalar body tags survive; custom groups require the same customized XML dictionary on both sides. Top-level body fields follow engine ordering. Embedded SOH in EncodedText is currently unsupported by the engine; no custom parser is added. See docs/PRIOR_ART.md.

This product includes software developed by quickfixengine.org (http://www.quickfixengine.org/). See [third-party notices](THIRD_PARTY_NOTICES.md).

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

**Project licensing is undecided by the owner's explicit choice.** No project `LICENSE` has been granted. Dependency notices are retained separately; do not infer permission to reuse this project's code from its dependencies' licenses. Public release and tagging remain deferred.

---

**Search terms:** FIX protocol proxy, FIX gateway, FIX session multiplexer, FIX session sharing, QuickFIX/Go, FIX acceptor-to-initiator bridge, FIX message fanout, FIX testing, FIX UAT.
