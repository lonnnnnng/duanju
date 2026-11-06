package com.duanju.player

import android.app.Application
import android.content.Context
import android.graphics.Rect
import android.graphics.Canvas
import android.graphics.Bitmap
import android.graphics.drawable.InsetDrawable
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.widget.HorizontalScrollView
import android.widget.TextView
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentManager
import androidx.core.content.ContextCompat
import androidx.core.graphics.ColorUtils
import com.duanju.player.core.DirectClient
import com.google.android.material.bottomsheet.BottomSheetDialog
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.shadows.ShadowDialog
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class, qualifiers = "w393dp-h780dp-port")
@LooperMode(LooperMode.Mode.PAUSED)
class HomeFilterRegressionTest {
    private val campus = DirectClient.HomeFilter("cate_4", "校园")
    private val empress = DirectClient.HomeFilter("cate_378", "女帝")
    private val modern = DirectClient.HomeFilter("cate_757", "现代")
    private val groups = listOf(
        DirectClient.HomeFilterGroup("综合", listOf(DirectClient.HomeFilter("new-general", "综合新增"))),
        DirectClient.HomeFilterGroup("时代背景", listOf(campus)),
        DirectClient.HomeFilterGroup("角色设定", listOf(empress))
    )

    @Test fun fixedEntryStaysVisibleWhileQuickFiltersScroll() = withHome(
        (1..20).map { DirectClient.HomeFilter("quick-$it", "快捷分类$it") }, groups
    ) { activity, _, _ ->
        val decor = activity.window.decorView
        fun layout() {
            decor.measure(View.MeasureSpec.makeMeasureSpec(393, View.MeasureSpec.EXACTLY),
                View.MeasureSpec.makeMeasureSpec(780, View.MeasureSpec.EXACTLY))
            decor.layout(0, 0, 393, 780)
        }
        layout()
        val entry = activity.findViewById<TextView>(R.id.filter_entry)
        val scroll = activity.findViewById<HorizontalScrollView>(R.id.filter_scroll)
        val before = IntArray(2).also { entry.getLocationOnScreen(it) }
        scroll.scrollTo(scroll.getChildAt(0).width, 0)
        layout()
        assertTrue(scroll.scrollX > 0)
        assertArrayEquals(before, IntArray(2).also { entry.getLocationOnScreen(it) })
        val visible = Rect()
        assertTrue(entry.getGlobalVisibleRect(visible))
        assertEquals(entry.width, visible.width())
        assertTrue(entry.height >= 48)
        // long: 点击区域可保持 48dp，但可见背景不能再次铺满它，避免按钮视觉高度回归膨胀。
        entry.draw(Canvas(Bitmap.createBitmap(entry.width, entry.height, Bitmap.Config.ARGB_8888)))
        assertTrue((entry.background as InsetDrawable).drawable!!.bounds.height() <= 32)
    }

    @Test fun fullPanelReplacesSelectionRestoresItsGroupAndClearsToRecommendation() = withHome(listOf(modern), groups) { activity, fragment, requests ->
        val entry = activity.findViewById<TextView>(R.id.filter_entry)
        entry.performClick()
        var sheet = latestSheet()
        assertEquals(listOf("快捷分类", "综合", "时代背景", "角色设定"), groupViews(sheet).map { it.text.toString() })
        // long: 顶部独有项和综合未来新增项都必须在完整面板里可达，不能依赖顶部重复项掩盖遗漏。
        assertNotNull(sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>(modern.id))
        groupViews(sheet).first { it.text == "综合" }.performClick()
        assertNotNull(sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>("new-general"))
        groupViews(sheet).first { it.text == "时代背景" }.performClick()
        sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>(campus.id).performClick()
        finishQueuedWork(fragment)
        assertEquals(Triple(0, "", campus.id), requests.last())
        assertEquals("筛选 · 校园", entry.text.toString())

        entry.performClick()
        sheet = latestSheet()
        assertTrue(groupViews(sheet).first { it.text == "时代背景" }.isSelected)
        assertTrue(sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>(campus.id).isSelected)
        groupViews(sheet).first { it.text == "角色设定" }.performClick()
        sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>(empress.id).performClick()
        finishQueuedWork(fragment)
        assertEquals(Triple(0, "", empress.id), requests.last())
        assertEquals("筛选 · 女帝", entry.text.toString())

        entry.performClick()
        sheet = latestSheet()
        assertTrue(groupViews(sheet).first { it.text == "角色设定" }.isSelected)
        assertEquals("当前：女帝（单选）", sheet.findViewById<TextView>(R.id.filter_current)!!.text.toString())
        val count = requests.size
        sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>(empress.id).performClick()
        finishQueuedWork(fragment)
        assertEquals(count, requests.size)
        entry.performClick()
        latestSheet().findViewById<View>(R.id.filter_reset)!!.performClick()
        finishQueuedWork(fragment)
        assertEquals(Triple(0, "", null), requests.last())
        assertEquals("筛选", entry.text.toString())
        assertFalse(entry.isSelected)
    }

    @Test fun groupsWithoutQuickFiltersRemainAccessible() = withHome(emptyList(), groups) { activity, fragment, requests ->
        val entry = activity.findViewById<TextView>(R.id.filter_entry)
        assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.filter_bar).visibility)
        assertTrue(entry.isEnabled)
        entry.performClick()
        val sheet = latestSheet()
        assertEquals(listOf("综合", "时代背景", "角色设定"), groupViews(sheet).map { it.text.toString() })
        sheet.findViewById<ViewGroup>(R.id.filter_items)!!.findViewWithTag<View>("new-general").performClick()
        finishQueuedWork(fragment)
        assertEquals("new-general", requests.last().third)
    }

    @Test
    @Config(qualifiers = "w320dp-h568dp-port-night")
    fun smallScreenAndLargeFontKeepLongOptionsReadableAndTouchable() {
        RuntimeEnvironment.setFontScale(1.3f)
        try {
            val longItems = listOf("欢喜冤家长名称", "打脸虐渣", "都市爱情").map { DirectClient.HomeFilter(it, it) }
            withHome(emptyList(), listOf(DirectClient.HomeFilterGroup("主题情节", longItems))) { activity, _, _ ->
                activity.findViewById<View>(R.id.filter_entry).performClick()
                val sheet = latestSheet()
                val decor = sheet.window!!.decorView
                decor.measure(View.MeasureSpec.makeMeasureSpec(320, View.MeasureSpec.EXACTLY),
                    View.MeasureSpec.makeMeasureSpec(568, View.MeasureSpec.EXACTLY))
                decor.layout(0, 0, 320, 568)
                val box = sheet.findViewById<ViewGroup>(R.id.filter_items)!!
                assertEquals(2, (box.getChildAt(1) as ViewGroup).childCount)
                val chip = box.findViewWithTag<TextView>(longItems.first().id)
                assertEquals(longItems.first().name, chip.text.toString())
                assertNull(chip.ellipsize)
                assertTrue(chip.height >= (48 * activity.resources.displayMetrics.density).toInt())
            }
        } finally { RuntimeEnvironment.setFontScale(1f) }
    }

    private fun latestSheet(): BottomSheetDialog {
        shadowOf(Looper.getMainLooper()).idle()
        return (ShadowDialog.getLatestDialog() as BottomSheetDialog).also { sheet ->
            assertTrue(sheet.isShowing)
            val container = sheet.findViewById<View>(com.google.android.material.R.id.design_bottom_sheet)!!
            assertTrue(container.clipToOutline)
            assertEquals(0f, container.elevation, 0f)
            val active = groupViews(sheet).single { it.isSelected }
            // long: 直接校验实际控件的文字色，防止浅色分组再次出现白字叠浅底。
            val selectedBackground = ContextCompat.getColor(sheet.context, R.color.home_filter_selected_background)
            assertTrue(ColorUtils.calculateContrast(active.currentTextColor, selectedBackground) >= 4.5)
            val clear = sheet.findViewById<TextView>(R.id.filter_reset)!!
            assertTrue(ColorUtils.calculateContrast(clear.currentTextColor, ContextCompat.getColor(sheet.context, R.color.app_surface)) >= 4.5)
        }
    }

    private fun groupViews(sheet: BottomSheetDialog): List<TextView> {
        val box = sheet.findViewById<ViewGroup>(R.id.filter_groups)!!
        return (0 until box.childCount).map { box.getChildAt(it) as TextView }
    }

    private fun withHome(quick: List<DirectClient.HomeFilter>, groups: List<DirectClient.HomeFilterGroup>,
        check: (MainActivity, LibraryFragment, MutableList<Triple<Int, String, String?>>) -> Unit) {
        val controller = Robolectric.buildActivity(MainActivity::class.java)
        val activity = controller.get().apply { setTheme(R.style.Theme_DuanjuPlayer) }
        val requests = mutableListOf<Triple<Int, String, String?>>()
        activity.supportFragmentManager.registerFragmentLifecycleCallbacks(object : FragmentManager.FragmentLifecycleCallbacks() {
            override fun onFragmentPreAttached(fm: FragmentManager, fragment: Fragment, context: Context) {
                if (fragment is LibraryFragment) fragment.homePageLoader = { _, offset, session, filter ->
                    requests.add(Triple(offset, session, filter))
                    DirectClient.HomePage(emptyList(), offset + 1, "session", true, quick, groups)
                }
            }
        }, false)
        try {
            controller.setup()
            val fragment = activity.supportFragmentManager.findFragmentByTag("library") as LibraryFragment
            finishQueuedWork(fragment)
            check(activity, fragment, requests)
        } finally {
            ShadowDialog.getLatestDialog()?.dismiss()
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
