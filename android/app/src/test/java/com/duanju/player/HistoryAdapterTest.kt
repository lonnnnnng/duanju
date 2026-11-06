package com.duanju.player

import android.app.Activity
import android.app.Application
import android.os.Looper
import android.view.View
import android.widget.FrameLayout
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
@LooperMode(LooperMode.Mode.PAUSED)
class HistoryAdapterTest {
    @Test fun seriesProgressUsesEpisodeCountEvenAtStartOrEndOfAnEpisode() = withHolder { adapter, holder ->
        for ((position, duration) in listOf(0L to 0L, 90_000L to 90_000L, 10_000L to 90_000L)) {
            adapter.submit(listOf(entry(10, 100).copy(positionMs = position, durationMs = duration)))
            adapter.onBindViewHolder(holder, 0)
            assertEquals(View.VISIBLE, holder.progress.visibility)
            assertEquals(100, holder.progress.max)
            assertEquals(10, holder.progress.progress)
            assertEquals("看到第10集 · 共100集", holder.subtitle.text.toString())
        }
        adapter.submit(listOf(entry(100, 100)))
        adapter.onBindViewHolder(holder, 0)
        assertEquals(View.VISIBLE, holder.progress.visibility)
        assertEquals(holder.progress.max, holder.progress.progress)
    }

    @Test fun recycledCardAndUnknownTotalDoNotRetainAnotherSeriesProgress() = withHolder { adapter, holder ->
        // long: 同一张卡片在布局完成前连续绑定不同剧，旧集数不能在稍后的消息队列中覆盖新进度。
        adapter.submit(listOf(entry(9, 10)))
        adapter.onBindViewHolder(holder, 0)
        adapter.submit(listOf(entry(1, 100)))
        adapter.onBindViewHolder(holder, 0)
        shadowOf(Looper.getMainLooper()).idle()
        assertEquals(100, holder.progress.max)
        assertEquals(1, holder.progress.progress)
        adapter.submit(listOf(entry(2, 0)))
        adapter.onBindViewHolder(holder, 0)
        assertEquals(View.GONE, holder.progress.visibility)
        assertEquals(0, holder.progress.progress)
        adapter.submit(listOf(entry(101, 100)))
        adapter.onBindViewHolder(holder, 0)
        assertEquals(View.VISIBLE, holder.progress.visibility)
        assertEquals(100, holder.progress.progress)
    }

    private fun entry(episode: Int, total: Int) = HistoryEntry("test", "测试剧", total, episode, 0, 0)

    private fun withHolder(check: (HistoryAdapter, HistoryAdapter.Holder) -> Unit) {
        val host = Robolectric.buildActivity(Activity::class.java).setup()
        try {
            val activity = host.get()
            activity.setTheme(R.style.Theme_DuanjuPlayer)
            val parent = FrameLayout(activity)
            activity.setContentView(parent)
            val adapter = HistoryAdapter({}, {})
            val holder = adapter.onCreateViewHolder(parent, 0)
            parent.addView(holder.itemView)
            check(adapter, holder)
        } finally { host.pause().stop().destroy() }
    }
}
