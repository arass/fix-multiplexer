# Architecture and delivery semantics

Per-client subscriber policies apply only to application routing. A read-only client cannot submit application messages upstream; a gateway BusinessMessageReject reports each denial. A client's optional receive list narrows the global fanout allowlist, and the global deny list wins. An omitted receive list inherits the global policy; an empty list receives no upstream application messages. Gateway-generated submission errors and shared upstream session Reject notices bypass receive filters. Session administration remains local to each FIX engine, and disconnected clients receive no new application backlog. Subscriptions are by message type, not order ownership.

## Connections

Each downstream client has a distinct FIX session with the multiplexer (acceptor). The multiplexer has exactly one upstream FIX session (initiator). The daemon terminates FIX at both boundaries: its application layer forwards business messages but **never** tunnels FIX admin frames or upstream sequence numbers downstream.

Optional per-client `allowed_ips` is checked against the TCP peer IP in the engine's connection validator, before session binding or Logon processing. Omission leaves source IP unrestricted; an empty list denies all sources. Unknown/disabled clients cannot obtain a binding, and an IP-policy denial cannot occupy another connection's session binding or consume FIX sequence numbers. Password authentication still runs for permitted sources. The gateway closes denied sockets and emits a rate-limited diagnostic with the observed IP and hashed claimed-session label; it does not trust a source address supplied inside FIX traffic. See [configuration](CONFIGURATION.md#per-client-source-ip-restrictions).

## Message handling and boundaries

Optional file outputs subscribe directly to upstream application callbacks, independently of network clients' logged-on state. They inherit or narrow the global fanout type policy and obey the global deny list. Each output owns a bounded queue and a single file writer; it has no FIX identity, listener, sequences or resend store. JSONL records preserve the upstream snapshot plus explicit original `PossDupFlag (43)`, `OrigSendingTime (122)` and upstream sequence metadata. Downstream frames continue to be reconstructed independently. Admin callbacks, gateway-generated notices and outbound replay callbacks are excluded from file capture. See [file output semantics](FILE_OUTPUTS.md).

Confirmed policy: deliver valid inbound PossDup application messages; the flag is not a reason to discard them. Outbound replay callbacks must not initiate a second routing operation. Duplicate business delivery remains possible and clients must tolerate it.

If forwarding cannot proceed, the gateway returns an identified FIX BusinessMessageReject where possible. This is not a venue execution report or acceptance, and a failed send can have an uncertain outcome.

1. **Downstream inbound:** engine validates transport, FIX session state and sequence. Application callback sends a validated business message to a bounded upstream forwarding path. Forwarding happens as a *new* application message on upstream session. Preserve business fields and custom tags, and do not preserve session ownership headers.
2. **Upstream inbound:** engine validates transport and FIX session state. Application callback clones business fields for each logged-on downstream session. Send each clone through the engine for fresh session headers and sequence numbers.
3. **Admin:** both ends independently process FIX session management. A downstream `ResendRequest (35=2)` is served from *that downstream session's* store, not relayed upstream. An upstream resend is independent and must not accidentally invoke a second business fanout.

4. **Business IDs:** no changes to `11`, `41`, `37`, `17`. Neither global uniqueness nor broker-side acceptance is guaranteed. Caller must coordinate or use sufficiently unique IDs if the counterparty checks them.

An upstream session Reject (`35=3`) remains an upstream admin message. The gateway emits a new `35=j` diagnostic notice to live downstream clients, including labeled upstream reject details in text and omitting downstream RefSeqNum. This notice bypasses application message type filters; it does not identify order ownership or imply that every recipient's order was rejected. Delivery uses the existing bounded live fanout and downstream session store.

### Application fields and header policy

The adapter reconstructs application bodies through QuickFIX/Go and sends a new message with independent session framing. Original header and trailer fields other than MsgType are discarded, including `8`, `9`, `10`, `34`, `49`, `52`, `56`, `43`, `122` and delegation headers. Unknown scalar body tags survive; repeating groups require definitions in the shared XML dictionary. Body field ordering follows the engine. Embedded SOH in EncodedText is unsupported; see [operations](OPERATIONS.md) for limitations.

### Recipient filtering and visibility

By default, globally allowed messages go to every authorized logged-on downstream session. Optional per-client receive lists narrow this by MsgType, but do not distinguish who owns an order. Clients receiving a type can see messages of that type concerning other clients and should handle unknown orders. Use mutually trusted participants. The global `fanout.deny_message_types` also applies to downstream-to-upstream submissions, in addition to fanout.

### Concurrent messages and order

A single upstream worker serializes application submissions; each downstream session has its own bounded queue and worker. When a downstream queue or shared byte budget is full, that recipient's message is dropped and counted, while other recipients continue. A full upstream queue produces a visible forwarding error. Queue admission does not imply peer or venue acceptance.

## Delivery and failure matrix

| Scenario | Expected semantics |
|---|---|
| All connections healthy | Best-effort upstream forwarding and live fanout; engine uses independent sequence streams. |
| Client disconnected | **No offline fanout queue**. Messages received while offline are not stored/delivered for that client. Other live clients unaffected. |
| Client reconnects after previously sent message lost in transit | Its FIX session can resend messages already persisted/assigned to its own outgoing sequence stream. |
| Client reconnects after offline period | It does not receive messages never assigned to its session while it was offline. |
| Upstream disconnected | Gateway becomes not-ready and visibly fails new downstream submissions. It cannot recover application messages the counterparty never makes available. |
| Upstream reconnects with gap | Rely on upstream engine's FIX resend protocol; forward valid inbound PossDup messages; outbound replay callbacks do not initiate business dispatch. |
| Gateway crashes | Durable stores recover assigned sequences and replay-eligible traffic, but there may be an ambiguous in-flight send window. No exactly-once guarantee. |
| One fanout target fails | Other targets still receive reports; failure is logged/metricized. No atomicity guarantee. |
| File capture queue/storage fails | The affected output latches unhealthy and counts failed/discarded captures; readiness fails and a metadata diagnostic is emitted. Normal FIX routing and other outputs continue. Fix/restart is required; no silent offline catch-up is implied. |
| Stale session cleaned | Only offline stores are eligible; session must be reset/reprovisioned on next use in a mutually agreed way. |
| Store full/corrupt | Fail closed or disable affected session, do not silently erase required replay data while implying FIX recovery remains intact. |

## Store growth and cleanup

A FIX message store contains outbound replay history as well as sequence-state metadata. Messages sent while a client was logged on may require replay. For a retired client, preserving all historical bytes forever is inappropriate; blindly deleting older messages from an *active* session's store can make protocol-correct resend impossible.

Current policy:

- Configure downstream replay history with an 8h default, independently of inactive-client TTL. No promise of reconstruction beyond the configured window. Expiry must lead to explicit reset/reprovisioning when needed; do not silently remove required history from active sessions. Upstream retention is a separate policy.

- Named/allowlisted clients use a configurable idle TTL (8 hours by default; the example sets 7 days). Connection state is recorded.
- Keep session stores for disconnected-but-not-yet-expired clients, within configured monitoring limits. No application backlog is created while disconnected.
- Explicit offline maintenance can retire and clean up **only closed** sessions past their TTL; cleanup is not automatic. Reuse requires coordinated reset or a new SessionID.
- Alert at disk thresholds; if limits are exhausted for an active client, fail visibly rather than quietly deleting the resend store.
- Support explicit admin `list-clients`, `disable-client` and `prune-stale --dry-run` operations or equivalent config-driven offline maintenance. Audit cleanup actions.
- Document upstream store treatment separately: one upstream session can have a much longer resend requirement than downstream sessions; never auto-prune it on the downstream client TTL.

## Readiness

`/live` checks that the daemon process serves requests. `/ready` requires upstream FIX `LOGGED_ON`, running routing workers, healthy required stores and healthy configured file outputs. A disconnected downstream client does not make the entire service unready. The management listener must bind to loopback.

## Protocol assumptions

FIX 4.4 is the only supported version, with identical configurable XML dictionaries on both boundaries. FIX 4.2 and version translation are not supported. Counterparties differ in message types, session policies and fanout permissions; validate compatibility separately.
