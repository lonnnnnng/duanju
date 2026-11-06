package com.duanju.player.core

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class RecipeSignatureTest {

    /** 用离线私钥对固定字符串 "duanju-recipe-test" 的 SHA256withECDSA 签名。 */
    private val signature = Base64.getDecoder().decode(
        "MEQCIEM93lZn++ciVi8wLnt/+ui0p9ipzSCnPf1L+6mCaRqeAiAI32M5Ft3IntGLI3P8oujYAdK+zM3RSzVcn8uGou5ImA=="
    )

    @Test
    fun acceptsSignatureFromReleaseKey() {
        assertTrue(RecipeUpdater.verifyBytes("duanju-recipe-test".toByteArray(), signature))
    }

    @Test
    fun rejectsTamperedPayload() {
        assertFalse(RecipeUpdater.verifyBytes("duanju-recipe-test2".toByteArray(), signature))
    }

    @Test
    fun rejectsTamperedSignature() {
        val broken = signature.copyOf().also { it[it.size / 2] = (it[it.size / 2] + 1).toByte() }
        assertFalse(RecipeUpdater.verifyBytes("duanju-recipe-test".toByteArray(), broken))
    }
}
