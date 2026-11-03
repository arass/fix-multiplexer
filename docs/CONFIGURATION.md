# FIX configuration and network setup

## Per-client read-only FIX subscribers

Set `read_only: true` on a downstream client to reject all its application submissions with a gateway BusinessMessageReject while preserving its FIX session. Omission defaults to writable. Writable clients still obey the global `upstream.app_message_types`; `[]` blocks all clients from submitting. `fanout.deny_message_types` takes precedence over both directional allowlists.

Omit `receive_message_types` to inherit the global fanout list; set it to `[]` for no upstream application messages, or specify an exact case-sensitive subset. Generated submission errors and upstream session Reject notices bypass receive filters. Subscriptions select message types, not order owners. Changing a client to read-only does not erase previously accepted messages from upstream resend history.

The annotated [example configuration](../config/example.yaml) shows writable clients and a read-only subscriber. This best-effort Drop Copy-style fanout receives only what the upstream sends and is not an authoritative audit trail.

## Configuration reference

Unknown YAML fields are rejected. Durations use Go notation such as `60s` or `8h`; bytes are integer counts. Required values have no usable default. Paths are relative to the process working directory. The example makes some explicit choices that differ from defaults, notably a 7-day inactive TTL.

| Key | Default and purpose |
|---|---|
| `service.name` | `fix-multiplexer`; service identifier. |
| `service.mode` | `nonproduction`; the only supported mode. |
| `service.bind` | `127.0.0.1`; validated loopback service address. |
| `service.health_bind` | `127.0.0.1:8081`; empty disables management HTTP. |
| `service.max_message_bytes` | `1048576`; maximum message size. |
| `service.upstream_queue_messages` | `1000`; upstream queue message limit. |
| `service.downstream_queue_messages_per_client` | `1000`; per-client queue message limit. |
| `service.startup_timeout`, `service.shutdown_timeout` | `15s`, `10s`; lifecycle deadlines. |
| `upstream.fix_version`, `downstream.fix_version` | `FIX.4.4`; both must match the only supported version. |
| `upstream.host`, `upstream.port` | Required counterparty host and port. |
| `upstream.sender_comp_id`, `upstream.target_comp_id` | Required gateway and counterparty identities. |
| `upstream.store_dir`, `downstream.store_dir` | Required distinct durable store directories. |
| `upstream.reconnect_interval` | `60s`; at least `1s`. |
| `upstream.app_message_types` | `D`, `F`, `G`, `H`, `V`, `x`; explicit `[]` blocks submissions. |
| `upstream.username`, `upstream.password` | Unset; optional secret references using exactly one of `env` or `file`. |
| `upstream.dictionary`, `downstream.dictionary` | Unset selects `spec/FIX44.xml`; configured paths must match. |
| `upstream.tls_enabled`, `downstream.tls_enabled` | `false`; opt in separately for each boundary. |
| `upstream.tls.*`, `downstream.tls.*` | `ca_file`, `cert_file`, `key_file`, `server_name` unset; `require_client_cert` false. Downstream TLS needs a certificate and key; downstream client-certificate verification needs a CA. |
| `downstream.listen`, `downstream.target_comp_id` | Required literal IP:port and gateway identity for downstream sessions. |
| `downstream.clients` | Required list of 1–128 distinct client entries. |
| `downstream.clients[].sender_comp_id` | Required client FIX identity. |
| `downstream.clients[].disabled`, `read_only` | Both false; disable the client or prohibit its submissions. |
| `downstream.clients[].password` | Unset secret reference; required for enabled clients on non-loopback listeners. |
| `downstream.clients[].receive_message_types` | Omitted inherits global fanout; `[]` receives no upstream application messages. |
| `downstream.duplicate_connection_policy` | `reject-new`; only supported policy. |
| `fanout.mode` | `live-only`; only supported mode. |
| `fanout.app_message_types` | `8`, `9`, `j`, `W`, `X`, `Y`, `y`; global downstream allowlist. |
| `fanout.deny_message_types` | Empty; denies matching application types in both directions. |
| `retention.downstream_replay_window` | `8h`; eligible downstream replay history. |
| `retention.upstream_replay_window` | `0s`; no upstream age cutoff. |
| `retention.inactive_client_ttl` | `8h`; minimum inactivity for explicit offline cleanup. |
| `retention.prune_active_sessions` | `false`; must remain false. |
| `retention.disk_warn_bytes`, `disk_limit_bytes` | `1073741824`, `2147483648`; storage warning and combined limit. |
| `retention.session_limit_bytes` | `67108864`; per-session limit. |
| `logging.mode` | `metadata`; only supported mode. |
| `logging.redact_tags` | Empty; metadata mode omits all message bodies regardless of this list. |

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
6. Configure each client's FIX initiator to connect to the configured `downstream.listen`. With the supplied example, client `DEV_A` connects to `127.0.0.1:9879` with `BeginString=FIX.4.4`, `SenderCompID=DEV_A`, and `TargetCompID=MULTIPLEXER`. The gateway presents the inverse identity on that session. Configure `DEV_B`, `DROP_COPY` and `QA_1` with their own sender IDs; `DROP_COPY` is read-only. Do not reuse one session identity for multiple simultaneous clients. Coordinate sequence numbers instead of enabling automatic resets.
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

For a shared Docker server that developers connect to from their own machines, see the [Docker setup guide](DOCKER_SETUP.md) for image handoff, configuration, port mapping, and firewall requirements.

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

Use a VPN or firewall to restrict access to trusted developers. The gateway does not yet provide per-IP login rate limiting, and its security review is incomplete; a TLS-enabled listener is not a public-internet security certification. By default, logged-on downstream clients receive allowed upstream broadcasts, including messages concerning other clients; per-client type filters can narrow what they receive. The local upstream bridge authenticates its engine connection but remains subject to local denial-of-service attempts. See the [security review](SECURITY_REVIEW.md).

### FIX recovery and troubleshooting

Downstream fanout is live-only: a disconnected client misses new broadcasts, while FIX resend can recover previously assigned messages still eligible under that session's store policy. The configurable downstream replay window defaults to eight hours; an expired replay requires coordinated reset or reprovisioning. A gateway-generated BusinessMessageReject during an upstream outage is not a venue execution report or acceptance. See [operations and recovery](OPERATIONS.md) for store protection, readiness, stale sessions, and crash recovery.

An upstream session Reject (`35=3`) generates a new gateway BusinessMessageReject (`35=j`) for each currently connected client, regardless of the application fanout allow/deny lists. Its `RefMsgType=3` identifies the exchange Reject, `BusinessRejectReason=0` means other, and `Text` labels available upstream RefSeqNum, RefMsgType, RefTagID, SessionRejectReason and exchange Text. The new message omits tag 45: the upstream sequence number is not a downstream sequence number. This is a shared diagnostic notice, not a claim that a particular client's order was rejected; upstream Rejects of session messages are also reported. Each supplied field is limited to 512 bytes, and exchange Text may contain sensitive details visible to all connected clients. Delivery is live-only and subject to queue and connection limits; monitor failure/drop metrics. Once assigned a downstream sequence number, ordinary downstream FIX resend applies.
