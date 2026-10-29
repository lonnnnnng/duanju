package com.duanju.player.core

import android.content.Context
import java.io.File

/**
 * 缓存分集的附加信息（清晰度）。
 *
 * 解密产物只是裸 mp4，文件名里没有清晰度；但状态行要显示「第 6 集 · 1080p · 4.4 MB」，
 * 而为了拿清晰度去调一次 media 接口太浪费（离线时还会失败）。
 * 所以下载完成时把 quality 记在本地，键=文件名，值="quality|字节数"；
 * 字节数对不上说明文件被 LRU 清掉后重新下载过，质量信息作废。
 */
object CacheMeta {

    private fun prefs(context: Context) =
        context.getSharedPreferences("duanju_cache_meta", Context.MODE_PRIVATE)

    fun put(context: Context, file: File, quality: Int) {
        if (!file.exists() || file.length() <= 0L) return
        prefs(context).edit().putString(file.name, "$quality|${file.length()}").apply()
    }

    /** 返回 (清晰度, 字节数)；quality 为 0 表示只知道大小不知道清晰度。文件不存在返回 null。 */
    fun get(context: Context, file: File): Pair<Int, Long>? {
        if (!file.exists() || file.length() <= 0L) return null
        val bytes = file.length()
        val raw = prefs(context).getString(file.name, null) ?: return Pair(0, bytes)
        val quality = raw.substringBefore('|').toIntOrNull() ?: 0
        val recorded = raw.substringAfter('|').toLongOrNull() ?: -1L
        // 文件被替换（重新下载）后旧清晰度不再可信。
        return Pair(if (recorded == bytes) quality else 0, bytes)
    }
}
