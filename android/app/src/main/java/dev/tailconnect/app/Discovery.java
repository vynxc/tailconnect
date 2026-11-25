package dev.tailconnect.app;

import android.content.Context;
import android.net.nsd.NsdManager;
import android.net.nsd.NsdServiceInfo;
import android.net.wifi.WifiManager;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.net.InetAddress;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * Best-effort mDNS discovery on the phone's LAN. The results are streamed to
 * the Go server over stdin; the server forwards them to the agent in the
 * PhoneContext. No discovery data is sent anywhere else.
 */
public final class Discovery {
    private static final String TAG = "TailconnectDiscovery";
    private static final String[] TYPES = {
            "_adb-tls-connect._tcp.",
            "_adb-tls-pairing._tcp.",
            "_googlecast._tcp.",
            "_airplay._tcp.",
            "_raop._tcp.",
            "_http._tcp.",
            "_https._tcp.",
            "_ipp._tcp.",
            "_printer._tcp.",
            "_smb._tcp.",
            "_ssh._tcp.",
            "_workstation._tcp."
    };

    private final Context context;
    private final Emitter emitter;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final Map<String, NsdManager.DiscoveryListener> listeners = new LinkedHashMap<>();
    private final Map<String, JSONObject> resolved = new LinkedHashMap<>();
    private final Map<String, Boolean> resolving = new LinkedHashMap<>();
    private NsdManager nsd;
    private WifiManager.MulticastLock multicastLock;
    private boolean running;
    private boolean emitPosted;

    public interface Emitter { void onDiscovery(String json); }

    public Discovery(Context context, Emitter emitter) {
        this.context = context.getApplicationContext();
        this.emitter = emitter;
    }

    public void start() {
        if (running) return;
        running = true;
        nsd = (NsdManager) context.getSystemService(Context.NSD_SERVICE);
        if (nsd == null) {
            Log.w(TAG, "NsdManager unavailable");
            return;
        }
        WifiManager wifi = (WifiManager) context.getApplicationContext().getSystemService(Context.WIFI_SERVICE);
        if (wifi != null) {
            multicastLock = wifi.createMulticastLock("tailconnect-discovery");
            multicastLock.setReferenceCounted(false);
            try { multicastLock.acquire(); } catch (Exception ignored) {}
        }
        for (String type : TYPES) {
            NsdManager.DiscoveryListener listener = newListener(type);
            listeners.put(type, listener);
            try {
                nsd.discoverServices(type, NsdManager.PROTOCOL_DNS_SD, listener);
            } catch (Exception e) {
                Log.w(TAG, "discover " + type + ": " + e.getMessage());
            }
        }
    }

    public void stop() {
        if (!running) return;
        running = false;
        if (nsd != null) {
            for (NsdManager.DiscoveryListener listener : listeners.values()) {
                try { nsd.stopServiceDiscovery(listener); } catch (Exception ignored) {}
            }
        }
        listeners.clear();
        resolving.clear();
        if (multicastLock != null && multicastLock.isHeld()) multicastLock.release();
        handler.removeCallbacksAndMessages(null);
    }

    private NsdManager.DiscoveryListener newListener(String type) {
        return new NsdManager.DiscoveryListener() {
            @Override public void onStartDiscoveryFailed(String serviceType, int errorCode) {
                Log.w(TAG, "start failed " + serviceType + " code=" + errorCode);
            }
            @Override public void onStopDiscoveryFailed(String serviceType, int errorCode) {
                Log.w(TAG, "stop failed " + serviceType + " code=" + errorCode);
            }
            @Override public void onDiscoveryStarted(String serviceType) {}
            @Override public void onDiscoveryStopped(String serviceType) {}
            @Override public void onServiceFound(NsdServiceInfo info) {
                resolve(type, info);
            }
            @Override public void onServiceLost(NsdServiceInfo info) {
                String key = key(type, info);
                resolved.remove(key);
                emitSoon();
            }
        };
    }

    private void resolve(String type, NsdServiceInfo info) {
        String key = key(type, info);
        if (resolved.containsKey(key) || resolving.containsKey(key)) return;
        resolving.put(key, true);
        try {
            nsd.resolveService(info, new NsdManager.ResolveListener() {
                @Override public void onResolveFailed(NsdServiceInfo serviceInfo, int errorCode) {
                    resolving.remove(key);
                }
                @Override public void onServiceResolved(NsdServiceInfo serviceInfo) {
                    resolving.remove(key);
                    InetAddress host = serviceInfo.getHost();
                    int port = serviceInfo.getPort();
                    if (host == null || port <= 0) return;
                    String address = host.getHostAddress();
                    if (address == null || address.isEmpty()) return;
                    JSONObject o = new JSONObject();
                    try {
                        o.put("name", serviceInfo.getServiceName());
                        o.put("type", type);
                        o.put("host", address);
                        o.put("port", port);
                    } catch (Exception ignored) {
                        return;
                    }
                    resolved.put(key, o);
                    emitSoon();
                }
            });
        } catch (Exception e) {
            resolving.remove(key);
            Log.w(TAG, "resolve: " + e.getMessage());
        }
    }

    private void emitSoon() {
        if (emitPosted) return;
        emitPosted = true;
        handler.postDelayed(() -> {
            emitPosted = false;
            emit();
        }, 400);
    }

    private void emit() {
        List<JSONObject> adb = new ArrayList<>();
        List<JSONObject> mdns = new ArrayList<>();
        for (Map.Entry<String, JSONObject> e : resolved.entrySet()) {
            JSONObject o = e.getValue();
            String type = o.optString("type", "");
            if (type.startsWith("_adb-tls-")) {
                try {
                    o.put("pairing", type.contains("pairing"));
                } catch (Exception ignored) {}
                adb.add(o);
            } else {
                mdns.add(o);
            }
        }
        try {
            JSONObject root = new JSONObject();
            root.put("type", "discovery");
            root.put("adb", new JSONArray(adb));
            root.put("mdns", new JSONArray(mdns));
            emitter.onDiscovery(root.toString());
        } catch (Exception e) {
            Log.w(TAG, "emit: " + e.getMessage());
        }
    }

    private static String key(String type, NsdServiceInfo info) {
        return type.toLowerCase(Locale.US) + "|" + info.getServiceName();
    }
}
