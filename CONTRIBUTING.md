# Contributing

Contributions are welcome from users of any FIX venue, engine or technology stack. Please keep the core vendor-neutral and focused on development/UAT.

1. Open an issue for a change that affects session semantics or message delivery guarantees.
2. Add deterministic unit tests and, where relevant, real FIX session integration tests.
3. Prefer configuration or small optional adapters for venue-specific extensions; do not commit private dictionaries or credentials.
4. Include documentation of compatibility, resend/retention implications and sample config changes.
5. Run the repo's documented test and static-check commands before submitting a pull request.

Security-sensitive issues: use a private maintainer disclosure channel once the repository has one configured, rather than posting credentials, logs or vulnerabilities publicly.

The software is for nonproduction use. Do not submit claims of guaranteed order delivery, perfect replay or venue certification without independently reproducible evidence.
