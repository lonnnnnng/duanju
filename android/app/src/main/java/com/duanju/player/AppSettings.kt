package com.duanju.player

import android.content.Context

object AppSettings {
    private fun prefs(context: Context) = context.getSharedPreferences("duanju", Context.MODE_PRIVATE)

    fun autoNext(context: Context): Boolean = prefs(context).getBoolean("auto_next", true)
    fun setAutoNext(context: Context, value: Boolean) = prefs(context).edit().putBoolean("auto_next", value).apply()

    fun speed(context: Context): Float = prefs(context).getFloat("speed", 1f)
    fun setSpeed(context: Context, value: Float) = prefs(context).edit().putFloat("speed", value).apply()

    fun prefetchNext(context: Context): Boolean = prefs(context).getBoolean("prefetch_next", true)
    fun setPrefetchNext(context: Context, value: Boolean) = prefs(context).edit().putBoolean("prefetch_next", value).apply()

    /** 播放缓存上限（MB），默认 2 GB。 */
    fun cacheLimitMb(context: Context): Int = prefs(context).getInt("cache_limit_mb", DEFAULT_CACHE_LIMIT_MB)
    fun setCacheLimitMb(context: Context, value: Int) =
        prefs(context).edit().putInt("cache_limit_mb", value).apply()

    const val DEFAULT_CACHE_LIMIT_MB = 2048
    const val MIN_CACHE_LIMIT_MB = 128
}
