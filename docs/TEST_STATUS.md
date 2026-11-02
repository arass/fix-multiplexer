# Acceptance evidence and remaining work

This checkpoint records executed coverage, not a claim that every variant of every test-plan row has shipped. Fast coverage excludes tagged real-socket integration and soak runs. The core package coverage gate requires at least 90% statement coverage for config, gateway, fixadapter, sessions and retention.

## Unit evidence

| Test plan | Evidence |
|---|---|
| U01–U02 | Config table: malformed/unknown/oversized YAML, endpoints, identity, limits, versions, secrets and retention validation. |
| U03 | Registry authorization/disable/generation tests; transport duplicate binding and connection budget tests. |
| U04–U06 | Core forwarding/fanout fakes; adapter business-ID and independent-copy tests. |
| U07–U08 | Nested groups, duplicate values, customized dictionary order, scalar extensions, encoded text, raw wire round-trip and session-header isolation. Embedded-SOH EncodedText is explicitly unsupported. |
| U09–U10 | Administrative MsgType exclusion, allow/deny policy and callback routing tests. |
| U11–U14 | Outage errors, failed recipient, slow consumer, queue/byte budget, serialized upstream worker and per-session workers. |
| U15 | Concurrent registry and queue-credit tests; full Linux race runs include real sockets. |
| U16–U17 | HTTP health/metrics table, readiness-store health, metadata logger and interpolated-secret suppression tests. |
| U18–U21 | Fake-clock TTL, active-session protection, offline leases, dry-run, persistent retirement, corruption/budget/metadata failures, reset and cleanup lifecycle regressions. |
| U22 | Valid inbound PossDup forwarded; outbound ToApp replay never routes; replay cannot be rejected merely for queue credit. |

## Real-socket and operational evidence

| Test plan | Status |
|---|---|
| I01–I04 | Three clients / one upstream; unchanged orders and custom tags; grouped broadcasts with independent headers; unknown-order reports accepted by the sample. |
| I05 | Logout, live-only exclusion and durable client reconnect; first subsequent report is new, with no offline backlog. |
| I06–I07 | Real downstream ResendRequest with replay flags; genuine missing upstream sequence recovered through engine resend, without routing outbound callbacks. |
| I08 | Graceful persistent restart plus subprocess hard kill; conservative stale-lease recovery and increasing restored sequences. |
| I09 | Real upstream disconnect, not-ready and gateway-identified standard FIX BusinessMessageReject. |
| I10 | Unknown CompID, verified upstream hostname, and downstream mTLS success/failure; password mismatch is unit-tested. Additional combined authentication variants remain useful. |
| I11 | Real oversize transport disconnect; connection count, byte limits, slow consumer and queue saturation tested with controlled transports/fakes. Sustained socket backpressure stress remains open. |
| I12 | Defaults disable daily/logon/logout/disconnect resets; explicit offline reset and retirement enforced. Custom daily schedules are not exposed in v1. |
| I13 | Go wire parser and all three peers verify groups/custom tags; independent-engine verification is the Phase 5 task. |
| I14–I15 | Container liveness, not-ready without upstream, metadata metrics and SIGTERM exit 0; race/cleanup tests and no raw credential logging. |
| I16 | Budget/write/metadata/corruption, partial fanout and hard crash injected; physical disk exhaustion during a real socket flood remains open. |

## Fuzz, soak and interoperability

- Message-copy fuzzing executed in bounded native runs; final milestone run counts are in IMPLEMENTATION_STATUS.md.
- Core soak passed: 50,000 upstream submissions, 490,000 fanout attempts, ten clients and 100 disconnect/reconnect cycles, including recipient failures. Both native and race runs passed. This uses injected senders; it is not a 50,000-message socket throughput claim.
- Extended socket soak and independent QuickFIX/J tests are next. Project licensing remains undecided; no public release or tag is authorized.
