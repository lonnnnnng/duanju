package com.duanju.player

import android.content.Context
import android.os.Looper
import android.view.View
import android.widget.TextView
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
@LooperMode(LooperMode.Mode.PAUSED)
class HomePaginationRegressionTest {
    @Test fun failedRefreshPreservesCursorAndItsRetryStartsAtFirstPage() {
        val controller = Robolectric.buildActivity(MainActivity::class.java)
        val activity = controller.get()
        val requests = mutableListOf<Pair<Int, String>>()
        activity.supportFragmentManager.registerFragmentLifecycleCallbacks(object : FragmentManager.FragmentLifecycleCallbacks() {
            override fun onFragmentPreAttached(fm: FragmentManager, fragment: Fragment, context: Context) {
                if (fragment is LibraryFragment) fragment.homePageLoader = { _, offset, session, _ ->
                    requests.add(offset to session)
                    if (requests.size == 2) throw java.io.IOException("模拟刷新失败")
                    DirectClient.HomePage(listOf(DirectClient.HomeDrama("$offset", "示例", 10, null, "", 0, null, null, "", 0, "", "", 0)),
                        offset + 9, "session", true, emptyList())
                }
            }
        }, false)
        try {
            controller.setup()
            val fragment = activity.supportFragmentManager.findFragmentByTag("library") as LibraryFragment
            finishQueuedWork(fragment)
            val load = LibraryFragment::class.java.getDeclaredMethod("load", Boolean::class.javaPrimitiveType).apply { isAccessible = true }
            load.invoke(fragment, true)
            finishQueuedWork(fragment)
            assertEquals("刷新失败，点击重试", activity.findViewById<TextView>(R.id.home_pagination_text).text.toString())
            assertEquals(9, LibraryFragment::class.java.getDeclaredField("offset").apply { isAccessible = true }.getInt(fragment))
            activity.findViewById<View>(R.id.home_pagination_status).performClick()
            finishQueuedWork(fragment)
            load.invoke(fragment, false)
            finishQueuedWork(fragment)
            assertEquals(listOf(0 to "", 0 to "", 0 to "", 9 to "session"), requests)
            assertEquals(2, (activity.findViewById<RecyclerView>(R.id.home_grid).adapter as HomeAdapter).size())
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun loadingStaysAboveNavigationAndRetryKeepsTheFailedPageCursor() {
        val controller = Robolectric.buildActivity(MainActivity::class.java)
        val activity = controller.get()
        val requests = mutableListOf<Int>()
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        activity.supportFragmentManager.registerFragmentLifecycleCallbacks(object : FragmentManager.FragmentLifecycleCallbacks() {
            override fun onFragmentPreAttached(fm: FragmentManager, fragment: Fragment, context: Context) {
                if (fragment is LibraryFragment) fragment.homePageLoader = { _, offset, _, _ ->
                    requests.add(offset)
                    if (requests.size == 2) {
                        started.countDown()
                        check(release.await(3, TimeUnit.SECONDS))
                        throw java.io.IOException("模拟分页失败")
                    }
                    val items = (offset until offset + 9).map {
                        DirectClient.HomeDrama("$it", "示例剧$it", 10, null, "", 0, null, null, "", 0, "", "", 0)
                    }
                    DirectClient.HomePage(items, offset + 9, "session", offset == 0, emptyList())
                }
            }
        }, false)
        try {
            controller.setup()
            val fragment = activity.supportFragmentManager.findFragmentByTag("library") as LibraryFragment
            finishQueuedWork(fragment)
            val grid = activity.findViewById<RecyclerView>(R.id.home_grid)
            val status = activity.findViewById<View>(R.id.home_pagination_status)
            val text = activity.findViewById<TextView>(R.id.home_pagination_text)
            assertEquals(9, grid.adapter!!.itemCount)
            LibraryFragment::class.java.getDeclaredMethod("load", Boolean::class.javaPrimitiveType)
                .apply { isAccessible = true }.invoke(fragment, false)
            assertTrue(started.await(3, TimeUnit.SECONDS))
            assertEquals(View.VISIBLE, status.visibility)
            assertFalse(status.isClickable)
            assertEquals("正在加载更多…", text.text.toString())
            // long: 用真实首页和导航一起量位置，防止以后调整菜单高度又把分页反馈盖在下面。
            val decor = activity.window.decorView
            decor.measure(View.MeasureSpec.makeMeasureSpec(393, View.MeasureSpec.EXACTLY),
                View.MeasureSpec.makeMeasureSpec(780, View.MeasureSpec.EXACTLY))
            decor.layout(0, 0, 393, 780)
            val statusLocation = IntArray(2).also { status.getLocationOnScreen(it) }
            val navLocation = IntArray(2).also { activity.findViewById<View>(R.id.bottom_nav).getLocationOnScreen(it) }
            assertTrue(status.height > 0)
            assertTrue(statusLocation[1] + status.height < navLocation[1])

            release.countDown()
            finishQueuedWork(fragment)
            assertTrue(status.isClickable)
            assertEquals("加载失败，点击重试", text.text.toString())
            assertEquals(9, grid.adapter!!.itemCount)
            status.performClick()
            finishQueuedWork(fragment)
            assertEquals(listOf(0, 9, 9), requests)
            assertEquals(View.GONE, status.visibility)
            assertEquals(18, (grid.adapter as HomeAdapter).size())
            assertEquals(19, grid.adapter!!.itemCount)
        } finally {
            release.countDown()
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
