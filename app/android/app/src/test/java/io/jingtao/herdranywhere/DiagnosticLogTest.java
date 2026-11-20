package io.jingtao.herdranywhere;

import static org.junit.Assert.*;
import android.content.Context;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.Collections;
import java.util.Map;
import org.json.JSONArray;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

@RunWith(RobolectricTestRunner.class)
@Config(sdk = 35, manifest = Config.NONE)
public class DiagnosticLogTest {
  private Context context;
  private File directory;

  @Before public void fresh() throws Exception {
    context = RuntimeEnvironment.getApplication();
    context.getSharedPreferences("diagnostics", Context.MODE_PRIVATE).edit().clear().commit();
    directory = new File(context.getNoBackupFilesDir(), "diagnostics");
    new DiagnosticLog(context).clear();
    Files.deleteIfExists(directory.toPath());
  }

  private void recordAll(DiagnosticLog log) {
    for (String event : new String[] {"touch.began", "performance.main-loop-gap", "network.interface", "resources.snapshot", "js.http-end"}) {
      log.record(event, Collections.singletonMap("result", "ok"));
    }
  }

  @Test public void freshAndNeverEnabledUpgradeWriteNothingEvenWithMasterOnly() throws Exception {
    for (boolean upgrade : new boolean[] {false, true}) {
      if (upgrade) context.getSharedPreferences("diagnostics", Context.MODE_PRIVATE).edit().putBoolean("enabled", false).commit();
      DiagnosticLog log = new DiagnosticLog(context);
      assertEquals(false, log.status().get("enabled"));
      for (Object selected : ((Map<?, ?>) log.status().get("categories")).values()) assertEquals(false, selected);
      recordAll(log);
      assertFalse(directory.exists());
      log.set(Collections.singletonMap("enabled", true));
      recordAll(log);
      assertFalse("master consent must not preselect or record any category", directory.exists());
      log = new DiagnosticLog(context);
      recordAll(log);
      assertFalse("master-only restart must not record", directory.exists());
      log.set(Collections.singletonMap("enabled", false));
    }
  }

  @Test public void optInCollectsOnlySelectedCategoryAndUpgradePreservesItWhileMasterGatesWrites() throws Exception {
    DiagnosticLog log = new DiagnosticLog(context);
    log.set(Map.of("enabled", true, "categories", Map.of("network", true)));
    recordAll(log);
    JSONArray events = log.snapshot(0);
    assertEquals(2, events.length());
    assertEquals("network.interface", events.getJSONObject(0).getString("event"));
    assertEquals("js.http-end", events.getJSONObject(1).getString("event"));
    log = new DiagnosticLog(context);
    assertEquals(true, log.status().get("enabled"));
    assertEquals(Map.of("network", true, "performance", false, "interaction", false, "resources", false), log.status().get("categories"));
    log.set(Collections.singletonMap("enabled", false));
    byte[] before = Files.readAllBytes(new File(directory, "events-0.jsonl").toPath());
    recordAll(log);
    log = new DiagnosticLog(context);
    recordAll(log);
    assertArrayEquals("opt-out must prevent disk writes, not just hide records on export", before, Files.readAllBytes(new File(directory, "events-0.jsonl").toPath()));
    assertEquals(true, ((Map<?, ?>) log.status().get("categories")).get("network"));
    log.set(Collections.singletonMap("enabled", true));
    recordAll(log);
    assertEquals(4, log.snapshot(0).length());
  }

  @Test public void boundedLogRotatesFiltersTimeAndClearsWithoutDiscardingConsent() throws Exception {
    DiagnosticLog log = new DiagnosticLog(context);
    log.set(Map.of("enabled", true, "categories", Map.of("interaction", true)));
    String padding = String.join("", Collections.nCopies(8000, "x"));
    for (int i = 0; i < 150; i++) log.record("touch.began", Map.of("sample", padding));
    File[] files = directory.listFiles();
    assertEquals(3, files.length);
    for (File file : files) assertTrue(file.length() <= 256 * 1024);
    JSONArray events = log.snapshot(0);
    assertTrue(events.length() > 0 && events.length() < 150);
    assertEquals(150, events.getJSONObject(events.length() - 1).getInt("seq"));
    assertEquals(0, log.snapshot(System.currentTimeMillis() + 1000).length());
    // Simulate a process interrupted part-way through its last write.
    Files.write(new File(directory, "events-0.jsonl").toPath(), "{unfinished".getBytes(StandardCharsets.UTF_8), java.nio.file.StandardOpenOption.APPEND);
    assertEquals(events.length(), log.snapshot(0).length());
    log.clear();
    assertEquals(0, directory.listFiles().length);
    assertTrue(log.collects("interaction"));
  }

  @Test public void nativeBoundaryRejectsContentAndArbitraryEvents() {
    assertNull(DiagnosticLog.fields("invented-private-event", Map.of("text", "invented body")));
    assertEquals(Map.of("elapsed", "42", "endpoint", "/api/chat/:terminal", "method", "GET"),
        DiagnosticLog.fields("http-end", Map.of("elapsed", "42", "endpoint", "/api/chat/:terminal", "method", "GET", "text", "invented body", "url", "https://example.invalid/private")));
    assertEquals(Collections.emptyMap(), DiagnosticLog.fields("rpc-start", Map.of("method", "private.method", "rpcId", "-3", "request", "private-id")));
    assertEquals(Collections.emptyMap(), DiagnosticLog.fields("connection-error", Map.of("name", "invented exception", "step", "/private/path", "attempt", "NaN")));
  }

  @Test public void exportRemapsCorrelationIdsAndClearLeavesOtherOpenedFilesAlone() throws Exception {
    android.app.Activity activity = org.robolectric.Robolectric.buildActivity(android.app.Activity.class).setup().get();
    Diagnostics diagnostics = new Diagnostics(activity);
    diagnostics.log.set(Map.of("enabled", true, "categories", Map.of("network", true)));
    diagnostics.log.record("js.http-end", Map.of("request", "d1", "endpoint", "/api/info"));
    diagnostics.log.record("js.http-end", Map.of("request", "d1", "endpoint", "/api/info"));
    org.json.JSONObject first = diagnostics.report(diagnostics.log.snapshot(0), 0, System.currentTimeMillis(), "none");
    org.json.JSONObject second = diagnostics.report(diagnostics.log.snapshot(0), 0, System.currentTimeMillis(), "none");
    assertEquals(1, first.getInt("schemaVersion"));
    assertEquals("network", first.getJSONArray("categories").getString(0));
    assertEquals(2, first.getJSONArray("events").length());
    org.json.JSONObject a = first.getJSONArray("events").getJSONObject(0), b = first.getJSONArray("events").getJSONObject(1);
    assertEquals(a.getString("sessionId"), b.getString("sessionId"));
    assertNotEquals(diagnostics.log.session, a.getString("sessionId"));
    assertEquals(a.getJSONObject("fields").getString("request"), b.getJSONObject("fields").getString("request"));
    assertNotEquals("d1", a.getJSONObject("fields").getString("request"));
    assertNotEquals(a.getString("sessionId"), second.getJSONArray("events").getJSONObject(0).getString("sessionId"));
    File opened = new File(context.getCacheDir(), "opened");
    File report = new File(opened, "ab/" + Diagnostics.EXPORT_NAME), other = new File(opened, "cd/invented.txt");
    report.getParentFile().mkdirs(); other.getParentFile().mkdirs();
    Files.write(report.toPath(), first.toString().getBytes(StandardCharsets.UTF_8));
    Files.write(other.toPath(), new byte[] {42});
    Opened.removeNamed(context, Diagnostics.EXPORT_NAME);
    assertFalse(report.exists());
    assertTrue(other.isFile());
    diagnostics.close();
  }
}
