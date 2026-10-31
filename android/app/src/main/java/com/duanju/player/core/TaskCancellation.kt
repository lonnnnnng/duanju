package com.duanju.player.core

import java.io.Closeable
import java.util.concurrent.CancellationException

/** long: 一个播放或搜索请求共同拥有网络连接与 native 会话，取消后禁止再提交结果。 */
class TaskCancellation {
    private val lock = Any()
    private val listeners = LinkedHashSet<() -> Unit>()
    @Volatile var isCancelled = false
        private set

    fun check() {
        if (isCancelled || Thread.currentThread().isInterrupted) throw CancellationException("任务已取消")
    }

    fun onCancel(action: () -> Unit): Closeable {
        val invokeNow = synchronized(lock) {
            if (isCancelled) true else { listeners.add(action); false }
        }
        // long: native 会话可能在用户取消之后才返回 ID，此时也必须立即停止它。
        if (invokeNow) runCatching(action)
        return Closeable { synchronized(lock) { listeners.remove(action) } }
    }

    fun cancel() {
        val actions = synchronized(lock) {
            if (isCancelled) return
            isCancelled = true
            listeners.toList().also { listeners.clear() }
        }
        actions.forEach { runCatching(it) }
    }

    fun <T> commit(action: () -> T): T = synchronized(lock) {
        // long: 与取消共用锁，避免“检查仍有效”后旧下载才把文件发布成可播放缓存。
        check()
        action()
    }
}
