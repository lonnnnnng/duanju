package com.duanju.player

import android.os.Bundle
import androidx.appcompat.app.AppCompatActivity
import androidx.fragment.app.Fragment
import com.google.android.material.bottomnavigation.BottomNavigationView

class MainActivity : AppCompatActivity() {

    private var currentTag = "library"

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        if (savedInstanceState == null) {
            supportFragmentManager.beginTransaction()
                .add(R.id.container, LibraryFragment(), "library")
                .commit()
        } else {
            currentTag = savedInstanceState.getString("tab", "library")
        }
        findViewById<BottomNavigationView>(R.id.bottom_nav).setOnItemSelectedListener { item ->
            when (item.itemId) {
                R.id.nav_library -> switchTo("library")
                R.id.nav_history -> switchTo("history")
                R.id.nav_settings -> switchTo("settings")
            }
            true
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
