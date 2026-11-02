# Architecture and delivery semantics

## Connections

Each downstream client has a distinct FIX session with the multiplexer (acceptor). The multiplexer has exactly one upstream FIX session (initiator). The daemon terminates FIX at both boundaries: its application layer forwards business messages but **never** tunnels FIX admin frames or upstream sequence numbers downstream.

## Message handling and boundaries

Confirmed policy: deliver valid inbound PossDup application messages; the flag is not a reason to discard them. Outbound replay callbacks must not initiate a second routing operation. Duplicate business delivery remains possible and clients must tolerate it.

If forwarding cannot proceed, send a standard FIX error identified as originating at the gateway, preferably BusinessMessageReject where applicable. Do not generate a venue execution report. Test request correlation and the chosen message-type-specific behavior before declaring it supported.

1. **Downstream inbound:** engine validates transport, FIX session state and sequence. Application callback sends a validated business message to a bounded upstream forwarding path. Forwarding happens as a *new* application message on upstream session. Preserve business fields and custom tags, and do not preserve session ownership headers.
2. **Upstream inbound:** engine validates transport and FIX session state. Application callback clones business fields for each logged-on downstream session. Send each clone through the engine for fresh session headers and sequence numbers.
3. **Admin:** both ends independently process FIX session management. A downstream `ResendRequest (35=2)` is served from *that downstream session's* store, not relayed upstream. An upstream resend is independent and must not accidentally invoke a second business fanout.
4. **Business IDs:** no changes to `11`, `41`, `37`, `17`. Neither global uniqueness nor broker-side acceptance is guaranteed. Caller must coordinate or use sufficiently unique IDs if the counterparty checks them.

### Application fields and header policy

Develop a documented field-copy allowlist/denylist based on QuickFIX/Go's actual message model, not ad hoc string splitting. Required session fields such as `8`, `9`, `10`, `34`, `49`, `52`, `56`, `43`, `122`, and session-specific comp/sub IDs cannot be carried over without a deliberate interpretation. Unknown body tags, custom fields and repeating groups must survive. Test encoded lengths/data, nested groups if supported, and dictionary customizations. Explicitly document any tags that cannot be preserved or require an extension mechanism.

### No recipient filtering

Default fanout goes to **every authorized logged-on downstream session**. One report might relate to a single order, but all clients may receive it. Downstream clients are responsible for ignoring unknown orders. Security boundaries are therefore **not** provided between downstream clients: use only mutually trusted test environments.

### Concurrent messages and order

Enforce upstream application send order via a defined serialization point or documented ordering policy. Avoid holding a global lock while waiting for slow downstream clients. A slow client must not stall unrelated clients; bounded per-client queues and explicit overflow/disconnect policy. Preserve per-session outgoing order. Do not allow uncontrolled goroutine spawning or unlimited buffering.

## Delivery and failure matrix

| Scenario | Expected semantics |
|---|---|
| All connections healthy | Best-effort upstream forwarding and live fanout; engine uses independent sequence streams. |
| Client disconnected | **No offline fanout queue**. Messages received while offline are not stored/delivered for that client. Other live clients unaffected. |
| Client reconnects after previously sent message lost in transit | Its FIX session can resend messages already persisted/assigned to its own outgoing sequence stream. |
| Client reconnects after offline period | It does not receive messages never assigned to its session while it was offline. |
| Upstream disconnected | Gateway becomes not-ready and visibly fails new downstream submissions. It cannot recover application messages counterparty never makes available. |
| Upstream reconnects with gap | Rely on upstream engine's FIX resend protocol; forward valid inbound PossDup messages; outbound replay callbacks do not initiate business dispatch. |
| Gateway crashes | Durable stores recover assigned sequences and replay-eligible traffic, but there may be an ambiguous in-flight send window. No exactly-once guarantee. |
| One fanout target fails | Other targets still receive reports; failure is logged/metricized. No atomicity guarantee. |
| Stale session cleaned | Only offline stores are eligible; session must be reset/reprovisioned on next use in a mutually agreed way. |
| Store full/corrupt | Fail closed or disable affected session, do not silently erase required replay data while implying FIX recovery remains intact. |

## Store growth and cleanup

A FIX message store contains outbound replay history as well as sequence-state metadata. Messages sent while a client was logged on may require replay. For a retired client, preserving all historical bytes forever is inappropriate; blindly deleting older messages from an *active* session's store can make protocol-correct resend impossible.

Recommended v1 policy:

- Configure downstream replay history with an 8h default, independently of inactive-client TTL. No promise of reconstruction beyond the configured window. Expiry must lead to explicit reset/reprovisioning when needed; do not silently remove required history from active sessions. Upstream retention is a separate policy.

- Provision named/allowlisted clients with a configured idle TTL (e.g., 7 days for UAT). Record last logout/connection/disconnection time.
- Keep session stores for disconnected-but-not-yet-expired clients, within configured monitoring limits. No application backlog is created while disconnected.
- On TTL expiration, disable/deprovision and clean up **only closed** sessions. Require explicit reset or new SessionID for reuse; don't silently restart at seq 1 against a peer expecting a higher number.
- Alert at disk thresholds; if limits are exhausted for an active client, fail visibly rather than quietly deleting the resend store.
- Support explicit admin `list-clients`, `disable-client` and `prune-stale --dry-run` operations or equivalent config-driven offline maintenance. Audit cleanup actions.
- Document upstream store treatment separately: one upstream session can have a much longer resend requirement than downstream UAT sessions; never auto-prune it on the downstream client TTL.

## Readiness

`/live` checks that the daemon process serves requests. `/ready` requires upstream FIX `LOGGED_ON`, running routing workers, and writable/healthy required stores. A disconnected downstream client does not make the entire service unready. HTTP management must bind to localhost or a private interface by default.

## Protocol assumptions

Initial release: FIX 4.4 between all parties, with configurable XML dictionary and custom tags. Do not assume counterparty or another venue accepts all MsgTypes, shares our session reset schedule, resends all application messages, or allows message fanout. Validate with local mocks; separately document vendor-specific constraints.
