package com.duanju.player.core

import java.io.File
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** long: 只有完整成功且未取消的下载才能成为缓存，native 停止前保留它独占的临时文件。 */
object CacheDownload {
    private val publishLock = Any()

    fun run(
        outFile: File,
        cancellation: TaskCancellation,
        onTempFile: (File) -> Unit = {},
        timeoutMs: Long = TimeUnit.MINUTES.toMillis(10),
        start: (File, (String?) -> Unit) -> (() -> Unit)
    ) {
        cancellation.check()
        val tmp = File.createTempFile(outFile.name + ".", ".tmp", outFile.parentFile)
        val protection = VideoCache.protect(tmp)
        val completed = CountDownLatch(1)
        val abandoned = AtomicBoolean(false)
        var error: String? = null
        var cancelNative: (() -> Unit)? = null
        var published = false
        try {
            onTempFile(tmp)
            cancellation.check()
            cancelNative = start(tmp) { failure ->
                error = failure
                completed.countDown()
                // long: 取消只发出停止信号；必须等 native 真正返回后再删除它仍可能写入的文件。
                if (abandoned.get()) tmp.delete()
                if (abandoned.get()) protection.close()
            }
            cancellation.onCancel(cancelNative).use {
                if (!completed.await(timeoutMs, TimeUnit.MILLISECONDS)) throw IOException("解密超时")
                cancellation.check()
                error?.let { throw IOException(it) }
                if (!tmp.isFile || tmp.length() == 0L) throw IOException("解密结果为空")
                cancellation.commit {
                    synchronized(publishLock) {
                        // long: 同一集即使有两个独立下载，也只能复用完整成品，不能覆盖另一个任务的产物。
                        if (outFile.isFile && outFile.length() > 0L) {
                            tmp.delete()
                        } else if (!tmp.renameTo(outFile)) {
                            throw IOException("缓存写入失败")
                        }
                    }
                }
                published = true
            }
        } finally {
            if (!published) {
                abandoned.set(true)
                cancelNative?.let { runCatching(it) }
                // long: 回调缺失时宁可保留隔离的残片，也不能删除仍由 native 持有的文件。
                if (cancelNative == null || completed.count == 0L) {
                    tmp.delete()
                    protection.close()
                }
            } else {
                protection.close()
            }
        }
    }
}
