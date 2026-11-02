# Implementation checkpoint

## Current state

Specification-only repository; no gateway implementation yet. Go is confirmed. User decisions are recorded in CODEX_TASK.md and take precedence over earlier suggestions.

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

Begin Phase 1 implementation and complete current engine/prior-art review. Engine source inspection has begun (message copying, replay callbacks, reject construction, store APIs and license); no gateway code or project test results exist yet.

## Pending milestones

1. Phase 1: inspect current engine APIs/licenses and prior art; implement validated config, injected routing interfaces, local FIX mocks, three-client connection proof, CI and runnable instructions.
2. Phase 2: field fidelity, application forwarding/fanout, standard gateway errors, inbound PossDup behavior, policy and socket tests.
3. Phase 3: durable stores, recovery/restart tests, explicit reset policy and configurable downstream replay window.
4. Phase 4: resource bounds, retention, TLS/secrets, observability, shutdown, fuzz/soak and container smoke tests.
5. Phase 5: independent-engine interoperability, clean-checkout demo, dependency/license review. Owner must settle project license before release.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

## Resume

Start with environment preflight results below, then Phase 1 in CODEX_TASK.md. No production/private-counterparty access is required or authorized.
