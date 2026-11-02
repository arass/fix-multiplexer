import quickfix.*;

import java.io.ByteArrayInputStream;
import java.nio.charset.StandardCharsets;
import java.net.URI;
import java.net.http.*;
import java.time.*;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Synthetic cross-engine fixture. No payload logging or external connectivity.
 */
public final class Interop {
    static final SessionID VENUE = new SessionID("FIX.4.4", "MOCKVENUE", "MULTIPLEXER");
    static final String[] CLIENTS = {"DEV_A", "DEV_B", "QA_1"};
    static final BlockingQueue<SessionID> logons = new ArrayBlockingQueue<>(16);
    static final BlockingQueue<SessionID> logouts = new ArrayBlockingQueue<>(16);
    static final BlockingQueue<Delivery> orders = new ArrayBlockingQueue<>(32);
    static final BlockingQueue<Delivery> reports = new ArrayBlockingQueue<>(32);
    static final BlockingQueue<Delivery> replays = new ArrayBlockingQueue<>(16);
    static final BlockingQueue<String> echoes = new ArrayBlockingQueue<>(32);
    static final AtomicReference<String> failure = new AtomicReference<>();
    static final DataDictionary DICTIONARY;
    static String stage = "initialization";

    static {
        try {
            DICTIONARY = new DataDictionary("/tmp/fix-mux-interop.xml");
        } catch (Exception e) {
            throw new ExceptionInInitializerError("cannot load public dictionary");
        }
    }

    record Delivery(Message message, SessionID session) {
    }

    static void check(boolean condition, String reason) {
        if (!condition) throw new AssertionError(reason);
    }

    static void fail(String reason) {
        failure.compareAndSet(null, reason);
    }

    static <T> T receive(BlockingQueue<T> queue) throws Exception {
        T item = queue.poll(20, TimeUnit.SECONDS);
        check(item != null, "timed out awaiting fixture event");
        check(failure.get() == null, "peer reported protocol/fixture failure");
        return item;
    }

    static void offer(BlockingQueue queue, Object item) {
        if (!queue.offer(item)) fail("bounded fixture queue overflow");
    }

    static final class App implements Application {
        public void onCreate(SessionID id) {
        }

        public void onLogon(SessionID id) {
            offer(logons, id);
        }

        public void onLogout(SessionID id) {
            offer(logouts, id);
        }

        public void toAdmin(Message message, SessionID id) {
        }

        public void toApp(Message message, SessionID id) {
            try {
                if (!id.equals(VENUE) && message.getHeader().getString(35).equals("D")) {
                    message.getHeader().setBoolean(43, true);
                    message.getHeader().setString(122, timestamp(LocalDateTime.now(ZoneOffset.UTC).minusSeconds(1)));
                }
            } catch (Exception e) {
                fail("outbound fixture callback failed");
            }
        }

        public void fromAdmin(Message message, SessionID id) {
            try {
                String type = message.getHeader().getString(35);
                if (type.equals("3")) fail("unexpected FIX session reject");
                if (type.equals("0") && message.isSetField(112)) offer(echoes, message.getString(112));
            } catch (Exception e) {
                fail("admin fixture callback failed");
            }
        }

        public void fromApp(Message message, SessionID id) {
            try {
                if (id.equals(VENUE)) {
                    check(message.getHeader().getString(35).equals("D"), "venue received unexpected app type");
                    check(message.getHeader().getString(49).equals("MULTIPLEXER"), "upstream identity leaked");
                    offer(orders, new Delivery(message, id));
                } else {
                    check(message.getHeader().getString(35).equals("8"), "client received unexpected app type");
                    offer(reports, new Delivery(message, id));
                }
            } catch (Throwable e) {
                fail("application fixture validation failed");
            }
        }
    }

    static LogFactory quietLogs = id -> new Log() {
        public void clear() {
        }

        public void onOutgoing(String ignored) {
        }

        public void onEvent(String ignored) {
        }

        public void onErrorEvent(String ignored) {
        }

        public void onIncoming(String raw) {
            try {
                Message parsed = new Message();
                parsed.fromString(raw, DICTIONARY, false);
                if (!id.equals(VENUE) && parsed.getHeader().getString(35).equals("8") && parsed.getHeader().isSetField(43) && parsed.getHeader().getBoolean(43)) {
                    offer(replays, new Delivery(parsed, id));
                }
            } catch (Exception e) {
                fail("independent engine could not parse incoming frame");
            }
        }
    };

    static SessionSettings settings(boolean venue) throws Exception {
        String common = "[default]\nBeginString=FIX.4.4\nNonStopSession=Y\nHeartBtInt=30\nReconnectInterval=1\nLogonTimeout=5\nLogoutTimeout=1\nResetOnLogon=N\nResetOnLogout=N\nResetOnDisconnect=N\nUseDataDictionary=Y\nDataDictionary=/tmp/fix-mux-interop.xml\nValidateUserDefinedFields=N\nFileStorePath=/tmp/qfj-" + (venue ? "venue" : "clients") + "\n";
        if (venue)
            return new SessionSettings(new ByteArrayInputStream((common + "ConnectionType=acceptor\nSocketAcceptAddress=127.0.0.1\nSocketAcceptPort=9880\n[session]\nSenderCompID=MOCKVENUE\nTargetCompID=MULTIPLEXER\n").getBytes(StandardCharsets.UTF_8)));
        StringBuilder ini = new StringBuilder(common + "ConnectionType=initiator\nSocketConnectHost=127.0.0.1\nSocketConnectPort=9879\n");
        for (String id : CLIENTS)
            ini.append("[session]\nSenderCompID=").append(id).append("\nTargetCompID=MULTIPLEXER\n");
        return new SessionSettings(new ByteArrayInputStream(ini.toString().getBytes(StandardCharsets.UTF_8)));
    }

    static SessionID client(String name) {
        return new SessionID("FIX.4.4", name, "MULTIPLEXER");
    }

    static Message message(String type) {
        Message m = new Message();
        m.getHeader().setString(35, type);
        return m;
    }

    static String timestamp(LocalDateTime now) {
        return now.format(DateTimeFormatter.ofPattern("yyyyMMdd-HH:mm:ss.SSS"));
    }

    static void send(Message message, SessionID id) throws Exception {
        check(Session.sendToTarget(message, id), "local peer send refused");
    }

    static void barrier(SessionID id, String marker) throws Exception {
        Message request = message("1");
        request.setString(112, marker);
        send(request, id);
        check(receive(echoes).equals(marker), "admin barrier crossed sessions");
    }

    static Message order(String id) {
        Message m = message("D");
        m.setString(11, id);
        m.setString(21, "1");
        m.setString(55, "TEST");
        m.setString(54, "1");
        m.setString(60, timestamp(LocalDateTime.now(ZoneOffset.UTC)));
        m.setString(38, "1");
        m.setString(40, "2");
        m.setString(44, "10");
        m.setString(59, "0");
        m.setString(5001, "synthetic-extension");
        return m;
    }

    static Message report(String id) {
        Message m = message("8");
        m.setString(11, id);
        m.setString(37, "synthetic-order");
        m.setString(17, "synthetic-exec-" + id);
        m.setString(150, "0");
        m.setString(39, "0");
        m.setString(55, "TEST");
        m.setString(54, "1");
        m.setString(151, "1");
        m.setString(14, "0");
        m.setString(6, "0");
        m.setString(5001, "synthetic-report");
        for (String party : new String[]{"SYNTHETIC_A", "SYNTHETIC_B"}) {
            Group g = new Group(453, 448, new int[]{448, 447, 452, 0});
            g.setString(448, party);
            g.setString(447, "D");
            g.setInt(452, 1);
            m.addGroup(g);
        }
        return m;
    }

    static Map<String, Integer> verifyReports(String expected) throws Exception {
        Map<String, Integer> sequences = new HashMap<>();
        for (int i = 0; i < 3; i++) {
            Delivery d = receive(reports);
            Message m = d.message;
            check(m.getString(11).equals(expected), "offline/duplicate/business ID mismatch");
            check(m.getString(37).equals("synthetic-order") && m.getString(17).equals("synthetic-exec-" + expected), "business identifiers rewritten");
            check(m.getString(5001).equals("synthetic-report"), "extension tag lost");
            check(m.getHeader().getString(49).equals("MULTIPLEXER") && m.getHeader().getString(56).equals(d.session.getSenderCompID()), "downstream identities leaked");
            check(m.getInt(453) == 2 && m.getGroup(2, new Group(453, 448)).getString(448).equals("SYNTHETIC_B"), "group ordering lost");
            check(sequences.put(d.session.getSenderCompID(), m.getHeader().getInt(34)) == null, "duplicate fanout recipient");
        }
        return sequences;
    }

    static void awaitLiveSessions(int count) throws Exception {
        HttpClient client = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(2)).build();
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5);
        while (System.nanoTime() < deadline) {
            HttpRequest request = HttpRequest.newBuilder(URI.create("http://127.0.0.1:8081/metrics")).timeout(Duration.ofSeconds(2)).build();
            String body = client.send(request, HttpResponse.BodyHandlers.ofString()).body();
            long actual = body.lines().filter(line -> line.startsWith("fix_mux_session_logged_on{") && line.endsWith(" 1")).count();
            if (actual == count) return;
        }
        throw new AssertionError("gateway registry did not reach expected connectivity");
    }

    static void verifyGapReports() throws Exception {
        Map<String, List<Delivery>> groups = new LinkedHashMap<>();
        groups.put("gap-recovered", new ArrayList<>());
        groups.put("after-gap", new ArrayList<>());
        for (int i = 0; i < 6; i++) {
            Delivery d = receive(reports);
            String id = d.message.getString(11);
            check(groups.containsKey(id), "unexpected recovered report");
            groups.get(id).add(d);
        }
        Map<String, Map<String, Integer>> sequences = new HashMap<>();
        for (var entry : groups.entrySet()) {
            check(entry.getValue().size() == 3, "gap fanout count");
            for (Delivery d : entry.getValue()) offer(reports, d);
            sequences.put(entry.getKey(), verifyReports(entry.getKey()));
        }
        for (String name : CLIENTS)
            check(sequences.get("after-gap").get(name) > sequences.get("gap-recovered").get(name), "per-session recovery order changed");
    }

    public static void main(String[] args) {
        SocketAcceptor acceptor = null;
        SocketInitiator initiator = null;
        try {
            App app = new App();
            SessionSettings venueSettings = settings(true), clientSettings = settings(false);
            acceptor = new SocketAcceptor(app, new FileStoreFactory(venueSettings), venueSettings, quietLogs, new DefaultMessageFactory());
            initiator = new SocketInitiator(app, new FileStoreFactory(clientSettings), clientSettings, quietLogs, new DefaultMessageFactory());
            acceptor.start();
            initiator.start();
            for (int i = 0; i < 4; i++) receive(logons);
            stage = "admin isolation";
            barrier(VENUE, "ready-up");
            for (String name : CLIENTS) barrier(client(name), "ready-" + name);
            stage = "orders";
            for (String name : CLIENTS) {
                String id = "synthetic-" + name;
                send(order(id), client(name));
                Delivery d = receive(orders);
                check(d.message.getString(11).equals(id) && d.message.getString(5001).equals("synthetic-extension"), "order identifiers/extensions changed");
            }
            stage = "grouped fanout";
            send(report("initial"), VENUE);
            Map<String, Integer> initial = verifyReports("initial");
            stage = "downstream resend";
            Message resend = message("2");
            resend.setInt(7, initial.get("DEV_A"));
            resend.setInt(16, initial.get("DEV_A"));
            send(resend, client("DEV_A"));
            Delivery replay = receive(replays);
            check(replay.message.getString(11).equals("initial") && replay.message.getHeader().isSetField(122), "downstream replay fidelity");
            stage = "upstream gap";
            Session venue = Session.lookupSession(VENUE);
            MessageStore store = venue.getStore();
            int seq = store.getNextSenderMsgSeqNum();
            Message missing = report("gap-recovered");
            missing.getHeader().setString(8, "FIX.4.4");
            missing.getHeader().setString(49, "MOCKVENUE");
            missing.getHeader().setString(56, "MULTIPLEXER");
            missing.getHeader().setInt(34, seq);
            missing.getHeader().setString(52, timestamp(LocalDateTime.now(ZoneOffset.UTC)));
            check(store.set(seq, missing.toString()), "gap fixture persistence failed");
            store.incrNextSenderMsgSeqNum();
            send(report("after-gap"), VENUE);
            verifyGapReports();
            stage = "offline live-only";
            initiator.stop();
            for (int i = 0; i < 3; i++) receive(logouts);
            awaitLiveSessions(1);
            send(report("offline-period"), VENUE);
            barrier(VENUE, "offline-cutpoint");
            stage = "persisted reconnect";
            initiator = new SocketInitiator(app, new FileStoreFactory(clientSettings), clientSettings, quietLogs, new DefaultMessageFactory());
            initiator.start();
            for (int i = 0; i < 3; i++) receive(logons);
            for (String name : CLIENTS) barrier(client(name), "returned-" + name);
            awaitLiveSessions(4);
            send(report("after-return"), VENUE);
            Map<String, Integer> next = verifyReports("after-return");
            for (String name : CLIENTS) check(next.get(name) > initial.get(name), "reconnect reset sequences");
            check(failure.get() == null, "fixture failure");
            System.out.println("PASS: QuickFIX/J 2.3.2, 3 clients/1 upstream, groups/custom fields, admin isolation, independent resends, persisted reconnect and live-only delivery");
        } catch (Throwable e) {
            String detail = e instanceof FieldNotFound ? e.getMessage().replaceAll("[^0-9]", "") : "";
            System.err.println("FAIL: QuickFIX/J interoperability stage=" + stage + " kind=" + e.getClass().getSimpleName() + " tag=" + detail);
            System.exit(1);
        } finally {
            if (initiator != null) initiator.stop();
            if (acceptor != null) acceptor.stop();
        }
    }
}
