package io.jingtao.tailpocket

import android.content.Context
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.longOrNull
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

/** Where a bridge keeps the phone key and links; [TailPocketKeystoreStore] is the default. */
interface TailPocketLinkStore {
    fun phoneKey(): String
    fun saveLink(link: TailPocketHostLink, slot: String = "default")
    fun link(slot: String = "default"): TailPocketHostLink?
    fun deleteLink(slot: String = "default")
}

/** Computers on this Wi-Fi by name; [TailPocketNearbyHosts] is the default. */
interface TailPocketNearbySource {
    fun start(onChange: (List<String>) -> Unit)
    fun stop()
    suspend fun address(name: String): String
}

/**
 * The SDK as a table of named async functions over JSON-like values (maps, lists, strings, numbers,
 * booleans, null), plus events through [emit]. Any JavaScript bridge (NSH, React TailPocketNative, Capacitor, a
 * WebView) registers each name and forwards calls: see docs/BRIDGE.md for the names and shapes. Failures
 * throw [TailPocketException]; bridges reject with its code.
 */
class TailPocketBridge(
    private val sdk: TailPocket,
    private val store: TailPocketLinkStore,
    private val nearbyFor: (service: String) -> TailPocketNearbySource,
    /** Shows a QR scanner and returns the first invite text with one of the prefixes; null makes "tailPocket.scan" fail with "unsupported". */
    private val scanner: (suspend (prefixes: List<String>) -> String)? = null,
    private val emit: (event: String, data: Any?) -> Unit = { _, _ -> },
) {
    private var nearby: TailPocketNearbySource? = null
    /** Open connections, with the store slot their link came from (null for a link passed in). */
    private val connections = ConcurrentHashMap<Long, Pair<TailPocketConnection, String?>>()
    private val pending = ConcurrentHashMap<Long, TailPocketPendingLocalPairing>()
    private val ops = AtomicLong()

    private val functions: Map<String, suspend (Map<String, Any?>) -> Any?> = mapOf(
        "tailPocket.newKey" to { _ -> sdk.newKey() },
        "tailPocket.publicKey" to { a -> sdk.publicKey(a.string("key")) },
        "tailPocket.inviteText" to { a -> TailPocket.inviteText(a.string("text"), a.prefix()) },
        "tailPocket.decodeInvite" to { a -> toValue(TailPocketInvite.serializer(), sdk.decodeInvite(a.string("text"), a.prefix())) },
        "tailPocket.decodeInvite2" to { a ->
            toValue(TailPocketInvite2.serializer(), sdk.decodeInvite2(a.string("text"), a.optString("prefix") ?: TailPocket.DEFAULT_PREFIX2))
        },
        "tailPocket.pair" to { a ->
            // Text and v2 objects pair over every route (a v1 text as its one tailcat route); a v1 object over Tailcat.
            val invite = when (val i = a["invite"]) {
                is String -> {
                    val prefix2 = a.optString("prefix2") ?: TailPocket.DEFAULT_PREFIX2
                    val text = TailPocket.inviteText(i, listOf(prefix2, a.prefix())) ?: i
                    sdk.decodeInvite2(text, if (text.startsWith(prefix2)) prefix2 else a.prefix())
                }
                is Map<*, *> -> if ((i["v"] as? Number)?.toInt() == 2) fromValue(TailPocketInvite2.serializer(), i) else {
                    val v1 = fromValue(TailPocketInvite.serializer(), i)
                    TailPocketInvite2(v = 2, name = v1.name, exp = v1.expiresAt, via = listOf(TailPocketRoute(t = "tailcat", inv = v1)))
                }
                else -> throw TailPocketException("invalid-arguments")
            }
            val key = (a["phoneKey"] as? String) ?: store.phoneKey()
            val link = sdk.pair(invite, key, a.optString("device") ?: "Phone", a.optString("derpMapUrl") ?: "", a.long("timeoutMs") ?: 90_000) { emoji ->
                emit("tailPocket.pair.emoji", mapOf("emoji" to emoji))
            }
            if (a["save"] == true) store.saveLink(link, a.optString("slot") ?: "default")
            toValue(TailPocketHostLink.serializer(), link)
        },
        "tailPocket.setTailcatOnly" to { a -> sdk.setTailcatOnly(a["on"] == true); null },
        "tailPocket.setRoutes" to { a ->
            // Only an explicit null allows every route. A call without `routes` is a mistake (JavaScript's
            // undefined is dropped on the way here), not a choice to widen the phone's routes.
            if (!a.containsKey("routes")) throw TailPocketException("invalid-arguments")
            val routes = when (val r = a["routes"]) {
                null -> null
                is List<*> -> r.map { it as? String ?: throw TailPocketException("invalid-arguments") }
                else -> throw TailPocketException("invalid-arguments")
            }
            sdk.setRoutes(routes); null
        },
        "tailPocket.connect" to { a ->
            var slot: String? = null
            val link = when (val l = a["link"]) {
                is Map<*, *> -> fromValue(TailPocketHostLink.serializer(), l)
                null -> (a.optString("slot") ?: "default").let { s -> slot = s; store.link(s) } ?: throw TailPocketException("not-paired")
                else -> throw TailPocketException("invalid-arguments")
            }
            val c = sdk.connect(link, a.optString("derpMapUrl") ?: "")
            val handle = ops.incrementAndGet()
            connections[handle] = c to slot
            mapOf("handle" to handle, "port" to c.port, "baseUrl" to c.baseUrl, "token" to c.token)
        },
        "tailPocket.disconnect" to { a ->
            connections.remove(a.long("handle") ?: -1)?.let { (c, slot) ->
                // Keep the routes the computer sent meanwhile, for a link that came from the store.
                if (slot != null) runCatching { c.currentLink() }.getOrNull()?.let { latest -> if (latest != store.link(slot)) store.saveLink(latest, slot) }
                c.close()
            }
            null
        },
        "tailPocket.sync" to { a ->
            val (c, slot) = connection(a)
            val link = c.sync(a.long("timeoutMs") ?: 30_000)
            if (slot != null) store.saveLink(link, slot)
            toValue(TailPocketHostLink.serializer(), link)
        },
        "tailPocket.path" to { a -> toValue(TailPocketPath.serializer(), connection(a).first.path(a.long("timeoutMs") ?: 5_000)) },
        "tailPocket.link" to { a -> toValue(TailPocketHostLink.serializer(), connection(a).first.currentLink()) },
        "tailPocket.nearby.start" to { a ->
            val service = a.string("service")
            synchronized(this) {
                nearby?.stop()
                nearby = nearbyFor(service).also { source ->
                    source.start { names -> emit("tailPocket.nearby", mapOf("hosts" to names.map { mapOf("name" to it) })) }
                }
            }
            null
        },
        "tailPocket.nearby.stop" to { _ -> synchronized(this) { nearby?.stop(); nearby = null }; null },
        "tailPocket.local.begin" to { a ->
            val source = synchronized(this) { nearby } ?: throw TailPocketException("nearby-not-started")
            val address = a.optString("address") ?: source.address(a.string("name"))
            val p = sdk.beginLocalPairing(address, a.optString("device") ?: "Phone", a.optString("label") ?: "")
            val op = ops.incrementAndGet()
            pending[op] = p
            mapOf("op" to op, "code" to p.code)
        },
        "tailPocket.local.finish" to { a ->
            val p = pending.remove(a.long("op") ?: -1) ?: throw TailPocketException("unknown-operation")
            toValue(TailPocketInvite.serializer(), p.finish(a.long("timeoutMs") ?: 300_000))
        },
        "tailPocket.local.cancel" to { a -> pending.remove(a.long("op") ?: -1)?.cancel(); null },
        "tailPocket.scan" to { a -> (scanner ?: throw TailPocketException("unsupported"))(a.scanPrefixes()) },
        "tailPocket.store.phoneKey" to { _ -> store.phoneKey() },
        "tailPocket.store.saveLink" to { a ->
            store.saveLink(fromValue(TailPocketHostLink.serializer(), a["link"] as? Map<*, *> ?: throw TailPocketException("invalid-arguments")), a.optString("slot") ?: "default")
            null
        },
        "tailPocket.store.link" to { a -> store.link(a.optString("slot") ?: "default")?.let { toValue(TailPocketHostLink.serializer(), it) } },
        "tailPocket.store.deleteLink" to { a -> store.deleteLink(a.optString("slot") ?: "default"); null },
    )

    /** Every function name this bridge answers. */
    val names: List<String> get() = functions.keys.toList()

    suspend fun call(name: String, args: Map<String, Any?>?): Any? {
        val fn = functions[name] ?: throw TailPocketException("unknown-function")
        return fn(args ?: emptyMap())
    }

    /** Stops discovery and closes every connection. */
    fun close() {
        synchronized(this) { nearby?.stop(); nearby = null }
        connections.values.forEach { it.first.close() }
        connections.clear()
        pending.values.forEach { it.cancel() }
        pending.clear()
    }

    private fun connection(a: Map<String, Any?>) = connections[a.long("handle") ?: -1] ?: throw TailPocketException("unknown-handle")

    companion object {
        val NAMES = listOf(
            "tailPocket.newKey", "tailPocket.publicKey", "tailPocket.inviteText", "tailPocket.decodeInvite", "tailPocket.decodeInvite2",
            "tailPocket.pair", "tailPocket.connect", "tailPocket.disconnect", "tailPocket.sync", "tailPocket.path", "tailPocket.link",
            "tailPocket.nearby.start", "tailPocket.nearby.stop", "tailPocket.local.begin", "tailPocket.local.finish", "tailPocket.local.cancel", "tailPocket.scan",
            "tailPocket.store.phoneKey", "tailPocket.store.saveLink", "tailPocket.store.link", "tailPocket.store.deleteLink",
            "tailPocket.setTailcatOnly", "tailPocket.setRoutes",
        )

        /** A bridge with the Keystore store and NsdManager discovery. */
        fun create(context: Context, scanner: (suspend (List<String>) -> String)? = null, emit: (String, Any?) -> Unit): TailPocketBridge {
            val app = context.applicationContext
            return TailPocketBridge(TailPocket.default, TailPocketKeystoreStore(app), { service -> NsdSource(TailPocketNearbyHosts(app, service)) }, scanner, emit)
        }
    }
}

/** Adapts [TailPocketNearbyHosts] to names. */
private class NsdSource(private val hosts: TailPocketNearbyHosts) : TailPocketNearbySource {
    private val byName = ConcurrentHashMap<String, TailPocketNearbyHost>()
    override fun start(onChange: (List<String>) -> Unit) = hosts.start(onChange = { list ->
        byName.clear()
        list.forEach { byName[it.name] = it }
        onChange(list.map { it.name })
    })
    override fun stop() = hosts.stop()
    override suspend fun address(name: String) = hosts.resolve(byName[name] ?: throw TailPocketException("local-failed"))
}

private fun Map<String, Any?>.string(key: String) = this[key] as? String ?: throw TailPocketException("invalid-arguments")
private fun Map<String, Any?>.optString(key: String) = this[key] as? String
private fun Map<String, Any?>.long(key: String) = (this[key] as? Number)?.toLong()
private fun Map<String, Any?>.prefix() = optString("prefix") ?: TailPocket.DEFAULT_PREFIX
/** The prefixes "tailPocket.scan" looks for: `prefix` and `prefix2` as given, or both defaults when neither is. */
private fun Map<String, Any?>.scanPrefixes() = listOfNotNull(optString("prefix"), optString("prefix2")).filter { it.isNotEmpty() }
    .ifEmpty { listOf(TailPocket.DEFAULT_PREFIX, TailPocket.DEFAULT_PREFIX2) }

private fun <T> toValue(serializer: KSerializer<T>, value: T): Any? = plain(json.encodeToJsonElement(serializer, value))
private fun <T> fromValue(serializer: KSerializer<T>, value: Map<*, *>): T =
    try { json.decodeFromJsonElement(serializer, element(value)) } catch (_: Exception) { throw TailPocketException("invalid-arguments") }

private fun plain(e: JsonElement): Any? = when (e) {
    is JsonNull -> null
    is JsonObject -> e.mapValues { plain(it.value) }
    is JsonArray -> e.map { plain(it) }
    is JsonPrimitive -> if (e.isString) e.content else e.booleanOrNull ?: e.longOrNull ?: e.doubleOrNull
}

private fun element(v: Any?): JsonElement = when (v) {
    null -> JsonNull
    is String -> JsonPrimitive(v)
    is Boolean -> JsonPrimitive(v)
    is Int, is Long, is Short, is Byte -> JsonPrimitive((v as Number).toLong())
    is Number -> v.toDouble().let { if (it == Math.floor(it) && !it.isInfinite() && kotlin.math.abs(it) < 9e15) JsonPrimitive(it.toLong()) else JsonPrimitive(it) }
    is Map<*, *> -> JsonObject(v.entries.associate { it.key.toString() to element(it.value) })
    is List<*> -> JsonArray(v.map { element(it) })
    else -> JsonPrimitive(v.toString())
}
