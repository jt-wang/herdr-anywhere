package io.jingtao.herdranywhere;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import android.webkit.MimeTypeMap;
import java.io.File;
import java.io.FileNotFoundException;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.security.SecureRandom;

/**
 * The files from the computer this app handed to another app (MainActivity page.openFile), for that app to read:
 * content://<package>.opened/<folder>/<name>. Each in a folder of its own, under its own name, in the cache; the last
 * few stay, so an app that opens one a while later still can, and older ones go as new ones come.
 */
public class Opened extends ContentProvider {
  static final int KEEP = 4;
  private static final SecureRandom RANDOM = new SecureRandom();

  private static File dir(Context c) { return new File(c.getCacheDir(), "opened"); }

  /** A name a file can have here: the last part of the computer's, without what a path or a shell would read. */
  static String safeName(String name) {
    String n = name == null ? "" : name.substring(name.lastIndexOf('/') + 1).replaceAll("[\\\\:*?\"<>|\\x00-\\x1f]", "_");
    return n.isEmpty() || n.equals(".") || n.equals("..") ? "file" : n;
  }

  /** Writes what `in` reads, at most `max` bytes, under `name`, and returns its address. */
  static Uri put(Context c, InputStream in, String name, long max) throws IOException {
    File d = dir(c);
    if (!d.isDirectory() && !d.mkdirs()) throw new IOException("can't make " + d);
    File[] old = d.listFiles();
    if (old != null && old.length >= KEEP) {
      java.util.Arrays.sort(old, (a, b) -> Long.compare(a.lastModified(), b.lastModified()));
      for (int i = 0; i <= old.length - KEEP; i++) wipe(old[i]);
    }
    File folder = new File(d, Long.toHexString(RANDOM.nextLong() & Long.MAX_VALUE));
    if (!folder.mkdirs()) throw new IOException("can't make " + folder);
    File f = new File(folder, safeName(name));
    long n = 0;
    try (FileOutputStream out = new FileOutputStream(f)) {
      byte[] b = new byte[64 << 10];
      for (int r; (r = in.read(b)) > 0; ) {
        n += r;
        if (n > max) throw new IOException("over " + max + " bytes");
        out.write(b, 0, r);
      }
    } catch (IOException e) {
      wipe(folder);
      throw e;
    }
    android.util.Log.i("herdr", "handed over: " + f.getName() + ", " + n + " bytes"); // e2e/android-chat.sh reads it
    return new Uri.Builder().scheme("content").authority(c.getPackageName() + ".opened").appendPath(folder.getName()).appendPath(f.getName()).build();
  }

  private static void wipe(File f) {
    File[] in = f.listFiles();
    if (in != null) for (File x : in) wipe(x);
    f.delete();
  }

  /** The type Android knows a name by, else none. */
  static String typeOfName(String name) {
    int dot = name.lastIndexOf('.');
    String t = dot < 0 ? null : MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substring(dot + 1).toLowerCase(java.util.Locale.ROOT));
    return t == null ? "application/octet-stream" : t;
  }

  /** The file an address names: only one written here, by its folder and name. */
  private File fileOf(Uri uri) throws FileNotFoundException {
    java.util.List<String> p = uri.getPathSegments();
    if (p.size() != 2 || !p.get(0).matches("[0-9a-f]{1,16}")) throw new FileNotFoundException(String.valueOf(uri));
    File f = new File(new File(dir(getContext()), p.get(0)), p.get(1));
    if (!p.get(1).equals(safeName(p.get(1))) || !f.isFile()) throw new FileNotFoundException(String.valueOf(uri));
    return f;
  }

  @Override public boolean onCreate() { return true; }

  @Override public String getType(Uri uri) {
    String name = uri.getLastPathSegment();
    return name == null ? null : typeOfName(name);
  }

  @Override public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
    if (!"r".equals(mode)) throw new SecurityException("read only");
    return ParcelFileDescriptor.open(fileOf(uri), ParcelFileDescriptor.MODE_READ_ONLY);
  }

  @Override public Cursor query(Uri uri, String[] projection, String selection, String[] args, String sort) {
    File f;
    try { f = fileOf(uri); } catch (FileNotFoundException e) { return null; }
    MatrixCursor c = new MatrixCursor(new String[] {OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE});
    c.addRow(new Object[] {f.getName(), f.length()});
    return c;
  }

  @Override public Uri insert(Uri uri, ContentValues values) { throw new UnsupportedOperationException(); }
  @Override public int update(Uri uri, ContentValues values, String s, String[] a) { throw new UnsupportedOperationException(); }
  @Override public int delete(Uri uri, String s, String[] a) { throw new UnsupportedOperationException(); }
}
