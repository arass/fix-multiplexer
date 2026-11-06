# Test plan

Per-client subscriber coverage must include read-only enforcement before upstream routing, writable clients, omitted versus empty lists, global deny precedence, generated notice exceptions, and real FIX session recovery and filtering without sequence consumption. Detailed acceptance cases are in `docs/CODEX_PER_CLIENT_DROP_COPY.md`.

The goal is comprehensive coverage of **our code and guarantees**, not testing QuickFIX/Go's entire protocol implementation. Separate fast deterministic unit tests from real FIX socket integration tests and optional long-running soak tests.

## Unit tests: table-driven, fake engine, fake clock and store

File output coverage must include omitted/empty receive lists, global deny precedence, strict schema and aggregate resource/path bounds; byte-exact JSON/base64 capture; absent/N/Y upstream duplicate flags and original sending time; original session metadata isolated from new network frames; queue-byte/message saturation, write/sync/close/rotation/retention failures, file leases and symlink/foreign-file rejection; startup failure cleanup; readiness/metrics and metadata-only errors; deterministic size/day rotation, restart, concurrent publish/close and healthy draining. Tests inject storage and clocks rather than relying on actual disk failure or sleeps.

| ID | Area | Tests and expected result |
|---|---|---|
| U01 | Config | Valid sample parses; defaults correct; malformed YAML, missing endpoint, duplicate session IDs, same source/dest IDs, invalid port, empty fanout allowlist, negative TTL, invalid size fail with specific errors. An explicit empty upstream allowlist blocks all submissions. |
| U02 | Config | Unsupported FIX version combination fails early; unknown settings rejected; secrets are never echoed in errors. |
| U03 | Identity | Allowed client succeeds; unknown/disabled client denied; duplicate connection policy enforced; SessionID cannot be spoofed via business field. |
| U03a | Source IP | Omitted versus empty `allowed_ips` survives YAML round trips; validate IP/CIDR syntax, normalization, duplicates and list bounds. Match IPv4, IPv6, mapped IPv4 and range boundaries against socket addresses; fail closed if a restricted source is unavailable. Enforce each client's own policy before binding; denied attempts leave the session free, allowed reconnects work, and concurrent denials are counted safely. |
| U04 | Routing upstream | Each allowed downstream application type forwarded once to correct upstream; body fields unchanged and original message unmodified. |
| U05 | Fanout | One upstream message independently cloned to N live sessions; none to offline/disabled sessions; no shared mutable message instance. |
| U06 | Business IDs | `11`, `41`, `37`, `17` unchanged across both directions; duplicate `ClOrdID` preserved (not rewritten); no order-affinity table. |
| U07 | Message fidelity | Optional/unknown tags, custom tags, duplicate values in repeating groups, nested groups where supported, encoded data with SOH-like bytes, field ordering and non-ASCII rejection/handling follow documented FIX engine semantics. |
| U08 | Session fields | `8`, `9`, `10`, `34`, `49`, `52`, `56`, `43`, `122` generated per downstream/upstream session; never borrowed blindly from the input. |
| U09 | Admin isolation | All administrative MsgTypes are filtered from bridge routing; downstream ResendRequest not forwarded upstream; upstream Heartbeat never emitted as downstream application payload. |
| U10 | Policy | Configurable application MsgType allow/deny rules work; unknown app type behavior explicit; never accidentally treat Logon as allowed application traffic. |
| U11 | Upstream outage | Not-ready; explicit failure for new submission; no silent discard and no fabricated venue execution reports; recovery allows new sends. |
| U12 | Partial fanout | One recipient fails/blocks; other recipients still receive; failure metric increments; bounded timeout behavior. |
| U13 | Queue limits | Full queues reject/drop according to documented direction-specific policy, count errors, don't allocate indefinitely; upstream order path never silently accepts dropped messages. |
| U14 | Ordering | Concurrent clients follow chosen upstream serialization/order policy; downstream per-session outbound order maintained. |
| U15 | Races | Parallel connect/disconnect, message dispatch, shutdown, pruning and health reads do not race; run with Go race detector. |
| U16 | Metrics | Counters, queue depth, readiness and session gauges reflect events without leaking IDs or sensitive content as labels. |
| U17 | Logging | Credential fields (`554`, `553`, `96`, etc.) and configured sensitive tags redacted; structured errors contain no raw payload or secrets. |
| U17a | IP diagnostics | IP-denial logs contain only the observed source IP, hashed claimed-session label and fixed diagnostic fields. Independent rate limit bounds floods and summarizes suppressed denials; metric counts every denial without IP labels. |
| U18 | Cleanup | Fake clock: idle >= TTL and disconnected cleaned, active or younger untouched; dry-run no deletes; disable and delete idempotent; cleanup audit events emitted. |
| U19 | Storage failure | Corrupt/unwritable/full store produces safe error/not-ready or session disablement; never silently discards replay data; restart/error path explicit. |
| U20 | Lifecycle | Graceful shutdown drains or fails bounded pending work explicitly, logs out sessions when feasible, closes stores and stops listeners; repeated shutdown safe. |
| U21 | Reset | Session reset requires deliberate handshake/policy after prune; invalid preserved-state/new-state mix rejected. |
| U22 | Recovery adapter | Valid inbound application messages with PossDup are forwarded; outbound replay callbacks never initiate new routing; no application-level deduplication or exactly-once guarantee. |

## Integration tests: real sockets, self-hosted mock FIX peers

File subscribers must capture while no downstream clients are logged on. A genuine upstream sequence gap/replay must reach the file with tag 43=Y and tag 122 retained; a downstream engine resend must not produce another capture. Preserve capture across clean gateway restart without appending to old files, and verify independent downstream replay/framing. Filesystem and container tests use synthetic traffic and dedicated directories.

| ID | Scenario | Assertions |
|---|---|---|
| I01 | Basic connection | 3 different downstream initiators log on; exactly 1 gateway upstream session logs on. |
| I02 | Orders | Each client sends a distinct NewOrderSingle; upstream sees each once with correct business fields and unique upstream FIX sequence numbers. |
| I03 | Broadcast | Mock venue sends execution report; all 3 live clients receive it; each sees its own gateway SenderCompID/TargetCompID and proper session seq; no business IDs rewritten. |
| I04 | Different state | Client receives report for unknown order; gateway still delivers, and a sample downstream app ignores it without disconnecting. |
| I05 | Disconnect live-only | A logs out, B remains logged on. Report during A outage reaches B, not A. A reconnects: old offline-period report is not magically delivered. |
| I06 | Downstream resend | A loses an already sequenced/sent report, reconnects without resetting, requests missing seq; stored report resent with correct FIX replay indicators. Verify **only messages assigned while logged on**. |
| I07 | Upstream resend | Interrupt venue↔gateway and create a genuine sequence gap; upstream engine requests/replays missing messages when mock supports it. No unintended double downstream business fanout. |
| I08 | Store durability | Kill/restart gateway using a persistent volume and same FIX session schedule; sequences and eligible resend history survive. |
| I09 | Upstream outage | Drop upstream socket; readiness fails; downstream app sends order and receives explicit documented failure/outcome; reconnect restores service. |
| I10 | Bad logon | Unknown SenderCompID, wrong target, wrong password where configured, unauthorized address/TLS cert fail; no session created. |
| I10a | Source IP | Local TCP connections with and without TLS: allowed source still requires the correct password, mismatched/empty IP lists close the socket before Logon and emit the expected diagnostic/counter. Allowed IP policy survives real FIX reconnect and gateway restart with independent replay. Tests use loopback peers, not public hosts. |
| I11 | Upstream session Reject | A new gateway diagnostic notice reaches each logged-on downstream session even if ordinary `j` fanout is disabled; upstream sequence references never appear as downstream RefSeqNum. |
| I11a | Limits | Large frame, too many sessions, saturated queue/slow consumer trigger documented safe behavior; healthy other sessions continue. |
| I12 | Clock and schedules | FIX session daily rollover / reset flag combinations follow documented policies; stale pruned session cannot silently reattach with conflicting seq numbers. |
| I13 | Data fidelity | Grouped message and custom extension tag survive forwarding and all recipients; verify with independent FIX peer parser. |
| I14 | Operational | Graceful SIGTERM, restart, readiness/metrics/health behavior; no leaked sockets/threads/goroutines. |
| I15 | Isolation | No cross-client transport/session sequence leakage; no upstream credentials in logs. |
| I16 | Error injection | Downstream send failure mid-fanout, upstream backpressure, disk write errors and mid-flight crash produce documented visible outcomes. |

## Cross-engine interoperability

Additional confirmed regressions: a valid inbound PossDup application message is forwarded, including recovery of a genuinely missing message; outbound replay callbacks do not cause new business routing. Test gateway-generated standard FIX errors during upstream outage, with request references and no implication of venue acceptance. Test the configurable 8h downstream history window, expiry/reset behavior, independent upstream retention, and persistence of retirement across restart.

- At least one test using a **different engine** as a peer, e.g. QuickFIX/J test initiator or acceptor, rather than only QuickFIX/Go on both ends.
- Verify Logon/Logout, sequence gaps and resend, groups/custom tags, and reconnect with persisted state.
- Use published open-source FIX dictionaries and explicitly document any venue-specific extensions.

## Property and fuzz testing

- Message-copy invariants: no session header fields leak; application fields/groups preserved as supported; original message never mutated.
- Input robustness: malformed/surprising tag values, unknown fields and extreme group counts must not panic or allocate without limit. Let FIX engine validate FIX framing, do not invent your own parser.
- Idempotent client cleanup and shutdown; state-machine transitions preserve registry invariants.

## Soak and resource bounds

- Run a reproducible local scenario of e.g. 10 clients, >=50,000 messages, 100 reconnect cycles, intentional slow clients, repeated process restarts and session retirement.
- Assert bounded goroutine count, queue bytes and memory growth; disk usage after stale client cleanup returns to expected baseline. Avoid fragile absolute performance thresholds in CI; record throughput/latency as observational benchmarks.

## Coverage and gates

- Fast tests: `go test ./... -short` (do not require Docker or network beyond loopback in explicitly marked tests).
- Full unit + integration: `go test -tags=integration ./...`.
- Race, including integration: `go test -race -tags=integration ./...`.
- Fast coverage: `go test -short -coverprofile=coverage.out ./...`, then `go run ./scripts/check-coverage.go coverage.out`; the gate requires **>=90%** for config, gateway, FIX adapter, sessions, retention and fileoutput. Tagged socket integration and soak tests are not included in this fast coverage measurement. Coverage percentage alone does not replace the matrix above.
- Static checks: `go vet ./...`, formatter and lint (choose a documented reproducible linter configuration).
- Container smoke: build container, launch mock venue + gateway + 3 clients, run I01-I05 and readiness checks without credentials.
- Unit tests must not reach real brokers, rely on private SDKs or require AWS/Redis/MySQL.

Every bug fix must add a reproducer test. CI must clearly separate fast unit, socket integration, race, and soak stages so contributors can iterate quickly.
