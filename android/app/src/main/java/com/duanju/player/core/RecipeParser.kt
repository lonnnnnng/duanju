package com.duanju.player.core

import org.json.JSONObject

/**
 * 接口配方（L1）：把「路径 / 参数 / 版本号 / 签名档位 / 重试 / 取值路径」从代码里挪到 JSON。
 *
 * 设计目标：上游改动大多数是声明式的，改配方即可，不必发版。
 * 只有出现全新的签名算法时才需要动代码（L2 的具名原语或 L3 插件）。
 */
data class Recipe(
    val version: Int,
    val updated: String,
    val hosts: Map<String, String>,
    val userAgents: Map<String, String>,
    val endpoints: Map<String, Endpoint>
) {
    fun endpoint(id: String): Endpoint =
        endpoints[id] ?: throw IllegalStateException("配方缺少接口: $id")
}

data class Endpoint(
    val id: String,
    val desc: String,
    val method: String,
    val host: String,
    val path: String,
    val versionCode: String,
    /** 有序的签名档位；前一个失败（空正文等）时自动尝试下一个。 */
    val signModes: List<String>,
    val params: List<Pair<String, String>>,
    val retry: RetryPolicy,
    /** 取值路径，`.` 分段，`$var` 段用变量替换；null 表示调用方自己解析整个响应。 */
    val extract: String?
)

data class RetryPolicy(
    val attempts: Int,
    val backoffMs: List<Long>,
    val on: Set<String>
) {
    fun backoffFor(attempt: Int): Long =
        backoffMs.getOrElse(attempt - 1) { backoffMs.lastOrNull() ?: 600L }
}

object RecipeParser {

    fun parse(text: String): Recipe {
        val root = JSONObject(text)
        val hosts = root.optJSONObject("hosts").toStringMap()
        val userAgents = root.optJSONObject("userAgents").toStringMap()
        val endpointsJson = root.optJSONObject("endpoints")
            ?: throw IllegalArgumentException("配方缺少 endpoints")

        val endpoints = LinkedHashMap<String, Endpoint>()
        for (id in endpointsJson.keys()) {
            val obj = endpointsJson.optJSONObject(id) ?: continue
            endpoints[id] = parseEndpoint(id, obj)
        }
        require(endpoints.isNotEmpty()) { "配方 endpoints 为空" }
        return Recipe(
            version = root.optInt("version", 0),
            updated = root.optString("updated"),
            hosts = hosts,
            userAgents = userAgents,
            endpoints = endpoints
        )
    }

    private fun parseEndpoint(id: String, obj: JSONObject): Endpoint {
        val path = obj.optString("path")
        require(path.startsWith("/")) { "端点 $id 的 path 必须以 / 开头" }
        val hostKey = obj.optString("host")
        require(hostKey.isNotEmpty()) { "端点 $id 缺少 host" }

        val params = ArrayList<Pair<String, String>>()
        val arr = obj.optJSONArray("params")
        if (arr != null) {
            for (i in 0 until arr.length()) {
                val pair = arr.optJSONArray(i) ?: continue
                params.add(pair.optString(0) to pair.optString(1))
            }
        }

        val retryJson = obj.optJSONObject("retry")
        val backoff = ArrayList<Long>()
        retryJson?.optJSONArray("backoffMs")?.let { list ->
            for (i in 0 until list.length()) backoff.add(list.optLong(i))
        }
        val retryOn = HashSet<String>()
        retryJson?.optJSONArray("on")?.let { list ->
            for (i in 0 until list.length()) retryOn.add(list.optString(i))
        }

        val signs = ArrayList<String>()
        obj.optJSONArray("sign")?.let { list ->
            for (i in 0 until list.length()) signs.add(list.optString(i))
        }
        if (signs.isEmpty()) signs.add("none")

        return Endpoint(
            id = id,
            desc = obj.optString("desc"),
            method = obj.optString("method", "GET").uppercase(),
            host = hostKey,
            path = path,
            versionCode = obj.optString("versionCode"),
            signModes = signs,
            params = params,
            retry = RetryPolicy(
                attempts = retryJson?.optInt("attempts", 3) ?: 3,
                backoffMs = if (backoff.isEmpty()) listOf(600L, 1200L) else backoff,
                on = retryOn
            ),
            extract = obj.optString("extract").takeIf { it.isNotEmpty() }
        )
    }

    private fun JSONObject?.toStringMap(): Map<String, String> {
        if (this == null) return emptyMap()
        val out = LinkedHashMap<String, String>()
        for (key in keys()) out[key] = optString(key)
        return out
    }
}
