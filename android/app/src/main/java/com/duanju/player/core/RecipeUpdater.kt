package com.duanju.player.core

import android.util.Log
import java.net.HttpURLConnection
import java.net.URL
import java.security.KeyFactory
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.util.Base64

/**
 * 配方远程更新：从 GitHub Release（固定 tag `recipe`）拉取 `recipe.json` 与其 ECDSA 签名。
 *
 * 安全约束：
 * - 只接受内置公钥验证通过的配方，防篡改/防劫持；
 * - 版本号必须严格大于当前版本，不允许降级；
 * - 任何失败（网络、签名、解析）都静默保留现有配方，绝不影响启动。
 */
object RecipeUpdater {

    private const val TAG = "RecipeUpdater"
    private const val BASE = "https://github.com/lonnnnnng/duanju/releases/download/recipe"
    private const val JSON_URL = "$BASE/recipe.json"
    private const val SIG_URL = "$BASE/recipe.json.sig"

    /** P-256 公钥（X.509 SubjectPublicKeyInfo，DER → Base64）。私钥只在发布端保存。 */
    private const val PUBLIC_KEY_B64 =
        "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEAv1a9+wZsWCzyWbcZrjzVEy1Ygn/ywqFZULjSzLfmbkx" +
            "WxJSZbay1bdOFCKYh5pt/OxXCkZ5PxIv8+vjtRpW8g=="

    /** 结果：null 表示失败；true 表示已更新；false 表示已是最新。 */
    fun refresh(): Boolean? {
        return try {
            val json = fetch(JSON_URL) ?: return null
            val sig = fetch(SIG_URL) ?: return null
            if (!verifyBytes(json, sig)) {
                Log.w(TAG, "配方签名校验失败，忽略远程版本")
                return null
            }
            val incoming = runCatching { RecipeParser.parse(String(json, Charsets.UTF_8)) }.getOrNull()
                ?: return null
            if (incoming.version <= RecipeStore.recipe().version) return false
            if (!RecipeStore.saveOverride(String(json, Charsets.UTF_8))) return null
            Log.d(TAG, "配方已更新到 v${incoming.version} (${incoming.updated})")
            true
        } catch (e: Exception) {
            Log.d(TAG, "配方更新失败：${e.message}")
            null
        }
    }

    fun refreshAsync() {
        Thread { runCatching { refresh() } }.apply { isDaemon = true }.start()
    }

    private fun fetch(url: String): ByteArray? {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 8000
            readTimeout = 8000
            instanceFollowRedirects = true
            setRequestProperty("Cache-Control", "no-cache")
        }
        return try {
            if (conn.responseCode != 200) return null
            conn.inputStream.use { it.readBytes() }.takeIf { it.isNotEmpty() }
        } finally {
            conn.disconnect()
        }
    }

    internal fun verifyBytes(json: ByteArray, signature: ByteArray): Boolean {
        val keyBytes = Base64.getDecoder().decode(PUBLIC_KEY_B64)
        val publicKey = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(keyBytes))
        val verifier = Signature.getInstance("SHA256withECDSA").apply {
            initVerify(publicKey)
            update(json)
        }
        return verifier.verify(signature)
    }
}
