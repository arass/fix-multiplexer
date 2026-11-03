# FIX Multiplexer

FIX Multiplexer is an open-source FIX protocol proxy that lets multiple FIX clients share one upstream FIX session for development, UAT and testing. It uses QuickFIX/Go and supports FIX 4.4 on both sides.

```text
Developer and QA FIX clients  ──>  FIX Multiplexer  ──>  One FIX counterparty session
                                  acceptor / initiator
```

Each client has its own FIX session, sequence numbers and resend history. The gateway forwards permitted application messages upstream and fans out permitted messages to connected clients. FIX session administration stays independent on each side. No order ownership database or `ClOrdID` rewriting is used.

- Share a limited UAT session between developers or test harnesses.
- Configure message types globally and per client. A read-only subscriber cannot submit application messages upstream.
- Receive a best-effort Drop Copy-style feed of messages actually supplied by the counterparty. This is not a venue-wide feed or authoritative audit trail.
- Use persistent stores, configurable retention, health checks, optional TLS and separate downstream passwords.

## Quick start: Docker local build

From the repository root, with Docker installed, run the isolated mock demo:

```sh
docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke
```

This builds the image locally, starts a simulated counterparty and three clients, checks FIX message flow and exits. It requires no real credentials or external FIX connection. No public image location is advertised yet.

To connect the gateway to your own UAT counterparty, follow the **[Docker setup guide](docs/DOCKER_SETUP.md)** for image handoff, configuration, persistent session stores, network access and troubleshooting. The annotated [example configuration](config/example.yaml) includes writable clients and a read-only subscriber.

## Limitations and security

**For nonproduction use only.** Clients share the counterparty's business context; messages of an allowed type can include other clients' orders. Use trusted participants and sufficiently unique business IDs. Fanout is live-only: a disconnected client misses new messages, although FIX resend can recover previously assigned messages retained in its session store. Delivery and venue acceptance are not guaranteed. FIX 4.2 and version translation are not supported.

For network access, use distinct per-client FIX passwords and restrict access with a VPN or firewall. TLS is optional; without TLS, messages and passwords are unencrypted on the network path. The security review is incomplete and the gateway has no per-IP login rate limit. See [security review](docs/SECURITY_REVIEW.md) and [configuration](docs/CONFIGURATION.md).

## Documentation

- [Docker installation and configuration](docs/DOCKER_SETUP.md)
- [FIX configuration, credentials and TLS](docs/CONFIGURATION.md)
- [Architecture and delivery semantics](docs/ARCHITECTURE.md)
- [Operations, recovery and troubleshooting](docs/OPERATIONS.md)
- [Test plan and acceptance status](docs/TEST_PLAN.md) · [test results](docs/TEST_STATUS.md)
- [Contributing](CONTRIBUTING.md) · [Support](SUPPORT.md) · [Security reporting](SECURITY.md)

## Professional FIX integration and support

FIX Multiplexer was created and is maintained by **Informed Technology Business Solutions LLC (ITBS)**. ITBS offers paid assistance with installation, Docker deployment, configuration, FIX integrations, certification testing, troubleshooting and custom enhancements. [Contact ITBS](https://www.itbsllc.com/). Paid services are optional.

## License

Copyright 2026 Informed Technology Business Solutions LLC. The project is available under the [Apache License 2.0](LICENSE); third-party components retain their own licenses and notices. This product includes software developed by quickfixengine.org (http://www.quickfixengine.org/). See [third-party notices](THIRD_PARTY_NOTICES.md).
