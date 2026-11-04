# Public release checklist

## Current status: public repository; release follow-up remains

The source repository is public and the initial GHCR image `v0.1.0` has been published. The checks below track follow-up work and verification; they do not mean publication is still pending.

- Owner confirmed the first-party project was written from scratch for this project and selected Apache 2.0. The first-party code is attributed to Informed Technology Business Solutions LLC; third-party components retain their licenses.
- QuickFIX/Go and the bundled FIX 4.4 dictionary use the QuickFIX Software License 1.0, including acknowledgment, non-endorsement and naming conditions. Preserve [third-party notices](../THIRD_PARTY_NOTICES.md) in source and binary distributions. Review the complete dependency/license combination and distribution obligations before publication; this is not a legal opinion. The separate QuickFIX/J test fixture and image have additional Maven/base-image dependencies that require their own notice review if distributed.
- Gitleaks v8.30.1 scanned 44 reachable local Git commits (`--all`) before the history rewrite and reported no secrets. This is not proof of absence and did not inspect untracked files or container image layers. At the owner's request, history was rewritten to remove the identified counterparty name, all rewritten commit trees were checked, and the cleaned branch was force-pushed. Existing clones, backups or cached copies may retain the old history. The untracked release brief also contains names and must be reviewed before inclusion.
- Private security disclosure still needs verification. Enable and test GitHub private vulnerability reporting or approve a private contact, then confirm the instructions in [SECURITY.md](../SECURITY.md). CodeQL default setup was enabled by the owner; verify its first scan separately under **Security → Code scanning**.
- The security review remains incomplete. Document its findings and remaining gaps without implying deployment approval. See [security review](SECURITY_REVIEW.md).
- The manual-only GHCR workflow published `ghcr.io/arass/fix-multiplexer:v0.1.0`. Verify the package visibility and anonymous pull access on its package settings page; package access is separate from repository visibility. The workflow does not publish `latest` or Docker Hub images.
- A local `fix-multiplexer.tar` may be available as a handoff, but the recommended installation paths are pulling the versioned GHCR image or building from source. Rebuild any tar from the intended release revision before distribution.
- Per-client recovery and filtering have real-session tests, but the broader acceptance matrix has open variants in [test status](TEST_STATUS.md). Record results without implying complete coverage.
- Release-preparation checks: `go test ./...`, `go vet ./...`, Docker test target (format, coverage gate, race integration and routing soak), Compose configuration validation, packaged configuration validation, and isolated image `-demo` passed. An initial Compose build lost the Docker daemon connection; after restarting Docker, the documented Compose demo passed with a temporary project name and its temporary images and container were removed.
- For the next version, review release contents and Docker context, validate configuration examples, test documentation links and Docker commands, review third-party source and binary notices, and refresh the linked-module inventory for the final executable.
- Before advertising the image, confirm the GHCR package visibility, anonymous pull access and advertised version. Later image publication remains manual and should use a new version tag.

## Verified packaging facts

`Dockerfile` builds a nonroot Linux image containing the Go daemon, FIX 4.4 dictionary and third-party notices. `examples/docker-compose.yml` runs a local network-isolated mock demo. Runtime configuration is mounted read-only; session stores must persist on writable protected storage. Management is loopback-only. See [Docker setup](DOCKER_SETUP.md).
