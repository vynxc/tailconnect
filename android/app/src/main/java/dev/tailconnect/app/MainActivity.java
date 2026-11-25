package dev.tailconnect.app;

import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import java.util.List;

/** Native control surface. It runs no VPN and asks for no VPN permission. */
public class MainActivity extends Activity implements TailconnectState.Listener {
    private TextView statusView;
    private TextView codeView;
    private TextView linkView;
    private TextView policyView;
    private TextView agentView;
    private TextView errorView;
    private LinearLayout servicesBox;
    private Button startButton;
    private Button stopButton;
    private CheckBox loopback;
    private String serviceSignature = "";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE);
        setContentView(buildUi());
        TailconnectState.addListener(this);
        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{android.Manifest.permission.POST_NOTIFICATIONS}, 7);
        }
    }

    @Override
    protected void onDestroy() {
        TailconnectState.removeListener(this);
        super.onDestroy();
    }

    private View buildUi() {
        int pad = dp(20);
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(pad, pad, pad, pad);
        root.setBackgroundColor(Color.WHITE);

        TextView title = new TextView(this);
        title.setText("Tailconnect");
        title.setTextSize(30);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        title.setTextColor(Color.BLACK);
        root.addView(title);

        TextView subtitle = new TextView(this);
        subtitle.setText("Serverless Tailcat tunnel. No VPN, no account, no middleman.");
        subtitle.setTextSize(14);
        subtitle.setTextColor(Color.DKGRAY);
        subtitle.setPadding(0, dp(4), 0, dp(12));
        root.addView(subtitle);

        statusView = text(16, Color.BLACK, Typeface.DEFAULT_BOLD);
        root.addView(statusView);

        codeView = text(30, Color.BLACK, Typeface.MONOSPACE);
        codeView.setTextIsSelectable(true);
        codeView.setPadding(0, dp(16), 0, dp(4));
        root.addView(codeView);

        linkView = text(12, Color.GRAY, Typeface.MONOSPACE);
        linkView.setTextIsSelectable(true);
        linkView.setPadding(0, 0, 0, dp(10));
        root.addView(linkView);

        policyView = text(14, Color.BLACK, Typeface.DEFAULT);
        root.addView(policyView);

        agentView = text(14, Color.BLACK, Typeface.DEFAULT);
        root.addView(agentView);

        loopback = new CheckBox(this);
        loopback.setText("Allow this phone's localhost (ADB / local servers)");
        loopback.setTextSize(14);
        loopback.setTextColor(Color.BLACK);
        loopback.setPadding(0, dp(6), 0, dp(6));
        root.addView(loopback);

        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setPadding(0, dp(8), 0, dp(8));
        startButton = button("Start");
        startButton.setOnClickListener(v -> startTunnel());
        row.addView(startButton, weight());
        stopButton = button("Stop");
        stopButton.setOnClickListener(v -> stopTunnel());
        row.addView(stopButton, weight());
        Button copy = button("Copy code");
        copy.setOnClickListener(v -> copyCode());
        row.addView(copy, weight());
        Button share = button("Share");
        share.setOnClickListener(v -> shareCode());
        row.addView(share, weight());
        root.addView(row);

        errorView = text(14, Color.rgb(160, 0, 0), Typeface.DEFAULT_BOLD);
        errorView.setPadding(0, dp(4), 0, dp(8));
        root.addView(errorView);

        TextView servicesTitle = text(18, Color.BLACK, Typeface.DEFAULT_BOLD);
        servicesTitle.setText("Agent services");
        servicesTitle.setPadding(0, dp(12), 0, dp(6));
        root.addView(servicesTitle);

        servicesBox = new LinearLayout(this);
        servicesBox.setOrientation(LinearLayout.VERTICAL);
        root.addView(servicesBox);

        TextView footer = text(12, Color.GRAY, Typeface.DEFAULT);
        footer.setText("Only the traffic you explicitly send through Tailconnect uses the tunnel. "
                + "Android's VPN slot is untouched.");
        footer.setPadding(0, dp(20), 0, dp(8));
        root.addView(footer);

        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(Color.WHITE);
        scroll.addView(root);
        return scroll;
    }

    private TextView text(int sp, int color, Typeface face) {
        TextView v = new TextView(this);
        v.setTextSize(sp);
        v.setTextColor(color);
        v.setTypeface(face);
        return v;
    }

    private Button button(String label) {
        Button b = new Button(this);
        b.setText(label);
        b.setAllCaps(false);
        return b;
    }

    private LinearLayout.LayoutParams weight() {
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
        lp.setMargins(dp(3), 0, dp(3), 0);
        return lp;
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void startTunnel() {
        Intent i = new Intent(this, TailconnectService.class);
        i.putExtra(TailconnectService.EXTRA_LOOPBACK, loopback.isChecked());
        if (Build.VERSION.SDK_INT >= 26) startForegroundService(i);
        else startService(i);
        Toast.makeText(this, "Starting…", Toast.LENGTH_SHORT).show();
    }

    private void stopTunnel() {
        stopService(new Intent(this, TailconnectService.class));
    }

    private void copyCode() {
        TailconnectState.Snapshot s = TailconnectState.get();
        if (s.code.isEmpty()) return;
        ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm != null) {
            cm.setPrimaryClip(ClipData.newPlainText("Tailconnect pair code", s.code));
            Toast.makeText(this, "Code copied", Toast.LENGTH_SHORT).show();
        }
    }

    private void shareCode() {
        TailconnectState.Snapshot s = TailconnectState.get();
        if (s.code.isEmpty()) return;
        String text = s.link.isEmpty() ? s.code : s.link;
        Intent i = new Intent(Intent.ACTION_SEND);
        i.setType("text/plain");
        i.putExtra(Intent.EXTRA_TEXT, text);
        startActivity(Intent.createChooser(i, "Share pairing code"));
    }

    @Override
    public void onState(TailconnectState.Snapshot s) {
        runOnUiThread(() -> render(s));
    }

    private void render(TailconnectState.Snapshot s) {
        statusView.setText(s.running
                ? (s.agentConnected ? "Connected" : "Waiting for agent")
                : "Stopped");
        statusView.setTextColor(s.running ? Color.rgb(0, 120, 60) : Color.DKGRAY);
        codeView.setText(s.code.isEmpty() ? "—" : s.code);
        linkView.setText(s.link);
        policyView.setText(s.policy.isEmpty() ? "Shared network: none" : "Shared network: " + s.policy);
        agentView.setText(s.agentConnected
                ? "Agent: " + (s.agentName.isEmpty() ? "connected" : s.agentName)
                : "Agent: not connected");
        errorView.setText(s.error);
        errorView.setVisibility(s.error.isEmpty() ? View.GONE : View.VISIBLE);
        startButton.setEnabled(!s.running);
        stopButton.setEnabled(s.running);
        loopback.setEnabled(!s.running);

        StringBuilder sig = new StringBuilder();
        for (TailconnectState.Service svc : s.services) {
            sig.append(svc.id).append('|').append(svc.port).append('|').append(svc.url).append(';');
        }
        if (!sig.toString().equals(serviceSignature)) {
            serviceSignature = sig.toString();
            renderServices(s.services);
        }
    }

    private void renderServices(List<TailconnectState.Service> services) {
        servicesBox.removeAllViews();
        if (services.isEmpty()) {
            TextView none = text(14, Color.GRAY, Typeface.DEFAULT);
            none.setText("No services offered yet.");
            servicesBox.addView(none);
            return;
        }
        for (TailconnectState.Service svc : services) {
            TextView label = text(15, Color.BLACK, Typeface.DEFAULT_BOLD);
            label.setText(svc.label.isEmpty() ? svc.id : svc.label);
            servicesBox.addView(label);
            Button open = button("Open " + svc.url);
            open.setAllCaps(false);
            open.setOnClickListener(v -> {
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(svc.url)));
                } catch (Exception e) {
                    Toast.makeText(this, "No browser for " + svc.url, Toast.LENGTH_SHORT).show();
                }
            });
            servicesBox.addView(open);
        }
    }
}
