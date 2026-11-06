package com.duanju.player

import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.os.Looper
import android.view.View
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageView
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import com.duanju.player.core.SeriesAggregator
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
@LooperMode(LooperMode.Mode.PAUSED)
class AsyncPageRegressionTest {
    @Test fun recreationRestoresSubmittedQueryAndResultsInsteadOfHistory() {
        val original = Robolectric.buildActivity(SearchActivity::class.java).setup()
        val saved = android.os.Bundle()
        try {
            val activity = original.get()
            activity.searchItems = { _, _ -> listOf(drama("first")) }
            activity.findViewById<EditText>(R.id.search_input).setText("示例")
            activity.findViewById<View>(R.id.search_go).performClick()
            finishQueuedWork(activity)
            original.saveInstanceState(saved)
        } finally { original.pause().stop().destroy() }
        val restored = Robolectric.buildActivity(SearchActivity::class.java)
        val queries = mutableListOf<String>()
        restored.get().searchItems = { keyword, _ -> queries.add(keyword); listOf(drama("restored")) }
        try {
            restored.create(saved).start().restoreInstanceState(saved).resume().visible()
            val activity = restored.get()
            finishQueuedWork(activity)
            assertEquals(listOf("示例"), queries)
            assertEquals("示例", activity.findViewById<EditText>(R.id.search_input).text.toString())
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.search_list).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_suggest).visibility)
        } finally { restored.pause().stop().destroy() }
    }

    @Test fun failedSearchCanRetrySameKeywordAndClearingRestoresInputFocus() {
        val controller = Robolectric.buildActivity(SearchActivity::class.java).setup()
        val activity = controller.get()
        var requests = 0
        activity.searchItems = { _, _ ->
            if (++requests == 1) throw java.io.IOException("模拟网络失败")
            listOf(drama("recovered"))
        }
        try {
            val input = activity.findViewById<EditText>(R.id.search_input)
            input.setText("示例")
            activity.findViewById<View>(R.id.search_go).performClick()
            finishQueuedWork(activity)
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.search_retry).visibility)
            activity.findViewById<View>(R.id.search_retry).performClick()
            finishQueuedWork(activity)
            assertEquals(2, requests)
            assertEquals(1, activity.findViewById<RecyclerView>(R.id.search_list).adapter!!.itemCount)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_retry).visibility)
            activity.findViewById<View>(R.id.search_clear).performClick()
            shadowOf(Looper.getMainLooper()).idle()
            assertTrue(input.hasFocus())
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.search_suggest).visibility)
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun relatedFailureIsRetryableInsteadOfCachedAsEmpty() {
        val intent = Intent(RuntimeEnvironment.getApplication(), DetailActivity::class.java)
            .putExtra("dramaId", "series").putExtra("title", "示例剧")
        val controller = Robolectric.buildActivity(DetailActivity::class.java, intent)
        val activity = controller.get()
        activity.loadDetail = { DirectClient.SeriesDetail("series", "示例剧", null, 1, 0, 1, "", emptyList(), "",
            listOf(DirectClient.SeriesEpisode(1, "vid", 60, false))) }
        var requests = 0
        activity.loadSeasons = {
            if (++requests == 1) throw java.io.IOException("模拟关联失败")
            listOf(SeriesAggregator.Season("second", "示例剧第二季", "示例剧", 2, 2, "短剧", true, null))
        }
        fun awaitRelated() {
            val thread = DetailActivity::class.java.getDeclaredField("relatedThread").apply { isAccessible = true }.get(activity) as Thread
            thread.join(2000)
            assertFalse(thread.isAlive)
            shadowOf(Looper.getMainLooper()).idle()
        }
        try {
            controller.setup()
            finishQueuedWork(activity)
            activity.findViewById<View>(R.id.detail_tab_related).performClick()
            awaitRelated()
            val status = activity.findViewById<android.widget.TextView>(R.id.detail_related_status)
            assertTrue(status.isClickable)
            assertEquals("加载失败，点击重试", status.text.toString())
            status.performClick()
            awaitRelated()
            assertEquals(2, requests)
            assertEquals(1, activity.findViewById<RecyclerView>(R.id.detail_related).adapter!!.itemCount)
            assertEquals(View.GONE, status.visibility)
        } finally { controller.pause().stop().destroy() }
    }

    @Test
    fun emptyCoverClearsImageWhenRelatedHolderIsReused() {
        val controller = Robolectric.buildActivity(SearchActivity::class.java).setup()
        try {
            val activity = controller.get()
            val adapter = RelatedAdapter("series") {}
            adapter.submit(listOf(SeriesAggregator.Season("second", "第二季", "示例剧", 2, 1, "短剧", true, null)))
            val holder = adapter.onCreateViewHolder(FrameLayout(activity), 0)
            holder.cover.setImageDrawable(ColorDrawable(Color.RED))
            adapter.onBindViewHolder(holder, 0)
            shadowOf(Looper.getMainLooper()).idle()
            assertNull(holder.cover.drawable)
        } finally { controller.pause().stop().destroy() }
    }

    @Test
    fun blankCoverAlsoClearsExistingImage() {
        val controller = Robolectric.buildActivity(SearchActivity::class.java).setup()
        try {
            val image = ImageView(controller.get())
            image.setImageDrawable(ColorDrawable(Color.RED))
            BitmapLoader.load("  ", image)
            shadowOf(Looper.getMainLooper()).idle()
            assertNull(image.drawable)
        } finally { controller.pause().stop().destroy() }
    }

    @Test
    fun clearingInputWhileOldSearchFinishesKeepsHistoryVisible() {
        val controller = Robolectric.buildActivity(SearchActivity::class.java).setup()
        val activity = controller.get()
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        activity.searchItems = { _, _ ->
            started.countDown()
            // long: 故意模拟不响应取消的旧服务器，验证界面仍会拒绝迟到结果。
            waitIgnoringInterrupt(release)
            listOf(drama("old"))
        }
        try {
            activity.findViewById<EditText>(R.id.search_input).setText("A")
            activity.findViewById<View>(R.id.search_go).performClick()
            assertTrue(started.await(2, TimeUnit.SECONDS))
            activity.findViewById<View>(R.id.search_clear).performClick()
            release.countDown()
            finishQueuedWork(activity)
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.search_suggest).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_list).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_loading).visibility)
            assertEquals(0, activity.findViewById<RecyclerView>(R.id.search_list).adapter!!.itemCount)
        } finally { release.countDown(); controller.pause().stop().destroy() }
    }

    @Test
    fun replacingKeywordDoesNotDropNewSearchOrDisplayOldResults() {
        val controller = Robolectric.buildActivity(SearchActivity::class.java).setup()
        val activity = controller.get()
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val queries = java.util.Collections.synchronizedList(mutableListOf<String>())
        activity.searchItems = { keyword, _ ->
            queries.add(keyword)
            if (keyword == "A") { started.countDown(); waitIgnoringInterrupt(release) }
            if (keyword == "A") listOf(drama("old")) else emptyList()
        }
        try {
            val input = activity.findViewById<EditText>(R.id.search_input)
            input.setText("A")
            activity.findViewById<View>(R.id.search_go).performClick()
            assertTrue(started.await(2, TimeUnit.SECONDS))
            input.setText("B")
            activity.findViewById<View>(R.id.search_go).performClick()
            release.countDown()
            finishQueuedWork(activity)
            assertEquals(listOf("A", "B"), queries)
            assertEquals("B", input.text.toString())
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.search_empty).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_list).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.search_loading).visibility)
            assertEquals(0, activity.findViewById<RecyclerView>(R.id.search_list).adapter!!.itemCount)
        } finally { release.countDown(); controller.pause().stop().destroy() }
    }

    @Test
    fun relatedResponseCannotReopenTabAfterUserReturnedToEpisodes() {
        val intent = Intent(RuntimeEnvironment.getApplication(), DetailActivity::class.java)
            .putExtra("dramaId", "series").putExtra("title", "示例剧")
        val controller = Robolectric.buildActivity(DetailActivity::class.java, intent)
        val activity = controller.get()
        activity.loadDetail = { DirectClient.SeriesDetail(
            "series", "示例剧", null, 1, 0, 1, "", emptyList(), "",
            listOf(DirectClient.SeriesEpisode(1, "vid", 60, false))
        ) }
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        activity.loadSeasons = {
            started.countDown()
            waitIgnoringInterrupt(release)
            listOf(SeriesAggregator.Season("second", "示例剧第二季", "示例剧", 2, 2, "短剧", true, null))
        }
        try {
            controller.setup()
            finishQueuedWork(activity)
            activity.findViewById<View>(R.id.detail_tab_related).performClick()
            assertTrue(started.await(2, TimeUnit.SECONDS))
            activity.findViewById<View>(R.id.detail_tab_episodes).performClick()
            release.countDown()
            val thread = DetailActivity::class.java.getDeclaredField("relatedThread").apply { isAccessible = true }.get(activity) as Thread
            thread.join(2000)
            assertFalse(thread.isAlive)
            shadowOf(Looper.getMainLooper()).idle()
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.detail_episodes).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.detail_related).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.detail_related_status).visibility)
            activity.findViewById<View>(R.id.detail_tab_related).performClick()
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.detail_related).visibility)
        } finally { release.countDown(); controller.pause().stop().destroy() }
    }

    @Test
    fun footerVisibilityChangesProduceStructuralAdapterNotifications() {
        val adapter = HomeAdapter {}
        adapter.submit(listOf(drama("one")))
        val events = mutableListOf<String>()
        adapter.registerAdapterDataObserver(object : RecyclerView.AdapterDataObserver() {
            override fun onItemRangeInserted(positionStart: Int, itemCount: Int) { events.add("insert:$positionStart:$itemCount") }
            override fun onItemRangeRemoved(positionStart: Int, itemCount: Int) { events.add("remove:$positionStart:$itemCount") }
            override fun onItemRangeChanged(positionStart: Int, itemCount: Int, payload: Any?) { events.add("change:$positionStart:$itemCount") }
        })
        adapter.setFooter(true)
        assertEquals(2, adapter.itemCount)
        adapter.setFooter(true)
        assertEquals(2, adapter.itemCount)
        adapter.setFooter(false)
        assertEquals(1, adapter.itemCount)
        assertEquals(listOf("insert:1:1", "remove:1:1"), events)
    }

    private fun finishQueuedWork(activity: Any) {
        val executor = activity.javaClass.getDeclaredField("executor").apply { isAccessible = true }.get(activity) as ExecutorService
        executor.submit {}.get(3, TimeUnit.SECONDS)
        shadowOf(Looper.getMainLooper()).idle()
    }

    private fun waitIgnoringInterrupt(latch: CountDownLatch) {
        while (true) {
            try { check(latch.await(3, TimeUnit.SECONDS)); return } catch (_: InterruptedException) { }
        }
    }

    private fun drama(id: String) = DirectClient.HomeDrama(
        id, id, 1, null, "", 0, null, null, "", 0, "", "", 0
    )
}
