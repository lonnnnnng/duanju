package com.duanju.player

import android.app.Application
import androidx.appcompat.app.AppCompatDelegate

class DuanjuApp : Application() {
    override fun onCreate() {
        super.onCreate()
        applyThemeMode()
    }

    /** 把持久化的主题模式应用到全局 night mode，冷启动与切换时统一走这里。 */
    fun applyThemeMode() {
        AppCompatDelegate.setDefaultNightMode(
            when (AppSettings.themeMode(this)) {
                AppSettings.THEME_DARK -> AppCompatDelegate.MODE_NIGHT_YES
                AppSettings.THEME_LIGHT -> AppCompatDelegate.MODE_NIGHT_NO
                else -> AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM
            }
        )
    }
}
