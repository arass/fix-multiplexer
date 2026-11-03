# GitHub publication preparation

These are proposed settings; editing this document does not change GitHub. Do not publish, create a release or change remote settings until authorized and the [release checklist](PUBLIC_RELEASE_CHECKLIST.md) is resolved.

- Repository name: `fix-multiplexer`
- Description: `Open-source FIX protocol proxy and gateway. Share one upstream FIX session across multiple clients for development, UAT, and testing.`
- Website: `https://www.itbsllc.com/`
- Topics: `fix-protocol`, `fixprotocol`, `fix-multiplexer`, `fix-proxy`, `fix-gateway`, `quickfixgo`, `golang`, `trading-systems`, `financial-markets`, `fintech`, `uat`, `fix-testing`, `docker`.
- License: Apache-2.0 for first-party work, subject to preserved third-party terms and review.
- Social preview idea: several independent FIX clients connecting through one gateway to one counterparty; label the tool nonproduction and avoid invented product screenshots.

## Manual release steps after approval

1. Confirm repository owner, visibility, private vulnerability reporting and About metadata in GitHub settings. Review all branches, tags and history for secrets and customer references before publication.
2. GitHub Container Registry is the intended registry. `ghcr.io/arass/fix-multiplexer` is a proposed image name based on the current repository owner, not a verified published image. Confirm the namespace and package visibility before using it. The first image can be built locally and pushed manually; subsequent builds can be automated with GitHub Actions. Publish versioned tags and optionally `latest` only after a reproducible build, smoke test and notice review.
3. Choose a SemVer release tag, create release notes covering compatibility and limitations, attach verified artifacts and matching image tags, and document checksums and the linked-module inventory.
4. Verify configuration mounts, persistent storage, FIX session smoke tests and documentation links against the exact released version. Re-check security and dependency advisories at release time.
