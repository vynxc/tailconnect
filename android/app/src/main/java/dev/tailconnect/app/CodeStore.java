package dev.tailconnect.app;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;
import android.util.Log;

import java.nio.charset.StandardCharsets;
import java.security.KeyStore;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** Stores the optional long-lived pairing code encrypted with Android Keystore. */
public final class CodeStore {
    private static final String TAG = "TailconnectCodeStore";
    private static final String PREFS = "tailconnect";
    private static final String KEY_CODE = "pair_code_v1";
    private static final String KEY_ENABLED = "persistent_enabled";
    private static final String KEY_ALIAS = "tailconnect.pair.code.v1";
    private static final String KEYSTORE = "AndroidKeyStore";
    private static final int GCM_TAG_BITS = 128;
    private static final int IV_BYTES = 12;

    private CodeStore() {}

    private static SharedPreferences prefs(Context c) {
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    public static boolean isEnabled(Context c) {
        return prefs(c).getBoolean(KEY_ENABLED, true);
    }

    public static void setEnabled(Context c, boolean enabled) {
        prefs(c).edit().putBoolean(KEY_ENABLED, enabled).apply();
        if (!enabled) clear(c);
    }

    public static boolean has(Context c) {
        return load(c) != null;
    }

    public static String load(Context c) {
        String blob = prefs(c).getString(KEY_CODE, null);
        if (blob == null || blob.isEmpty()) return null;
        try {
            byte[] raw = Base64.decode(blob, Base64.NO_WRAP);
            if (raw.length <= IV_BYTES) return null;
            byte[] iv = new byte[IV_BYTES];
            System.arraycopy(raw, 0, iv, 0, IV_BYTES);
            byte[] ct = new byte[raw.length - IV_BYTES];
            System.arraycopy(raw, IV_BYTES, ct, 0, ct.length);
            SecretKey key = getKey(false);
            if (key == null) return null;
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(GCM_TAG_BITS, iv));
            return new String(cipher.doFinal(ct), StandardCharsets.UTF_8);
        } catch (Exception e) {
            Log.w(TAG, "could not read persisted code");
            return null;
        }
    }

    public static void save(Context c, String code) {
        if (code == null || code.isEmpty()) return;
        try {
            SecretKey key = getKey(true);
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.ENCRYPT_MODE, key);
            byte[] iv = cipher.getIV();
            if (iv == null || iv.length != IV_BYTES) {
                // Keystore should provide a 12-byte GCM IV; regenerate with an
                // explicit one if a vendor returns something else.
                iv = new byte[IV_BYTES];
                new java.security.SecureRandom().nextBytes(iv);
                cipher.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(GCM_TAG_BITS, iv));
            }
            byte[] ct = cipher.doFinal(code.getBytes(StandardCharsets.UTF_8));
            byte[] out = new byte[iv.length + ct.length];
            System.arraycopy(iv, 0, out, 0, iv.length);
            System.arraycopy(ct, 0, out, iv.length, ct.length);
            prefs(c).edit().putString(KEY_CODE, Base64.encodeToString(out, Base64.NO_WRAP)).apply();
        } catch (Exception e) {
            Log.w(TAG, "could not persist code");
        }
    }

    public static void clear(Context c) {
        prefs(c).edit().remove(KEY_CODE).apply();
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            if (ks.containsAlias(KEY_ALIAS)) ks.deleteEntry(KEY_ALIAS);
        } catch (Exception ignored) {
        }
    }

    private static SecretKey getKey(boolean create) throws Exception {
        KeyStore ks = KeyStore.getInstance(KEYSTORE);
        ks.load(null);
        KeyStore.Entry entry = ks.getEntry(KEY_ALIAS, null);
        if (entry instanceof KeyStore.SecretKeyEntry) {
            return ((KeyStore.SecretKeyEntry) entry).getSecretKey();
        }
        if (!create) return null;
        KeyGenerator kg = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE);
        kg.init(new KeyGenParameterSpec.Builder(KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build());
        return kg.generateKey();
    }
}
