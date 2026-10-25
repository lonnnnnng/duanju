package com.duanju.player.core

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class ContentKeyVectorTest {

    @Test
    fun realSpadeSample() {
        val spadeA = "o7wtxFW+HPdNiwTCf44Bwn+ENcl4gAPJfLE3/XytNtZKkAKjow=="
        val expected = hexToBytes("bbad229f22c5f4e03652c1fa34281bb0")
        val actual = HongguoSign.contentKey(spadeA)
        assertArrayEquals(expected, actual)
    }

    private fun hexToBytes(s: String): ByteArray =
        ByteArray(s.length / 2) { ((Character.digit(s[it * 2], 16) shl 4) + Character.digit(s[it * 2 + 1], 16)).toByte() }
}
