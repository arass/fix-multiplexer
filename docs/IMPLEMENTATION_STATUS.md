# Implementation checkpoint

## Current state

Phases 1/2 and initial Phase 3 recovery implemented. Runnable gateway and local three-client demo include grouped reports and PossDup submissions. Durable restart/resend, configured replay windows, disk budgets and offline retirement/reset exist. Operational hardening and independent-engine interoperability remain pending. User decisions in CODEX_TASK.md take precedence over earlier suggestions.

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

Begin Phase 4 observability, transport/resource and shutdown hardening; continue remaining matrix scenarios. Phase 3 added engine-delegated durable stores with budget guards, downstream 8h/upstream independent replay eligibility, persistent retirement markers, exclusive root leases, and offline list/prune/reset CLI operations. Existing state layout changed to per-session hash directories; the preimplementation layout has no compatibility promise. No engine modifications.

Milestone 1a checks: native go fmt, go vet ./..., go test ./..., coverage and CLI sample validation passed. Config statement coverage: 99.1%. Linux go test -race ./... passed. Actual Dockerfile built and application container -check-config passed. CLI coverage is not included in the core target. Added CI and Docker packaging. Socket integration coverage remains pending.

Milestone 1b: native unit tests, vet and real-socket integration passed. Linux race tests including integration passed after lifecycle cleanup changes. Actual gateway container built and its -demo passed. Core fast-test coverage: config 99.1%, routing 100%, sessions 100%; adapter coverage still incomplete and measured separately. Demo proves three clients/one upstream, custom-tag/business-ID fidelity, report fanout, live-only logout behavior and identified gateway 35=j outage error. Nested repeating-group regression passes. Embedded-SOH EncodedText is an engine limitation, documented and tested as unsupported. Retention fields are parsed but NOT YET enforced. No project license granted yet.

## Pending milestones

1. Phase 1: inspect current engine APIs/licenses and prior art; implement validated config, injected routing interfaces, local FIX mocks, three-client connection proof, CI and runnable instructions.
2. Phase 2: field fidelity, application forwarding/fanout, standard gateway errors, inbound PossDup behavior, policy and socket tests.
3. Phase 3: durable stores, recovery/restart tests, explicit reset policy and configurable downstream replay window.
4. Phase 4: resource bounds, retention, TLS/secrets, observability, shutdown, fuzz/soak and container smoke tests.
5. Phase 5: independent-engine interoperability, clean-checkout demo, dependency/license review. Owner must settle project license before release.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

Milestone 2: extended real-socket demo to inbound PossDup orders and repeating groups in reports, preserved on every target. Found and fixed parsed-group tail-slice serialization in our adapter; added a full wire parse/validate round-trip reproducer. Config loading now rejects inputs exceeding 1MiB instead of accepting a truncated document. Fast core coverage remains config 99.1%, routing/sessions 100%. Native vet/unit tests and a three-second message-copy fuzz run passed (110,137 executions on final run). Rebuilt container; Linux race/integration and actual container smoke passed. Full TEST_PLAN matrix is not complete: recovery/restart, retention, management, resource boundaries and independent parser are pending.

Milestone 3: native format/vet/unit/coverage and local restart/resend tests pass. Core fast-test coverage: config 99.1%, adapter 90.6%, routing/sessions 100%, retention 90.0%; maintenance CLI orchestration 85.3% is reported separately. Real FIX tests exercise downstream stored resend with replay flags, genuine upstream gap recovery, restart with sequences preserved and hard-kill recovery after explicit stale-lease removal. Race testing initially caught unsynchronized mock fault injection; the test peers now synchronize store access and wait for sequence-advance events. No sleeps added as test synchronization. Read-only maintenance works even over budget; retirement-marker failure cannot delete history. Active stores are never pruned. Final Linux race/integration checks and actual container build/demo passed.

## Resume

Start with environment preflight results below, then Phase 1 in CODEX_TASK.md. No production/private-counterparty access is required or authorized.
