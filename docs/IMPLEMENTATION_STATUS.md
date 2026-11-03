# Implementation checkpoint

## Current state

Phases 1–5 have working implementations and local regression evidence. Independent-engine interoperability, extended socket soak, linked-module/license inventory and clean-checkout demo reproduction passed. The gateway, durable recovery, offline retention, bounded transport, TLS/mTLS, metadata logging, health and metrics are implemented. Owner explicitly chose to keep project licensing undecided; no LICENSE, publication or release tag should be added. User decisions in CODEX_TASK.md take precedence over earlier suggestions.

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
- Git index writes require elevated permission. First local milestone committed as 3bfb5d3. No remote push.
- IntelliJ MCP verified at the user-provided local Streamable HTTP endpoint. Initialization and tools/list succeeded, exposing terminal, run configuration, inspection, refactoring, file and Git tools. Saved project connection in ignored .codex/config.toml; native Codex registration may require extension restart. Shell-level MCP access is verified already.
- Java/Maven are absent from PATH. Plan an independent FIX peer in a container during Phase 5.

## Next action

Security review is in progress. The owner requested local tag `security-review-baseline` at 67f1284 to separate review changes. See docs/SECURITY_REVIEW.md for the open upstream bridge local-process trust issue and remaining review tasks. Review docs/TEST_STATUS.md for unexecuted stress variants. Public release and a project license require new owner direction. Phase 4 fixed an important store isolation regression: engine SessionSettings() returns copies, so paths must be set before AddSession. Real filesystem tests verify engine body files are inside per-session hash directories and history is removed only by offline retirement. Legacy root-level engine files block startup rather than silently resetting state. No engine modifications.

Security review confirmed with a synthetic local integration test that the upstream file store persists outgoing Logon credentials in plaintext. Store protection and safe mitigation remain open, alongside the local bridge issue. No customer data or real credentials were used.

README setup guidance now covers configuring an upstream counterparty, client CompIDs, credentials/TLS, readiness and recovery, including the open local bridge risk and loopback-only listener constraint. The example YAML comments were updated; its configuration validation passed. Security review tasks remain open in SECURITY_REVIEW.md.

Milestone 1a checks: native go fmt, go vet ./..., go test ./..., coverage and CLI sample validation passed. Config statement coverage: 99.1%. Linux go test -race ./... passed. Actual Dockerfile built and application container -check-config passed. CLI coverage is not included in the core target. Added CI and Docker packaging. Socket integration coverage remains pending.

Milestone 1b: native unit tests, vet and real-socket integration passed. Linux race tests including integration passed after lifecycle cleanup changes. Actual gateway container built and its -demo passed. Core fast-test coverage: config 99.1%, routing 100%, sessions 100%; adapter coverage still incomplete and measured separately. Demo proves three clients/one upstream, custom-tag/business-ID fidelity, report fanout, live-only logout behavior and identified gateway 35=j outage error. Nested repeating-group regression passes. Embedded-SOH EncodedText is an engine limitation, documented and tested as unsupported. Retention fields are parsed but NOT YET enforced. No project license granted yet.

## Milestone status

1. Implemented: Phase 1 config/core/mock engines/three-client proof and initial CI.
2. Implemented: Phase 2 field fidelity, routing/fanout, gateway errors and replay policy.
3. Implemented: Phase 3 durable recovery, retirement and replay windows; store isolation regression corrected in Phase 4.
4. Implemented: Phase 4 primary hardening and core soak; extended socket stress variants remain for final audit.
5. Implemented and locally verified: independent-engine interoperability, extended socket soak, linked-module/license inventory and clean-checkout Compose demo. CI jobs configured, not remotely executed. Owner has deferred licensing, so no public release/tag.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

Milestone 2: extended real-socket demo to inbound PossDup orders and repeating groups in reports, preserved on every target. Found and fixed parsed-group tail-slice serialization in our adapter; added a full wire parse/validate round-trip reproducer. Config loading now rejects inputs exceeding 1MiB instead of accepting a truncated document. Fast core coverage remains config 99.1%, routing/sessions 100%. Native vet/unit tests and a three-second message-copy fuzz run passed (110,137 executions on final run). Rebuilt container; Linux race/integration and actual container smoke passed. Full TEST_PLAN matrix is not complete: recovery/restart, retention, management, resource boundaries and independent parser are pending.

Milestone 3: native format/vet/unit/coverage and local restart/resend tests pass. Core fast-test coverage: config 99.1%, adapter 90.6%, routing/sessions 100%, retention 90.0%; maintenance CLI orchestration 85.3% is reported separately. Real FIX tests exercise downstream stored resend with replay flags, genuine upstream gap recovery, restart with sequences preserved and hard-kill recovery after explicit stale-lease removal. Race testing initially caught unsynchronized mock fault injection; the test peers now synchronize store access and wait for sequence-advance events. No sleeps added as test synchronization. Read-only maintenance works even over budget; retirement-marker failure cannot delete history. Active stores are never pruned. Final Linux race/integration checks and actual container build/demo passed.

Milestone 4: native go fmt, go vet and unit suite pass; fast coverage gate passes. Linux verification image passed format, vet, fast coverage gate (config 99.1%, adapter 90.6%, routing 93.1%, sessions 100%, retention 91.8%), race/socket integration and core soak. Native retention coverage is 90.1% (Windows skips the symlink privilege case). Observability and transport are reported separately. TLS hostname verification, mTLS and oversize/unknown-client socket cases passed. Container live/ready/metrics probes and SIGTERM exit 0 passed. Network-isolated Compose smoke passed. Core soak: 50,000 submissions, 490,000 fanout attempts, 100 reconnect cycles, synthetic recipient failures; native and race runs passed. A final unit regression additionally permits recording logout of an already retired session, so offline cleanup does not retain a stale active marker. No project license granted.

## Resume

Continue from Current state and Next action above. Historical milestone/preflight entries describe what was true at that step. No production/private-counterparty access is required or authorized.

Milestone 5 verification: native formatting/vet/unit and fast coverage gate passed (config 99.1%, adapter 90.7%, routing 93.1%, sessions 100%, retention 90.5%). Rebuilt QuickFIX/J 2.3.2 fixture passed. Extended race-enabled socket soak passed 50,000 orders/reports, ten durable clients, 100 reconnects and 499,900 report deliveries in 221.7s, followed by offline cleanup to 820 metadata bytes. Store accounting now reserves conservative bytes per write and reconciles periodically, avoiding per-write full-root scans; reservations never decrease while running and can exhaust budgets early. Initial soak mount/timeout failures are recorded in TEST_STATUS.md. Dependency texts and an actual linked-binary SHA256 inventory are retained without granting a project license. Obsolete project Docker images are removed by exact identity only; no broad prune or removal of pre-existing user images.

The full Linux verification image also passed formatting, vet, fast coverage gate (config 99.1%, adapter 90.7%, routing 95.0%, sessions 100%, retention 92.0%), race-enabled integration across all packages, and the separate core race soak. CI workflow jobs are configured but have not been run on GitHub; no remote push is requested.

Clean-checkout verification: locally cloned 6039d6f under ignored .tools/phase5-reproduction, then executed the documented `docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke`. The nonroot, read-only, network-isolated demo exited 0 after its three-client assertions. Removed its temporary container. Its executable SHA256 was 7aed9e6fb878fab9ffddd4e1d8620cfcae4b5bc40dee3eaad66bd7b7883d8fbe; the inventory has since been regenerated for a newer build. Go and shell files retain LF in Windows checkouts. Final Docker cleanup removes only exact task-owned obsolete image identities; current gateway and interoperability images remain available, with pre-existing user images/containers preserved.

Client-limit simplification: removed the configurable client-count field. The allowlist retains a hard limit of 128 entries, including disabled clients; transport capacity is twice the enabled count with a minimum of two slots. Existing YAML containing the removed field must delete it. Native fmt, vet, unit tests and coverage gate, Linux race-enabled integration, example config validation, and the container demo passed. The linked-module inventory was regenerated from the tested container executable. Security review remains in progress.
