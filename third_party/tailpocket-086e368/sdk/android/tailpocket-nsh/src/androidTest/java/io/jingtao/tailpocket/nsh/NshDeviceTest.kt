package io.jingtao.tailpocket.nsh

import android.app.Activity
import android.view.accessibility.AccessibilityNodeInfo
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.longbridge.nsh.NshView
import io.jingtao.tailpocket.TailPocketKeystoreStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.UUID

class NshTestActivity : Activity()

/**
 * The example NSH app (examples/nsh-phone) in an NshView with the bridge attached: it pairs with a
 * real computer over Tailcat and shows what the computer answered. Run through
 * scripts/e2e-nsh-android.sh, which starts the computer side and passes the invite.
 */
@RunWith(AndroidJUnit4::class)
class NshDeviceTest {
    @Test
    fun theNshAppPairsAndTalksToTheComputer() {
        val invite = InstrumentationRegistry.getArguments().getString("tailPocketNshInvite")
        assumeTrue("run through scripts/e2e-nsh-android.sh", invite != null)
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val store = TailPocketKeystoreStore(context)
        store.deleteLink()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        val errors = mutableListOf<String>()
        ActivityScenario.launch(NshTestActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                val view = NshView(activity)
                activity.setContentView(view)
                view.setErrorListener { errors.add(it) }
                view.setStorageDirectory(File(activity.cacheDir, "nsh-${UUID.randomUUID()}").apply { mkdirs() })
                view.setParams(mapOf("invite" to invite))
                TailPocketNsh.attach(view, scope)
                assertTrue("bundle loads", view.loadAssetBundle("nsh-phone"))
            }
            var texts = emptyList<String>()
            val deadline = System.currentTimeMillis() + 90_000
            while (System.currentTimeMillis() < deadline) {
                texts = texts(InstrumentationRegistry.getInstrumentation().uiAutomation.rootInActiveWindow)
                if (texts.any { "hello from the computer" in it }) break
                Thread.sleep(500)
            }
            assertTrue("on screen: $texts, errors: $errors", texts.any { "hello from the computer" in it })
            assertNotNull("the app saved the computer", store.link())
        }
        scope.cancel()
        store.deleteLink()
    }

    /** What the screen shows, read the way a screen reader does (NSH draws its own text views). */
    private fun texts(node: AccessibilityNodeInfo?): List<String> {
        if (node == null) return emptyList()
        val out = mutableListOf<String>()
        node.text?.let { if (it.isNotEmpty()) out.add(it.toString()) }
        node.contentDescription?.let { if (it.isNotEmpty()) out.add(it.toString()) }
        for (i in 0 until node.childCount) out += texts(node.getChild(i))
        return out
    }
}
