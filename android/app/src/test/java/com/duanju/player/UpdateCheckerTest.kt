package com.duanju.player

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UpdateCheckerTest {

    @Test
    fun newerVersions() {
        assertTrue(UpdateChecker.isNewer("0.1.21", "0.1.20"))
        assertTrue(UpdateChecker.isNewer("v0.1.21", "0.1.20"))
        assertTrue(UpdateChecker.isNewer("0.2.0", "0.1.99"))
        assertTrue(UpdateChecker.isNewer("1.0", "0.9.9"))
    }

    @Test
    fun sameOrOlderVersions() {
        assertFalse(UpdateChecker.isNewer("0.1.21", "0.1.21"))
        assertFalse(UpdateChecker.isNewer("v0.1.21", "0.1.21"))
        assertFalse(UpdateChecker.isNewer("0.1.20", "0.1.21"))
        assertFalse(UpdateChecker.isNewer("0.1.2", "0.1.20"))
        assertFalse(UpdateChecker.isNewer("", "0.1.21"))
        assertFalse(UpdateChecker.isNewer("unknown", "0.1.21"))
    }
}
