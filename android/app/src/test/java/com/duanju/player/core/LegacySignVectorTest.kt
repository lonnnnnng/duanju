
package com.duanju.player.core

import org.junit.Assert.assertEquals
import org.junit.Test

class LegacySignVectorTest {

    private val query = "aid=8662&app_name=novelread&device_id=1234567890123456789&iid=1234567890123456789&_rticket=1770000000000"
    private val nonceBytes = hexToBytes("5a03112233445566778899")

    private fun hexToBytes(s: String): ByteArray =
        ByteArray(s.length / 2) { ((Character.digit(s[it * 2], 16) shl 4) + Character.digit(s[it * 2 + 1], 16)).toByte() }

    @Test
    fun legacyGorgon_matchesGo() {
        val g = HongguoSign.legacyGorgonForTest(query, 1770000000L, nonceBytes)
        assertEquals("8404e0e30000ba33e65ef25f82589dd263f2c2bd76a8485d0859", g)
    }

    @Test
    fun legacyLadon_matchesGo() {
        val l = HongguoSign.legacyLadonForTest("8662", 1770000000L, nonceBytes)
        assertEquals("ESIzRL0XbfXrzTOS8J3D2hvcx3cVn1SD/yyTcUbeUUHUR5cj", l)
    }

    @Test
    fun legacyArgus_matchesGo() {
        val a = HongguoSign.legacyArgusForTest(query, 1770000000L, nonceBytes)
        assertEquals("PMxhgUKIXtho/6stSub0VASKJDoVY13IeJ5USEZ0fLHWQWnLTpChHkhAADS2/PVyyIHFcV18P0e8s6/oOPvRl9Dz+RxeaMg7TJfqCC0GTeB1QvaILUmM5AKl/lOhp7Eqp3Xxe0zFPRQTvu/KczTid9LtjyVS4wy2R7BpBtunMm/QhvpSPklR+9qhr+YGwuXB4jcYwX67WdKnsLwkiwbI9dkiY3soMh14hIl5RP6A28gauA==", a)
    }
}
