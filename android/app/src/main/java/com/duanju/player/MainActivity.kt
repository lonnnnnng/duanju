package com.duanju.player

import android.os.Bundle
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.fragment.app.Fragment

class MainActivity : AppCompatActivity() {

    private var currentTag = "library"

    private data class NavEntry(val row: LinearLayout, val iconId: Int, val labelId: Int)

    private lateinit var navItems: Map<String, NavEntry>

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        navItems = mapOf(
            "library" to NavEntry(findViewById(R.id.nav_item_home), R.id.nav_icon_home, R.id.nav_label_home),
            "history" to NavEntry(findViewById(R.id.nav_item_history), R.id.nav_icon_history, R.id.nav_label_history),
            "settings" to NavEntry(findViewById(R.id.nav_item_settings), R.id.nav_icon_settings, R.id.nav_label_settings)
        )
        navItems.forEach { (tag, entry) ->
            entry.row.setOnClickListener { switchTo(tag) }
        }
        if (savedInstanceState == null) {
            supportFragmentManager.beginTransaction()
                .add(R.id.container, LibraryFragment(), "library")
                .commit()
        } else {
            currentTag = savedInstanceState.getString("tab", "library")
            // FragmentManager 恢复后所有曾添加的 fragment 都处于可见态，重新隐藏非当前页
            supportFragmentManager.fragments.forEach { fragment ->
                if (fragment.tag != currentTag && !fragment.isHidden) {
                    supportFragmentManager.beginTransaction().hide(fragment).commit()
                }
            }
        }
        renderNav(currentTag)
    }

    private fun renderNav(activeTag: String) {
        navItems.forEach { (tag, entry) ->
            val active = tag == activeTag
            entry.row.findViewById<ImageView>(entry.iconId)
                .setColorFilter(ContextCompat.getColor(this, if (active) R.color.nav_icon_active else R.color.nav_icon_inactive))
            entry.row.findViewById<TextView>(entry.labelId).apply {
                setTextColor(ContextCompat.getColor(context, if (active) R.color.app_on_bg else R.color.nav_icon_inactive))
                paint.isFakeBoldText = active
            }
        }
    }

    /**
     * 用 hide/show 切换底部 tab，保留各页面状态：
     * 首页不会每次切回都重新拉接口，历史页返回时再由 onResume 刷新。
     */
    private fun switchTo(tag: String) {
        if (tag == currentTag) return
        val manager = supportFragmentManager
        val target = manager.findFragmentByTag(tag) ?: create(tag)
        val transaction = manager.beginTransaction()
        manager.fragments.forEach { fragment ->
            if (fragment !== target && !fragment.isHidden) transaction.hide(fragment)
        }
        if (target.isAdded) {
            transaction.show(target)
        } else {
            transaction.add(R.id.container, target, tag)
        }
        transaction.commit()
        currentTag = tag
        renderNav(tag)
    }

    private fun create(tag: String): Fragment = when (tag) {
        "history" -> HistoryFragment()
        "settings" -> SettingsFragment()
        else -> LibraryFragment()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString("tab", currentTag)
    }
}
