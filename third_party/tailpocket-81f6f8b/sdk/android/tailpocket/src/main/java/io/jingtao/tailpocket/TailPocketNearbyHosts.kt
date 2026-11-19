package io.jingtao.tailpocket

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeout
import java.net.Inet4Address
import java.util.concurrent.ConcurrentHashMap
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** A computer advertising its pairing window on this Wi-Fi. */
data class TailPocketNearbyHost(val name: String, internal val info: NsdServiceInfo)

/**
 * Lists computers on this Wi-Fi with an open pairing window, using the system's NsdManager (no multicast
 * permission needed). [service] is the Bonjour type, e.g. "_myapp._tcp".
 */
class TailPocketNearbyHosts(context: Context, private val service: String) {
    private val nsd = context.applicationContext.getSystemService(NsdManager::class.java)
    private val found = ConcurrentHashMap<String, TailPocketNearbyHost>()
    private var listener: NsdManager.DiscoveryListener? = null

    /** Calls [onChange] (on a binder thread) with the current list whenever it changes, until [stop]. */
    @Synchronized
    fun start(onChange: (List<TailPocketNearbyHost>) -> Unit, onError: (Int) -> Unit = {}) {
        if (listener != null) return
        val l = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(serviceType: String) {}
            override fun onDiscoveryStopped(serviceType: String) {}
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) = onError(errorCode)
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
            override fun onServiceFound(info: NsdServiceInfo) {
                found[info.serviceName] = TailPocketNearbyHost(info.serviceName, info)
                onChange(found.values.sortedBy { it.name })
            }
            override fun onServiceLost(info: NsdServiceInfo) {
                found.remove(info.serviceName)
                onChange(found.values.sortedBy { it.name })
            }
        }
        listener = l
        nsd.discoverServices(service, NsdManager.PROTOCOL_DNS_SD, l)
    }

    @Synchronized
    fun stop() {
        listener?.let { runCatching { nsd.stopServiceDiscovery(it) } }
        listener = null
        found.clear()
    }

    /** Resolves a host to "ipv4:port" (the computer serves pairing on IPv4 only). */
    suspend fun resolve(host: TailPocketNearbyHost, timeoutMs: Long = 10_000): String = withTimeout(timeoutMs) {
        suspendCancellableCoroutine { cont ->
            @Suppress("DEPRECATION") // resolveService is the only resolver below API 34.
            nsd.resolveService(host.info, object : NsdManager.ResolveListener {
                override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) {
                    if (cont.isActive) cont.resumeWithException(TailPocketException("local-failed"))
                }
                override fun onServiceResolved(info: NsdServiceInfo) {
                    val addresses = if (Build.VERSION.SDK_INT >= 34) info.hostAddresses else @Suppress("DEPRECATION") listOfNotNull(info.host)
                    val ip = addresses.firstOrNull { it is Inet4Address }?.hostAddress
                    if (!cont.isActive) return
                    if (ip == null || info.port !in 1..65535) cont.resumeWithException(TailPocketException("local-failed"))
                    else cont.resume("$ip:${info.port}")
                }
            })
        }
    }
}
