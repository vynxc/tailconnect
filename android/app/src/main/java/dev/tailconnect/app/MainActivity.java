package dev.tailconnect.app;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.res.ColorStateList;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.TypedValue;
import android.view.View;
import android.view.WindowManager;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;

import com.google.android.material.button.MaterialButton;
import com.google.android.material.card.MaterialCardView;
import com.google.android.material.checkbox.MaterialCheckBox;

import java.util.List;

/** Dark Material 3 control surface. It runs no VPN and asks for no VPN permission. */
public class MainActivity extends AppCompatActivity implements TailconnectState.Listener {
    private TextView statusView;
    private MaterialCardView statusCard;
    private TextView codeView;
    private TextView linkView;
    private TextView policyView;
    private TextView agentView;
    private TextView errorView;
    private LinearLayout servicesBox;
    private MaterialButton startButton;
    private MaterialButton stopButton;
    private MaterialCheckBox loopback;
    private MaterialCheckBox persistent;
    private TextView codeLabel;
    private String serviceSignature = "";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE);
        getWindow().setStatusBarColor(color(com.google.android.material.R.attr.colorSurface));
        getWindow().setNavigationBarColor(color(com.google.android.material.R.attr.colorSurface));
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
        root.setPadding(pad, pad, pad, dp(32));
        root.setBackgroundColor(color(com.google.android.material.R.attr.colorSurface));

        TextView title = text("Tailconnect", 34, Typeface.DEFAULT_BOLD,
                color(com.google.android.material.R.attr.colorOnSurface));
        root.addView(title);

        TextView subtitle = text("Serverless Tailcat tunnel\nNo VPN · no account · no middleman",
                14, Typeface.DEFAULT, color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        subtitle.setLineSpacing(0, 1.15f);
        subtitle.setPadding(0, dp(6), 0, dp(18));
        root.addView(subtitle);

        statusCard = new MaterialCardView(this);
        styleCard(statusCard);
        statusCard.setCardBackgroundColor(color(com.google.android.material.R.attr.colorSurfaceVariant));
        statusView = text("Stopped", 15, Typeface.DEFAULT_BOLD,
                color(com.google.android.material.R.attr.colorOnSurface));
        statusView.setPadding(dp(18), dp(14), dp(18), dp(14));
        statusCard.addView(statusView);
        root.addView(statusCard, matchWrap());

        codeLabel = label("PAIR CODE", dp(20));
        root.addView(codeLabel);

        MaterialCardView codeCard = card();
        LinearLayout codeBody = cardBody(codeCard, dp(18));
        codeView = text("—", 32, Typeface.MONOSPACE,
                color(com.google.android.material.R.attr.colorOnSurface));
        codeView.setTextIsSelectable(true);
        codeView.setLetterSpacing(0.08f);
        codeBody.addView(codeView);
        linkView = text("", 12, Typeface.MONOSPACE,
                color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        linkView.setTextIsSelectable(true);
        linkView.setPadding(0, dp(6), 0, dp(10));
        codeBody.addView(linkView);

        LinearLayout codeActions = new LinearLayout(this);
        codeActions.setOrientation(LinearLayout.HORIZONTAL);
        MaterialButton copy = button("Copy code", false);
        copy.setOnClickListener(v -> copyCode());
        codeActions.addView(copy, weight());
        MaterialButton share = button("Share link", false);
        share.setOnClickListener(v -> shareCode());
        codeActions.addView(share, weight());
        MaterialButton rotate = button("Rotate", false);
        rotate.setOnClickListener(v -> rotateCode());
        codeActions.addView(rotate, weight());
        codeBody.addView(codeActions);
        root.addView(codeCard, matchWrap());

        LinearLayout controls = new LinearLayout(this);
        controls.setOrientation(LinearLayout.HORIZONTAL);
        controls.setPadding(0, dp(8), 0, dp(4));
        startButton = button("Start", true);
        startButton.setOnClickListener(v -> startTunnel());
        controls.addView(startButton, weight());
        stopButton = button("Stop", false);
        stopButton.setOnClickListener(v -> stopTunnel());
        controls.addView(stopButton, weight());
        root.addView(controls);

        loopback = new MaterialCheckBox(this);
        loopback.setText("Allow this phone's localhost (ADB / local servers)");
        loopback.setTextSize(14);
        loopback.setTextColor(color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        loopback.setPadding(0, dp(6), 0, dp(6));
        root.addView(loopback);

        persistent = new MaterialCheckBox(this);
        persistent.setText("Keep this code across restarts (long-lived credential)");
        persistent.setTextSize(14);
        persistent.setTextColor(color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        persistent.setChecked(CodeStore.isEnabled(this));
        persistent.setPadding(0, dp(2), 0, dp(6));
        persistent.setOnCheckedChangeListener((b, checked) -> {
            CodeStore.setEnabled(this, checked);
            updateCodeLabel();
        });
        root.addView(persistent);

        root.addView(label("PHONE NETWORK", dp(20)));
        MaterialCardView networkCard = card();
        LinearLayout networkBody = cardBody(networkCard, dp(18));
        policyView = text("No LAN shared", 14, Typeface.DEFAULT,
                color(com.google.android.material.R.attr.colorOnSurface));
        networkBody.addView(policyView);
        agentView = text("Agent: not connected", 14, Typeface.DEFAULT,
                color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        agentView.setPadding(0, dp(6), 0, 0);
        networkBody.addView(agentView);
        root.addView(networkCard, matchWrap());

        errorView = text("", 14, Typeface.DEFAULT_BOLD,
                color(com.google.android.material.R.attr.colorError));
        errorView.setPadding(0, dp(10), 0, 0);
        root.addView(errorView);

        root.addView(label("AGENT SERVICES", dp(20)));
        servicesBox = new LinearLayout(this);
        servicesBox.setOrientation(LinearLayout.VERTICAL);
        root.addView(servicesBox);

        TextView footer = text("Only the traffic you explicitly send through Tailconnect uses the tunnel. "
                        + "Android's VPN slot is untouched.",
                12, Typeface.DEFAULT, color(com.google.android.material.R.attr.colorOnSurfaceVariant));
        footer.setPadding(0, dp(28), 0, 0);
        root.addView(footer);
        updateCodeLabel();

        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(color(com.google.android.material.R.attr.colorSurface));
        scroll.addView(root);
        return scroll;
    }

    private TextView label(String text, int topPadding) {
        TextView v = text(text, 11, Typeface.DEFAULT_BOLD,
                color(com.google.android.material.R.attr.colorPrimary));
        v.setLetterSpacing(0.14f);
        v.setPadding(0, topPadding, 0, dp(8));
        return v;
    }

    private TextView text(String value, int sp, Typeface face, int color) {
        TextView v = new TextView(this);
        v.setText(value);
        v.setTextSize(sp);
        v.setTextColor(color);
        v.setTypeface(face);
        return v;
    }

    private MaterialCardView card() {
        MaterialCardView c = new MaterialCardView(this);
        styleCard(c);
        return c;
    }

    private void styleCard(MaterialCardView c) {
        c.setCardBackgroundColor(color(com.google.android.material.R.attr.colorSurfaceVariant));
        c.setStrokeColor(color(com.google.android.material.R.attr.colorOutline));
        c.setStrokeWidth(dp(1));
        c.setRadius(dp(22));
        c.setCardElevation(0);
    }

    private LinearLayout cardBody(MaterialCardView card, int pad) {
        LinearLayout body = new LinearLayout(this);
        body.setOrientation(LinearLayout.VERTICAL);
        body.setPadding(pad, pad, pad, pad);
        card.addView(body, matchWrap());
        return body;
    }

    private MaterialButton button(String text, boolean filled) {
        MaterialButton b = new MaterialButton(this);
        b.setText(text);
        b.setAllCaps(false);
        b.setCornerRadius(dp(18));
        b.setInsetTop(0);
        b.setInsetBottom(0);
        b.setTextSize(14);
        if (filled) {
            b.setBackgroundTintList(ColorStateList.valueOf(color(com.google.android.material.R.attr.colorPrimary)));
            b.setTextColor(color(com.google.android.material.R.attr.colorOnPrimary));
            b.setStrokeWidth(0);
        } else {
            b.setBackgroundTintList(ColorStateList.valueOf(Color.TRANSPARENT));
            b.setTextColor(color(com.google.android.material.R.attr.colorPrimary));
            b.setStrokeColor(ColorStateList.valueOf(color(com.google.android.material.R.attr.colorOutline)));
            b.setStrokeWidth(dp(1));
        }
        return b;
    }

    private LinearLayout.LayoutParams matchWrap() {
        return new LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT);
    }

    private LinearLayout.LayoutParams weight() {
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
        lp.setMargins(dp(3), 0, dp(3), 0);
        return lp;
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private int color(int attr) {
        TypedValue tv = new TypedValue();
        if (getTheme().resolveAttribute(attr, tv, true)) {
            if (tv.resourceId != 0) return getResources().getColor(tv.resourceId, getTheme());
            return tv.data;
        }
        return Color.WHITE;
    }

    private void updateCodeLabel() {
        if (codeLabel == null || persistent == null) return;
        codeLabel.setText(persistent.isChecked() ? "PAIR CODE · PERSISTENT" : "PAIR CODE · SESSION");
    }

    private void rotateCode() {
        boolean wasRunning = TailconnectState.get().running;
        CodeStore.clear(this);
        TailconnectState.set(TailconnectState.Snapshot.empty());
        stopService(new Intent(this, TailconnectService.class));
        if (wasRunning) {
            new Handler(Looper.getMainLooper()).postDelayed(this::startTunnel, 800);
        } else {
            Toast.makeText(this, "Next start will use a new code", Toast.LENGTH_SHORT).show();
        }
    }

    private void startTunnel() {
        CodeStore.setEnabled(this, persistent.isChecked());
        Intent i = new Intent(this, TailconnectService.class);
        i.putExtra(TailconnectService.EXTRA_LOOPBACK, loopback.isChecked());
        i.putExtra(TailconnectService.EXTRA_PERSISTENT, persistent.isChecked());
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
        statusView.setText(s.running ? (s.agentConnected ? "Connected" : "Waiting for agent") : "Stopped");
        statusView.setTextColor(color(s.running
                ? com.google.android.material.R.attr.colorOnPrimaryContainer
                : com.google.android.material.R.attr.colorOnSurface));
        statusCard.setCardBackgroundColor(color(s.running
                ? com.google.android.material.R.attr.colorPrimaryContainer
                : com.google.android.material.R.attr.colorSurfaceVariant));
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
        persistent.setEnabled(!s.running);
        updateCodeLabel();

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
            TextView none = text("No services offered yet.", 14, Typeface.DEFAULT,
                    color(com.google.android.material.R.attr.colorOnSurfaceVariant));
            servicesBox.addView(none);
            return;
        }
        for (TailconnectState.Service svc : services) {
            MaterialCardView card = card();
            LinearLayout body = cardBody(card, dp(16));
            TextView label = text(svc.label.isEmpty() ? svc.id : svc.label, 16, Typeface.DEFAULT_BOLD,
                    color(com.google.android.material.R.attr.colorOnSurface));
            body.addView(label);
            TextView url = text(svc.url, 12, Typeface.MONOSPACE,
                    color(com.google.android.material.R.attr.colorOnSurfaceVariant));
            url.setTextIsSelectable(true);
            url.setPadding(0, dp(4), 0, dp(10));
            body.addView(url);
            MaterialButton open = button("Open in browser", false);
            open.setOnClickListener(v -> {
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(svc.url)));
                } catch (Exception e) {
                    Toast.makeText(this, "No browser for " + svc.url, Toast.LENGTH_SHORT).show();
                }
            });
            body.addView(open, matchWrap());
            LinearLayout.LayoutParams lp = matchWrap();
            lp.setMargins(0, dp(4), 0, dp(8));
            servicesBox.addView(card, lp);
        }
    }
}
