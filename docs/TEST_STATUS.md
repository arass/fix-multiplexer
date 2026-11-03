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
| I10 | Unknown CompID, verified upstream hostname, downstream mTLS success/failure, and configured-client password success/failure over both plaintext and TLS sockets. Additional combined authentication variants remain useful. |
| I11 | Upstream session Reject generates a new diagnostic `j` for two live FIX clients, bypassing an ordinary `j` deny policy and omitting downstream RefSeqNum. |
| I11 | Real oversize transport disconnect; connection count, byte limits, slow consumer and queue saturation tested with controlled transports/fakes. Sustained socket backpressure stress remains open. |
| I12 | Defaults disable daily/logon/logout/disconnect resets; explicit offline reset and retirement enforced. Custom daily schedules are not exposed in v1. |
| I13 | Go wire parser and all three peers verify groups/custom tags; independent QuickFIX/J 2.3.2 peers also passed groups/custom scalar and independent session identities. |
| I14–I15 | Container liveness, not-ready without upstream, metadata metrics and SIGTERM exit 0; race/cleanup tests and no raw credential logging. |
| I16 | Budget/write/metadata/corruption, partial fanout and hard crash injected; physical disk exhaustion during a real socket flood remains open. |

## Fuzz, soak and interoperability

- Message-copy fuzzing executed in bounded native runs; final milestone run counts are in IMPLEMENTATION_STATUS.md.
- Core soak passed: 50,000 upstream submissions, 490,000 fanout attempts, ten clients and 100 disconnect/reconnect cycles, including recipient failures. Both native and race runs passed. This uses injected senders; it is not a 50,000-message socket throughput claim.
- Extended race-enabled socket soak passed: 50,000 orders and reports, ten durable socket clients, 100 reconnects, 499,900 report deliveries, no forwarding failures, and zero routing/engine pending bytes after shutdown. Elapsed 221.7s on this development machine; this is observational, not a performance guarantee. Offline prune reduced downstream store bytes from 103,501,141 to 820, preserving upstream history. Container limits: 1GiB RAM and 512MiB temporary filesystem. This run exercises live-only reconnect, not sustained malicious slow-reader traffic or physical disk exhaustion. Separate earlier hard-kill tests cover process recovery.
- Following the Logon-store security change, the same extended race-enabled socket soak passed again in 223.09s with 50,000 orders/reports, ten clients, 100 reconnects and 499,900 report deliveries; offline downstream store cleanup reduced 103,499,738 bytes to 818 bytes. No public-internet or malicious slow-reader test is implied.
- Initial soak attempts timed out because GOTMPDIR moved Go 1.27 test stores onto Docker overlay storage instead of the intended tmpfs. The documented command uses an executable tmpfs for test binaries and data. No timeout run is counted as a pass.
- Independent QuickFIX/J 2.3.2 passed three-client orders, grouped/custom-tag fanout, PossDup, admin isolation, genuine resends in both directions, durable reconnect and live-only delivery against the real Go daemon. See interop/quickfixj/README.md.
- Project licensing remains undecided; no public release or tag is authorized. Remaining variants in the table are not covered by a broad production-readiness claim.
- Clean local clone at 6039d6f passed the documented network-isolated Compose demo with exit 0. Its executable matched the inventory recorded at that checkpoint; the inventory has since been regenerated for newer builds. All CI-equivalent verification reported here was local; the configured GitHub workflow has not been remotely executed.
