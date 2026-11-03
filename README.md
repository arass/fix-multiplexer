# FIX Multiplexer: FIX Gateway for Shared Sessions

**FIX protocol proxy and gateway for sharing one upstream FIX session across multiple clients.**

FIX Multiplexer is a standalone **FIX-to-FIX session multiplexer** for trading-system development, QA, certification rehearsals, and shared UAT environments. It lets multiple independent FIX clients connect to a single FIX counterparty session without changing those clients to use REST, WebSockets, or a proprietary SDK.

> **Project status:** A nonproduction development checkpoint with local regression evidence, including QuickFIX/J interoperability. Licensing is undecided and no release is published. A security review is in progress; use an isolated host or network namespace containing only trusted processes and see the [security review](docs/SECURITY_REVIEW.md).

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

### Per-client read-only FIX subscribers

Each configured downstream client can be independently restricted:

```yaml
downstream:
  clients:
    - sender_comp_id: TRADING
    - sender_comp_id: RISK
      read_only: true
      receive_message_types: ["8", "9"]
```

`read_only` defaults to false. A read-only client's application submissions are rejected with a gateway-generated BusinessMessageReject; its FIX session and administrative messages continue normally. Writable clients still obey `upstream.app_message_types`; set that list to `[]` to block all clients from submitting application messages. Omit `receive_message_types` to inherit the global fanout list, or use `[]` to receive no upstream application messages. Explicit types must be in the global fanout allowlist, and `fanout.deny_message_types` always takes precedence. Gateway-generated submission errors and shared upstream session Reject notices are delivered independently of receive filters.

Subscribers see matching messages for all clients sharing the upstream session. This is best-effort Drop Copy-style fan-out, not a consolidated venue feed or authoritative audit trail: the upstream must send the messages, and disconnected clients miss new broadcasts. FIX resend only recovers messages already assigned to that client's session and retained in its store. This remains a nonproduction tool. See `config/drop-copy.example.yaml` for a complete local configuration.

Changing a client to read-only does not erase previously accepted messages from the upstream FIX session's resend history; that session may replay messages already assigned to it according to normal FIX recovery rules.

**FIX Multiplexer is intended for development and testing, not production trading.** All clients share the counterparty's business state and potentially its order-ID uniqueness scope and rate limits. All connected downstream clients may receive each other's execution reports or other application messages.

- Use only with **trusted, authorized participants** and synthetic or otherwise permitted test data.
- Applications should tolerate unrelated application messages and use appropriately unique `ClOrdID` values.
- Do not assume this is a transparent substitute for a dedicated FIX session when testing counterparty-specific session recovery or certification behavior.
- Do not use for production order routing without substantially more isolation, delivery guarantees, security controls, and validation.

## Getting started

Go 1.23 or newer is required (development checks currently use 1.27.3). Run commands from the repository root. To try the FIX gateway without a counterparty or credentials:

```sh
go run ./cmd/fix-multiplexer -demo
```

On this Windows workspace, the verified local toolchain is `.tools/go/bin/go.exe`; it is not added to PATH. Linux race checks can run in the official Go container. The example listens on loopback; a non-loopback FIX listener requires per-client passwords. TLS is optional. Management remains loopback-only. Unknown fields and unsupported FIX versions fail startup. Credential values belong in environment/file references. Defaults and the sample are validated by tests.

The `-demo` command starts a local mock venue, the gateway and three clients, verifies orders (including inbound PossDup), grouped fanout, live-only disconnection/reconnection behavior and a standard gateway error on upstream outage, then exits. It uses temporary session stores and synthetic data. No venue credentials are required. The same scenario runs in a nonroot, network-isolated container:

```sh
docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke
```

The demo contains:

1. A mock FIX acceptor acting as the upstream counterparty.
2. The FIX multiplexer with one upstream initiator and multiple downstream acceptor sessions.
3. Three independent FIX test clients.
4. Automated smoke tests proving that orders reach the upstream counterparty and application-level reports reach connected clients.

## Configure a FIX session multiplexer for your counterparty

The gateway initiates **one** FIX 4.4 session to your counterparty and accepts separate FIX 4.4 sessions from your clients. Before starting, obtain the counterparty's UAT host, port, your assigned SenderCompID, its TargetCompID, any logon credentials, and TLS requirements. Agree on the initial sequence numbers and message types with the counterparty. Use synthetic or otherwise permitted test data.

1. Copy `config/example.yaml` to a local file, for example `config/local.yaml`, and edit that copy. Paths in the configuration are resolved from the working directory; the commands below assume the repository root.
2. In `upstream`, set `host`, `port`, `sender_comp_id` (the gateway's identity assigned by the counterparty), and `target_comp_id` (the counterparty's identity). Set `tls_enabled: true` if the counterparty requires TLS. Configure credentials and certificates as shown below when required.
3. In `downstream`, set `target_comp_id` to the gateway identity your clients will use as their TargetCompID. Add a distinct `sender_comp_id` under `clients` for each client's SenderCompID. The list supports up to 128 entries, including disabled clients; connection limits are calculated from enabled clients. Choose separate persistent `upstream.store_dir` and `downstream.store_dir` directories protected from other users; preserve them across restarts.
4. Check `upstream.app_message_types` and `fanout.app_message_types` against the messages you need. When omitted, upstream defaults to `D`, `F`, `G`, `H`, `V`, `x`; downstream defaults to `8`, `9`, `j`, `W`, `X`, `Y`, `y`. The example lists these defaults with their meanings. Add other application types your counterparty uses, such as `Z` (Quote Cancel) in the appropriate direction. Unsupported downstream submissions receive a gateway-generated FIX error; upstream types outside the fanout policy are dropped and counted.

   Set `upstream.reconnect_interval` to control the delay between exchange connection attempts, for example `60s` or `1m`. The default is 60 seconds (previous versions retried after one second); the minimum is one second. The first attempt is immediate. This controls the gateway's outbound exchange session. Downstream developers initiate their own connections and set their reconnect intervals in their own FIX clients; the gateway's concurrent connection limits do not rate-limit repeated attempts.

5. Validate and start the gateway:

   ```sh
   go run ./cmd/fix-multiplexer -config config/local.yaml -check-config
   go run ./cmd/fix-multiplexer -config config/local.yaml
   ```

   Validation checks the configuration schema and constraints; it does not prove credentials, certificates, connectivity, or a successful FIX logon. The daemon loads credentials, dictionaries and certificates when starting.
6. Configure each client's FIX initiator to connect to the configured `downstream.listen`. With the supplied example, client `DEV_A` connects to `127.0.0.1:9879` with `BeginString=FIX.4.4`, `SenderCompID=DEV_A`, and `TargetCompID=MULTIPLEXER`. The gateway presents the inverse identity on that session. Configure `DEV_B` and `QA_1` with their own sender IDs; do not reuse one session identity for multiple simultaneous clients. Coordinate sequence numbers instead of enabling automatic resets.
7. In another terminal, check the upstream session and store health:

   ```sh
   go run ./cmd/fix-multiplexer -config config/local.yaml -probe live
   go run ./cmd/fix-multiplexer -config config/local.yaml -probe ready
   go run ./cmd/fix-multiplexer -config config/local.yaml -probe metrics
   ```

   `/live` indicates that management is serving. `/ready` succeeds when the upstream FIX session is logged on and required routing and storage are healthy; it does not require every downstream client to be connected. If readiness fails, check the endpoint, CompIDs, credentials, TLS trust, and counterparty session status. If a client cannot log on, also check its allowlisted SenderCompID, TargetCompID, password and sequence numbers. Logs contain metadata rather than FIX payloads.

### FIX credentials and TLS configuration

The following are **partial YAML examples** to merge into the matching sections of your local configuration. Set the referenced environment variables securely in the gateway process environment; do not put credential values in YAML or commit them. A `file` reference can be used instead of `env`, for example `password: {file: /path/to/mounted/secret}`. Credentials must be nonempty, at most 4,096 bytes, and free of FIX delimiter and newline characters.

```yaml
upstream:
  tls_enabled: true
  username: {env: FIX_UPSTREAM_USERNAME}
  password: {env: FIX_UPSTREAM_PASSWORD}
  tls:
    ca_file: /path/to/venue-ca.pem       # omit to use system roots
    server_name: fix-uat.example.com     # must match the venue certificate
    # cert_file: /path/to/client-cert.pem # if the venue requires mTLS
    # key_file: /path/to/client-key.pem
downstream:
  clients:
    - sender_comp_id: DEV_A
      password: {env: FIX_DEV_A_PASSWORD}
```

For downstream TLS, set `downstream.tls_enabled: true` and provide `downstream.tls.cert_file` and `downstream.tls.key_file`. For mutual TLS, also set `downstream.tls.ca_file` and `downstream.tls.require_client_cert: true`; clients must trust the gateway certificate and present an accepted client certificate. The gateway verifies the certificate against its CA but does not bind it to a particular SenderCompID; a configured per-client password is checked separately in the FIX Logon. Without a password or mTLS, an allowlisted CompID alone is not a secret. An upstream TLS certificate is verified against the configured `server_name`, or the upstream host when that field is omitted.

### Allow developers to connect over a network

For a shared Docker server that developers connect to from their own machines, see the [Docker setup guide](docs/DOCKER_SETUP.md) for image handoff, configuration, port mapping, and firewall requirements.

To opt in, set `downstream.listen` to a literal IP address reachable by your developers, such as an address on your private network. A wildcard such as `0.0.0.0:9879` listens on all IPv4 interfaces. For any non-loopback listener, startup requires a password reference for **every enabled client**. Each client identifies its FIX session with its unique SenderCompID and sends its own password in Logon tag 554. These passwords are independent of the upstream counterparty username and password. An optional separate developer username in tag 553 is not currently validated.

TLS is optional. Without it, FIX passwords and messages travel unencrypted and can be observed or altered by anyone with access to the network path; use an encrypted VPN and restrict network access. To enable TLS, add the certificate and key shown below and configure clients to verify the certificate.

```yaml
downstream:
  listen: 0.0.0.0:9879
  target_comp_id: MULTIPLEXER
  tls_enabled: true
  tls:
    cert_file: /path/to/gateway-cert.pem
    key_file: /path/to/gateway-key.pem
  clients:
    - sender_comp_id: DEV_A
      password: {env: FIX_DEV_A_PASSWORD}
    - sender_comp_id: DEV_B
      password: {env: FIX_DEV_B_PASSWORD}
  store_dir: ./state/downstream
```

This is a TLS-enabled replacement for the `downstream` section of the copied config. Provide the referenced secrets and certificate files to the gateway; each client must verify the gateway certificate and use the matching SenderCompID, TargetCompID and password. For plaintext FIX over an encrypted VPN, omit `tls_enabled` and the `tls` section. If running in Docker, mount the configuration, any certificates, secret references and durable state appropriately and publish only the intended FIX port. Docker networking, host firewalls and TLS certificate names must match the actual deployment; the management listener remains loopback-only.

Use a VPN or firewall to restrict access to trusted developers. The gateway does not yet provide per-IP login rate limiting, and its security review is incomplete; a TLS-enabled listener is not a public-internet security certification. All logged-on downstream clients receive allowed upstream broadcasts, including messages concerning other clients. The local upstream bridge authenticates its engine connection but remains subject to local denial-of-service attempts. See the [security review](docs/SECURITY_REVIEW.md).

### FIX recovery and troubleshooting

Downstream fanout is live-only: a disconnected client misses new broadcasts, while FIX resend can recover previously assigned messages still eligible under that session's store policy. The configurable downstream replay window defaults to eight hours; an expired replay requires coordinated reset or reprovisioning. A gateway-generated BusinessMessageReject during an upstream outage is not a venue execution report or acceptance. See [operations and recovery](docs/OPERATIONS.md) for store protection, readiness, stale sessions, and crash recovery.

An upstream session Reject (`35=3`) generates a new gateway BusinessMessageReject (`35=j`) for each currently connected client, regardless of the application fanout allow/deny lists. Its `RefMsgType=3` identifies the exchange Reject, `BusinessRejectReason=0` means other, and `Text` labels available upstream RefSeqNum, RefMsgType, RefTagID, SessionRejectReason and exchange Text. The new message omits tag 45: the upstream sequence number is not a downstream sequence number. This is a shared diagnostic notice, not a claim that a particular client's order was rejected; upstream Rejects of session messages are also reported. Each supplied field is limited to 512 bytes, and exchange Text may contain sensitive details visible to all connected clients. Delivery is live-only and subject to queue and connection limits; monitor failure/drop metrics. Once assigned a downstream sequence number, ordinary downstream FIX resend applies.

### Development checks

```sh
go vet ./...
go test -short ./...
go test -tags=integration ./...
go test -race -tags=soak ./internal/gateway -run TestRoutingSoak -timeout=60s
```

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
