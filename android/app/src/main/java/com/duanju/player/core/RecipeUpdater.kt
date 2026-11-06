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
     * long: 比较全部验签且兼容的来源，采用最高版本，避免旧镜像遮住新配置；全部失败保留现有配方。
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
    fun refresh(): Boolean? = refreshFrom(SOURCES, ::fetch)

    internal fun refreshFrom(
        sources: List<String>,
        fetcher: (String) -> ByteArray?,
        verify: (ByteArray, ByteArray) -> Boolean = ::verifyBytes
    ): Boolean? {
        var validSource = false
        var bestText: String? = null
        var bestVersion = RecipeStore.recipe().version
        for (base in sources) {
            // 带时间戳绕过 CDN 边缘缓存：Release 资产覆盖后旧版本可能仍被缓存。
            val bust = System.currentTimeMillis()
            val json = runCatching { fetcher("$base/recipe.json?ts=$bust") }.getOrNull() ?: continue
            val sig = runCatching { fetcher("$base/recipe.json.sig?ts=$bust") }.getOrNull() ?: continue
            if (!runCatching { verify(json, sig) }.getOrDefault(false)) {
                Log.w(TAG, "签名校验失败，跳过源: $base")
                continue
            }
            val incoming = runCatching { RecipeParser.parse(String(json, Charsets.UTF_8)) }.getOrNull()
            if (incoming == null) {
                Log.w(TAG, "配方解析失败，跳过源: $base")
                continue
            }
            validSource = true
            Log.d(TAG, "命中配方源: $base (v${incoming.version})")
            // long: 镜像可能缓存旧文件；比较所有可用源，不能用一个旧版本遮住后面的新版本。
            if (incoming.version > bestVersion) {
                bestVersion = incoming.version
                bestText = String(json, Charsets.UTF_8)
            }
        }
        bestText?.let { text ->
            if (RecipeStore.saveOverride(text)) return true
            // long: 并发检查可能已经先保存了更新版本，这时应报告当前已更新而不是覆盖它。
            return if (RecipeStore.recipe().version >= bestVersion) false else null
        }
        if (validSource) return false
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
            conn.inputStream.use { input ->
                val output = java.io.ByteArrayOutputStream()
                val buffer = ByteArray(8192)
                while (true) {
                    val count = input.read(buffer)
                    if (count < 0) break
                    if (output.size() + count > 512 * 1024) return null
                    output.write(buffer, 0, count)
                }
                output.toByteArray().takeIf { it.isNotEmpty() }
            }
        } finally {
            conn.disconnect()
        }
    }

    internal fun verifyBytes(json: ByteArray, signature: ByteArray): Boolean = runCatching {
        val keyBytes = Base64.getDecoder().decode(PUBLIC_KEY_B64)
        val publicKey = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(keyBytes))
        val verifier = Signature.getInstance("SHA256withECDSA").apply {
            initVerify(publicKey)
            update(json)
        }
        verifier.verify(signature)
    }.getOrDefault(false)
}
