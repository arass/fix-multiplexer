# GitHub publication preparation

This document records the current GitHub publishing setup. Repository visibility and package visibility are separate; verify the GHCR package's access settings before directing users to pull it.

- Repository name: `fix-multiplexer`
- Suggested description: `FIX 4.4 gateway in Go with QuickFIX/Go. Share one upstream session across clients, add read-only FIX subscribers, or learn from a runnable Docker example.`
- Website: `https://www.itbsllc.com/`
- Suggested topics: `fix-protocol`, `fixprotocol`, `fix-multiplexer`, `fix-proxy`, `fix-gateway`, `quickfix`, `quickfixgo`, `go`, `golang`, `fix44`, `drop-copy`, `fix-initiator`, `fix-acceptor`, `trading-systems`, `financial-markets`, `fintech`, `uat`, `fix-testing`, `docker`.
- License: Apache-2.0 for first-party work, subject to preserved third-party terms and review.
- Social preview idea: several independent FIX clients connecting through one gateway to one counterparty; show the actual session topology and avoid invented product screenshots.

The description and topics above are recommendations; this document does not apply them to GitHub. Edit them through the repository's **About** settings. GitHub's default repository search uses the name, description and topics; README content is searchable with `in:readme`. See [repository search](https://docs.github.com/en/search-github/searching-on-github/searching-for-repositories) and [repository topics](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics). Use terms that describe the implemented behavior and intended audience.

## Manual release steps after approval

1. The repository is public. Verify private vulnerability reporting and About metadata in GitHub settings; review new branches, tags and changes for secrets and customer references before future publication.
2. The manual-only [GHCR workflow](../.github/workflows/publish-ghcr.yml) has published `ghcr.io/arass/fix-multiplexer:v0.1.0`. It builds the selected revision, runs the Docker test target and image smoke tests, and uses the built-in `GITHUB_TOKEN`; it does not create a Git tag, publish `latest`, change package visibility or push to Docker Hub. Image tags can be overwritten by a later authorized run, so use a new version for each release. GHCR visibility is configured separately from repository visibility; check package settings before promising anonymous pulls.
3. Choose a SemVer release tag, create release notes covering compatibility and limitations, attach verified artifacts and matching image tags, and document checksums and the linked-module inventory.
4. Verify configuration mounts, persistent storage, FIX session smoke tests and documentation links against the exact released version. Re-check security and dependency advisories at release time.

To publish a later version, open **Actions → Publish versioned GHCR image → Run workflow**, choose the reviewed branch or Git tag and enter a new version. Confirm the selected revision and tests before dispatching. The workflow publishes the versioned image if checks pass; it does not create a GitHub Release or attach a tar. Set or verify package visibility on the package page separately.
