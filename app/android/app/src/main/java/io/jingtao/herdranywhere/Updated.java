package io.jingtao.herdranywhere;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.os.Build;

/** After an update the app started, the system ends the app and leaves the person on the home screen; Android lets
 *  nothing open the app again by itself. This new version says it is installed, and a tap opens it. */
public class Updated extends BroadcastReceiver {
  static final String CHANNEL = "updates";
  /** This one's tag (the app is updated), and MainActivity's for a new version that is out. */
  static final String TAG = "update", AVAILABLE = "new-version";

  static NotificationChannel channel(Context c) {
    return new NotificationChannel(CHANNEL, c.getString(R.string.channel_updates), NotificationManager.IMPORTANCE_DEFAULT);
  }

  /** Set just before the app hands an update to the system, so an install from elsewhere says nothing. */
  static SharedPreferences prefs(Context c) { return c.getSharedPreferences("update", Context.MODE_PRIVATE); }

  @Override
  public void onReceive(Context context, Intent intent) {
    if (!Intent.ACTION_MY_PACKAGE_REPLACED.equals(intent.getAction())) return;
    // A new version's notification (MainActivity) is out of date once a version is installed, from wherever.
    context.getSystemService(NotificationManager.class).cancel(AVAILABLE, 1);
    SharedPreferences p = prefs(context);
    if (!p.getBoolean("fromApp", false)) return;
    p.edit().remove("fromApp").apply();
    if (Build.VERSION.SDK_INT >= 33 && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return;
    NotificationManager nm = context.getSystemService(NotificationManager.class);
    nm.createNotificationChannel(channel(context));
    String version = "";
    try { version = context.getPackageManager().getPackageInfo(context.getPackageName(), 0).versionName; } catch (Exception e) {}
    Intent open = new Intent(context, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
    Notification n = new Notification.Builder(context, CHANNEL)
        .setSmallIcon(R.drawable.ic_notify)
        .setContentTitle(context.getString(R.string.updated_title))
        .setContentText(context.getString(R.string.updated_detail))
        .setSubText(version)
        .setContentIntent(PendingIntent.getActivity(context, 2, open, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT))
        .setAutoCancel(true)
        .build();
    nm.notify(TAG, 2, n);
  }
}
