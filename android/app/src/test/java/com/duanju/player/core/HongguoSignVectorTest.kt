package com.duanju.player.core

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 与服务器 Go 实现生成的固定向量对照（/tmp/sign_vectors.json），
 * 保证 Kotlin 移植字节级一致。
 */
class HongguoSignVectorTest {

    private val query =
        "aid=8662&app_name=novelread&device_id=1234567890123456789&iid=1234567890123456789&_rticket=1770000000000"

    @Test
    fun modelGorgon_fixedVector() {
        val body = """{"mixed_video_id_map":{"1004":["7693086454326496281"]}}""".toByteArray()
        val out = HongguoSign.modelGorgon(query, body, 1770000000L, 4242)
        assertEquals("840492104001b1a0c3cca554d53d9df21e4fd403bce7ded12dc8", out)
    }

    @Test
    fun modelGorgon_noBody() {
        val out = HongguoSign.modelGorgon(query, null, 1770000000L, 0)
        assertEquals("84040000400115fe0d065f71ffb8deec244161d94d7b04f14e0a", out)
    }

    @Test
    fun sm3_emptyInput() {
        // 标准测试向量：SM3("") 已知结果
        val out = HongguoSign.sm3(ByteArray(0)).joinToString("") { "%02x".format(it) }
        assertEquals("1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b", out)
    }

    @Test
    fun sm3_abc() {
        val out = HongguoSign.sm3("abc".toByteArray()).joinToString("") { "%02x".format(it) }
        assertEquals("66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0", out)
    }
}