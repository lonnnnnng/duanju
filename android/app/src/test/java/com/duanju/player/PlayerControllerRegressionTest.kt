package com.duanju.player

import android.app.Activity
import android.app.Application
import android.graphics.Rect
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import androidx.media3.ui.PlayerControlView
import androidx.media3.ui.PlayerView
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
@LooperMode(LooperMode.Mode.PAUSED)
class PlayerControllerRegressionTest {
    @Test fun timeoutHidesTimelineAndButtonsTogetherAtSmallAndFullscreenSizes() {
        val host = Robolectric.buildActivity(Activity::class.java).setup()
        try {
            val activity = host.get()
            activity.setTheme(R.style.Theme_DuanjuPlayer)
            activity.setContentView(R.layout.activity_player)
            val player = activity.findViewById<PlayerView>(R.id.player_view)
            val controller = player.findViewById<PlayerControlView>(androidx.media3.ui.R.id.exo_controller)
            val panel = player.findViewById<ViewGroup>(androidx.media3.ui.R.id.exo_bottom_bar)
            val timeline = player.findViewById<View>(androidx.media3.ui.R.id.exo_progress)
            val tools = player.findViewById<View>(androidx.media3.ui.R.id.exo_basic_controls)
            assertSame(panel, timeline.parent.parent)
            assertSame(panel, tools.parent)
            assertNotNull(panel.background)
            for ((width, height) in listOf(320 to 180, 1080 to 608, 1080 to 2100, 2100 to 1080)) {
                controller.hideImmediately()
                controller.showTimeoutMs = 5000
                controller.show()
                player.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
                    View.MeasureSpec.makeMeasureSpec(height, View.MeasureSpec.EXACTLY))
                player.layout(0, 0, width, height)
                shadowOf(Looper.getMainLooper()).idle()
                assertTrue(timeline.isShown)
                assertTrue(tools.isShown)
                // long: 防止重排或旋转后恢复旧进度条偏移，导致再次占两行或挤到工具按钮下面。
                val timelineBounds = Rect(0, 0, timeline.width, timeline.height)
                panel.offsetDescendantRectToMyCoords(timeline, timelineBounds)
                assertEquals((48 * activity.resources.displayMetrics.density).toInt(), panel.height)
                assertTrue(timelineBounds.width() > 0)
                assertEquals(0, timelineBounds.top)
                assertEquals(panel.height, timelineBounds.bottom)
                assertTrue(timelineBounds.right <= tools.left)
                // long: 布局与尺寸变更可能推进测试时钟，显隐边界从布局完成后的这次显示开始计算。
                controller.hideImmediately()
                controller.show()
                shadowOf(Looper.getMainLooper()).idleFor(4999, TimeUnit.MILLISECONDS)
                assertTrue(timeline.isShown)
                assertTrue(tools.isShown)
                // long: 原有 Media3 在此时只收起按钮、保留进度条两秒；超时边界必须整组消失。
                shadowOf(Looper.getMainLooper()).idleFor(1, TimeUnit.MILLISECONDS)
                assertFalse(timeline.isShown)
                assertFalse(tools.isShown)
                assertFalse(player.findViewById<View>(androidx.media3.ui.R.id.exo_play_pause).isShown)
                controller.show()
                controller.hide()
                assertFalse(timeline.isShown)
                assertFalse(tools.isShown)
            }
        } finally { host.pause().stop().destroy() }
    }
}
