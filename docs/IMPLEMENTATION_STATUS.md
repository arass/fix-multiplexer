# Implementation checkpoint

## Current state

Specification-only repository; no gateway implementation yet. Go is confirmed. User decisions are recorded in CODEX_TASK.md and take precedence over earlier suggestions.

## Completed

- Read all original Markdown documents and reviewed example configuration.
- Recorded gateway FIX errors, inbound PossDup forwarding, configurable 8h downstream history, three-client demo, and local milestone commits.
- Checked tooling: Git installed and commit identity configured; Docker CLI installed but Linux engine unavailable; Go not initially installed or on PATH; Java/Maven not found on PATH.
- Verified GitHub access with approved elevated git ls-remote after sandbox network access failed.

## In progress

- Install Go and preflight build, dependency-download, race-test, and container permissions.

## Pending milestones

1. Phase 1: inspect current engine APIs/licenses and prior art; implement validated config, injected routing interfaces, local FIX mocks, three-client connection proof, CI and runnable instructions.
2. Phase 2: field fidelity, application forwarding/fanout, standard gateway errors, inbound PossDup behavior, policy and socket tests.
3. Phase 3: durable stores, recovery/restart tests, explicit reset policy and configurable downstream replay window.
4. Phase 4: resource bounds, retention, TLS/secrets, observability, shutdown, fuzz/soak and container smoke tests.
5. Phase 5: independent-engine interoperability, clean-checkout demo, dependency/license review. Owner must settle project license before release.

For every completed implementation step: update this file and affected task documents, run applicable checks, record outcomes, then commit locally. Do not claim checks passed without execution. Do not include unrelated IDE files in commits.

## Resume

Start with environment preflight results below, then Phase 1 in CODEX_TASK.md. No production/private-counterparty access is required or authorized.
