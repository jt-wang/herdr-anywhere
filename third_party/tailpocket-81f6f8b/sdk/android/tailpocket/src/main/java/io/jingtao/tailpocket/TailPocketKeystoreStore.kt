package io.jingtao.tailpocket

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Keeps the phone key and the [TailPocketHostLink] encrypted with an AES key held in the Android Keystore
 * (it never leaves the device); the ciphertext lives in private SharedPreferences.
 */
class TailPocketKeystoreStore(context: Context, private val name: String = "tailpocket", private val sdk: TailPocket = TailPocket.default) : TailPocketLinkStore {
    private val prefs = context.applicationContext.getSharedPreferences(name, Context.MODE_PRIVATE)
    private val alias = "$name.aes"

    /** The phone key, created on first use. */
    @Synchronized
    override fun phoneKey(): String {
        read("phone-key")?.takeIf { sdk.publicKey(it) != null }?.let { return it }
        return sdk.newKey().also { write("phone-key", it) }
    }

    override fun saveLink(link: TailPocketHostLink, slot: String) = write("link.$slot", link.encode())

    override fun link(slot: String): TailPocketHostLink? = read("link.$slot")?.let { runCatching { TailPocketHostLink.decode(it) }.getOrNull() }

    override fun deleteLink(slot: String) = prefs.edit().remove("link.$slot").apply()

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(alias, null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256).build())
        }.generateKey()
    }

    private fun write(slot: String, value: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val sealed = cipher.iv + cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        check(prefs.edit().putString(slot, Base64.encodeToString(sealed, Base64.NO_WRAP)).commit()) { "could not save" }
    }

    private fun read(slot: String): String? {
        val sealed = prefs.getString(slot, null)?.let { Base64.decode(it, Base64.NO_WRAP) } ?: return null
        if (sealed.size < 12 + 16) return null
        return runCatching {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, sealed, 0, 12)) }
            String(cipher.doFinal(sealed, 12, sealed.size - 12), Charsets.UTF_8)
        }.getOrNull()
    }
}
