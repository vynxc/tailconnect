package dev.tailconnect.app;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;

import java.net.Inet4Address;
import java.net.InetAddress;

/** Reads the active Wi-Fi/Ethernet IPv4 subnet so the Go server can allowlist it. */
public final class Net {
    private Net() {}

    public static String activeLanPrefix(Context context) {
        try {
            ConnectivityManager cm = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
            if (cm == null) return null;
            Network network = cm.getActiveNetwork();
            if (network == null) return null;
            NetworkCapabilities caps = cm.getNetworkCapabilities(network);
            if (caps == null) return null;
            if (!caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
                    && !caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) {
                return null;
            }
            LinkProperties lp = cm.getLinkProperties(network);
            if (lp == null) return null;
            for (LinkAddress la : lp.getLinkAddresses()) {
                InetAddress address = la.getAddress();
                if (!(address instanceof Inet4Address)) continue;
                if (address.isLoopbackAddress() || address.isLinkLocalAddress() || address.isAnyLocalAddress()) continue;
                int prefix = la.getPrefixLength();
                if (prefix < 0 || prefix > 32) continue;
                byte[] b = address.getAddress();
                int value = ((b[0] & 0xff) << 24) | ((b[1] & 0xff) << 16)
                        | ((b[2] & 0xff) << 8) | (b[3] & 0xff);
                int mask = prefix == 0 ? 0 : (0xffffffff << (32 - prefix));
                int networkValue = value & mask;
                String networkAddress = String.format(java.util.Locale.US, "%d.%d.%d.%d",
                        (networkValue >>> 24) & 0xff,
                        (networkValue >>> 16) & 0xff,
                        (networkValue >>> 8) & 0xff,
                        networkValue & 0xff);
                if (!isPrivateV4(b)) continue;
                return networkAddress + "/" + prefix;
            }
        } catch (Exception ignored) {
            // Network state is best-effort; the service still runs without LAN sharing.
        }
        return null;
    }

    private static boolean isPrivateV4(byte[] b) {
        int a0 = b[0] & 0xff;
        int a1 = b[1] & 0xff;
        if (a0 == 10) return true;
        if (a0 == 172 && a1 >= 16 && a1 <= 31) return true;
        return a0 == 192 && a1 == 168;
    }
}
