# Independent-engine fixture

Build from the repository root, then run with no network access beyond container loopback:

```sh
docker build -f interop/quickfixj/Dockerfile -t fix-multiplexer-interop .
docker run --rm --network none --memory 512m fix-multiplexer-interop
```

The image builds the real Go daemon and QuickFIX/J 2.3.2 peers using the versions in pom.xml. It requires public dependency downloads during build, but no credentials or external venue. Runtime uses one Java venue acceptor, three Java client initiators, and one Go upstream session. All data is synthetic; raw FIX logging is suppressed. Temporary state disappears with the container.

Assertions cover logon, unchanged business IDs, custom scalar tag 5001, report groups, per-session headers, inbound PossDup forwarding, admin isolation, downstream stored replay, an upstream sequence gap, durable reconnect, and no offline report backlog. Protocol and metrics events synchronize the fixture; there are no synchronization sleeps. Fanout across clients is not assumed atomic.

The fixture generates a temporary dictionary from the public stock FIX44 dictionary with tag 5001 declared for the two tested messages. QuickFIX/J needs that declaration to distinguish a custom body field following a repeating group. No vendor dictionary or private integration is used. This is interoperability evidence for these cases, not exchange certification or support for every FIX 4.4 message.
