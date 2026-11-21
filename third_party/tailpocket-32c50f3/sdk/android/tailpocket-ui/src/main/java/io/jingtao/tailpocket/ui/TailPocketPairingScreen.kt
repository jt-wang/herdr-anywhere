package io.jingtao.tailpocket.ui

import android.os.Build
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.jingtao.tailpocket.TailPocketHostLink
import io.jingtao.tailpocket.TailPocketInvite
import io.jingtao.tailpocket.TailPocketNearbyHost
import io.jingtao.tailpocket.TailPocketNearbyHosts
import io.jingtao.tailpocket.TailPocket
import io.jingtao.tailpocket.TailPocketException
import kotlinx.coroutines.launch

/**
 * A ready-made pairing screen: computers on this Wi-Fi (when [service] is set), a QR scanner and a paste
 * field. It returns the [TailPocketHostLink] through [onPaired]; storing it and your app's look stay yours. Build
 * your own from [TailPocketNearbyHosts], [TailPocketQrScanner] and [TailPocket] if this one does not fit.
 */
@Composable
fun TailPocketPairingScreen(
    phoneKey: String,
    onPaired: (TailPocketHostLink) -> Unit,
    modifier: Modifier = Modifier,
    service: String? = null,
    prefix: String = TailPocket.DEFAULT_PREFIX,
    label: String = "",
    device: String = Build.MODEL,
    sdk: TailPocket = TailPocket.default,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var hosts by remember { mutableStateOf(emptyList<TailPocketNearbyHost>()) }
    var scanning by remember { mutableStateOf(false) }
    var pasted by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<String?>(null) }
    var code by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val browser = remember(service) { service?.let { TailPocketNearbyHosts(context, it) } }
    DisposableEffect(browser) {
        browser?.start(onChange = { hosts = it })
        onDispose { browser?.stop() }
    }

    fun message(e: Exception) = when ((e as? TailPocketException)?.code) {
        "expired" -> "This pairing code has expired. Show a new one on the computer."
        "taken" -> "Another phone already used this code."
        "denied" -> "The computer did not approve this phone."
        "unreachable", "timeout" -> "Could not reach the computer. Check that it is online."
        "invalid-invite" -> "That is not a pairing code."
        else -> "Pairing failed."
    }

    suspend fun finish(invite: TailPocketInvite) {
        status = "Pairing with ${invite.name ?: "the computer"}…"
        onPaired(sdk.pair(invite, phoneKey, device))
        status = null
    }

    fun run(block: suspend () -> Unit) = scope.launch {
        busy = true
        try { block() } catch (e: Exception) { status = message(e) } finally { busy = false; code = null }
    }

    if (scanning) {
        TailPocketQrScanner(prefix = prefix, onInvite = { text -> scanning = false; run { finish(sdk.decodeInvite(text, prefix)) } }, modifier = modifier)
        return
    }
    Column(modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        code?.let {
            Text("Confirm on the computer", style = MaterialTheme.typography.titleMedium)
            Text(it, fontSize = 40.sp, fontFamily = FontFamily.Monospace)
            Text("Check that the computer shows the same code, then approve it there.", style = MaterialTheme.typography.bodySmall)
        }
        status?.let { Text(it) }
        if (busy) CircularProgressIndicator()
        if (browser != null) {
            Text("On this Wi-Fi", style = MaterialTheme.typography.titleSmall)
            if (hosts.isEmpty()) Text("Looking for computers…", style = MaterialTheme.typography.bodySmall)
            LazyColumn(Modifier.fillMaxWidth().height(160.dp)) {
                items(hosts, key = { it.name }) { host ->
                    Text(host.name, Modifier.fillMaxWidth().clickable(enabled = !busy) {
                        run {
                            status = "Connecting to ${host.name}…"
                            val pending = sdk.beginLocalPairing(browser.resolve(host), device, label)
                            code = pending.code
                            status = null
                            finish(pending.finish())
                        }
                    }.padding(vertical = 12.dp))
                }
            }
        }
        Button(onClick = { scanning = true }, enabled = !busy) { Text("Scan QR code") }
        OutlinedTextField(value = pasted, onValueChange = { pasted = it }, label = { Text("Or paste the code") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Button(onClick = {
            val text = TailPocket.inviteText(pasted, prefix)
            if (text == null) status = "That is not a pairing code." else run { finish(sdk.decodeInvite(text, prefix)) }
        }, enabled = !busy && pasted.isNotBlank()) { Text("Pair") }
    }
}
