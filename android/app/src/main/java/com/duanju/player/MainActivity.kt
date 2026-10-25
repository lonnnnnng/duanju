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
            show(LibraryFragment(), "library")
        }
        findViewById<BottomNavigationView>(R.id.bottom_nav).setOnItemSelectedListener { item ->
            when (item.itemId) {
                R.id.nav_library -> show(LibraryFragment(), "library")
                R.id.nav_history -> show(HistoryFragment(), "history")
                R.id.nav_settings -> show(SettingsFragment(), "settings")
            }
            true
        }
    }

    private fun show(fragment: Fragment, tag: String) {
        if (tag == currentTag) {
            supportFragmentManager.popBackStack(null, androidx.fragment.app.FragmentManager.POP_BACK_STACK_INCLUSIVE)
        }
        currentTag = tag
        supportFragmentManager.beginTransaction()
            .replace(R.id.container, fragment, tag)
            .commit()
    }
}