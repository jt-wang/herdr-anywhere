package io.jingtao.herdranywhere;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.SystemClock;
import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;

/** App-only, opt-in breadcrumbs. Never pass content, credentials, URLs, paths or exception messages here. */
final class DiagnosticLog {
  static final String[] CATEGORIES = {"interaction", "performance", "network", "resources"};
  static final int GENERATION_BYTES = 256 * 1024, GENERATIONS = 3;
  final String session = UUID.randomUUID().toString();
  private final SharedPreferences preferences;
  private final File directory;
  private final Map<String, Boolean> categories = new LinkedHashMap<>();
  private boolean enabled;
  private String interaction = "", failure;
  private long sequence;

  DiagnosticLog(Context context) {
    preferences = context.getSharedPreferences("diagnostics", Context.MODE_PRIVATE);
    directory = new File(context.getNoBackupFilesDir(), "diagnostics");
    enabled = preferences.getBoolean("enabled", false);
    for (String category : CATEGORIES) categories.put(category, preferences.getBoolean("category." + category, false));
  }

  synchronized boolean collects(String category) { return enabled && Boolean.TRUE.equals(categories.get(category)); }

  synchronized Map<String, Object> status() {
    Map<String, Object> status = new HashMap<>();
    status.put("supported", true); status.put("enabled", enabled);
    status.put("categories", new LinkedHashMap<>(categories));
    status.put("session", session); status.put("interaction", interaction); status.put("error", failure);
    return status;
  }

  synchronized void set(Map<?, ?> changes) {
    Object selected = changes.get("categories");
    if (selected instanceof Map) for (String category : CATEGORIES) {
      Object value = ((Map<?, ?>) selected).get(category);
      if (value instanceof Boolean) categories.put(category, (Boolean) value);
    }
    if (changes.get("enabled") instanceof Boolean) enabled = (Boolean) changes.get("enabled");
    SharedPreferences.Editor edit = preferences.edit().putBoolean("enabled", enabled);
    for (String category : CATEGORIES) edit.putBoolean("category." + category, categories.get(category));
    edit.apply();
  }

  synchronized void beginInteraction() { if (collects("interaction")) interaction = UUID.randomUUID().toString(); }

  static String category(String event) {
    for (String prefix : Arrays.asList("network.", "js.http-", "js.rpc-", "js.socket-", "js.tunnel-", "js.connection-")) {
      if (event.startsWith(prefix)) return "network";
    }
    if (event.startsWith("performance.")) return "performance";
    if (event.startsWith("resources.")) return "resources";
    return "interaction";
  }

  synchronized void record(String event, Map<String, String> fields) {
    String category = category(event);
    if (!collects(category)) return;
    try {
      JSONObject row = new JSONObject();
      row.put("seq", ++sequence); row.put("event", event); row.put("category", category);
      row.put("sessionId", session); row.put("interactionId", interaction);
      row.put("at", System.currentTimeMillis()); row.put("monoMs", SystemClock.elapsedRealtime());
      row.put("fields", new JSONObject(fields));
      byte[] bytes = (row + "\n").getBytes(StandardCharsets.UTF_8);
      if (bytes.length > GENERATION_BYTES) throw new IOException("event too large");
      if (!directory.isDirectory() && !directory.mkdirs()) throw new IOException("directory unavailable");
      if (file(0).length() + bytes.length > GENERATION_BYTES) {
        Files.deleteIfExists(file(GENERATIONS - 1).toPath());
        for (int n = GENERATIONS - 2; n >= 0; n--) if (file(n).exists()) Files.move(file(n).toPath(), file(n + 1).toPath());
      }
      try (FileOutputStream out = new FileOutputStream(file(0), true)) { out.write(bytes); }
    } catch (IOException | JSONException e) { failure = "Diagnostic storage failed"; }
  }

  synchronized JSONArray snapshot(long since) throws IOException, JSONException {
    if (failure != null) throw new IOException(failure);
    JSONArray events = new JSONArray();
    for (int n = GENERATIONS - 1; n >= 0; n--) if (file(n).isFile()) {
      for (String line : Files.readAllLines(file(n).toPath(), StandardCharsets.UTF_8)) {
        JSONObject row;
        // A killed process can leave an incomplete final row; keep complete earlier rows.
        try { row = new JSONObject(line); } catch (JSONException e) { continue; }
        if (Boolean.TRUE.equals(categories.get(row.optString("category"))) && row.optLong("at") >= since) events.put(row);
      }
    }
    return events;
  }

  synchronized void clear() throws IOException {
    for (int n = 0; n < GENERATIONS; n++) Files.deleteIfExists(file(n).toPath());
    sequence = 0; interaction = ""; failure = null;
  }

  private File file(int n) { return new File(directory, "events-" + n + ".jsonl"); }

  // Revalidate at the native boundary: arbitrary JS host calls must not turn this into a content recorder.
  private static final Map<String, String> RULES = new HashMap<>();
  static {
    RULES.put("app-start", "");
    RULES.put("ui-handler", "action"); RULES.put("ui-open-space", "step");
    RULES.put("ui-nav", "route depth"); RULES.put("ui-page", "route");
    String request = "request elapsed status timeout requestBytes responseBytes result";
    for (String name : Arrays.asList("http-start", "http-end")) RULES.put(name, request + " endpoint method");
    for (String name : Arrays.asList("rpc-start", "rpc-end")) RULES.put(name, request + " method rpcId");
    RULES.put("socket-start", request + " enc"); RULES.put("socket-open", request); RULES.put("socket-end", request + " reason");
    RULES.put("tunnel-start", request); RULES.put("tunnel-end", request);
    RULES.put("connection-connect", "attempt step"); RULES.put("connection-connect-catch", "attempt name");
    RULES.put("connection-lost", "wanted retrying attempt"); RULES.put("connection-step", "attempt step");
    RULES.put("connection-return", "attempt reason"); RULES.put("connection-retry-fired", "attempt wanted hasClient");
    RULES.put("connection-retry-scheduled", "attempt delay"); RULES.put("connection-error", "attempt step name");
  }
  private static final String WORDS = "start hello-before hello-after subscribe-before subscribe-after ready idle connecting reconnecting unpaired closed open home space settings diagnostics chat terminal progress task pair background foreground true false ok error timeout cancelled network unknown abort Error TypeError RangeError SyntaxError ReferenceError URIError EvalError AbortError TimeoutError GET POST PUT DELETE PATCH HEAD OPTIONS enter entry selected popped pushed already-open open-space open-terminal disconnected same-attempt not-wanted retry-pending stop-loops-before stop-loops-after clear-client-before clear-client-after conn-before conn-after lost-why-before lost-why-after error-before error-after bin b64 no-core binary-unsupported other";
  private static final String METHODS = "hello terminal.run terminal.list terminal.kill terminal.label terminal.attach terminal.keep terminal.get terminal.send terminal.keys terminal.read doc.get doc.set events.subscribe machine.list machine.exec server.status other";
  private static final String ENDPOINTS = "/api/pair /api/info /api/folders /api/chat/:terminal /api/chat/:terminal/steps/:id /api/chat/:terminal/images/:id /api/chat-setups /api/chat-setups/:agent /api/file /api/file/check /api/file/link /api/files /api/image /api/image/check /api/update /api/update/check /api/update/checks /api/update/auto other";
  private static boolean contains(String choices, String value) { return !value.isEmpty() && Arrays.asList(choices.split(" ")).contains(value); }

  static Map<String, String> fields(String event, Object input) {
    if (!RULES.containsKey(event)) return null;
    Map<?, ?> values = input instanceof Map ? (Map<?, ?>) input : Collections.emptyMap();
    Map<String, String> safe = new HashMap<>();
    for (String key : RULES.get(event).split(" ")) {
      if (!(values.get(key) instanceof String)) continue;
      String value = (String) values.get(key);
      if (contains("attempt delay elapsed status timeout depth rpcId requestBytes responseBytes", key)) {
        if (value.matches("^(0|[1-9][0-9]{0,14})$")) safe.put(key, value);
      } else if (contains("wanted retrying hasClient", key)) {
        if (contains("true false", value)) safe.put(key, value);
      } else if (key.equals("request")) {
        if (value.matches("^d[1-9][0-9]{0,14}$")) safe.put(key, value);
      } else if (key.equals("endpoint")) { if (contains(ENDPOINTS, value)) safe.put(key, value); }
      else if (key.equals("method")) { if (contains(event.startsWith("rpc-") ? METHODS : "GET POST PUT DELETE PATCH HEAD OPTIONS other", value)) safe.put(key, value); }
      else if (contains(WORDS, value)) safe.put(key, value);
    }
    return safe;
  }
}
