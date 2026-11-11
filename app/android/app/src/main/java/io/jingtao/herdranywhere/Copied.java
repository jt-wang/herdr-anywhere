package io.jingtao.herdranywhere;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import java.io.File;
import java.io.FileNotFoundException;
import java.io.FileOutputStream;
import java.io.IOException;

/**
 * The one picture this app put on the clipboard (MainActivity page.copyImage), for the app that pastes it:
 * content://<package>.copied/picture.png and the like. Android's clipboard takes a picture only as such an address; it
 * lets the app that pastes read it, no other. One file in the cache, replaced by the next copy: nothing piles up.
 */
public class Copied extends ContentProvider {
  private static final String[] TYPES = {"png", "image/png", "jpg", "image/jpeg", "gif", "image/gif", "webp", "image/webp"};

  private static File dir(Context c) { return new File(c.getCacheDir(), "copied"); }

  /** Writes the picture in place of the last one and returns its address; ext is png, jpg, gif or webp. */
  static Uri put(Context c, byte[] bytes, String ext) throws IOException {
    if (typeOf(ext) == null) throw new IOException("not a picture type: " + ext);
    File d = dir(c);
    if (!d.isDirectory() && !d.mkdirs()) throw new IOException("can't make " + d);
    File[] old = d.listFiles();
    if (old != null) for (File f : old) f.delete();
    File f = new File(d, "picture." + ext);
    try (FileOutputStream out = new FileOutputStream(f)) { out.write(bytes); }
    return Uri.parse("content://" + c.getPackageName() + ".copied/" + f.getName());
  }

  private static String typeOf(String ext) {
    for (int i = 0; i < TYPES.length; i += 2) if (TYPES[i].equals(ext)) return TYPES[i + 1];
    return null;
  }

  /** The file an address names: only the picture there now. */
  private File fileOf(Uri uri) throws FileNotFoundException {
    String name = uri.getLastPathSegment();
    File f = name == null ? null : new File(dir(getContext()), name);
    if (f == null || !name.matches("picture\\.(png|jpg|gif|webp)") || !f.isFile()) throw new FileNotFoundException(String.valueOf(uri));
    return f;
  }

  @Override public boolean onCreate() { return true; }

  @Override public String getType(Uri uri) {
    String name = uri.getLastPathSegment();
    return name == null ? null : typeOf(name.substring(name.lastIndexOf('.') + 1));
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
