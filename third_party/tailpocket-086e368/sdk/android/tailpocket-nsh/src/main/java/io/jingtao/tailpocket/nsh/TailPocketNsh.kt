package io.jingtao.tailpocket.nsh

import android.os.Handler
import android.os.Looper
import com.longbridge.nsh.NshView
import io.jingtao.tailpocket.TailPocketBridge
import io.jingtao.tailpocket.TailPocketException
import io.jingtao.tailpocket.ui.TailPocketQrScanActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * Registers every bridge function on an NshView, so the NSH app calls them with
 * `host.call('tailPocket.pair', {...})` (see sdk/js/client.ts) and receives `tailPocket.nearby` and `tailPocket.pair.emoji` events.
 *
 *     TailPocketNsh.attach(view, lifecycleScope)
 */
object TailPocketNsh {
    fun attach(view: NshView, scope: CoroutineScope): TailPocketBridge {
        val main = Handler(Looper.getMainLooper())
        val bridge = TailPocketBridge.create(
            view.context,
            scanner = { prefixes -> TailPocketQrScanActivity.scan(view.context, prefixes) },
            emit = { event, data -> main.post { view.send(event, data) } },
        )
        for (name in bridge.names) {
            view.handle(name) { data, reply ->
                scope.launch {
                    try {
                        @Suppress("UNCHECKED_CAST")
                        reply.resolve(bridge.call(name, data as? Map<String, Any?>))
                    } catch (e: TailPocketException) {
                        reply.reject(e.code)
                    } catch (e: Exception) {
                        reply.reject("failed")
                    }
                }
            }
        }
        return bridge
    }
}
