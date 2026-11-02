package com.duanju.player

import android.content.Context
import android.os.Looper
import android.view.View
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.Shadows.shadowOf
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
@LooperMode(LooperMode.Mode.PAUSED)
class HomeLifecycleRegressionTest {
    @Test
    fun hidingTabAndReturningFromBackgroundDoNotReloadHome() {
        val controller = Robolectric.buildActivity(MainActivity::class.java)
        val activity = controller.get()
        val requests = AtomicInteger()
        // long: 只替换首页请求，保留真实导航与生命周期，以便发现 Tab 切换引起的页面重建。
        activity.supportFragmentManager.registerFragmentLifecycleCallbacks(object : FragmentManager.FragmentLifecycleCallbacks() {
            override fun onFragmentPreAttached(fm: FragmentManager, fragment: Fragment, context: Context) {
                if (fragment is LibraryFragment) fragment.homePageLoader = { _, _, _, _ ->
                    requests.incrementAndGet()
                    val drama = DirectClient.HomeDrama("one", "示例剧", 1, null, "", 0, null, null, "", 0, "", "", 0)
                    DirectClient.HomePage(listOf(drama), 0, "", true, emptyList())
                }
            }
        }, false)

        try {
            controller.setup()
            val fragment = activity.supportFragmentManager.findFragmentByTag("library") as LibraryFragment
            finishQueuedWork(fragment)
            assertEquals(1, requests.get())
            val adapter = fragment.requireView().findViewById<RecyclerView>(R.id.home_grid).adapter

            listOf(R.id.nav_item_history, R.id.nav_item_home, R.id.nav_item_settings, R.id.nav_item_home).forEach { id ->
                activity.findViewById<View>(id).performClick()
                activity.supportFragmentManager.executePendingTransactions()
            }
            controller.pause().stop().start().resume().visible()
            controller.pause().resume()
            finishQueuedWork(fragment)
            assertEquals(1, requests.get())
            assertSame(adapter, fragment.requireView().findViewById<RecyclerView>(R.id.home_grid).adapter)
            assertEquals(1, adapter!!.itemCount)
        } finally {
            controller.pause().stop().destroy()
        }
    }

    private fun finishQueuedWork(fragment: LibraryFragment) {
        val executor = LibraryFragment::class.java.getDeclaredField("executor")
            .apply { isAccessible = true }.get(fragment) as ExecutorService
        executor.submit {}.get(3, TimeUnit.SECONDS)
        shadowOf(Looper.getMainLooper()).idle()
    }
}
