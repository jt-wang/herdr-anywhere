package io.jingtao.herdranywhere;

import android.app.Activity;
import android.app.ActivityManager;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.Intent;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.Uri;
import android.os.BatteryManager;
import android.os.Build;
import android.os.Debug;
import android.os.Handler;
import android.os.Looper;
import android.os.Process;
import android.os.SystemClock;
import android.view.MotionEvent;
import com.longbridge.nsh.NshHostReply;
import com.longbridge.nsh.NshView;
import java.io.ByteArrayInputStream;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

/** Native collectors are active only for the categories the person chose, while the app is in front. */
final class Diagnostics {
  static final String EXPORT_NAME = "Herdr Anywhere Diagnostics.json";
  final DiagnosticLog log;
  private final Activity activity;
  private final Handler handler = new Handler(Looper.getMainLooper());
  private boolean front, exporting;
  private long lastTick, lastResource;
  private AlertDialog dialog;

  Diagnostics(Activity activity) {
    this.activity = activity;
    log = new DiagnosticLog(activity);
    log.record("process.start", Collections.emptyMap());
  }

  void attach(NshView view) {
    view.handle("page.diagnostics.status", (data, reply) -> reply.resolve(log.status()));
    view.handle("page.diagnostics.set", (data, reply) -> activity.runOnUiThread(() -> {
      if (!(data instanceof Map)) { reply.reject("Invalid diagnostic switch"); return; }
      log.set((Map<?, ?>) data);
      configure();
      if (log.collects("network")) network();
      reply.resolve(log.status());
    }));
    view.handle("page.diagnostics.event", (data, reply) -> {
      if (data instanceof Map) {
        Map<?, ?> request = (Map<?, ?>) data;
        if (request.get("event") instanceof String) {
          String event = (String) request.get("event");
          Map<String, String> fields = DiagnosticLog.fields(event, request.get("fields"));
          if (fields != null) log.record("js." + event, fields);
        }
      }
      reply.resolve(null);
    });
    view.handle("page.diagnostics.clear", (data, reply) -> activity.runOnUiThread(() -> {
      try { log.clear(); Opened.removeNamed(activity, EXPORT_NAME); reply.resolve(null); }
      catch (Exception e) { reply.reject("Could not clear diagnostic files"); }
    }));
    view.handle("page.diagnostics.export", (data, reply) -> activity.runOnUiThread(() -> export(reply)));
  }

  void front(boolean active) {
    front = active;
    log.record(active ? "lifecycle.active" : "lifecycle.background", Collections.emptyMap());
    configure();
    if (active) network();
  }

  void touch(MotionEvent event) {
    if (!log.collects("interaction")) return;
    int action = event.getActionMasked();
    if (action == MotionEvent.ACTION_DOWN) log.beginInteraction();
    String phase = action == MotionEvent.ACTION_DOWN ? "began" : action == MotionEvent.ACTION_UP ? "ended" : action == MotionEvent.ACTION_CANCEL ? "cancelled" : null;
    if (phase != null) log.record("touch." + phase, Collections.singletonMap("pointers", String.valueOf(event.getPointerCount())));
  }

  void network() {
    if (!front || !log.collects("network")) return;
    ConnectivityManager cm = activity.getSystemService(ConnectivityManager.class);
    Network current = cm.getActiveNetwork();
    NetworkCapabilities caps = cm.getNetworkCapabilities(current);
    String kind = caps == null ? "offline" : caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ? "wifi"
        : caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) ? "cellular" : caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) ? "wired" : "other";
    log.record("network.interface", Collections.singletonMap("connectionType", kind));
  }

  private void configure() {
    handler.removeCallbacks(tick);
    if (front && (log.collects("performance") || log.collects("resources"))) {
      lastTick = SystemClock.elapsedRealtime(); lastResource = 0;
      handler.postDelayed(tick, 1000);
    }
  }

  private final Runnable tick = new Runnable() {
    @Override public void run() {
      if (!front || !(log.collects("performance") || log.collects("resources"))) return;
      long now = SystemClock.elapsedRealtime();
      if (log.collects("performance") && now - lastTick > 1500) {
        Map<String, String> fields = new HashMap<>();
        fields.put("durationMs", String.valueOf(now - lastTick)); fields.put("sampleIntervalMs", "1000");
        log.record("performance.main-loop-gap", fields);
      }
      lastTick = now;
      if (log.collects("resources") && (lastResource == 0 || now - lastResource >= 60000)) { resources(); lastResource = now; }
      handler.postDelayed(this, 1000);
    }
  };

  private void resources() {
    ActivityManager manager = activity.getSystemService(ActivityManager.class);
    Debug.MemoryInfo[] memory = manager.getProcessMemoryInfo(new int[] {Process.myPid()});
    BatteryManager battery = activity.getSystemService(BatteryManager.class);
    Map<String, String> values = new HashMap<>();
    values.put("appPssBytes", String.valueOf((long) memory[0].getTotalPss() * 1024));
    values.put("cpuMs", String.valueOf(Process.getElapsedCpuTime()));
    values.put("batteryPercent", String.valueOf(battery.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)));
    values.put("charging", String.valueOf(battery.isCharging()));
    values.put("appActive", "true");
    log.record("resources.snapshot", values);
  }

  void close() {
    front = false; handler.removeCallbacks(tick);
    if (dialog != null) dialog.dismiss();
  }

  private AlertDialog.Builder alert(String title, NshHostReply reply) {
    return new AlertDialog.Builder(activity).setTitle(title)
        .setNegativeButton("取消 / Cancel", (d, which) -> finish(reply))
        .setOnCancelListener(d -> finish(reply));
  }

  private void finish(NshHostReply reply) { exporting = false; dialog = null; reply.resolve(Collections.singletonMap("shared", false)); }

  private void export(NshHostReply reply) {
    if (exporting) { reply.reject("Diagnostic export already open"); return; }
    exporting = true;
    dialog = alert("选择时间范围 / Time range", reply)
        .setItems(new String[] {"最近15分钟 / Last 15 min", "最近1小时 / Last hour", "全部保留记录 / All retained"}, (d, choice) -> {
          long seconds = new long[] {900, 3600, 0}[choice];
          dialog = alert("可选说明 / Optional note", reply)
              .setItems(new String[] {"不添加 / None", "首击无响应 / First tap", "内容慢 / Slow content", "连接异常 / Connection"}, (noteDialog, note) ->
                  preview(reply, seconds, new String[] {"none", "first-tap", "slow-content", "connection"}[note])).show();
        }).show();
  }

  /** Snapshot and filter before preview: sharing writes exactly what the person reviewed, with fresh report-local IDs. */
  private void preview(NshHostReply reply, long seconds, String note) {
    try {
      long until = System.currentTimeMillis(), since = seconds == 0 ? 0 : until - seconds * 1000;
      JSONArray events = log.snapshot(since);
      JSONObject report = report(events, since, until, note);
      String message = report.getJSONArray("categories") + "\n" + events.length() + " events\n"
          + new java.util.Date(report.getJSONObject("range").getLong("from")) + " → " + new java.util.Date(until) + "\n"
          + "无正文、输入、凭据、URL或路径。资源数据不能精确归因 App 耗电。 / No content, input, credentials, URLs or paths. Battery readings cannot attribute app energy usage.";
      dialog = alert("分享前预览 / Review before sharing", reply).setMessage(message)
          .setPositiveButton("分享 / Share", (d, which) -> {
            try {
              Opened.removeNamed(activity, EXPORT_NAME);
              byte[] bytes = report.toString(2).getBytes(StandardCharsets.UTF_8);
              Uri uri = Opened.put(activity, new ByteArrayInputStream(bytes), EXPORT_NAME, bytes.length);
              Intent send = new Intent(Intent.ACTION_SEND).setType("application/json").putExtra(Intent.EXTRA_STREAM, uri)
                  .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
              send.setClipData(ClipData.newRawUri(EXPORT_NAME, uri));
              activity.startActivity(Intent.createChooser(send, "分享诊断 / Share diagnostics"));
              // Android does not confirm delivery to the chosen app. Do not claim a report was actually shared.
              finish(reply);
            } catch (Exception e) { failed(reply); }
          }).show();
    } catch (Exception e) { failed(reply); }
  }

  JSONObject report(JSONArray events, long since, long until, String note) throws JSONException {
    Map<String, String> ids = new HashMap<>();
    for (int i = 0; i < events.length(); i++) {
      JSONObject row = events.getJSONObject(i);
      String oldSession = row.optString("sessionId");
      for (String key : new String[] {"sessionId", "interactionId"}) {
        String old = row.optString(key);
        row.put(key, old.isEmpty() ? "" : ids.computeIfAbsent(old, k -> UUID.randomUUID().toString()));
      }
      JSONObject fields = row.getJSONObject("fields");
      if (fields.has("request")) fields.put("request", ids.computeIfAbsent(oldSession + "/" + fields.getString("request"), k -> UUID.randomUUID().toString()));
    }
    JSONArray categories = new JSONArray();
    Map<?, ?> selected = (Map<?, ?>) log.status().get("categories");
    for (String category : DiagnosticLog.CATEGORIES) if (Boolean.TRUE.equals(selected.get(category))) categories.put(category);
    Object first = events.length() == 0 ? JSONObject.NULL : events.getJSONObject(0).get("at");
    Object last = events.length() == 0 ? JSONObject.NULL : events.getJSONObject(events.length() - 1).get("at");
    JSONObject app = new JSONObject();
    try {
      android.content.pm.PackageInfo info = activity.getPackageManager().getPackageInfo(activity.getPackageName(), 0);
      app.put("version", info.versionName); app.put("build", info.getLongVersionCode());
    } catch (android.content.pm.PackageManager.NameNotFoundException e) { throw new IllegalStateException(e); }
    app.put("platform", "android"); app.put("os", Build.VERSION.RELEASE); app.put("runtime", "NativeShell-0.3.0");
    return new JSONObject().put("schemaVersion", 1).put("exportId", UUID.randomUUID().toString()).put("app", app)
        .put("range", new JSONObject().put("from", since == 0 ? (first == JSONObject.NULL ? until : first) : since).put("to", until))
        .put("actualRange", new JSONObject().put("from", first).put("to", last)).put("categories", categories)
        .put("reproduction", new JSONObject().put("time", until).put("note", note)).put("events", events)
        .put("limits", "App-only breadcrumbs; no system logs. Battery readings cannot attribute app energy usage. No automatic upload.");
  }

  private void failed(NshHostReply reply) {
    exporting = false;
    dialog = new AlertDialog.Builder(activity).setTitle("无法导出 / Export failed")
        .setMessage("诊断存储不可用，请检查剩余空间或清空后重试。 / Check device storage or clear saved diagnostics and retry.")
        .setPositiveButton("OK", null).show();
    reply.reject("Diagnostic export failed");
  }
}
