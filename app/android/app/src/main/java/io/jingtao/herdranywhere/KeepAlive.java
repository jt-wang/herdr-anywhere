package io.jingtao.herdranywhere;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;

/**
 * Keeps the app's process running after it leaves the screen, so its connection to the computer stays open and the
 * agents' alerts still arrive. It does nothing else; the connection is the app's. Android requires the notification.
 * `specialUse`: `dataSync` is limited to 6 hours a day, and nothing else describes a connection left open to one's own
 * computer. The app stops it when it has no computer, the person turns it off, or the app goes (MainActivity).
 */
public class KeepAlive extends Service {
  static final String CHANNEL = "connection";

  /** Silent, in the status bar: Android raises a foreground service's MIN channel to LOW on its first notification
   *  (NotificationManagerService, "Increase the importance of fgs/uij notifications"), so it says LOW itself. */
  static NotificationChannel channel(Context c) {
    NotificationChannel ch = new NotificationChannel(CHANNEL, c.getString(R.string.channel_connection), NotificationManager.IMPORTANCE_LOW);
    ch.setShowBadge(false);
    return ch;
  }

  @Override
  public int onStartCommand(Intent intent, int flags, int startId) {
    NotificationManager nm = getSystemService(NotificationManager.class);
    nm.createNotificationChannel(channel(this));
    PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE);
    Notification n = new Notification.Builder(this, CHANNEL)
        .setSmallIcon(R.drawable.ic_notify)
        .setContentTitle(getString(R.string.keep_alive_title))
        .setContentIntent(open)
        .setOngoing(true)
        .build();
    if (Build.VERSION.SDK_INT >= 34) startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
    else startForeground(1, n);
    return START_NOT_STICKY;
  }

  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
