package io.jingtao.tailpocket

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicLong
import kotlin.coroutines.resume

/** A pairing invite from a QR code, link or same-Wi-Fi delivery. It lets a phone pair until it expires: do not log it. */
@Serializable
data class TailPocketInvite(
    val v: Int,
    val address: String,
    val port: Int,
    val key: String,
    val proof: String,
    val expiresAt: Long,
    val name: String? = null,
)

/** One way to reach a computer (v0.3): `tailcat`, `relay` (your tailpocket-relay) or `lan`. Phones skip types they do not know. */
@Serializable
data class TailPocketRoute(
    val t: String,
    val a: List<String>? = null,
    val p: Int? = null,
    val u: String? = null,
    val r: String? = null,
    /** "opaque": the relay carries the app's own encryption, not TailPocket's. */
    val m: String? = null,
    /** In an invite's tailcat route: the v1 pairing window. */
    val inv: TailPocketInvite? = null,
)

/** A v2 invite (`tailpocket2:`): every route and the computer's end-to-end key. A v1 invite decodes as one tailcat route. */
@Serializable
data class TailPocketInvite2(
    val v: Int,
    val name: String? = null,
    val exp: Long,
    val pk: String? = null,
    val psk: String? = null,
    val via: List<TailPocketRoute>,
)

/**
 * How a connection reaches its computer now: `direct` or `relay` (Tailcat), `relay` (tailpocket-relay, [relay] its host name), or `lan`.
 * Since v0.3.1 [kind] tells them apart: `lan`, `direct` (Tailcat peer to peer), `derp` (Tailcat through a DERP relay: [derpRegion],
 * and [derpCode] and [derpHost] when known) or `relay` (tailpocket-relay: [relayHost]).
 */
@Serializable
data class TailPocketPath(
    val route: String,
    val relay: String = "",
    val kind: String = "",
    val derpRegion: Int = 0,
    val derpCode: String = "",
    val derpHost: String = "",
    val relayHost: String = "",
)

/**
 * Everything a paired phone needs to reach its computer. Holds the phone's private key: keep it in [TailPocketKeystoreStore].
 * [hostKey] and [via] (v0.3) are the computer's end-to-end key and its routes besides Tailcat; a link paired before v0.3 has
 * neither until [TailPocketConnection.sync], and one paired over a relay may have an empty [address]. Store the link again
 * whenever [TailPocketConnection.sync] or [TailPocketConnection.currentLink] returns a new one.
 */
@Serializable
data class TailPocketHostLink(
    val address: String = "",
    val port: Int = 0,
    val name: String,
    /** What the computer's app sent along (a token, settings…), or null. */
    val payload: String? = null,
    val key: String,
    @SerialName("pk") val hostKey: String? = null,
    val via: List<TailPocketRoute>? = null,
) {
    fun encode(): String = json.encodeToString(serializer(), this)

    companion object {
        fun decode(text: String): TailPocketHostLink = json.decodeFromString(serializer(), text)
    }
}

/**
 * A failure with a fixed code: invalid-invite, invalid-key, invalid-link, expired, unauthorized (after the pairing
 * emoji: the computer's user said no or the code was spent; show a new code on the computer), taken,
 * unreachable, revoked, mismatch, handshake-closed, denied, local-failed, cancelled, timeout, unknown-handle, failed.
 * Only "revoked" means the computer removed this phone: delete its link.
 */
class TailPocketException(val code: String) : Exception(code)

/**
 * A local route to the computer: point OkHttp, HttpURLConnection or a WebSocket client at [baseUrl].
 *
 * Any program on the phone can reach a loopback port, so the forward lets through only requests that carry [token].
 * Build every URL by appending to [baseUrl] (`"$baseUrl/api/info"`; the computer sees `/api/info`). A URL resolved
 * against "/" (OkHttp's `resolve("/api/info")`, `URL(URL(baseUrl), "/api/info")`) drops the prefix and gets 403.
 */
class TailPocketConnection internal constructor(
    private val native: TailPocketNative,
    private val handle: Long,
    val port: Int,
    /**
     * The secret every connection to the forward presents. HTTP and WebSocket clients get it through [baseUrl] (or the
     * header "Tailpocket-Token"); any other protocol (your own TLS) starts the connection with the line "TP-FWD <token>\n".
     * Keep it in memory only: do not store or log it.
     */
    val token: String,
    private val base: String,
) : AutoCloseable {
    private var closed = false
    /** http://127.0.0.1:<port>/_tailpocket/<token>: traffic continues to the computer with the prefix removed. */
    val baseUrl: String get() = "http://127.0.0.1:$port$base"

    /** The link with the computer's latest routes; store it when it differs from the saved one. */
    fun currentLink(): TailPocketHostLink = decode(TailPocketHostLink.serializer(), native.link(handle))

    /** Registers this phone's end-to-end key over Tailcat and returns the updated link to store. Links paired before v0.3 need it once; no new QR. */
    suspend fun sync(timeoutMs: Long = 30_000): TailPocketHostLink =
        withContext(Dispatchers.IO) { decode(TailPocketHostLink.serializer(), native.sync(handle, timeoutMs)) }

    /** How the connection reaches the computer now. Throws "revoked" once the computer removed this phone. */
    suspend fun path(timeoutMs: Long = 5_000): TailPocketPath =
        withContext(Dispatchers.IO) { decode(TailPocketPath.serializer(), native.path(handle, timeoutMs)) }

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        native.stop(handle)
    }
}

/** A same-Wi-Fi pairing in progress: show [code]; [finish] returns the invite once the computer's user approves. */
class TailPocketPendingLocalPairing internal constructor(private val native: TailPocketNative, private val op: Long, val code: String) {
    suspend fun finish(timeoutMs: Long = 300_000): TailPocketInvite =
        blocking(native, op) { native.localPairFinish(op, timeoutMs) }.let { decode(TailPocketInvite.serializer(), it) }

    fun cancel() = native.cancel(op)
}

internal val json = Json { ignoreUnknownKeys = true; encodeDefaults = false; explicitNulls = false }
private val ops = AtomicLong()
private val callers = Executors.newCachedThreadPool { r -> Thread(r, "tailpocket").apply { isDaemon = true } }

private fun failure(text: String): String? = try {
    json.parseToJsonElement(text).jsonObject["error"]?.jsonPrimitive?.content
} catch (_: Exception) { null }

internal fun <T> decode(serializer: kotlinx.serialization.KSerializer<T>, text: String): T {
    failure(text)?.let { throw TailPocketException(it) }
    return try { json.decodeFromString(serializer, text) } catch (_: Exception) { throw TailPocketException("failed") }
}

/** The emoji of a first-stage v2 pairing answer ({"emoji":[…]}), or null for anything else (a link, an error). */
private fun emojiOf(text: String): List<String>? = try {
    (json.parseToJsonElement(text).jsonObject["emoji"] as? JsonArray)?.map { it.jsonPrimitive.content }
} catch (_: Exception) { null }

/** Runs a blocking native call on its own thread; cancelling the coroutine cancels the native operation. */
internal suspend fun blocking(native: TailPocketNative, op: Long, call: () -> String): String =
    suspendCancellableCoroutine { cont ->
        cont.invokeOnCancellation { native.cancel(op) }
        callers.execute { val out = call(); if (cont.isActive) cont.resume(out) }
    }

class TailPocket private constructor(private val native: TailPocketNative) {
    /** A new phone identity. Create it once and keep it ([TailPocketKeystoreStore.phoneKey] does both). */
    fun newKey(): String = native.newKey()

    /** The "nodekey:…" the computer lists for this phone, or null for an invalid key. */
    fun publicKey(key: String): String? = native.publicKey(key).ifEmpty { null }

    fun decodeInvite(text: String, prefix: String = DEFAULT_PREFIX): TailPocketInvite = decode(TailPocketInvite.serializer(), native.decodeInvite(text, prefix))

    /** Decodes a v2 invite with [prefix], or a v1 one (tailpocket1:) as a v2 invite with one tailcat route. */
    fun decodeInvite2(text: String, prefix: String = DEFAULT_PREFIX2): TailPocketInvite2 = decode(TailPocketInvite2.serializer(), native.decodeInvite2(text, prefix))

    /**
     * Pairs over whichever of the invite's routes works first; returns the [TailPocketHostLink] to store. Once the
     * computer waits for its user to compare them, [onEmoji] gets four emoji: show them with "Check that your computer
     * shows the same, then confirm there" and return; pairing then waits for the computer's answer (its user has up to
     * a minute more than [timeoutMs]). A refusal there throws "unauthorized": ask for a new code on the computer. A
     * phone pairing again after a lost reply gets the link without [onEmoji].
     */
    suspend fun pair(invite: TailPocketInvite2, phoneKey: String, device: String, derpMapUrl: String = "", timeoutMs: Long = 90_000,
                     onEmoji: suspend (emoji: List<String>) -> Unit): TailPocketHostLink {
        val op = ops.incrementAndGet()
        val text = json.encodeToString(TailPocketInvite2.serializer(), invite)
        val first = blocking(native, op) { native.pair2(op, text, phoneKey, device, derpMapUrl, timeoutMs) }
        val emoji = emojiOf(first) ?: return decode(TailPocketHostLink.serializer(), first)
        try {
            onEmoji(emoji)
        } catch (e: Throwable) {
            native.cancel(op)
            throw e
        }
        // The native side's own deadline (timeoutMs and a minute, from the start) ends the wait first.
        return decode(TailPocketHostLink.serializer(), blocking(native, op) { native.pair2Finish(op, timeoutMs + 60_000) })
    }

    /** Joins the computer's pairing window; returns the [TailPocketHostLink] to store. */
    suspend fun pair(invite: TailPocketInvite, phoneKey: String, device: String, derpMapUrl: String = "", timeoutMs: Long = 90_000): TailPocketHostLink {
        val op = ops.incrementAndGet()
        val text = json.encodeToString(TailPocketInvite.serializer(), invite)
        return decode(TailPocketHostLink.serializer(), blocking(native, op) { native.pair(op, text, phoneKey, device, derpMapUrl, timeoutMs) })
    }

    /**
     * The phone's preference to reach computers over Tailcat only: [pair], [connect] and [TailPocketConnection.sync] then
     * never use the relay or local network routes a computer lists. Set it before [connect]; open connections keep theirs.
     */
    fun setTailcatOnly(on: Boolean) = native.setTailcatOnly(on)

    /**
     * The route types the phone may use: any of `"tailcat"`, `"lan"`, `"relay"` (v0.3.2). Without `"tailcat"`, [pair] and
     * [connect] never start a Tailcat client, so the phone talks to no Tailcat or DERP server. `null` allows every route.
     * Throws [TailPocketException] `invalid-routes` for an empty, unknown or repeated route. It turns [setTailcatOnly] off, and setTailcatOnly(true) clears it: the last call decides.
     * Set it before [connect]; open connections keep theirs.
     */
    fun setRoutes(routes: List<String>?) {
        if (routes != null && (routes.isEmpty() || routes.any { it.isEmpty() || it.contains(',') })) throw TailPocketException("invalid-routes")
        failure(native.setRoutes(routes?.joinToString(",") ?: ""))?.let { throw TailPocketException(it) }
    }

    /** Opens a local route to the computer. Nothing goes over the network until a client connects. */
    fun connect(link: TailPocketHostLink, derpMapUrl: String = ""): TailPocketConnection {
        val out = native.connect(link.encode(), derpMapUrl)
        failure(out)?.let { throw TailPocketException(it) }
        val obj = json.parseToJsonElement(out).jsonObject
        return TailPocketConnection(native, obj.getValue("handle").jsonPrimitive.content.toLong(), obj.getValue("port").jsonPrimitive.content.toInt(),
            obj.getValue("token").jsonPrimitive.content, obj.getValue("base").jsonPrimitive.content)
    }

    /** Starts same-Wi-Fi pairing with a host at "ip:port" (see [TailPocketNearbyHosts.resolve]); returns the code to show. */
    suspend fun beginLocalPairing(address: String, device: String, label: String = ""): TailPocketPendingLocalPairing {
        val op = ops.incrementAndGet()
        val out = blocking(native, op) { native.localPairStart(op, address, label, device) }
        failure(out)?.let { throw TailPocketException(it) }
        val code = json.parseToJsonElement(out).jsonObject.getValue("code").jsonPrimitive.content
        return TailPocketPendingLocalPairing(native, op, code)
    }

    companion object {
        const val DEFAULT_PREFIX = "tailpocket1:"
        /** The prefix of v2 invites (v0.3), which carry every route. */
        const val DEFAULT_PREFIX2 = "tailpocket2:"

        /** The SDK backed by the bundled native library. */
        val default: TailPocket by lazy { TailPocket(GoNative) }

        /** The SDK over another native layer (tests). */
        fun with(native: TailPocketNative) = TailPocket(native)

        /** The first of [prefixes] found in [text] wins. */
        fun inviteText(text: String, prefixes: List<String>): String? = prefixes.firstNotNullOfOrNull { inviteText(text, it) }

        /** Finds invite text in scanned or pasted text: a bare code, or a link containing it. */
        fun inviteText(text: String, prefix: String = DEFAULT_PREFIX): String? {
            val start = text.indexOf(prefix).takeIf { it >= 0 } ?: return null
            val tail = text.substring(start + prefix.length).takeWhile { it.isLetterOrDigit() && it.code < 128 || it == '-' || it == '_' }
            return if (tail.isEmpty()) null else prefix + tail
        }
    }
}
