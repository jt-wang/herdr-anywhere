package io.jingtao.tailpocket

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.net.HttpURLConnection
import java.net.URL

/**
 * Runs on a device or emulator. The host side is started by scripts/e2e-android.sh, which passes
 * `tailPocketInvite` (QR text) or `tailPocketLocal` ("ip:port" of the same-Wi-Fi pairing service).
 */
@RunWith(AndroidJUnit4::class)
class DeviceTest {
    private val args = InstrumentationRegistry.getArguments()
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val sdk = TailPocket.default

    @Test fun keystoreStoreKeepsTheKeyAndLink() {
        val store = TailPocketKeystoreStore(context, "tailPocket-test")
        val key = store.phoneKey()
        assertEquals(key, store.phoneKey())
        assertNotNull(sdk.publicKey(key))
        val link = TailPocketHostLink("tcX", 48931, "Mac", "p", key)
        store.saveLink(link)
        assertEquals(link, TailPocketKeystoreStore(context, "tailPocket-test").link())
        store.deleteLink()
        assertEquals(null, store.link())
    }

    @Test fun pairByQrAndFetchOverTheTunnel() = runBlocking {
        val qr = args.getString("tailPocketInvite")
        assumeTrue("run through scripts/e2e-android.sh", qr != null)
        val link = sdk.pair(sdk.decodeInvite(qr!!), sdk.newKey(), "Android emulator")
        assertEquals("welcome", link.payload)
        assertEquals("hello from the computer\n", fetchHello(link))
    }

    @Test fun pairOverSameWiFiAndFetch() = runBlocking {
        val address = args.getString("tailPocketLocal")
        assumeTrue("run through scripts/e2e-android.sh", address != null)
        val pending = sdk.beginLocalPairing(address!!, "Android emulator (Wi-Fi)")
        assertEquals(6, pending.code.length)
        val invite = pending.finish()
        val link = sdk.pair(invite, sdk.newKey(), "Android emulator (Wi-Fi)")
        assertEquals("hello from the computer\n", fetchHello(link))
    }

    /** The function table a JavaScript bridge (NSH, React TailPocketNative) forwards to, on a real device. */
    @Test fun bridgePairsConnectsAndStores() = runBlocking {
        val qr = args.getString("tailPocketBridgeInvite")
        assumeTrue("run through scripts/e2e-android.sh", qr != null)
        val events = mutableListOf<String>()
        val bridge = TailPocketBridge.create(context, emit = { name, _ -> events += name })
        @Suppress("UNCHECKED_CAST")
        val link = bridge.call("tailPocket.pair", mapOf("invite" to qr, "device" to "Android bridge", "save" to true, "slot" to "bridge-test")) as Map<String, Any?>
        assertEquals("welcome", link["payload"])
        @Suppress("UNCHECKED_CAST")
        val c = bridge.call("tailPocket.connect", mapOf("slot" to "bridge-test")) as Map<String, Any?>
        val body = withContext(Dispatchers.IO) {
            var out = ""
            repeat(6) {
                if (out.isEmpty()) out = runCatching {
                    (URL("${c["baseUrl"]}/hello.txt").openConnection() as HttpURLConnection).apply { connectTimeout = 20_000; readTimeout = 20_000 }
                        .inputStream.bufferedReader().use { it.readText() }
                }.getOrDefault("")
            }
            out
        }
        assertEquals("hello from the computer\n", body)
        bridge.call("tailPocket.disconnect", mapOf("handle" to c["handle"]))
        bridge.call("tailPocket.store.deleteLink", mapOf("slot" to "bridge-test"))
        bridge.close()
    }

    private suspend fun fetchHello(link: TailPocketHostLink): String = withContext(Dispatchers.IO) {
        sdk.connect(link).use { connection ->
            var last: Exception? = null
            repeat(6) {
                try {
                    val http = URL("${connection.baseUrl}/hello.txt").openConnection() as HttpURLConnection
                    http.connectTimeout = 20_000
                    http.readTimeout = 20_000
                    return@withContext http.inputStream.bufferedReader().use { it.readText() }
                } catch (e: Exception) { last = e }
            }
            throw last!!
        }
    }
}
