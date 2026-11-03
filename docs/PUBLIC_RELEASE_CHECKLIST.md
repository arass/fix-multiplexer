# Public release checklist

## Current status: not ready to publish

- Owner confirmed the first-party project was written from scratch for this project and selected Apache 2.0. The first-party code is attributed to Informed Technology Business Solutions LLC; third-party components retain their licenses.
- QuickFIX/Go and the bundled FIX 4.4 dictionary use the QuickFIX Software License 1.0, including acknowledgment, non-endorsement and naming conditions. Preserve [third-party notices](../THIRD_PARTY_NOTICES.md) in source and binary distributions. Review the complete dependency/license combination and distribution obligations before publication; this is not a legal opinion. The separate QuickFIX/J test fixture and image have additional Maven/base-image dependencies that require their own notice review if distributed.
- Git history contains customer/counterparty-specific naming in historical Markdown. Current tracked agent instructions were made generic, but this does not remove historical content. Owner must decide whether to publish that history or authorize a separate cleanup. Do not rewrite history automatically. Secret scanning so far is targeted and is not proof of absence; perform a full source, history and artifact review before publication.
- Private security disclosure has no verified enabled channel; enable and test GitHub private vulnerability reporting or approve a private contact, then update [SECURITY.md](../SECURITY.md).
- The security review remains incomplete. The project is for nonproduction use and does not claim public-internet or production readiness. See [security review](SECURITY_REVIEW.md).
- No public release, image namespace or registry tag has been verified. The old local Docker tar predates subscriber functionality. Build and test a fresh artifact once release contents are final; do not treat the old archive as a release image.
- Per-client recovery and filtering have real-session tests, but the broader acceptance matrix has open variants in [test status](TEST_STATUS.md). Record results without implying complete coverage.
- Release-preparation checks: `go test ./...`, `go vet ./...`, Docker test target (format, coverage gate, race integration and routing soak), Compose configuration validation, packaged configuration validation, and isolated image `-demo` passed. An initial Compose build lost the Docker daemon connection; after restarting Docker, the documented Compose demo passed with a temporary project name and its temporary images and container were removed.
- Before release, review current untracked files and Docker context, validate configuration examples, test documentation links and Docker commands, review third-party source and binary notices, and refresh the linked-module inventory for the final executable.
- Publication, remote GitHub changes, release tags and registry pushes require separate owner authorization. The project maintainer must choose image coordinates and confirm the advertised services.

## Verified packaging facts

`Dockerfile` builds a nonroot Linux image containing the Go daemon, FIX 4.4 dictionary and third-party notices. `examples/docker-compose.yml` runs a local network-isolated mock demo. Runtime configuration is mounted read-only; session stores must persist on writable protected storage. Management is loopback-only. See [Docker setup](DOCKER_SETUP.md).
