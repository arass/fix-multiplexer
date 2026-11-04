# FIX Multiplexer: FIX 4.4 gateway in Go

FIX Multiplexer is an open-source session multiplexer and application gateway for the Financial Information eXchange (FIX) protocol. Built in Go (Golang) with QuickFIX/Go, it lets multiple FIX clients share one upstream FIX 4.4 session. Run it with Docker or use the source as a working QuickFIX/Go integration example.

```text
Multiple FIX clients         ──>  FIX Multiplexer  ──>  One FIX counterparty session
                                  acceptor / initiator
```

Each client has its own FIX session, sequence numbers and resend history. The gateway forwards permitted application messages upstream and fans out permitted messages to connected clients. FIX session administration stays independent on each side. No order ownership database or `ClOrdID` rewriting is used.

- Share one counterparty session across applications, developers or test harnesses.
- Configure message types globally and per client. A read-only subscriber cannot submit application messages upstream.
- Receive a best-effort Drop Copy-style feed of messages actually supplied by the counterparty. This is not a venue-wide feed or authoritative audit trail.
- Use persistent stores, configurable retention, health checks, optional TLS and separate downstream passwords.

## When to use a FIX session multiplexer

| What you need | How this project helps |
|---|---|
| Share one broker or exchange connection | Multiple FIX clients connect through one upstream session, with independent downstream session identities and sequence numbers. |
| Subscribe to execution reports without submitting orders | Read-only FIX clients receive selected message types from the existing upstream feed. |
| Test FIX client connectivity and recovery | Run a local mock counterparty and clients, then examine tests for reconnects, resend requests and upstream outages. |

## FIX Drop Copy and read-only subscribers

FIX Multiplexer supports **read-only FIX subscribers** alongside writable downstream clients sharing a FIX 4.4 connection. Configure a client with `read_only: true` to prevent its application messages from being sent upstream, and use `receive_message_types` to choose which globally enabled upstream message types it receives. For example, subscribe to execution reports (`35=8`) or other application messages your counterparty sends. The [example YAML](config/example.yaml) includes a `DROP_COPY` subscriber; see [configuration details](docs/CONFIGURATION.md) for the options.

This is best-effort, **Drop Copy-style fanout** over the gateway's existing upstream FIX session. It does not establish a separate venue Drop Copy session or create a venue-wide feed: subscribers can only receive messages the configured counterparty sends to that session. Delivery is live-only, so disconnected subscribers miss new messages, and the service is not an authoritative audit trail. All connected subscribers may see reports about other clients' orders; use only with trusted participants. See [delivery and security limitations](docs/ARCHITECTURE.md).

## Quick start: published Docker image

Try the published image without building from source. This runs the isolated mock demo:

```sh
docker pull ghcr.io/arass/fix-multiplexer:v0.1.0
docker run --rm --network none ghcr.io/arass/fix-multiplexer:v0.1.0 -demo
```

If the GHCR package is public, it can be pulled anonymously. If it is private, authenticate with a GitHub token that has `read:packages` access first. For deployment configuration, see the [Docker setup guide](docs/DOCKER_SETUP.md).

## Quick start: Docker local build

To build from source instead, run the isolated Compose mock demo from the repository root with Docker installed:

```sh
docker compose -f examples/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke
```

This builds the image locally, starts a simulated counterparty and three clients, checks FIX message flow and exits. It requires no real credentials or external FIX connection.

To connect the gateway to your counterparty, follow the **[Docker setup guide](docs/DOCKER_SETUP.md)** for image handoff, configuration, persistent session stores, network access and troubleshooting. The annotated [example configuration](config/example.yaml) includes writable clients and a read-only subscriber.

## QuickFIX/Go example: FIX initiator, acceptor and message routing

This repository also serves as a working example of using QuickFIX/Go to build a FIX application in Go. The gateway runs a FIX initiator toward the counterparty and a FIX acceptor for downstream clients. It demonstrates application callbacks, message forwarding, session stores, custom dictionaries and repeating groups, with runnable mock peers and recovery tests.

Start with the [architecture](docs/ARCHITECTURE.md), then follow the code for the part you need:

| Example | Where to look |
|---|---|
| Configure and start initiator and acceptor sessions | [Session settings](internal/fixadapter/settings.go) and [engine lifecycle](internal/fixadapter/runtime.go) |
| Handle Logon and application callbacks | [QuickFIX/Go Application implementation](internal/fixadapter/application.go) |
| Copy application fields and dictionary-defined repeating groups into a new session | [Message adapter](internal/fixadapter/message.go) |
| Build mock FIX clients and a counterparty for local tests | [Test peers](internal/testfix/peer.go) and [recovery integration tests](internal/fixadapter/recovery_integration_test.go) |

Each side keeps its own FIX session administration and sequence stream; the gateway forwards permitted application messages. For smaller introductory examples, see the upstream [QuickFIX/Go examples](https://github.com/quickfixgo/examples).

## Limitations and security

Deployment is an operator decision: assess the delivery, recovery and security limits against your requirements. The project makes no production-readiness or venue-certification claim.

Clients share the counterparty's business context; messages of an allowed type can include other clients' orders. Use trusted participants and sufficiently unique business IDs. Fanout is live-only: a disconnected client misses new messages, although FIX resend can recover previously assigned messages retained in its session store. Delivery and venue acceptance are not guaranteed. FIX 4.2 and version translation are not supported.

For network access, use distinct per-client FIX passwords and restrict access with a VPN or firewall. TLS is optional; without TLS, messages and passwords are unencrypted on the network path. The security review is incomplete and the gateway has no per-IP login rate limit. See [security review](docs/SECURITY_REVIEW.md) and [configuration](docs/CONFIGURATION.md).

## Documentation

- [Docker installation and configuration](docs/DOCKER_SETUP.md)
- [FIX configuration, credentials and TLS](docs/CONFIGURATION.md)
- [Architecture and delivery semantics](docs/ARCHITECTURE.md)
- [Operations, recovery and troubleshooting](docs/OPERATIONS.md)
- [Test plan and acceptance status](docs/TEST_PLAN.md) · [test results](docs/TEST_STATUS.md)
- [Contributing](CONTRIBUTING.md) · [Support](SUPPORT.md) · [Security reporting](SECURITY.md)

## FIX integration consulting and custom software development

FIX Multiplexer was created and is maintained by **Informed Technology Business Solutions LLC (ITBS)**. ITBS offers paid engineering assistance for teams implementing FIX connectivity or developing software around it:

- **FIX integration and troubleshooting:** session configuration, Docker deployment, counterparty-specific dictionaries and extension tags, reconnect and resend issues, and assistance with certification testing.
- **Custom FIX development:** client integrations, message processing, test harnesses and gateway enhancements for your workflow.
- **Broader software development:** backend services, APIs, systems integration, automation and help getting stalled software projects moving again.

[Discuss your FIX integration or software project with ITBS](https://www.itbsllc.com/). Describe what you need to connect, build or fix so we can discuss the scope. Paid services are optional; the project remains available under the Apache License 2.0.

## License

Copyright 2026 Informed Technology Business Solutions LLC. The project is available under the [Apache License 2.0](LICENSE); third-party components retain their own licenses and notices. This product includes software developed by quickfixengine.org (http://www.quickfixengine.org/). See [third-party notices](THIRD_PARTY_NOTICES.md).
