package io.jingtao.herdranywhere;

import android.util.Log;
import android.view.View;
import android.view.ViewGroup;
import android.view.inputmethod.InputMethodManager;
import android.webkit.WebView;
import android.Manifest;
import android.app.Activity;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.service.notification.StatusBarNotification;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.ClipData;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageInfo;
import android.content.pm.PackageInstaller;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.media.AudioAttributes;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.provider.OpenableColumns;
import android.provider.Settings;
import com.longbridge.nsh.NshHostReply;
import com.longbridge.nsh.NshView;
import com.longbridge.nsh.Standalone;
import io.github.jtwang.tailpocket.TailPocketBridge;
import io.github.jtwang.tailpocket.nsh.TailPocketNsh;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import kotlinx.coroutines.CoroutineScope;
import kotlinx.coroutines.CoroutineScopeKt;

/**
 * The app's screen: NSH's AppActivity (javap of nsh.aar 0.3.0), as a class of our own that can answer host calls.
 * TailPocket answers the app's tailPocket.* calls (src/tunnel.ts); this class answers the page.* calls of src/page.ts
 * that post the system's notifications, as web-host.js does in a browser. NSH 0.3.0 has no notification API of its
 * own, so there are no actions on them: a tap opens the pane, where the answer is. It also answers page.pickFiles and
 * page.uploadFile (src/link.ts): NSH 0.3.0 has a picker for photos and none for other files.
 */
public class MainActivity extends Activity {
  /** A question or an approval, and a finished agent: each pops up with Herdr's sound for it (assets/sounds, copied to
   *  res/raw by android/build.sh); the person can lower either on its page in Android's settings. */
  private static final String NEEDS_YOU = "agent-needs-you", FINISHED = "agent-finished";
  /** Their ids until 0.1.13, when a finished agent was silent and a question had the phone's sound. Android keeps a
   *  channel as it was first made, so a change needs a new id (Signal's NotificationChannels does the same). */
  private static final String OLD_NEEDS_YOU = "needs-you", OLD_FINISHED = "finished";
  private static final String TERMINAL = "terminal";
  private static final int ASK = 1, PICK = 2;

  private NshView view;
  private final CoroutineScope scope = CoroutineScopeKt.MainScope();
  private TailPocketBridge tailPocket;
  private boolean front;
  /** A tapped notification's terminal, until the app is listening (it asks page.front once it starts). */
  private String opening;
  private NshHostReply asking, picking;
  private int pickLimit = 9;
  private boolean keepAlive;

  @Override
  protected void onCreate(Bundle state) {
    super.onCreate(state);
    Standalone.edgeToEdge(this);
    view = new NshView(this);
    Standalone.limits(view);
    setContentView(view);
    tailPocket = TailPocketNsh.INSTANCE.attach(view, scope);
    channels();
    opening = terminalOf(getIntent());
    view.handle("page.front", (data, reply) -> {
      reply.resolve(front);
      runOnUiThread(this::flushOpening);
    });
    view.handle("page.notify", (data, reply) -> reply.resolve(notify(data)));
    view.handle("page.notifications", (data, reply) -> reply.resolve(permission()));
    view.handle("page.allowNotifications", (data, reply) -> runOnUiThread(() -> allow(reply)));
    view.handle("page.notifyChannels", (data, reply) -> reply.resolve(channelStates()));
    view.handle("page.openChannel", (data, reply) -> runOnUiThread(() -> reply.resolve(openChannel(data))));
    view.handle("page.pickFiles", (data, reply) -> runOnUiThread(() -> pickFiles(data, reply)));
    view.handle("page.uploadFile", this::uploadFile);
    view.handle("page.installUpdate", this::installUpdate);
    listenForInstall();
    view.handle("page.keepAlive", (data, reply) -> runOnUiThread(() -> {
      keepAlive = data instanceof Map && Boolean.TRUE.equals(((Map<?, ?>) data).get("on"));
      applyKeepAlive();
      reply.resolve(keepAlive);
    }));
    view.handle("page.focusTerminal", (data, reply) -> runOnUiThread(() -> {
      WebView web = webViewIn(view);
      reply.resolve(web != null && web.requestFocus());
    }));
    // The page focused its field without a touch on it, and WebView asks for no keyboard then (2026-11-06: the input
    // method's log showed onStartInput for the page and no show request). The page says when its field has the focus.
    view.handle("page.showKeyboard", (data, reply) -> runOnUiThread(() -> showKeyboard(reply, 0)));
    view.loadAssetBundle("nsh");
  }

  /** Asks for the keyboard for the terminal's page. Right after the key bar's button put the keyboard away, the input
   *  method still serves the root view for a moment and refuses (ImeTracker: onFailed at PHASE_CLIENT_VIEW_SERVED,
   *  2026-11-06), so it asks again every 50 ms for up to a second. */
  private void showKeyboard(NshHostReply reply, int tries) {
    WebView web = webViewIn(view);
    InputMethodManager imm = getSystemService(InputMethodManager.class);
    if (web == null || imm == null || !web.hasFocus()) { reply.resolve(false); return; }
    if (imm.showSoftInput(web, 0)) { reply.resolve(true); return; }
    if (tries >= 20) { reply.resolve(false); return; }
    web.postDelayed(() -> showKeyboard(reply, tries + 1), 50);
  }

  /** Android lets an app start a foreground service only while it is on screen; onResume starts it if it couldn't. */
  private void applyKeepAlive() {
    Intent service = new Intent(this, KeepAlive.class);
    if (!keepAlive) { stopService(service); return; }
    if (!front) return;
    try { startForegroundService(service); } catch (RuntimeException e) { Log.w("HerdrAnywhere", "no foreground service: " + e); }
  }

  /** The shown terminal's page. A phone shows one at a time. */
  private static WebView webViewIn(View v) {
    if (v instanceof WebView && v.isShown()) return (WebView) v;
    if (!(v instanceof ViewGroup)) return null;
    ViewGroup g = (ViewGroup) v;
    for (int i = 0; i < g.getChildCount(); i++) { WebView w = webViewIn(g.getChildAt(i)); if (w != null) return w; }
    return null;
  }

  private void channels() {
    NotificationManager nm = getSystemService(NotificationManager.class);
    nm.createNotificationChannel(channel(nm, NEEDS_YOU, OLD_NEEDS_YOU, R.string.channel_needs_you, R.string.channel_needs_you_detail, "request"));
    nm.createNotificationChannel(channel(nm, FINISHED, OLD_FINISHED, R.string.channel_finished, R.string.channel_finished_detail, "done"));
    nm.deleteNotificationChannel(OLD_NEEDS_YOU);
    nm.deleteNotificationChannel(OLD_FINISHED);
    nm.createNotificationChannel(KeepAlive.channel(this));
  }

  /** Sound and pop-up with Herdr's sound. Made the first time over an older version, it keeps what the person chose on
   *  the old channel: its importance (off and silent included) and its sound. Made again later, it changes nothing. */
  private NotificationChannel channel(NotificationManager nm, String id, String old, int name, int detail, String sound) {
    NotificationChannel c = new NotificationChannel(id, getString(name), NotificationManager.IMPORTANCE_HIGH);
    c.setDescription(getString(detail));
    c.setSound(Uri.parse("android.resource://" + getPackageName() + "/raw/" + sound), new AudioAttributes.Builder()
        .setUsage(AudioAttributes.USAGE_NOTIFICATION).setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build());
    NotificationChannel was = nm.getNotificationChannel(old);
    if (was == null || nm.getNotificationChannel(id) != null) return c;
    // Before Android 10 there is no telling whether the person set it; off is theirs either way.
    boolean setImportance = Build.VERSION.SDK_INT >= 29 ? was.hasUserSetImportance() : was.getImportance() == NotificationManager.IMPORTANCE_NONE;
    if (setImportance || was.getImportance() == NotificationManager.IMPORTANCE_NONE) c.setImportance(was.getImportance());
    if (Build.VERSION.SDK_INT >= 29 && was.hasUserSetSound()) c.setSound(was.getSound(), was.getAudioAttributes());
    c.enableVibration(was.shouldVibrate());
    if (was.getVibrationPattern() != null) c.setVibrationPattern(was.getVibrationPattern());
    c.setLockscreenVisibility(was.getLockscreenVisibility());
    c.setShowBadge(was.canShowBadge());
    return c;
  }

  /** What Android does with each kind now (src/page.ts ChannelState), for Settings. */
  private Map<String, String> channelStates() {
    NotificationManager nm = getSystemService(NotificationManager.class);
    boolean on = nm.areNotificationsEnabled();
    Map<String, String> states = new HashMap<>();
    states.put("blocked", on ? state(nm.getNotificationChannel(NEEDS_YOU)) : "off");
    states.put("done", on ? state(nm.getNotificationChannel(FINISHED)) : "off");
    states.put("connection", on ? state(nm.getNotificationChannel(KeepAlive.CHANNEL)) : "off");
    return states;
  }

  private static String state(NotificationChannel c) {
    if (c == null || c.getImportance() == NotificationManager.IMPORTANCE_NONE) return "off";
    if (c.getImportance() < NotificationManager.IMPORTANCE_DEFAULT || c.getSound() == null) return "silent";
    return c.getImportance() >= NotificationManager.IMPORTANCE_HIGH ? "alert" : "sound";
  }

  /** A kind's page in Android's settings, or the app's (`app`). */
  private boolean openChannel(Object data) {
    Object kind = data instanceof Map ? ((Map<?, ?>) data).get("kind") : null;
    String channel = "blocked".equals(kind) ? NEEDS_YOU : "done".equals(kind) ? FINISHED : "connection".equals(kind) ? KeepAlive.CHANNEL : null;
    Intent i = channel == null
        ? new Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, getPackageName())
        : new Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, getPackageName()).putExtra(Settings.EXTRA_CHANNEL_ID, channel);
    try { startActivity(i); return true; } catch (RuntimeException e) { Log.w("HerdrAnywhere", "no settings page: " + e); return false; }
  }

  /** Posts one, only while the app is not in front (its toast shows otherwise). One per terminal: the newest replaces. */
  private boolean notify(Object data) {
    Map<?, ?> a = data instanceof Map ? (Map<?, ?>) data : Collections.emptyMap();
    if (front || !(a.get("title") instanceof String) || !"granted".equals(permission())) return false;
    String terminal = a.get(TERMINAL) instanceof String ? (String) a.get(TERMINAL) : "";
    String body = a.get("body") instanceof String ? (String) a.get("body") : null;
    boolean done = "done".equals(a.get("kind"));
    // New words for the notification of this terminal (src/page.ts notifyFromPage): only onto one still shown, silently.
    boolean quiet = Boolean.TRUE.equals(a.get("quiet"));
    if (quiet && !showing(terminal)) return false;
    Intent tap = new Intent(this, MainActivity.class).putExtra(TERMINAL, terminal).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP);
    PendingIntent open = PendingIntent.getActivity(this, terminal.hashCode(), tap,
        PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    Notification n = new Notification.Builder(this, done ? FINISHED : NEEDS_YOU)
        .setSmallIcon(R.drawable.ic_notify)
        .setContentTitle((String) a.get("title"))
        .setContentText(body)
        .setStyle(new Notification.BigTextStyle().bigText(body))
        .setCategory(done ? Notification.CATEGORY_STATUS : Notification.CATEGORY_MESSAGE)
        .setContentIntent(open)
        .setAutoCancel(true)
        .setOnlyAlertOnce(quiet)
        .build();
    getSystemService(NotificationManager.class).notify(terminal, 1, n);
    return true;
  }

  private boolean showing(String terminal) {
    for (StatusBarNotification s : getSystemService(NotificationManager.class).getActiveNotifications())
      if (terminal.equals(s.getTag()) && s.getId() == 1) return true;
    return false;
  }

  /** The browser's words (src/page.ts Permission): Android 13+ asks once; before it, notifications are on unless turned off. */
  private String permission() {
    if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
      boolean asked = getPreferences(MODE_PRIVATE).getBoolean("asked", false);
      return !asked || shouldShowRequestPermissionRationale(Manifest.permission.POST_NOTIFICATIONS) ? "default" : "denied";
    }
    return getSystemService(NotificationManager.class).areNotificationsEnabled() ? "granted" : "denied";
  }

  private void allow(NshHostReply reply) {
    if (Build.VERSION.SDK_INT < 33 || !"default".equals(permission())) {
      reply.resolve(permission());
      return;
    }
    if (asking != null) asking.resolve(permission());
    asking = reply;
    getPreferences(MODE_PRIVATE).edit().putBoolean("asked", true).apply();
    requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, ASK);
  }

  @Override
  public void onRequestPermissionsResult(int code, String[] permissions, int[] results) {
    super.onRequestPermissionsResult(code, permissions, results);
    if (code == ASK && asking != null) {
      asking.resolve(permission());
      asking = null;
    }
  }

  private void pickFiles(Object data, NshHostReply reply) {
    if (picking != null) picking.resolve(Collections.emptyList());
    picking = reply;
    Map<?, ?> a = data instanceof Map ? (Map<?, ?>) data : Collections.emptyMap();
    if (a.get("limit") instanceof Number) pickLimit = ((Number) a.get("limit")).intValue();
    startActivityForResult(new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*")
        .putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true), PICK);
  }

  @Override
  protected void onActivityResult(int code, int result, Intent data) {
    super.onActivityResult(code, result, data);
    if (code != PICK || picking == null) return;
    NshHostReply reply = picking;
    picking = null;
    List<Uri> uris = new ArrayList<>();
    if (result == RESULT_OK && data != null) {
      ClipData many = data.getClipData();
      if (many != null) for (int i = 0; i < many.getItemCount(); i++) uris.add(many.getItemAt(i).getUri());
      else if (data.getData() != null) uris.add(data.getData());
    }
    List<Map<String, Object>> files = new ArrayList<>();
    for (Uri uri : uris.subList(0, Math.min(pickLimit, uris.size()))) files.add(describe(uri));
    reply.resolve(files);
  }

  /** The id page.uploadFile sends it by (its URI), its name and its size; -1 when the provider doesn't say. */
  private Map<String, Object> describe(Uri uri) {
    Map<String, Object> file = new HashMap<>();
    file.put("id", uri.toString());
    file.put("name", "file");
    file.put("size", -1L);
    try (Cursor c = getContentResolver().query(uri, new String[] {OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE}, null, null, null)) {
      if (c != null && c.moveToFirst()) {
        if (!c.isNull(0)) file.put("name", c.getString(0));
        if (!c.isNull(1)) file.put("size", c.getLong(1));
      }
    }
    return file;
  }

  /** Streams the picked file to the companion, read as it goes: it may be far bigger than the app's memory. */
  private void uploadFile(Object data, NshHostReply reply) {
    Map<?, ?> a = data instanceof Map ? (Map<?, ?>) data : Collections.emptyMap();
    new Thread(() -> {
      HttpURLConnection http = null;
      try (InputStream in = getContentResolver().openInputStream(Uri.parse((String) a.get("id")))) {
        http = (HttpURLConnection) new URL((String) a.get("url")).openConnection();
        http.setRequestMethod("POST");
        http.setDoOutput(true);
        http.setChunkedStreamingMode(64 * 1024);
        http.setRequestProperty("Authorization", "Bearer " + a.get("token"));
        http.setRequestProperty("Content-Type", "application/octet-stream");
        try (OutputStream out = http.getOutputStream()) {
          byte[] chunk = new byte[64 * 1024];
          for (int n; (n = in.read(chunk)) >= 0; ) out.write(chunk, 0, n);
        }
        int status = http.getResponseCode();
        Map<String, Object> answer = new HashMap<>();
        answer.put("status", status);
        InputStream body = status < 400 ? http.getInputStream() : http.getErrorStream();
        ByteArrayOutputStream text = new ByteArrayOutputStream();
        if (body != null) {
          byte[] chunk = new byte[4096];
          for (int n; (n = body.read(chunk)) >= 0; ) text.write(chunk, 0, n);
        }
        answer.put("body", text.toString("UTF-8"));
        reply.resolve(answer);
      } catch (Exception e) {
        reply.reject(String.valueOf(e.getMessage()));
      } finally {
        if (http != null) http.disconnect();
      }
    }).start();
  }

  private static String terminalOf(Intent intent) {
    String t = intent == null ? null : intent.getStringExtra(TERMINAL);
    return t == null || t.isEmpty() ? null : t;
  }

  private void flushOpening() {
    if (opening == null) return;
    view.send("page.open", Collections.singletonMap(TERMINAL, opening));
    opening = null;
  }

  @Override
  protected void onNewIntent(Intent intent) {
    super.onNewIntent(intent);
    String t = terminalOf(intent);
    if (t != null) view.send("page.open", Collections.singletonMap(TERMINAL, t));
  }

  @Override
  protected void onResume() {
    super.onResume();
    setFront(true);
    if (keepAlive) applyKeepAlive();
  }

  @Override
  protected void onPause() {
    setFront(false);
    super.onPause();
  }

  private void setFront(boolean now) {
    if (front == now) return;
    front = now;
    view.send("page.front", Collections.singletonMap("front", now));
  }

  @Override
  public void onConfigurationChanged(Configuration config) {
    super.onConfigurationChanged(config);
    Standalone.systemBars(this);
  }

  @Override
  @SuppressWarnings("deprecation")
  public void onBackPressed() {
    if (!view.handleBack()) super.onBackPressed();
  }

  @Override
  protected void onDestroy() {
    stopService(new Intent(this, KeepAlive.class));
    tailPocket.close();
    CoroutineScopeKt.cancel(scope, null);
    view.destroy();
    super.onDestroy();
  }

  // --- A new version of this app ---------------------------------------------------------------------------------

  private static final String INSTALL_STATUS = "io.jingtao.herdranywhere.INSTALL_STATUS";
  private NshHostReply installing;

  /** Downloads the release's APK and hands it to the system, which asks the person to confirm. Android refuses a file
   *  not signed with this app's key, so the address the computer gave can't put anything else here. */
  private void installUpdate(Object data, NshHostReply reply) {
    if (!(data instanceof Map)) { reply.reject("bad request"); return; }
    Map<?, ?> a = (Map<?, ?>) data;
    String url = String.valueOf(a.get("url")), digest = String.valueOf(a.get("digest"));
    if (!getPackageManager().canRequestPackageInstalls()) {
      // Asked again on the way back from that screen, it doesn't open a second time.
      if (!Boolean.FALSE.equals(a.get("ask")))
        runOnUiThread(() -> startActivity(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + getPackageName()))));
      reply.resolve("needsPermission");
      return;
    }
    new Thread(() -> {
      File apk = new File(getCacheDir(), "update.apk");
      try {
        download(url, digest, apk);
        PackageInfo info = getPackageManager().getPackageArchiveInfo(apk.getPath(), 0);
        if (info == null || !getPackageName().equals(info.packageName)) throw new IllegalStateException("not this app");
        PackageInstaller installer = getPackageManager().getPackageInstaller();
        PackageInstaller.SessionParams params = new PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL);
        params.setAppPackageName(getPackageName());
        int id = installer.createSession(params);
        try (PackageInstaller.Session session = installer.openSession(id)) {
          try (InputStream in = new FileInputStream(apk); OutputStream out = session.openWrite("app.apk", 0, apk.length())) {
            byte[] chunk = new byte[256 * 1024];
            for (int n; (n = in.read(chunk)) >= 0; ) out.write(chunk, 0, n);
            session.fsync(out);
          }
          installing = reply;
          Updated.prefs(this).edit().putBoolean("fromApp", true).apply();
          Intent status = new Intent(INSTALL_STATUS).setPackage(getPackageName());
          session.commit(PendingIntent.getBroadcast(this, id, status, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_MUTABLE).getIntentSender());
        }
      } catch (Exception e) {
        Log.w("Herdr", "update: " + e);
        installing = null;
        reply.reject(String.valueOf(e.getMessage()));
      } finally {
        apk.delete();
      }
    }).start();
  }

  private void download(String url, String digest, File to) throws Exception {
    HttpURLConnection http = (HttpURLConnection) new URL(url).openConnection();
    try {
      if (http.getResponseCode() != 200) throw new IllegalStateException("download: " + http.getResponseCode());
      long total = http.getContentLengthLong(), done = 0, told = 0;
      MessageDigest sha = MessageDigest.getInstance("SHA-256");
      try (InputStream in = http.getInputStream(); OutputStream out = new FileOutputStream(to)) {
        byte[] chunk = new byte[256 * 1024];
        for (int n; (n = in.read(chunk)) >= 0; ) {
          out.write(chunk, 0, n);
          sha.update(chunk, 0, n);
          done += n;
          if (total > 0 && done - told > total / 50) {
            told = done;
            view.send("page.updateProgress", Collections.singletonMap("done", (double) done / total));
          }
        }
      }
      StringBuilder hex = new StringBuilder("sha256:");
      for (byte b : sha.digest()) hex.append(String.format("%02x", b));
      if (!hex.toString().equals(digest)) throw new IllegalStateException("the download doesn't match the release's digest");
    } finally {
      http.disconnect();
    }
  }

  /** The system answers the commit here: first that the person must confirm, later how it ended. */
  private void listenForInstall() {
    BroadcastReceiver receiver = new BroadcastReceiver() {
      @Override
      public void onReceive(Context context, Intent intent) {
        int status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE);
        NshHostReply reply = installing;
        installing = null;
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
          Intent confirm = intent.getParcelableExtra(Intent.EXTRA_INTENT);
          if (confirm != null) startActivity(confirm);
          if (reply != null) reply.resolve("installing");
          return;
        }
        String message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE);
        if (status != PackageInstaller.STATUS_SUCCESS) Updated.prefs(context).edit().remove("fromApp").apply();
        Log.i("Herdr", "update: status " + status + " " + message);
        if (reply != null && status != PackageInstaller.STATUS_SUCCESS) reply.reject(String.valueOf(message));
        // The person said no: nothing to report. Anything else, such as a file signed with another key, is worth saying.
        else if (status != PackageInstaller.STATUS_SUCCESS && status != PackageInstaller.STATUS_FAILURE_ABORTED)
          view.send("page.updateFailed", Collections.singletonMap("message", String.valueOf(message)));
      }
    };
    IntentFilter filter = new IntentFilter(INSTALL_STATUS);
    if (Build.VERSION.SDK_INT >= 33) registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED);
    else registerReceiver(receiver, filter);
  }
}
