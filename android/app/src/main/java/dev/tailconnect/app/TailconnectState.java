package dev.tailconnect.app;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;

/** Process-wide state shared by the foreground service and the activity. */
public final class TailconnectState {
    public interface Listener { void onState(Snapshot snapshot); }

    public static final class Service {
        public final String id;
        public final String label;
        public final int port;
        public final String url;

        public Service(String id, String label, int port, String url) {
            this.id = id;
            this.label = label;
            this.port = port;
            this.url = url;
        }
    }

    public static final class Snapshot {
        public final boolean running;
        public final boolean agentConnected;
        public final String code;
        public final String link;
        public final String policy;
        public final String agentName;
        public final String error;
        public final int region;
        public final List<Service> services;

        public Snapshot(boolean running, boolean agentConnected, String code, String link,
                        String policy, String agentName, String error, int region,
                        List<Service> services) {
            this.running = running;
            this.agentConnected = agentConnected;
            this.code = code;
            this.link = link;
            this.policy = policy;
            this.agentName = agentName;
            this.error = error;
            this.region = region;
            this.services = services == null ? Collections.emptyList() : services;
        }

        public static Snapshot empty() {
            return new Snapshot(false, false, "", "", "", "", "", 0, Collections.emptyList());
        }

        public Snapshot withError(String message) {
            return new Snapshot(running, agentConnected, code, link, policy, agentName,
                    message, region, services);
        }

        public Snapshot withRunning(boolean value) {
            return new Snapshot(value, agentConnected, code, link, policy, agentName,
                    error, region, services);
        }
    }

    private static final CopyOnWriteArrayList<Listener> listeners = new CopyOnWriteArrayList<>();
    private static volatile Snapshot current = Snapshot.empty();

    private TailconnectState() {}

    public static Snapshot get() { return current; }

    public static void set(Snapshot snapshot) {
        current = snapshot;
        for (Listener listener : listeners) listener.onState(snapshot);
    }

    public static void addListener(Listener listener) {
        listeners.addIfAbsent(listener);
        listener.onState(current);
    }

    public static void removeListener(Listener listener) {
        listeners.remove(listener);
    }

    public static List<Service> copyServices(List<Service> in) {
        return new ArrayList<>(in);
    }
}
