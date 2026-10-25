package com.duanju.player.core

import java.security.MessageDigest
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.spec.IvParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * 红果 App 接口签名与密钥派生（与服务器 Go 实现字节级一致，含固定向量自检）。
 */
object HongguoSign {

    const val UA_MODEL =
        "com.phoenix.read/73532 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"
    const val UA_LEGACY = UA_MODEL

    fun baseQuery(deviceId: String, installId: String): List<Pair<String, String>> = listOf(
        "aid" to "8662", "app_name" to "novelread", "version_code" to "73532",
        "version_name" to "7.3.5.32", "manifest_version_code" to "73532",
        "update_version_code" to "73532", "channel" to "update_64",
        "device_platform" to "android", "os" to "android", "ssmix" to "a",
        "device_type" to "25053RT47C", "device_brand" to "Redmi", "language" to "zh",
        "os_api" to "36", "os_version" to "16", "resolution" to "1280*2772",
        "dpi" to "520", "ac" to "wifi", "device_id" to deviceId, "iid" to installId
    )

    fun encodeQuery(pairs: List<Pair<String, String>>): String =
        pairs.joinToString("&") { (k, v) ->
            java.net.URLEncoder.encode(k, "UTF-8") + "=" + java.net.URLEncoder.encode(v, "UTF-8")
        }

    private fun md5(data: ByteArray): ByteArray = MessageDigest.getInstance("MD5").digest(data)
    private fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it) }
    private fun hexToBytes(s: String): ByteArray =
        ByteArray(s.length / 2) { ((Character.digit(s[it * 2], 16) shl 4) + Character.digit(s[it * 2 + 1], 16)).toByte() }

    private fun reverseBits8(v: Int): Int {
        var r = 0
        for (i in 0 until 8) r = (r shl 1) or ((v shr i) and 1)
        return r
    }

    private fun rol64(v: Long, n: Int) = java.lang.Long.rotateLeft(v, n)
    private fun ror64(v: Long, n: Int) = java.lang.Long.rotateRight(v, n)
    private fun le64(b: ByteArray, off: Int): Long {
        var v = 0L
        for (i in 7 downTo 0) v = (v shl 8) or (b[off + i].toLong() and 0xFF)
        return v
    }

    private fun u8(b: Byte) = b.toInt() and 0xFF

    // ==================== 新版 X-Gorgon（播放接口） ====================

    fun modelGorgon(query: String, body: ByteArray?, timestamp: Long, random: Int): String {
        val queryHash = md5(query.toByteArray(Charsets.UTF_8))
        val input = ByteArray(20)
        System.arraycopy(queryHash, 0, input, 0, 4)
        if (body != null) {
            val bh = md5(body)
            System.arraycopy(bh, 0, input, 4, 4)
        }
        var sdk = 67503104L
        for (i in 0 until 4) { input[12 + i] = (sdk and 0xFF).toByte(); sdk = sdk ushr 8 }
        input[16] = ((timestamp ushr 24) and 0xFF).toByte()
        input[17] = ((timestamp ushr 16) and 0xFF).toByte()
        input[18] = ((timestamp ushr 8) and 0xFF).toByte()
        input[19] = (timestamp and 0xFF).toByte()
        val key = byteArrayOf(
            0x4a, 0x40, 0x16, ((random shr 8) and 0xFF).toByte(),
            0x47, 0x6c, 0x01, (random and 0xFF).toByte()
        )
        // KSA：box[i] = box[j]（单向赋值，与 Go 一致，不是交换）
        val box = ByteArray(256) { it.toByte() }
        var j = 0
        for (i in 0 until 256) {
            j = (j + u8(box[i]) + u8(key[i % 8])) and 0xFF
            box[i] = box[j]
        }
        j = 0
        val out = ByteArray(input.size)
        for (n in input.indices) {
            val i = n + 1
            j += u8(box[i])
            val v = box[j and 0xFF]
            box[i] = v
            out[n] = (input[n].toInt() xor u8(box[(u8(v) + u8(v)) and 0xFF])).toByte()
        }
        for (i in out.indices) {
            val b = u8(out[i])
            val value = ((b shr 4) or (b shl 4)) and 0xFF
            val next = u8(out[(i + 1) % out.size])
            out[i] = ((reverseBits8(next xor value) xor 20).inv()).toByte()
        }
        val prefix = byteArrayOf(
            0x84.toByte(), 0x04, (random and 0xFF).toByte(),
            ((random shr 8) and 0xFF).toByte(), 0x40, 0x01
        )
        return (prefix + out).toHex()
    }

    fun signModelHeaders(query: String, body: ByteArray?): Map<String, String> {
        val tsMs = System.currentTimeMillis()
        val ts = tsMs / 1000
        val random = (System.nanoTime() and 0xFFFF).toInt()
        val headers = mutableMapOf<String, String>()
        if (body != null) headers["X-SS-STUB"] = md5(body).toHex().uppercase()
        headers["X-Khronos"] = ts.toString()
        headers["X-Gorgon"] = modelGorgon(query, body, ts, random)
        headers["X-SS-Req-Ticket"] = tsMs.toString()
        headers["X-Ladon"] = Base64.getEncoder().encodeToString(byteArrayOf(
            ((ts ushr 24) and 0xFF).toByte(), ((ts ushr 16) and 0xFF).toByte(),
            ((ts ushr 8) and 0xFF).toByte(), (ts and 0xFF).toByte()
        ))
        headers["X-Argus"] = Base64.getEncoder().encodeToString(byteArrayOf(
            (ts and 0xFF).toByte(), ((ts shr 8) and 0xFF).toByte(),
            ((ts shr 16) and 0xFF).toByte(), ((ts shr 24) and 0xFF).toByte()
        ))
        headers["X-Tt-Request-Tag"] = "t=0;n=0"
        headers["Sdk-Version"] = "2"
        headers["Passport-Sdk-Version"] = "50561"
        headers["X-Xs-From-Web"] = "0"
        headers["X-Vc-Bdturing-Sdk-Version"] = "3.7.2.cn"
        return headers
    }

    // ==================== 旧版签名（详情/目录：X-Argus/X-Ladon/X-Gorgon） ====================

    private class Nonce(val b: ByteArray)

    private fun legacyGorgon(query: String, stamp: Long, nonce: Nonce): String {
        val b = nonce.b
        val head2 = (((u8(b[0]) % 7) + 1) shl 5) and 0xFF
        val head3 = 0xE0 or (u8(b[1]) and 0x0F)
        val seed = ByteArray(20)
        System.arraycopy(md5(query.toByteArray(Charsets.UTF_8)), 0, seed, 0, 4)
        seed[12] = 0; seed[13] = 1; seed[14] = 7; seed[15] = 4
        seed[16] = ((stamp ushr 24) and 0xFF).toByte()
        seed[17] = ((stamp ushr 16) and 0xFF).toByte()
        seed[18] = ((stamp ushr 8) and 0xFF).toByte()
        seed[19] = (stamp and 0xFF).toByte()
        val key = byteArrayOf(0x4a, 0x00, 0x16, head3.toByte(), 0x47, 0x6c, 0x00, head2.toByte())
        // KSA：标准交换
        val box = ByteArray(256) { it.toByte() }
        var j = 0
        for (i in 0 until 256) {
            j = (j + u8(box[i]) + u8(key[i and 7])) and 0xFF
            box[i] = box[j].also { box[j] = box[i] }
        }
        j = 0
        val out = ByteArray(20)
        for (offset in 0 until 20) {
            val i = offset + 1
            j = (j + u8(box[i])) and 0xFF
            box[i] = box[j].also { box[j] = box[i] }
            out[offset] = ((seed[offset].toInt()) xor box[(u8(box[i]) + u8(box[j])) and 0xFF].toInt()).toByte()
        }
        for (i in 0 until 20) {
            val v = u8(out[i])
            val rotated = ((v shl 4) or (v ushr 4)) and 0xFF
            val next = u8(out[(i + 1) % 20])
            out[i] = ((reverseBits8(next xor rotated) xor 0xEB).inv()).toByte()
        }
        return (byteArrayOf(0x84.toByte(), 0x04, head2.toByte(), head3.toByte(), 0, 0) + out).toHex()
    }

    private fun legacyLadon(aid: String, stamp: Long, nonce: Nonce): String {
        val prefix = nonce.b.copyOfRange(2, 6)
        val digest = md5(prefix + aid.toByteArray(Charsets.UTF_8)).toHex().toByteArray(Charsets.UTF_8)
        var left = le64(digest, 0)
        var right = le64(digest, 8)
        val queue = ArrayDeque<Long>()
        queue.add(le64(digest, 16)); queue.add(le64(digest, 24))
        val keys = LongArray(34)
        for (i in 0 until 34) {
            keys[i] = left
            val mixed = (ror64(right, 8) + left) xor i.toLong()
            queue.add(mixed)
            left = mixed xor rol64(left, 3)
            right = queue.removeFirst()
        }
        val data = pad16("$stamp-1611921764-3019".toByteArray(Charsets.UTF_8))
        for (off in data.indices step 16) {
            var a = le64(data, off)
            var b = le64(data, off + 8)
            for (k in keys) {
                b = k xor (a + ror64(b, 8))
                a = b xor rol64(a, 61)
            }
            for (k in 0 until 8) data[off + k] = ((a ushr (8 * k)) and 0xFF).toByte()
            for (k in 0 until 8) data[off + 8 + k] = ((b ushr (8 * k)) and 0xFF).toByte()
        }
        return Base64.getEncoder().encodeToString(prefix + data)
    }

    private fun legacyArgus(query: String, stamp: Long, nonce: Nonce): String {
        val key = hexToBytes("ac1adaae95a7af94a5114ab3b3a97dd80050aa0a39314c40528caec95256c28c")
        var message = ByteArray(0)
        fun varint(field: Long, value: Long) {
            message += uvarint(field * 8); message += uvarint(value)
        }
        fun bytes(field: Long, value: ByteArray) {
            message += uvarint(field * 8 + 2); message += uvarint(value.size.toLong()); message += value
        }
        varint(1, 0x20200929L * 2)
        varint(2, 2)
        varint(3, le32(nonce.b, 6).toLong() and 0xFFFFFFFFL)
        bytes(4, "3019".toByteArray())
        bytes(6, "1611921764".toByteArray())
        bytes(7, "6.8.1.32".toByteArray())
        bytes(8, "v04.07.01-ml-android".toByteArray())
        varint(9, 135135744)
        bytes(10, ByteArray(8))
        varint(12, stamp * 2)
        bytes(13, sm3(ByteArray(16)).copyOf(6))
        bytes(14, sm3(query.toByteArray(Charsets.UTF_8)).copyOf(6))
        var nested = ByteArray(0)
        nested += uvarint(8); nested += uvarint((2 + (u8(nonce.b[10]) % 49) * 2).toLong())
        for (f in longArrayOf(2, 3, 4)) { nested += uvarint(f * 8); nested += uvarint(1388734) }
        bytes(15, nested)
        bytes(20, "none".toByteArray())
        varint(21, 738)
        // 轮密钥
        val seed = key + byteArrayOf(0x3c, 0xcc.toByte(), 0x56, 0x7b) + key
        val derived = sm3(seed)
        val rounds = LongArray(72)
        for (i in 0 until 4) rounds[i] = le64(derived, i * 8)
        for (i in 4 until 72) {
            var mixed = ror64(rounds[i - 1], 3) xor rounds[i - 3]
            mixed = mixed xor rol64(mixed, 63)
            rounds[i] = rounds[i - 4].inv() xor mixed xor (((0x3dc94c3a046d678bL ushr ((i - 4) % 62)) and 1L)) xor 3L
        }
        var data = pad16(message)
        for (off in data.indices step 16) {
            var left = le64(data, off)
            var right = le64(data, off + 8)
            for (round in rounds) {
                val newLeft = right xor (rol64(left, 1) and rol64(left, 8)) xor rol64(left, 2) xor round
                right = left
                left = newLeft
            }
            for (k in 0 until 8) data[off + k] = ((left ushr (8 * k)) and 0xFF).toByte()
            for (k in 0 until 8) data[off + 8 + k] = ((right ushr (8 * k)) and 0xFF).toByte()
        }
        val xorKey = byteArrayOf(0xd0.toByte(), 0x4f.toByte(), 0xfd.toByte(), 0xff.toByte(), 0xd0.toByte(), 0x4f.toByte(), 0xfd.toByte(), 0xff.toByte())
        for (i in data.indices) data[i] = ((data[i].toInt()) xor (xorKey[i % 8].toInt())).toByte()
        data = xorKey + data
        data.reverse()
        var container = byteArrayOf(
            0xa6.toByte(), 0xe7.toByte(), 0x83.toByte(), 0xee.toByte(), 0x70, 0x01, 0x10, 0x09, 0x18
        ) + data + byteArrayOf(0x56, 0x7b)
        container = pad16(container)
        val aesKey = md5(key.copyOfRange(0, 16))
        val aesIv = md5(key.copyOfRange(16, 32))
        val cipher = Cipher.getInstance("AES/CBC/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(aesKey, "AES"), IvParameterSpec(aesIv))
        val encrypted = cipher.doFinal(container)
        return Base64.getEncoder().encodeToString(byteArrayOf(0x3c, 0xcc.toByte()) + encrypted)
    }

    private fun le32(b: ByteArray, off: Int): Int =
        (u8(b[off])) or (u8(b[off + 1]) shl 8) or (u8(b[off + 2]) shl 16) or (u8(b[off + 3]) shl 24)

    private fun uvarint(v: Long): ByteArray {
        var n = v
        val out = ArrayList<Byte>()
        while (n > 127) { out.add(((n and 0x7F) or 0x80).toByte()); n = n ushr 7 }
        out.add(n.toByte())
        return out.toByteArray()
    }

    fun signLegacyHeaders(query: String, aid: String): Map<String, String> {
        val tsMs = System.currentTimeMillis()
        val ts = tsMs / 1000
        val nonceBytes = ByteArray(11)
        java.security.SecureRandom().nextBytes(nonceBytes)
        val nonce = Nonce(nonceBytes)
        return mapOf(
            "X-Khronos" to ts.toString(),
            "X-Gorgon" to legacyGorgon(query, ts, nonce),
            "X-SS-Req-Ticket" to tsMs.toString(),
            "X-SS-STUB" to "",
            "X-Argus" to legacyArgus(query, ts, nonce),
            "X-Ladon" to legacyLadon(aid, ts, nonce)
        )
    }

    // ==================== SM3 ====================

    fun sm3(input: ByteArray): ByteArray {
        var data = input + byteArrayOf(0x80.toByte())
        while (data.size % 64 != 56) data += 0
        val lenBytes = ByteArray(8)
        val bitLen = input.size.toLong() * 8
        for (i in 0 until 8) lenBytes[i] = ((bitLen ushr (8 * (7 - i))) and 0xFF).toByte()
        data += lenBytes
        var state = intArrayOf(
            0x7380166f.toInt(), 0x4914b2b9.toInt(), 0x172442d7.toInt(), 0xda8a0600.toInt(),
            0xa96f30bc.toInt(), 0x163138aa.toInt(), 0xe38dee4d.toInt(), 0xb0fb0e4e.toInt()
        )
        for (off in data.indices step 64) {
            val w = IntArray(68)
            for (i in 0 until 16) w[i] = be32(data, off + i * 4)
            for (i in 16 until 68) {
                val v = w[i - 16] xor w[i - 9] xor rol32(w[i - 3], 15)
                w[i] = v xor rol32(v, 15) xor rol32(v, 23) xor rol32(w[i - 13], 7) xor w[i - 6]
            }
            var a = state[0]; var b = state[1]; var c = state[2]; var d = state[3]
            var e = state[4]; var f = state[5]; var g = state[6]; var h = state[7]
            for (i in 0 until 64) {
                val t: Int; val ff: Int; val gg: Int
                if (i < 16) {
                    t = 0x79cc4519; ff = a xor b xor c; gg = e xor f xor g
                } else {
                    t = 0x7a879d8a.toInt(); ff = (a and b) or (a and c) or (b and c); gg = (e and f) or (e.inv() and g)
                }
                val ss1 = rol32(rol32(a, 12) + e + rol32(t, i), 7)
                val tt1 = ff + d + (ss1 xor rol32(a, 12)) + (w[i] xor w[i + 4])
                val tt2 = gg + h + ss1 + w[i]
                val na = tt1; val nb = a; val nc = rol32(b, 9); val nd = c
                val ne = tt2 xor rol32(tt2, 9) xor rol32(tt2, 17)
                val nf = e
                val ng = rol32(f, 19)
                val nh = g
                a = na; b = nb; c = nc; d = nd
                e = ne; f = nf; g = ng; h = nh
            }
            for (i in 0 until 8) state[i] = state[i] xor intArrayOf(a, b, c, d, e, f, g, h)[i]
        }
        val result = ByteArray(32)
        for (i in 0 until 8) {
            result[i * 4] = ((state[i] ushr 24) and 0xFF).toByte()
            result[i * 4 + 1] = ((state[i] ushr 16) and 0xFF).toByte()
            result[i * 4 + 2] = ((state[i] ushr 8) and 0xFF).toByte()
            result[i * 4 + 3] = (state[i] and 0xFF).toByte()
        }
        return result
    }

    private fun rol32(v: Int, n: Int): Int = Integer.rotateLeft(v, n)
    private fun be32(b: ByteArray, off: Int): Int =
        (u8(b[off]) shl 24) or (u8(b[off + 1]) shl 16) or (u8(b[off + 2]) shl 8) or u8(b[off + 3])

    private fun pad16(data: ByteArray): ByteArray {
        val pad = 16 - data.size % 16
        return data + ByteArray(pad) { pad.toByte() }
    }

    // ==================== spade_a → AES 密钥 ====================

    fun contentKey(spadeA: String): ByteArray? {
        return try {
            val cleaned = spadeA.trim().replace("-", "+").replace("_", "/")
            val raw = runCatching { Base64.getDecoder().decode(cleaned) }.getOrNull()
                ?: runCatching { Base64.getDecoder().decode(cleaned + "==".repeat((4 - cleaned.length % 4) % 4)) }.getOrNull()
                ?: return null
            if (raw.size < 3) return null
            val tagLength = (u8(raw[0]) xor u8(raw[1]) xor u8(raw[2])) - 48
            val contentLength = raw.size - tagLength - 1
            if (tagLength < 1 || contentLength < 33 || contentLength >= raw.size) return null
            val seed = (u8(raw[raw.size - tagLength - 2]) xor u8(raw[raw.size - tagLength - 1]))
            val tag = ByteArray(tagLength) { i -> ((u8(raw[raw.size - tagLength + i]) xor seed)).toByte() }
            val tagStr = String(tag, Charsets.UTF_8)
            if (tagStr == "app_v2" || tagStr == "web_v2") return null
            val web = tagStr == "22" || tagStr == "webdevice"
            if (!web && tagStr != "11" && tagStr != "appdevice") return null
            val decoded = ByteArray(contentLength)
            var prevEven: Byte = 250.toByte()
            var prevOdd: Byte = 85.toByte()
            for (i in 0 until contentLength) {
                val current = raw[1 + i]
                val previous: Byte
                if (i % 2 == 0) { previous = prevEven; prevEven = current }
                else { previous = prevOdd; prevOdd = current }
                val delta = 21 + Integer.bitCount(i)
                val d = if (web) delta else -delta
                decoded[i] = ((previous.toInt() xor current.toInt()) + d).toByte()
            }
            val padding = Character.digit(u8(decoded[0]), 36)
            if (padding < 0 || contentLength - padding - 1 != 32) return null
            val key = hexToBytes(String(decoded, 1, 32, Charsets.UTF_8))
            if (key.size != 16) return null
            key
        } catch (_: Exception) { null }
    }
}