package io.jingtao.tailpocket.ui

import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import java.nio.ByteBuffer

/** Offline QR decoding of a camera frame's luminance plane (adapted from DSH Mobile, MIT; see NOTICE). */
internal class QrDecoder {
    private val reader = QRCodeReader()
    private val hints = mapOf(DecodeHintType.CHARACTER_SET to "UTF-8", DecodeHintType.TRY_HARDER to true)

    fun decode(buffer: ByteBuffer, width: Int, height: Int, rowStride: Int, pixelStride: Int): String? {
        if (width <= 0 || height <= 0 || width.toLong() * height > 4 * 1024 * 1024 || pixelStride <= 0 ||
            rowStride.toLong() < (width - 1L) * pixelStride + 1) return null
        val source = buffer.duplicate()
        val start = source.position()
        if (start.toLong() + (height - 1L) * rowStride + (width - 1L) * pixelStride >= source.limit()) return null
        val luminance = ByteArray(width * height)
        return try {
            for (y in 0 until height) for (x in 0 until width) luminance[y * width + x] = source.get(start + y * rowStride + x * pixelStride)
            reader.decode(BinaryBitmap(HybridBinarizer(PlanarYUVLuminanceSource(luminance, width, height, 0, 0, width, height, false))), hints).text
        } catch (_: ReaderException) { null } finally { reader.reset() }
    }
}
