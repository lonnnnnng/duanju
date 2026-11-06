package com.duanju.player

import android.content.Context
import android.content.Intent
import android.os.Looper
import android.view.View
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
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
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
@LooperMode(LooperMode.Mode.PAUSED)
class UiInteractionRegressionTest {
    @Test fun cacheLimitRejectsNonFiniteOverflowAndTooSmallValues() {
        for (value in listOf("", "0", "-1", "0.124", "NaN", "Infinity", "1e300", "abc")) {
            assertNull(value, AppSettings.parseCacheLimitGb(value))
        }
        assertEquals(128, AppSettings.parseCacheLimitGb("0.125"))
        assertEquals(2048, AppSettings.parseCacheLimitGb(" 2 "))
        assertEquals(1536, AppSettings.parseCacheLimitGb("1.5"))
    }

    @Test fun invalidCacheInputStaysOpenAndKeepsExistingLimit() {
        val controller = mainHost()
        try {
            val activity = controller.get()
            activity.findViewById<View>(R.id.nav_item_settings).performClick()
            activity.supportFragmentManager.executePendingTransactions()
            val fragment = activity.supportFragmentManager.findFragmentByTag("settings") as SettingsFragment
            val original = AppSettings.cacheLimitMb(activity)
            SettingsFragment::class.java.getDeclaredMethod("inputCacheLimit").apply { isAccessible = true }.invoke(fragment)
            shadowOf(Looper.getMainLooper()).idle()
            val dialog = ShadowDialog.getLatestDialog() as AlertDialog
            val input = findInput(dialog.window!!.decorView)!!
            input.setText("0.1")
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
            assertTrue(dialog.isShowing)
            val field = generateSequence(input as View) { it.parent as? View }
                .filterIsInstance<com.google.android.material.textfield.TextInputLayout>().first()
            assertNotNull(field.error)
            assertEquals("0.1", input.text.toString())
            assertEquals(original, AppSettings.cacheLimitMb(activity))
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun updateCheckDoesNotDuplicateOrShowDialogOverAnotherTab() {
        val controller = mainHost()
        val release = CountDownLatch(1)
        val started = CountDownLatch(1)
        var calls = 0
        try {
            val activity = controller.get()
            activity.findViewById<View>(R.id.nav_item_settings).performClick()
            activity.supportFragmentManager.executePendingTransactions()
            val settings = activity.supportFragmentManager.findFragmentByTag("settings") as SettingsFragment
            settings.fetchUpdate = { calls++; started.countDown(); check(release.await(3, TimeUnit.SECONDS)); null }
            val row = activity.findViewById<View>(R.id.row_version)
            row.performClick()
            assertTrue(started.await(2, TimeUnit.SECONDS))
            row.performClick()
            activity.findViewById<View>(R.id.nav_item_home).performClick()
            activity.supportFragmentManager.executePendingTransactions()
            release.countDown()
            val thread = SettingsFragment::class.java.getDeclaredField("updateThread").apply { isAccessible = true }.get(settings) as Thread
            thread.join(2000)
            shadowOf(Looper.getMainLooper()).idle()
            assertEquals(1, calls)
            assertTrue(row.isEnabled)
            assertNull(ShadowDialog.getLatestDialog())
        } finally { release.countDown(); controller.pause().stop().destroy() }
    }

    @Test fun playbackRetryKeepsEpisodeAndGroupSelectionUsesCurrentEpisode() {
        val intent = Intent(RuntimeEnvironment.getApplication(), PlayerActivity::class.java)
            .putExtra("dramaId", "fixture").putExtra("title", "测试剧").putExtra("episodes", 60)
        val controller = Robolectric.buildActivity(PlayerActivity::class.java, intent)
        val activity = controller.get()
        val requests = mutableListOf<Int>()
        activity.prepareEpisode = { episode, _, _, _ -> requests.add(episode); throw java.io.IOException("空响应（HTTP 200）") }
        try {
            controller.setup()
            finishPlayback(activity)
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.playback_error).visibility)
            assertEquals(View.GONE, activity.findViewById<View>(R.id.loading).visibility)
            activity.findViewById<View>(R.id.playback_retry).performClick()
            finishPlayback(activity)
            assertEquals(listOf(1, 1), requests)
            PlayerActivity::class.java.getDeclaredMethod("play", Int::class.javaPrimitiveType, Long::class.javaPrimitiveType)
                .apply { isAccessible = true }.invoke(activity, 8, 0L)
            finishPlayback(activity)
            val groups = activity.findViewById<LinearLayout>(R.id.episode_groups)
            groups.getChildAt(1).performClick()
            groups.getChildAt(0).performClick()
            assertEquals((13 * activity.resources.displayMetrics.density).toInt(), groups.getChildAt(0).paddingLeft)
            val adapter = activity.findViewById<RecyclerView>(R.id.episode_list).adapter as EpisodeAdapter
            val holder = adapter.onCreateViewHolder(FrameLayout(activity), 0)
            adapter.onBindViewHolder(holder, 7)
            assertTrue(holder.itemView.isSelected)
            assertEquals(3, requests.size)
            activity.findViewById<View>(R.id.custom_fullscreen).performClick()
            assertEquals(View.GONE, activity.findViewById<View>(R.id.player_top_bar).visibility)
            activity.onBackPressedDispatcher.onBackPressed()
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.player_top_bar).visibility)
            assertFalse(activity.isFinishing)
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun episodeGridLeavesRoomAtSmallWidthAndLargeFont() {
        assertEquals(6, EpisodeGroups.columns(393, 1f))
        assertEquals(5, EpisodeGroups.columns(320, 1f))
        assertEquals(4, EpisodeGroups.columns(320, 1.3f))
    }

    @Test fun emptyHistoryProvidesReturnToHomeAndUpdatesNavigationSelection() {
        val controller = mainHost()
        try {
            val activity = controller.get()
            activity.findViewById<View>(R.id.nav_item_history).performClick()
            activity.supportFragmentManager.executePendingTransactions()
            assertEquals(View.VISIBLE, activity.findViewById<View>(R.id.history_empty).visibility)
            activity.findViewById<View>(R.id.history_home).performClick()
            activity.supportFragmentManager.executePendingTransactions()
            assertTrue(activity.findViewById<View>(R.id.nav_item_home).isSelected)
            assertFalse(activity.findViewById<View>(R.id.nav_item_history).isSelected)
            assertTrue(activity.supportFragmentManager.findFragmentByTag("history")!!.isHidden)
        } finally { controller.pause().stop().destroy() }
    }

    private fun mainHost(): org.robolectric.android.controller.ActivityController<MainActivity> {
        val controller = Robolectric.buildActivity(MainActivity::class.java)
        controller.get().supportFragmentManager.registerFragmentLifecycleCallbacks(object : FragmentManager.FragmentLifecycleCallbacks() {
            override fun onFragmentPreAttached(fm: FragmentManager, fragment: Fragment, context: Context) {
                if (fragment is LibraryFragment) fragment.homePageLoader = { _, _, _, _ -> DirectClient.HomePage(emptyList(), 0, "", false, emptyList()) }
            }
        }, false)
        controller.setup()
        return controller
    }

    private fun finishPlayback(activity: PlayerActivity) {
        val executor = PlayerActivity::class.java.getDeclaredField("executor").apply { isAccessible = true }.get(activity) as ExecutorService
        executor.submit {}.get(3, TimeUnit.SECONDS)
        shadowOf(Looper.getMainLooper()).idle()
    }

    private fun findInput(view: View): EditText? {
        if (view is EditText) return view
        if (view is android.view.ViewGroup) for (i in 0 until view.childCount) findInput(view.getChildAt(i))?.let { return it }
        return null
    }
}
