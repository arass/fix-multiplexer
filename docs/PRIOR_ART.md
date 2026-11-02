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

## Implementation review (2026-11-02)

Reviewed the linked primary repositories and QuickFIX/Go v0.9.12 downloaded source. A targeted search did not verify a ready-made product meeting the complete external many-to-one, live-fanout requirements; this is a limited finding, not a claim that no such product exists.

Engine API findings from application.go, registry.go, session.go, message.go, field_map.go and store/file/file_store.go:

- Route only inbound FromApp. Outbound resends also invoke ToApp; that callback must never route.
- SendToTarget persists and queues a new message. A nil result is local acceptance, not socket delivery or venue business acceptance. The engine can queue while disconnected, so application connectivity checks are essential.
- File stores default to fsync but are not automatically age/size bounded. The gateway must own limits and close stores explicitly.
- FieldMap.CopyInto copies only the first TagValue of a group. The adapter uses dictionary-derived RepeatingGroup/GetGroup/SetGroup reconstruction instead, with nested-group regression coverage; no engine patch or parser fork.
- The engine parser handles length-delimited XMLData specially but does not support embedded SOH in EncodedText. Ordinary encoded text works. Embedded-SOH bodies are unsupported and the reconstruction regression asserts visible failure.
- No StartTime/EndTime means no scheduled daily boundary. ResetOnLogon/Logout/Disconnect are explicitly disabled. Reset requires coordinated maintenance.

QuickFIX/Go and QuickFIX/J use the QuickFIX Software License 1.0. The actual Go dependency license and dictionary attribution are retained in third_party and THIRD_PARTY_NOTICES.md. Other projects remain references only; verify exact versions and licensing before any future reuse. Independent QuickFIX/J interoperability remains pending.
