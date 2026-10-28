package com.duanju.player.core

import android.content.Context
import java.io.File
import java.util.Locale

/**
 * 播放缓存目录（整集解密后的 mp4）的大小统计与 LRU 清理。
 *
 * 文件按 `${seriesId}_$episode.mp4` 命名，最后修改时间充当"最近访问时间"：
 * 播放命中缓存时调用 [touch] 刷新，清理时从最旧的开始删。
 */
object VideoCache {

    private const val MB = 1024L * 1024L
    private const val GB = 1024L * MB

    fun dir(context: Context): File = File(context.cacheDir, "direct").apply { mkdirs() }

    /** 当前缓存占用（只统计成品 mp4，不含下载中的 .tmp）。 */
    fun sizeBytes(context: Context): Long = sizeBytes(dir(context))

    fun sizeBytes(dir: File): Long = cachedFiles(dir).sumOf { it.length() }

    /** 播放命中缓存时刷新访问时间，保证 LRU 不会先删正在看的剧。 */
    fun touch(file: File) {
        if (file.exists()) runCatching { file.setLastModified(System.currentTimeMillis()) }
    }

    /**
     * 超出上限时按访问时间从旧到新删除，返回释放的字节数。
     * [keepNames] 中的文件永不删除（正在播放的集、刚预取完成的集）。
     */
    fun trim(dir: File, limitBytes: Long, keepNames: Set<String> = emptySet()): Long {
        if (limitBytes <= 0L) return 0L
        val all = cachedFiles(dir)
        var total = all.sumOf { it.length() }
        if (total <= limitBytes) return 0L
        var freed = 0L
        for (file in all.filter { it.name !in keepNames }.sortedBy { it.lastModified() }) {
            if (total <= limitBytes) break
            val length = file.length()
            if (file.delete()) {
                total -= length
                freed += length
            }
        }
        return freed
    }

    /** 清空视频缓存（含半成品 .tmp），返回释放的字节数。CA 证书等辅助文件保留。 */
    fun clear(context: Context): Long {
        val dir = dir(context)
        var freed = 0L
        dir.listFiles()?.forEach { file ->
            if (!file.isFile) return@forEach
            if (!file.name.endsWith(".mp4") && !file.name.endsWith(".tmp")) return@forEach
            val length = file.length()
            if (file.delete()) freed += length
        }
        return freed
    }

    private fun cachedFiles(dir: File): List<File> =
        dir.listFiles()?.filter { it.isFile && it.name.endsWith(".mp4") } ?: emptyList()

    fun format(bytes: Long): String = when {
        bytes >= GB -> String.format(Locale.US, "%.1f GB", bytes.toDouble() / GB)
        bytes >= MB -> String.format(Locale.US, "%.0f MB", bytes.toDouble() / MB)
        else -> String.format(Locale.US, "%.0f KB", bytes.toDouble() / 1024)
    }

    fun mbToBytes(mb: Int): Long = mb.toLong() * MB
}
