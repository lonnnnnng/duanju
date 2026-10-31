package com.duanju.player.core

import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CancellationException
import java.util.concurrent.atomic.AtomicInteger

class TaskCancellationTest {
    @Test
    fun cancelsNetworkAndNativeSessionOnlyOnce() {
        val task = TaskCancellation()
        val cancelled = AtomicInteger()
        task.onCancel { cancelled.incrementAndGet() }
        task.onCancel { cancelled.incrementAndGet() }
        task.cancel()
        task.cancel()
        assertEquals(2, cancelled.get())
    }

    @Test
    fun sessionRegisteredAfterCancellationIsStillStopped() {
        val task = TaskCancellation()
        task.cancel()
        var cancelled = false
        task.onCancel { cancelled = true }
        assertTrue(cancelled)
    }

    @Test
    fun unregisteringFinishedWorkDoesNotCancelItLater() {
        val task = TaskCancellation()
        var cancelled = false
        task.onCancel { cancelled = true }.close()
        task.cancel()
        assertFalse(cancelled)
    }

    @Test
    fun cancelledWorkCannotPublishAndCannotCancelNextRequest() {
        val old = TaskCancellation()
        val next = TaskCancellation()
        old.cancel()
        assertThrows(CancellationException::class.java) { old.commit { fail("旧请求提交了结果") } }
        assertEquals("new", next.commit { "new" })
    }

    @Test
    fun oneBrokenCleanupDoesNotPreventOtherResourcesBeingCancelled() {
        val task = TaskCancellation()
        var stopped = false
        task.onCancel { throw IllegalStateException("模拟连接已关闭") }
        task.onCancel { stopped = true }
        task.cancel()
        assertTrue(stopped)
    }
}
