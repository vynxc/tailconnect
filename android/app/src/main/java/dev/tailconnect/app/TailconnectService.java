package dev.tailconnect.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.drawable.Icon;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

/** Foreground service that owns the bundled Go server process. No VpnService. */
public class TailconnectService extends Service {
    private static final String TAG = "TailconnectService";
    private static final String CHANNEL = "tailconnect";
    private static final int NOTIF_ID = 41;

    public static final String ACTION_STOP = "dev.tailconnect.app.STOP";
    public static final String EXTRA_LOOPBACK = "loopback";
    public static final String EXTRA_PERSISTENT = "persistent";

    private Process process;
    private Thread reader;
    private PowerManager.WakeLock wakeLock;
    private volatile boolean stopping;
    private OutputStream stdin;
    private Discovery discovery;
    private boolean persistent;

    @Override
    public void onCreate() {
        super.onCreate();
        createChannel();
        PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
        if (pm != null) {
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "tailconnect:server");
            wakeLock.setReferenceCounted(false);
            wakeLock.acquire(24L * 60 * 60 * 1000);
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopSelf();
            return START_NOT_STICKY;
        }
        startInForeground("Waiting for agent");
        if (process != null && process.isAlive()) return START_NOT_STICKY;

        boolean allowLoopback = intent != null && intent.getBooleanExtra(EXTRA_LOOPBACK, false);
        persistent = intent != null
                ? intent.getBooleanExtra(EXTRA_PERSISTENT, CodeStore.isEnabled(this))
                : CodeStore.isEnabled(this);
        CodeStore.setEnabled(this, persistent);
        String savedCode = persistent ? CodeStore.load(this) : null;
        Net.LanInfo lan = Net.activeLanInfo(this);
        String prefix = lan.prefix;
        File bin = new File(getApplicationInfo().nativeLibraryDir, "libtailconnect.so");
        if (!bin.canExecute()) {
            // Most package managers extract native libraries executable. If
            // this one did not, try once before giving up.
            bin.setExecutable(true, false);
        }
        if (!bin.canExecute()) {
            fail("Bundled server is not executable");
            return START_NOT_STICKY;
        }

        List<String> cmd = new ArrayList<>();
        cmd.add(bin.getAbsolutePath());
        cmd.add("serve");
        cmd.add("--json");
        cmd.add("--parent-pid");
        cmd.add(String.valueOf(android.os.Process.myPid()));
        cmd.add("--phone-name");
        cmd.add(Net.phoneName());
        cmd.add("--app-version");
        cmd.add(Net.appVersion(this));
        cmd.add("--network-type");
        cmd.add(lan.type);
        if (savedCode != null && !savedCode.isEmpty()) {
            cmd.add("--code");
            cmd.add(savedCode);
        }
        if (!lan.address.isEmpty()) {
            cmd.add("--lan-address");
            cmd.add(lan.address);
        }
        if (!lan.prefix.isEmpty()) {
            cmd.add("--lan-prefix");
            cmd.add(lan.prefix);
            cmd.add("--allow");
            cmd.add(lan.prefix);
        }
        if (!lan.gateway.isEmpty()) {
            cmd.add("--gateway");
            cmd.add(lan.gateway);
        }
        for (String dns : lan.dns) {
            cmd.add("--dns");
            cmd.add(dns);
        }
        if (allowLoopback) cmd.add("--allow-loopback");

        try {
            ProcessBuilder pb = new ProcessBuilder(cmd);
            pb.redirectErrorStream(true);
            pb.environment().put("HOME", getFilesDir().getAbsolutePath());
            process = pb.start();
            stdin = process.getOutputStream();
            stopping = false;
            TailconnectState.set(new TailconnectState.Snapshot(
                    true, false, "", "", prefix == null ? "" : prefix, "", "", 0, null));
            startReader();
            discovery = new Discovery(this, this::sendDiscovery);
            discovery.start();
            startInForeground(prefix == null ? "Waiting for agent (no LAN)" : "Sharing " + prefix);
            return START_NOT_STICKY;
        } catch (Exception e) {
            fail("Could not start server: " + e.getMessage());
            return START_NOT_STICKY;
        }
    }

    private void startReader() {
        reader = new Thread(() -> {
            int exit = -1;
            try (BufferedReader in = new BufferedReader(
                    new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
                String line;
                while ((line = in.readLine()) != null) {
                    if (line.startsWith("{")) handleJson(line);
                    else if (!line.isEmpty()) Log.i(TAG, line);
                }
                exit = process.waitFor();
            } catch (Exception e) {
                if (!stopping) Log.w(TAG, "server reader: " + e.getMessage());
            }
            if (!stopping) {
                TailconnectState.Snapshot s = TailconnectState.get();
                TailconnectState.set(s.withRunning(false).withError(
                        exit == 0 ? "" : "Server exited" + (exit > 0 ? " (" + exit + ")" : "")));
                stopSelf();
            }
        }, "tailconnect-reader");
        reader.setDaemon(true);
        reader.start();
    }

    private void sendDiscovery(String json) {
        OutputStream out = stdin;
        if (out == null) return;
        try {
            synchronized (this) {
                out.write(json.getBytes(StandardCharsets.UTF_8));
                out.write('\n');
                out.flush();
            }
        } catch (Exception ignored) {
            // The server may be stopping; discovery is best-effort.
        }
    }

    private void handleJson(String line) {
        try {
            JSONObject o = new JSONObject(line);
            String type = o.optString("type");
            TailconnectState.Snapshot old = TailconnectState.get();
            if ("started".equals(type)) {
                String code = o.optString("code", "");
                if (persistent && !code.isEmpty()) CodeStore.save(this, code);
                TailconnectState.set(new TailconnectState.Snapshot(
                        true, false,
                        code,
                        o.optString("link", ""),
                        o.optString("policy", ""),
                        "", "",
                        o.optInt("region", 0),
                        null));
                startInForeground(persistent ? "Waiting for agent (persistent code)" : "Waiting for agent");
            } else if ("state".equals(type)) {
                JSONObject s = o.optJSONObject("state");
                if (s == null) return;
                JSONObject agent = s.optJSONObject("agent");
                boolean connected = agent != null && agent.optBoolean("connected", false);
                String agentName = agent == null ? "" : agent.optString("name", "");
                String policy = s.optString("policy", old.policy);
                String code = s.optString("code", old.code);
                List<TailconnectState.Service> services = new ArrayList<>();
                JSONArray arr = s.optJSONArray("services");
                if (arr != null) {
                    for (int i = 0; i < arr.length(); i++) {
                        JSONObject svc = arr.optJSONObject(i);
                        if (svc == null) continue;
                        services.add(new TailconnectState.Service(
                                svc.optString("id", ""),
                                svc.optString("label", ""),
                                svc.optInt("port", 0),
                                svc.optString("url", "")));
                    }
                }
                TailconnectState.set(new TailconnectState.Snapshot(
                        true, connected, code, old.link, policy, agentName, "",
                        old.region, services));
                startInForeground(connected ? "Agent: " + (agentName.isEmpty() ? "connected" : agentName)
                        : "Waiting for agent");
            }
        } catch (Exception e) {
            Log.w(TAG, "bad state line: " + e.getMessage());
        }
    }

    private void fail(String message) {
        TailconnectState.set(TailconnectState.get().withRunning(false).withError(message));
        startInForeground(message);
        stopSelf();
    }

    private void startInForeground(String text) {
        Notification n = buildNotification(text);
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(NOTIF_ID, n);
        }
    }

    private Notification buildNotification(String text) {
        Intent open = new Intent(this, MainActivity.class);
        PendingIntent openPi = PendingIntent.getActivity(this, 0, open,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        Intent stop = new Intent(this, TailconnectService.class).setAction(ACTION_STOP);
        PendingIntent stopPi = PendingIntent.getService(this, 1, stop,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        Notification.Builder b = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(this, CHANNEL)
                : new Notification.Builder(this);
        return b.setContentTitle("Tailconnect")
                .setContentText(text)
                .setSmallIcon(R.drawable.ic_tailconnect)
                .setOngoing(true)
                .setContentIntent(openPi)
                .addAction(new Notification.Action.Builder(Icon.createWithResource(this, R.drawable.ic_tailconnect), "Stop", stopPi).build())
                .build();
    }

    private void createChannel() {
        if (Build.VERSION.SDK_INT < 26) return;
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (nm == null) return;
        NotificationChannel ch = new NotificationChannel(CHANNEL, "Tailconnect", NotificationManager.IMPORTANCE_LOW);
        ch.setDescription("Serverless Tailcat tunnel");
        nm.createNotificationChannel(ch);
    }

    @Override
    public void onDestroy() {
        stopping = true;
        if (discovery != null) discovery.stop();
        if (stdin != null) {
            try { stdin.close(); } catch (Exception ignored) {}
        }
        if (process != null) {
            process.destroy();
            try {
                if (!process.waitFor(2, TimeUnit.SECONDS)) process.destroyForcibly();
            } catch (InterruptedException ignored) {
                process.destroyForcibly();
            }
        }
        if (reader != null) reader.interrupt();
        if (wakeLock != null && wakeLock.isHeld()) wakeLock.release();
        TailconnectState.set(TailconnectState.get().withRunning(false));
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }
}
