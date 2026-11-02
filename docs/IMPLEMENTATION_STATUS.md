# Implementation checkpoint

## Current state

Phase 1 in progress. Implemented strict YAML validation, bounded settings, FIX 4.4-only policy, safe credential references, and CLI -check-config. Session runtime is the next step. User decisions in CODEX_TASK.md take precedence over earlier suggestions.

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

Complete Phase 1 with injected routing/session interfaces, local mock engines, three-client connection proof and runnable gateway. Engine v0.9.12 source inspected: FromApp handles inbound traffic, ToApp is also invoked for outbound resend; SendToTarget persists/queues but does not acknowledge peer receipt; file store has no built-in age/size retention. No engine modifications planned.

Milestone 1a checks: native go fmt, go vet ./..., go test ./..., coverage and CLI sample validation passed. Config statement coverage: 99.1%. Linux go test -race ./... passed. Actual Dockerfile built and application container -check-config passed. CLI coverage is not included in the core target. Added CI and Docker packaging. Socket integration coverage remains pending.

## Pending milestones

1. Phase 1: inspect current engine APIs/licenses and prior art; implement validated config, injected routing interfaces, local FIX mocks, three-client connection proof, CI and runnable instructions.
2. Phase 2: field fidelity, application forwarding/fanout, standard gateway errors, inbound PossDup behavior, policy and socket tests.
3. Phase 3: durable stores, recovery/restart tests, explicit reset policy and configurable downstream replay window.
4. Phase 4: resource bounds, retention, TLS/secrets, observability, shutdown, fuzz/soak and container smoke tests.
5. Phase 5: independent-engine interoperability, clean-checkout demo, dependency/license review. Owner must settle project license before release.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

## Resume

Start with environment preflight results below, then Phase 1 in CODEX_TASK.md. No production/private-counterparty access is required or authorized.
