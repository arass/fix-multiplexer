# Implementation checkpoint

## Current state

Documentation describes session sharing without repeated UAT labels or blanket deployment restrictions. Operators assess suitability against the documented delivery, recovery and security limits; no production-readiness or certification claim is made. Agent and contributor guidance follows the same policy. Runtime behavior is unchanged, including the required `service.mode: nonproduction` compatibility value.

Wording-cleanup verification: 51 relative documentation links and `git diff --check` passed. Native `go fmt ./...`, `go vet ./...`, `go test ./...` and configuration validation passed. The Docker test target passed formatting, vet, short coverage, the coverage gate, race-enabled integration and routing soak (config 99.3%, gateway 99.1%, fixadapter 90.4%, sessions 100%, retention 92.6%). The Compose smoke demo was attempted twice afterward but could not start because Docker returned HTTP 500 engine API errors; an engine-info check also returned that error. Container smoke remains unverified for this change.

The GitHub interoperability fixture timed out at initialization; the log did not identify the missing logon. Starting the mock acceptor late reproduced the same failure when the gateway used its 60-second default retry. The fixture now explicitly uses a one-second reconnect interval, and both ordinary and delayed local runs pass; the actual default remains 60 seconds. The config regression and Docker test target passed. GitHub Actions run 17 passed unit, race integration, container, routing soak and interoperability after the fix; the optional manual socket soak was skipped.

README documents FIX Drop Copy and read-only subscribers, session sharing and QuickFIX/Go examples. It links to the example configuration and relevant source files, and explains live-only delivery and the limits of the upstream feed. Local link targets and whitespace were checked.

The manual-only GHCR workflow published `ghcr.io/arass/fix-multiplexer:v0.1.0` after its Docker test and smoke gates passed. README and Docker setup now explain pulling the published version as well as building locally. Package visibility remains independently controlled in GHCR settings; verify anonymous pulls before advertising it as public. The workflow does not publish automatically, change visibility, create Git tags or integrate Docker Hub.

Gitleaks v8.30.1 scanned 44 reachable local commits with `--all` and reported no secrets. The owner then authorized rewriting local history to replace the historical counterparty name; all 45 rewritten commit trees were checked, and the cleaned branch was force-pushed with authorization. Existing clones, backups or cached copies may retain the old history. The scanner image was removed afterward.

The owner confirmed first-party ownership, selected Apache 2.0, made the repository public and published the initial GHCR image. Local release materials are prepared; follow-up checks are in `docs/PUBLIC_RELEASE_CHECKLIST.md`. Docker runtime demo, configuration validation, full Docker test target, coverage gate and Compose demo passed. The first Compose attempt lost the Docker connection; the retry passed after Docker restarted, and temporary images and the container were removed.

The owner consolidated the mixed-client subscriber example into `config/example.yaml`; it includes a read-only `DROP_COPY` client alongside the original writable `DEV_A`, `DEV_B` and `QA_1` examples. The separate example was removed. Configuration validation and `go test ./...` pass with the consolidated example.

Documentation audit: architecture, operations, Docker setup, README and test guides now describe the current per-client filters, message-copy limits, maintenance and test commands. The milestone entries below are historical records; their then-pending items are not necessarily current. See `TEST_STATUS.md` and the subscriber task for remaining test variants.

Per-client read-only and receive filtering has an implementation checkpoint. Real-session tests cover reconnect, restart, previously delivered message resend, no offline backlog, excluded messages absent from the downstream resend store, and `AE` fanout with a repeating group. The broader acceptance matrix still includes explicit real-session `F`, unlisted-type and admin TestRequest cases; see `docs/CODEX_PER_CLIENT_DROP_COPY.md`. The local Docker handoff tar was rebuilt with this functionality and verified using the packaged config and isolated demo.

The Docker test target passed formatting, vet, short tests, the coverage gate (config 100%, gateway 97.3%, fixadapter 90.4%, sessions 100%, retention 92.6%), full race-enabled integration and routing soak. The final extended recovery and multi-character socket tests also passed separately under the race detector. The focused socket tests passed repeatedly on Windows; `go test ./...` passed.

Phases 1–5 have working implementations and local regression evidence. Independent-engine interoperability, extended socket soak, linked-module/license inventory and clean-checkout demo reproduction passed. The gateway, durable recovery, offline retention, bounded transport, TLS/mTLS, metadata logging, health and metrics are implemented. The owner chose Apache 2.0 and published the initial source and image; future version releases remain manual. User decisions in docs/CODEX_TASK.md take precedence over earlier suggestions.

## Completed

- Read all original Markdown documents and reviewed example configuration.
- Recorded gateway FIX errors, inbound PossDup forwarding, configurable 8h downstream history, three-client demo, and local milestone commits.
- Checked tooling: Git installed and commit identity configured; Docker CLI installed but Linux engine unavailable; Go not initially installed or on PATH; Java/Maven not found on PATH.
- Verified GitHub access with approved elevated git ls-remote after sandbox network access failed.

## Preflight results

- WinGet installation failed because its package source data is missing. Instead downloaded the official Go 1.27.3 Windows archive, verified its SHA256 against go.dev release metadata, and installed under ignored .tools/go. Use ./.tools/go/bin/go.exe (PATH is unchanged).
- Go module downloads require elevated network access in this sandbox. Downloaded QuickFIX/Go v0.9.12 into .tools/modcache for source inspection; no project dependency pin has been committed yet.
- Docker Desktop Linux engine started successfully; server version 28.3.2. Pulled official golang:1.27.3 image.
- Built ignored .tools/preflight/Dockerfile as fix-multiplexer-preflight. Its go vet and go test -race passed, including a loopback listener. Running that container also passed. These are toolchain checks, not gateway acceptance tests.
- Native Windows race testing lacks a C compiler on PATH; use the tested Linux container route.
- Git index writes require elevated permission. First local milestone committed as b12338d. No remote push.
- IntelliJ MCP verified at the user-provided local Streamable HTTP endpoint. Initialization and tools/list succeeded, exposing terminal, run configuration, inspection, refactoring, file and Git tools. Saved project connection in ignored .codex/config.toml; native Codex registration may require extension restart. Shell-level MCP access is verified already.
- Java/Maven are absent from PATH. Plan an independent FIX peer in a container during Phase 5.

## Next action

Security review is in progress. The owner requested local tag `security-review-baseline` at 0e7f4d5 to separate review changes. The upstream bridge now authenticates its local engine connection with a process-specific ephemeral TLS identity, verified by unauthorized-peer, recovery, TLS, race, interoperability and smoke tests. See docs/SECURITY_REVIEW.md for residual risks and remaining review tasks. Review docs/TEST_STATUS.md for unexecuted stress variants. The owner chose Apache 2.0; public release requires separate authorization. Phase 4 fixed an important store isolation regression: engine SessionSettings() returns copies, so paths must be set before AddSession. Real filesystem tests verify engine body files are inside per-session hash directories and history is removed only by offline retirement. Legacy root-level engine files block startup rather than silently resetting state. No engine modifications.

Upstream session Rejects now generate gateway BusinessMessageReject diagnostic notices for live clients, independently of ordinary message filters; upstream sequence numbers appear only as labeled text. A two-client real FIX socket test covers policy bypass, fields and downstream acceptance. The example lists both default application type policies and describes optional Quote Cancel (`Z`). Native formatting, vet, unit tests and core coverage gate, Linux race-enabled integration and container smoke passed; binary inventory was refreshed. Delivery is subject to existing live-only and bounded-queue limits; see README.

The upstream reconnect interval is now configurable and defaults to 60 seconds instead of one second. A local socket test verifies actual spacing after disconnection; tests configure shorter intervals for their recovery scenarios. Downstream retry intervals remain controlled by the initiating clients. Native formatting, vet, unit and core coverage checks, Linux race-enabled integration, and container smoke passed; the binary inventory was refreshed.

At the owner's request, downstream TLS is optional even for network listeners; enabled network clients still require individual passwords. Native formatting, vet, unit and core coverage checks, Linux race-enabled integration (including valid and invalid plaintext and TLS Logons) and container smoke passed. Use an encrypted VPN when TLS is disabled. The binary inventory was refreshed.

Security review confirmed that older upstream file stores persist outgoing Logon credentials in plaintext. New Logon records now omit the body and retain only session headers and sequence metadata; the original message still reaches the venue, and a synthetic real FIX resend gap-fill test passes. Application replay bytes remain unchanged. Native fmt, vet, unit and coverage gate, Linux race-enabled integration, QuickFIX/J interoperability and the container demo passed. The binary inventory was refreshed. Existing stores and backups are not rewritten and require protected, coordinated handling. No customer data or real credentials were used.

After the Logon-storage change, the extended race-enabled socket soak passed again: 50,000 orders and reports, ten clients, 100 reconnects, and 499,900 report deliveries in 223.09 seconds; offline cleanup reduced downstream store bytes from 103,499,738 to 818. This is an observational local test, not a performance guarantee. The temporary build image was removed.

Security review also bounded session-metadata reads before allocation; oversized markers are rejected during startup and maintenance. Native fmt, vet, unit tests and core coverage gate passed. Remote developer FIX access is opt-in via a non-loopback listener requiring separate per-client passwords. TLS is optional at the owner's request; use an encrypted VPN if it is disabled. The management listener and default configuration remain loopback-only. Network scope, optional separate usernames, rate limiting and broader security review remain open. See SECURITY_REVIEW.md.

A synthetic socket integration test verifies that downstream FIX Logon uses the configured client password independently of the upstream password, rejecting missing and incorrect values. The existing mTLS success test uses a certificate subject different from the FIX SenderCompID and confirms there is no certificate-to-session identity binding. Linux race-enabled integration and the isolated container smoke passed after the metadata read fix; the linked-module inventory was refreshed from the new container. Additional cross-client and remote exposure tests remain open.

Generic engine-error logging is now globally rate-limited to one record per second. Every error still increments the metric and the next emitted record reports the number suppressed. A deterministic concurrent test, native vet/unit/coverage, Linux race-enabled integration and the container smoke passed. Other log categories remain unthrottled; per-IP authentication rate limiting remains open. The binary inventory was refreshed.

Security review also aligned environment credential limits with file credentials: both reject values over 4,096 bytes. Boundary and rejection tests cover the change.

Failed configured-client passwords are counted and emit a redacted, rate-limited event. The counter does not cover unknown sessions or failed TLS handshakes; per-IP attempt limiting remains open. Native fmt, vet, unit tests and coverage gate, Linux race-enabled integration and container smoke passed. The binary inventory was refreshed.

The local example shows where to configure each downstream client's password and optional TLS fields. The commented credentials preserve the runnable local mock defaults.

The first remote-listener implementation required TLS and client passwords; TLS is now optional at the owner's request while per-client passwords remain required. The initial tests covered encrypted Logon success and failure and plaintext rejection using local sockets. No cross-host connectivity or public internet security test has been performed; rate limiting and network exposure decisions remain open.

Dependency review updated golang.org/x/net from v0.24.0 to v0.43.0, retaining Go 1.23 support. A repeat govulncheck reported no reachable or imported-package vulnerabilities and 17 module-only advisories in uncalled packages. Native fmt, vet, unit tests and coverage gate, Linux race-enabled integration, the isolated container demo and QuickFIX/J interoperability passed; the binary inventory and license notice were refreshed. Newer x/net releases fixing remaining module advisories require a newer Go version. See SECURITY_REVIEW.md.

README setup guidance covers configuring an upstream counterparty, client CompIDs, credentials/TLS, readiness and recovery. The example YAML comments were updated; its configuration validation passed. The local bridge authentication and remote listener were subsequently revised; security review tasks remain open in SECURITY_REVIEW.md.

Milestone 1a checks: native go fmt, go vet ./..., go test ./..., coverage and CLI sample validation passed. Config statement coverage: 99.1%. Linux go test -race ./... passed. Actual Dockerfile built and application container -check-config passed. CLI coverage is not included in the core target. Added CI and Docker packaging. Socket integration coverage remains pending.

Milestone 1b: native unit tests, vet and real-socket integration passed. Linux race tests including integration passed after lifecycle cleanup changes. Actual gateway container built and its -demo passed. Core fast-test coverage: config 99.1%, routing 100%, sessions 100%; adapter coverage still incomplete and measured separately. Demo proves three clients/one upstream, custom-tag/business-ID fidelity, report fanout, live-only logout behavior and identified gateway 35=j outage error. Nested repeating-group regression passes. Embedded-SOH EncodedText is an engine limitation, documented and tested as unsupported. Retention fields are parsed but NOT YET enforced. No project license granted yet.

## Milestone status

1. Implemented: Phase 1 config/core/mock engines/three-client proof and initial CI.
2. Implemented: Phase 2 field fidelity, routing/fanout, gateway errors and replay policy.
3. Implemented: Phase 3 durable recovery, retirement and replay windows; store isolation regression corrected in Phase 4.
4. Implemented: Phase 4 primary hardening and core soak; extended socket stress variants remain for final audit.
5. Implemented and locally verified: independent-engine interoperability, extended socket soak, linked-module/license inventory and clean-checkout Compose demo. CI jobs configured, not remotely executed. At that milestone licensing was deferred; Apache 2.0 was selected later, while publication and release tags still require authorization.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

Milestone 2: extended real-socket demo to inbound PossDup orders and repeating groups in reports, preserved on every target. Found and fixed parsed-group tail-slice serialization in our adapter; added a full wire parse/validate round-trip reproducer. Config loading now rejects inputs exceeding 1MiB instead of accepting a truncated document. Fast core coverage remains config 99.1%, routing/sessions 100%. Native vet/unit tests and a three-second message-copy fuzz run passed (110,137 executions on final run). Rebuilt container; Linux race/integration and actual container smoke passed. Full TEST_PLAN matrix is not complete: recovery/restart, retention, management, resource boundaries and independent parser are pending.

Milestone 3: native format/vet/unit/coverage and local restart/resend tests pass. Core fast-test coverage: config 99.1%, adapter 90.6%, routing/sessions 100%, retention 90.0%; maintenance CLI orchestration 85.3% is reported separately. Real FIX tests exercise downstream stored resend with replay flags, genuine upstream gap recovery, restart with sequences preserved and hard-kill recovery after explicit stale-lease removal. Race testing initially caught unsynchronized mock fault injection; the test peers now synchronize store access and wait for sequence-advance events. No sleeps added as test synchronization. Read-only maintenance works even over budget; retirement-marker failure cannot delete history. Active stores are never pruned. Final Linux race/integration checks and actual container build/demo passed.

Milestone 4: native go fmt, go vet and unit suite pass; fast coverage gate passes. Linux verification image passed format, vet, fast coverage gate (config 99.1%, adapter 90.6%, routing 93.1%, sessions 100%, retention 91.8%), race/socket integration and core soak. Native retention coverage is 90.1% (Windows skips the symlink privilege case). Observability and transport are reported separately. TLS hostname verification, mTLS and oversize/unknown-client socket cases passed. Container live/ready/metrics probes and SIGTERM exit 0 passed. Network-isolated Compose smoke passed. Core soak: 50,000 submissions, 490,000 fanout attempts, 100 reconnect cycles, synthetic recipient failures; native and race runs passed. A final unit regression additionally permits recording logout of an already retired session, so offline cleanup does not retain a stale active marker. No project license granted.

## Resume

Continue from Current state and Next action above. Historical milestone/preflight entries describe what was true at that step. No production/private-counterparty access is required or authorized.

Milestone 5 verification: native formatting/vet/unit and fast coverage gate passed (config 99.1%, adapter 90.7%, routing 93.1%, sessions 100%, retention 90.5%). Rebuilt QuickFIX/J 2.3.2 fixture passed. Extended race-enabled socket soak passed 50,000 orders/reports, ten durable clients, 100 reconnects and 499,900 report deliveries in 221.7s, followed by offline cleanup to 820 metadata bytes. Store accounting now reserves conservative bytes per write and reconciles periodically, avoiding per-write full-root scans; reservations never decrease while running and can exhaust budgets early. Initial soak mount/timeout failures are recorded in TEST_STATUS.md. Dependency texts and an actual linked-binary SHA256 inventory are retained without granting a project license. Obsolete project Docker images are removed by exact identity only; no broad prune or removal of pre-existing user images.

The full Linux verification image also passed formatting, vet, fast coverage gate (config 99.1%, adapter 90.7%, routing 95.0%, sessions 100%, retention 92.0%), race-enabled integration across all packages, and the separate core race soak. CI workflow jobs are configured but have not been run on GitHub; no remote push is requested.

Clean-checkout verification: locally cloned 612b49b under ignored .tools/phase5-reproduction, then executed the documented `docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke`. The nonroot, read-only, network-isolated demo exited 0 after its three-client assertions. Removed its temporary container. Its executable SHA256 was 7aed9e6fb878fab9ffddd4e1d8620cfcae4b5bc40dee3eaad66bd7b7883d8fbe; the inventory has since been regenerated for a newer build. Go and shell files retain LF in Windows checkouts. Final Docker cleanup removes only exact task-owned obsolete image identities; current gateway and interoperability images remain available, with pre-existing user images/containers preserved.

Client-limit simplification: removed the configurable client-count field. The allowlist retains a hard limit of 128 entries, including disabled clients; transport capacity is twice the enabled count with a minimum of two slots. Existing YAML containing the removed field must delete it. Native fmt, vet, unit tests and coverage gate, Linux race-enabled integration, example config validation, and the container demo passed. The linked-module inventory was regenerated from the tested container executable. Security review remains in progress.
