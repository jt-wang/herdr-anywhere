package io.jingtao.tailpocket.ui

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import io.jingtao.tailpocket.TailPocket
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * A camera preview that reports the first QR code containing invite text, once. Asks for the camera
 * permission itself; frames are decoded on the device and never stored.
 */
@Composable
fun TailPocketQrScanner(onInvite: (String) -> Unit, modifier: Modifier = Modifier, prefix: String = TailPocket.DEFAULT_PREFIX) =
    TailPocketQrScanner(prefixes = listOf(prefix), onInvite = onInvite, modifier = modifier)

/** As above, for the first QR code with invite text for any of [prefixes], such as a v1 and a v2 prefix. */
@Composable
fun TailPocketQrScanner(prefixes: List<String>, onInvite: (String) -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    var granted by remember { mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted = it }
    LaunchedEffect(Unit) { if (!granted) ask.launch(Manifest.permission.CAMERA) }
    if (!granted) {
        Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text("Camera access is needed to scan the code.") }
        return
    }
    val owner = LocalLifecycleOwner.current
    val view = remember { PreviewView(context) }
    DisposableEffect(owner) {
        val worker = Executors.newSingleThreadExecutor()
        val reported = AtomicBoolean(false)
        val decoder = QrDecoder()
        val future = ProcessCameraProvider.getInstance(context)
        var provider: ProcessCameraProvider? = null
        future.addListener({
            val cameras = runCatching { future.get() }.getOrNull() ?: return@addListener
            provider = cameras
            val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
            val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
            analysis.setAnalyzer(worker) { image ->
                try {
                    val plane = image.planes.firstOrNull()
                    val text = plane?.let { decoder.decode(it.buffer, image.width, image.height, it.rowStride, it.pixelStride) }
                    val invite = text?.let { TailPocket.inviteText(it, prefixes) }
                    if (invite != null && reported.compareAndSet(false, true)) ContextCompat.getMainExecutor(context).execute { onInvite(invite) }
                } catch (_: Exception) {
                } finally { image.close() }
            }
            val selector = if (cameras.hasCamera(CameraSelector.DEFAULT_BACK_CAMERA)) CameraSelector.DEFAULT_BACK_CAMERA else CameraSelector.DEFAULT_FRONT_CAMERA
            runCatching { cameras.unbindAll(); cameras.bindToLifecycle(owner, selector, preview, analysis) }
        }, ContextCompat.getMainExecutor(context))
        onDispose {
            runCatching { provider?.unbindAll() }
            worker.shutdown()
        }
    }
    AndroidView(factory = { view }, modifier = modifier.fillMaxSize())
}
