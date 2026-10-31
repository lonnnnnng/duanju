package com.duanju.player.core

import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.io.IOException
import java.util.concurrent.CancellationException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

class CacheDownloadTest {
    @get:Rule val temp = TemporaryFolder()

    @Test
    fun successfulNativeOutputBecomesCompleteCache() {
        val out = File(temp.root, "series_1.mp4")
        CacheDownload.run(out, TaskCancellation()) { tmp, complete ->
            tmp.writeText("complete-mp4")
            complete(null);
            { fail("成功任务不应被取消") }
        }
        assertEquals("complete-mp4", out.readText())
        assertTrue(temp.root.listFiles()!!.none { it.name.endsWith(".tmp") })
    }

    @Test
    fun failedAndEmptyDownloadsNeverBecomeCacheHits() {
        val out = File(temp.root, "series_1.mp4")
        assertThrows(IOException::class.java) {
            CacheDownload.run(out, TaskCancellation()) { tmp, complete ->
                tmp.writeText("partial");
                complete("模拟网络失败");
                {}
            }
        }
        assertFalse(out.exists())
        assertThrows(IOException::class.java) {
            CacheDownload.run(out, TaskCancellation()) { _, complete ->
                complete(null);
                {}
            }
        }
        assertFalse(out.exists())
        assertTrue(temp.root.listFiles()!!.isEmpty())
    }

    @Test
    fun cancellationBeforeSessionRegistrationStillCancelsAndNeverPublishes() {
        val out = File(temp.root, "series_1.mp4")
        val task = TaskCancellation()
        var cancelled = false
        assertThrows(CancellationException::class.java) {
            CacheDownload.run(out, task) { tmp, complete ->
                tmp.writeText("old")
                task.cancel()
                complete(null);
                { cancelled = true }
            }
        }
        assertTrue(cancelled)
        assertFalse(out.exists())
        assertTrue(temp.root.listFiles()!!.isEmpty())
    }

    @Test
    fun timeoutCancelsButKeepsNativeOutputUntilActualCompletion() {
        val out = File(temp.root, "series_1.mp4")
        lateinit var nativeTmp: File
        lateinit var completeNative: (String?) -> Unit
        var cancelled = false
        assertThrows(IOException::class.java) {
            CacheDownload.run(out, TaskCancellation(), timeoutMs = 1) { tmp, complete ->
                nativeTmp = tmp
                completeNative = complete
                tmp.writeText("partial");
                { cancelled = true }
            }
        }
        assertTrue(cancelled)
        assertTrue(nativeTmp.exists())
        assertFalse(out.exists())
        completeNative("cancelled")
        assertFalse(nativeTmp.exists())
    }

    @Test
    fun interruptedOldNativeWriterCannotDeleteOrOverwriteNewDownload() {
        val out = File(temp.root, "series_1.mp4")
        val task = TaskCancellation()
        val started = CountDownLatch(1)
        val stoppedWaiting = CountDownLatch(1)
        val cancelledNative = CountDownLatch(1)
        lateinit var oldTmp: File
        lateinit var completeOld: (String?) -> Unit
        val executor = Executors.newSingleThreadExecutor()
        try {
            val future = executor.submit {
                try {
                    CacheDownload.run(out, task) { tmp, complete ->
                        oldTmp = tmp
                        completeOld = complete
                        tmp.writeText("old-partial")
                        started.countDown();
                        { cancelledNative.countDown() }
                    }
                } finally { stoppedWaiting.countDown() }
            }
            assertTrue(started.await(2, TimeUnit.SECONDS))
            task.cancel()
            future.cancel(true)
            assertTrue(cancelledNative.await(2, TimeUnit.SECONDS))
            assertTrue(stoppedWaiting.await(2, TimeUnit.SECONDS))
            assertTrue(oldTmp.exists())
            CacheDownload.run(out, TaskCancellation()) { tmp, complete ->
                assertNotEquals(oldTmp, tmp)
                tmp.writeText("new-complete")
                complete(null);
                {}
            }
            oldTmp.appendText("-late-native-write")
            completeOld(null)
            assertEquals("new-complete", out.readText())
            assertFalse(oldTmp.exists())
        } finally { executor.shutdownNow() }
    }

    @Test
    fun parallelSuccessfulPrefetchCannotReplaceAlreadyPublishedCache() {
        val out = File(temp.root, "series_1.mp4")
        CacheDownload.run(out, TaskCancellation()) { firstTmp, completeFirst ->
            firstTmp.writeText("late-first")
            CacheDownload.run(out, TaskCancellation()) { secondTmp, completeSecond ->
                assertNotEquals(firstTmp, secondTmp)
                secondTmp.writeText("winner")
                completeSecond(null);
                {}
            }
            completeFirst(null);
            {}
        }
        assertEquals("winner", out.readText())
        assertTrue(temp.root.listFiles()!!.none { it.name.endsWith(".tmp") })
    }
}
