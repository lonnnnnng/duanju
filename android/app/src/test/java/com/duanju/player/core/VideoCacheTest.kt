package com.duanju.player.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class VideoCacheTest {

    @get:Rule
    val temp = TemporaryFolder()

    private fun file(name: String, size: Int, mtime: Long): File {
        val f = File(temp.root, name)
        f.writeBytes(ByteArray(size))
        f.setLastModified(mtime)
        return f
    }

    @Test
    fun keepsEverythingUnderLimit() {
        val a = file("1_1.mp4", 100, 1000)
        val b = file("1_2.mp4", 100, 2000)
        assertEquals(0L, VideoCache.trim(temp.root, 1000))
        assertTrue(a.exists() && b.exists())
        assertEquals(200L, VideoCache.sizeBytes(temp.root))
    }

    @Test
    fun evictsOldestFirst() {
        val a = file("1_1.mp4", 400, 1000)
        val b = file("1_2.mp4", 400, 2000)
        val c = file("1_3.mp4", 400, 3000)
        val freed = VideoCache.trim(temp.root, 900)
        assertEquals(400L, freed)
        assertTrue(!a.exists())
        assertTrue(b.exists() && c.exists())
    }

    @Test
    fun neverDeletesKeptFiles() {
        val a = file("1_1.mp4", 400, 1000)
        val b = file("1_2.mp4", 400, 2000)
        val c = file("1_3.mp4", 400, 3000)
        val freed = VideoCache.trim(temp.root, 900, setOf("1_1.mp4"))
        assertEquals(400L, freed)
        assertTrue(a.exists())
        assertTrue(!b.exists())
        assertTrue(c.exists())
    }

    @Test
    fun ignoresPartialAndNonVideoFiles() {
        val a = file("1_1.mp4", 400, 1000)
        val tmp = file("1_2.mp4.tmp", 400, 500)
        val other = file("system-ca.pem", 400, 500)
        assertEquals(0L, VideoCache.trim(temp.root, 1000))
        assertTrue(a.exists() && tmp.exists() && other.exists())
        assertEquals(400L, VideoCache.sizeBytes(temp.root))
    }

    @Test
    fun zeroLimitMeansNoEviction() {
        val a = file("1_1.mp4", 400, 1000)
        assertEquals(0L, VideoCache.trim(temp.root, 0))
        assertTrue(a.exists())
    }

    @Test
    fun formatsFileSizes() {
        assertEquals("512 B", VideoCache.formatSize(512))
        assertEquals("8 KB", VideoCache.formatSize(8192))
        assertEquals("7.7 MB", VideoCache.formatSize(8_073_677))
        assertEquals("1.50 GB", VideoCache.formatSize(1_610_612_736))
    }

    @Test
    fun formatsSizes() {
        assertEquals("512 KB", VideoCache.format(512L * 1024))
        assertEquals("2.0 GB", VideoCache.format(2L * 1024 * 1024 * 1024))
        assertEquals("512 MB", VideoCache.format(512L * 1024 * 1024))
    }
}
