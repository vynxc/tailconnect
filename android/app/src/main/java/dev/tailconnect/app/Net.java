package dev.tailconnect.app;

import android.content.Context;
import android.content.pm.PackageInfo;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.RouteInfo;
import android.os.Build;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/** Reads the active network context the agent needs, without any VPN APIs. */
public final class Net {
    private Net() {}

    public static final class LanInfo {
        public final String type;
        public final String interfaceName;
        public final String address;
        public final String prefix;
        public final String gateway;
        public final List<String> dns;

        LanInfo(String type, String interfaceName, String address, String prefix,
                String gateway, List<String> dns) {
            this.type = type;
            this.interfaceName = interfaceName;
            this.address = address;
            this.prefix = prefix;
            this.gateway = gateway;
            this.dns = dns;
        }

        public static LanInfo none(String type) {
            return new LanInfo(type, "", "", "", "", new ArrayList<>());
        }
    }

    public static String activeLanPrefix(Context context) {
        return activeLanInfo(context).prefix;
    }

    public static LanInfo activeLanInfo(Context context) {
        try {
            ConnectivityManager cm = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
            if (cm == null) return LanInfo.none("unknown");
            Network network = cm.getActiveNetwork();
            if (network == null) return LanInfo.none("none");
            NetworkCapabilities caps = cm.getNetworkCapabilities(network);
            String type = "unknown";
            if (caps != null) {
                if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) type = "wifi";
                else if (caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) type = "ethernet";
                else if (caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) type = "cellular";
            }
            if (!"wifi".equals(type) && !"ethernet".equals(type)) {
                return LanInfo.none(type);
            }
            LinkProperties lp = cm.getLinkProperties(network);
            if (lp == null) return LanInfo.none(type);

            String address = "";
            String prefix = "";
            for (LinkAddress la : lp.getLinkAddresses()) {
                InetAddress a = la.getAddress();
                if (!(a instanceof Inet4Address)) continue;
                if (a.isLoopbackAddress() || a.isLinkLocalAddress() || a.isAnyLocalAddress()) continue;
                int bits = la.getPrefixLength();
                if (bits < 0 || bits > 32) continue;
                byte[] b = a.getAddress();
                if (!isPrivateV4(b)) continue;
                address = a.getHostAddress();
                int value = ((b[0] & 0xff) << 24) | ((b[1] & 0xff) << 16)
                        | ((b[2] & 0xff) << 8) | (b[3] & 0xff);
                int mask = bits == 0 ? 0 : (0xffffffff << (32 - bits));
                int networkValue = value & mask;
                String networkAddress = String.format(Locale.US, "%d.%d.%d.%d",
                        (networkValue >>> 24) & 0xff,
                        (networkValue >>> 16) & 0xff,
                        (networkValue >>> 8) & 0xff,
                        networkValue & 0xff);
                prefix = networkAddress + "/" + bits;
                break;
            }

            String gateway = "";
            for (RouteInfo route : lp.getRoutes()) {
                if (!route.isDefaultRoute()) continue;
                InetAddress gw = route.getGateway();
                if (gw instanceof Inet4Address && !gw.isAnyLocalAddress()) {
                    gateway = gw.getHostAddress();
                    break;
                }
            }

            List<String> dns = new ArrayList<>();
            for (InetAddress d : lp.getDnsServers()) {
                String s = d.getHostAddress();
                if (s != null && !s.isEmpty()) dns.add(s);
            }
            return new LanInfo(type, safe(lp.getInterfaceName()), address, prefix, gateway, dns);
        } catch (Exception ignored) {
            return LanInfo.none("unknown");
        }
    }

    public static String phoneName() {
        String maker = safe(Build.MANUFACTURER);
        String model = safe(Build.MODEL);
        if (model.isEmpty()) return maker;
        if (maker.isEmpty() || model.toLowerCase(Locale.US).startsWith(maker.toLowerCase(Locale.US))) {
            return model;
        }
        return maker + " " + model;
    }

    public static String appVersion(Context context) {
        try {
            PackageInfo pi = context.getPackageManager().getPackageInfo(context.getPackageName(), 0);
            return pi.versionName == null ? "" : pi.versionName;
        } catch (Exception ignored) {
            return "";
        }
    }

    private static String safe(String s) {
        return s == null ? "" : s.trim();
    }

    private static boolean isPrivateV4(byte[] b) {
        int a0 = b[0] & 0xff;
        int a1 = b[1] & 0xff;
        if (a0 == 10) return true;
        if (a0 == 172 && a1 >= 16 && a1 <= 31) return true;
        return a0 == 192 && a1 == 168;
    }
}
