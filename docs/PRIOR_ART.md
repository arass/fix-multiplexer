# Related projects and inspiration

These projects provide ideas and/or underlying engines. As reviewed for this plan, none was verified as a drop-in implementation of **one upstream FIX session shared by multiple downstream FIX sessions with upstream fanout**.

| Project | What to learn | What not to assume |
|---|---|---|
| [QuickFIX/Go](https://github.com/quickfixgo/quickfix) | Open-source Go session engine: initiators, acceptors, FIX versions/dictionaries, persistence, TLS and resend. | It does not automatically make a many-to-one order-unaware gateway. Implement routing separately. |
| [QuickFIX/Go examples](https://github.com/quickfixgo/examples) | Working executor/initiator apps, packaging and Docker examples. | An order matching engine example is not equivalent to a mux. |
| [Nest QuickFIX](https://github.com/sotarak/nest-quickfix) | Multi-session room-style broadcast API, resource caps, TLS, redacted logging and extensive fault/reconnect tests. | Its session engine is JavaScript/TypeScript and should not be copied or partially reimplemented in Go. |
| [Artio](https://github.com/artiofix/artio) | Mature separation of FIX engine and application, archival/replay ideas, performance discipline. | Internal Aeron multiplexing is not our external client fanout feature. |
| [QuickFIX/J](https://github.com/quickfix-j/quickfixj) | Independent interoperability peer; alternative platform for a future implementation. | It does not remove the need for application routing/broadcast code. |

Inspect current license and code before reusing anything. This document is descriptive, not a license grant or confirmation that these projects meet this spec.
