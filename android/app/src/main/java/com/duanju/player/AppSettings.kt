package com.duanju.player

import android.content.Context

object AppSettings {
    private fun prefs(context: Context) = context.getSharedPreferences("duanju", Context.MODE_PRIVATE)

    fun autoNext(context: Context): Boolean = prefs(context).getBoolean("auto_next", true)
    fun setAutoNext(context: Context, value: Boolean) = prefs(context).edit().putBoolean("auto_next", value).apply()

    fun speed(context: Context): Float = prefs(context).getFloat("speed", 1f)
    fun setSpeed(context: Context, value: Float) = prefs(context).edit().putFloat("speed", value).apply()
}