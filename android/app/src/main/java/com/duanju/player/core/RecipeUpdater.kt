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
    /**
     * 多源兜底：GitHub Release → jsDelivr（国内通常可达）→ raw。
     * 任意一个源拿到「JSON + 验签通过」就采用；全部失败则保留现有配方。
     */
    private val SOURCES = listOf(
        // 手机网络通常直连不了 GitHub，加速地址放最前。
        "https://gh-proxy.com/https://github.com/lonnnnnng/duanju/releases/download/recipe",
        "https://github.com/lonnnnnng/duanju/releases/download/recipe",
        "https://cdn.jsdelivr.net/gh/lonnnnnng/duanju@main/recipes",
        "https://raw.githubusercontent.com/lonnnnnng/duanju/main/recipes"
    )

    /** P-256 公钥（X.509 SubjectPublicKeyInfo，DER → Base64）。私钥只在发布端保存。 */
    private const val PUBLIC_KEY_B64 =
        "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEAv1a9+wZsWCzyWbcZrjzVEy1Ygn/ywqFZULjSzLfmbkx" +
            "WxJSZbay1bdOFCKYh5pt/OxXCkZ5PxIv8+vjtRpW8g=="

    /** 结果：null 表示失败；true 表示已更新；false 表示已是最新。 */
    fun refresh(): Boolean? {
        for (base in SOURCES) {
            // 带时间戳绕过 CDN 边缘缓存：Release 资产覆盖后旧版本可能仍被缓存。
            val bust = System.currentTimeMillis()
            val json = runCatching { fetch("$base/recipe.json?ts=$bust") }.getOrNull() ?: continue
            val sig = runCatching { fetch("$base/recipe.json.sig?ts=$bust") }.getOrNull() ?: continue
            if (!verifyBytes(json, sig)) {
                Log.w(TAG, "签名校验失败，跳过源: $base")
                continue
            }
            val incoming = runCatching { RecipeParser.parse(String(json, Charsets.UTF_8)) }.getOrNull()
            if (incoming == null) {
                Log.w(TAG, "配方解析失败，跳过源: $base")
                continue
            }
            Log.d(TAG, "命中配方源: $base (v${incoming.version})")
            if (incoming.version <= RecipeStore.recipe().version) return false
            if (!RecipeStore.saveOverride(String(json, Charsets.UTF_8))) return null
            Log.d(TAG, "配方已更新到 v${incoming.version} (${incoming.updated})")
            return true
        }
        Log.d(TAG, "所有配方源均不可用，继续使用本地版本")
        return null
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
            useCaches = false
            setRequestProperty("Cache-Control", "no-cache, no-store")
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
