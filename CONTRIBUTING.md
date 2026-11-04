# Contributing

Contributions are welcome from users of any FIX venue, engine or technology stack. Please keep the core vendor-neutral and focused on FIX session sharing and application-message forwarding.

Contributions intentionally submitted for inclusion are subject to the project's [Apache 2.0 license](LICENSE), unless a separate agreement applies. Submit only work you have the right to contribute, and retain applicable third-party notices.

1. Open an issue for a change that affects session semantics or message delivery guarantees.
2. Add deterministic unit tests and, where relevant, real FIX session integration tests.
3. Prefer configuration or small optional adapters for venue-specific extensions; do not commit private dictionaries or credentials.
4. Include documentation of compatibility, resend/retention implications and sample config changes.
5. Run the repo's documented test and static-check commands before submitting a pull request.

Security-sensitive issues: use a private maintainer disclosure channel once the repository has one configured, rather than posting credentials, logs or vulnerabilities publicly.

Describe verified behavior and its limits. Claims about delivery, replay, deployment suitability or venue certification need independently reproducible evidence.

From the repository root, with Go 1.23 or newer:

```sh
go vet ./...
go test -short -coverprofile=coverage.out ./...
go run ./scripts/check-coverage.go coverage.out
go test -race -tags=integration ./...
```

Use `gofmt` on Go changes. The [test plan](docs/TEST_PLAN.md) describes socket, race and recovery scenarios. The independent FIX peer checks are documented in [interop/quickfixj](interop/quickfixj/README.md). Integration tests use local mock counterparties and synthetic data, not live exchange credentials.

The extended socket soak and linked-module inventory can be run in a disposable build container:

```sh
docker build --target build -t fix-multiplexer-build .
docker run --rm --network none --tmpfs /tmp:exec,size=512m --memory 1g fix-multiplexer-build go test -race -tags=integration,socketsoak ./internal/fixadapter -run TestSocketSoak -timeout=21m -v
docker run --rm --network none fix-multiplexer-build go run ./scripts/sbom /fix-multiplexer
```
