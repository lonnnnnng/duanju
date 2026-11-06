package com.duanju.player.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LegacyHeadersTest {

    @Test
    fun legacyHeaders_coversAllRequiredFieldsAndMatchesStamp() {
        val query = "aid=8662&device_id=1234567890123456789&_rticket=1770000000000"
        val now = 1770000000123L
        val headers = HongguoSign.legacyHeaders(query, "8662", now)

        assertEquals(
            setOf("X-Gorgon", "X-Argus", "X-Ladon", "X-Khronos", "X-SS-Req-Ticket", "X-SS-STUB"),
            headers.keys
        )
        assertEquals("1770000000", headers["X-Khronos"])
        assertEquals("1770000000123", headers["X-SS-Req-Ticket"])
        assertEquals("", headers["X-SS-STUB"])
        assertTrue(headers["X-Gorgon"]!!.startsWith("8404"))
        assertTrue(headers["X-Ladon"]!!.isNotEmpty())
        assertTrue(headers["X-Argus"]!!.isNotEmpty())
    }
}
