package com.duanju.player.core

import org.json.JSONObject
import java.net.URI

/**
 * 接口配方（L1）：把「路径 / 参数 / 版本号 / 签名档位 / 重试 / 取值路径」从代码里挪到 JSON。
 *
 * long: 已支持的参数与策略变化通过配方更新，避免为纯配置变化重复发版。
 * long: 配方只能组合宿主已支持的请求和解析能力；新协议、签名或媒体格式仍需新增实现。
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
    /** long: 当前档位先消耗有限重试预算，仍为可重试故障才切换下一档。 */
    val signModes: List<String>,
    val params: List<Pair<String, String>>,
    val retry: RetryPolicy,
    /** long: 取值路径与请求配置一同保存；宿主解析器只接受自己支持的业务结构。 */
    val extract: String?,
    val headers: Map<String, String> = emptyMap(),
    val body: String? = null
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
    private val requiredEndpoints = setOf("home", "search", "detail", "media")
    private val variables = mapOf(
        "home" to setOf("tabType", "offset", "sessionId", "selectedItems"),
        "search" to setOf("query"),
        "detail" to setOf("seriesId"),
        "media" to setOf("videoId")
    )
    private val retryKinds = setOf("network", "http5xx", "http429", "empty", "nonJson", "missingData")
    private val variablePattern = Regex("\\$([A-Za-z_][A-Za-z0-9_]*)")

    fun parse(text: String): Recipe {
        val root = JSONObject(text)
        val schema = if (root.has("schemaVersion")) integer(root.get("schemaVersion"), "schemaVersion") else 1L
        require(schema == 1L) { "配方 schemaVersion 不兼容" }
        val version = integer(root.get("version"), "version")
        require(version in 1L..Int.MAX_VALUE.toLong()) { "配方 version 必须为正整数" }
        val hosts = root.optJSONObject("hosts").toStringMap()
        val userAgents = root.optJSONObject("userAgents").toStringMap()
        require(userAgents.values.none { it.contains('\r') || it.contains('\n') }) { "配方 User-Agent 无效" }
        val endpointsJson = root.optJSONObject("endpoints")
            ?: throw IllegalArgumentException("配方缺少 endpoints")

        val endpoints = LinkedHashMap<String, Endpoint>()
        for (id in endpointsJson.keys()) {
            val obj = endpointsJson.optJSONObject(id) ?: throw IllegalArgumentException("端点 $id 必须为对象")
            endpoints[id] = parseEndpoint(id, obj)
        }
        require(endpoints.keys.containsAll(requiredEndpoints)) { "配方缺少必需端点: ${requiredEndpoints - endpoints.keys}" }
        for ((id, endpoint) in endpoints) {
            val uri = runCatching { URI(hosts[endpoint.host] ?: endpoint.host) }.getOrNull()
            require(uri != null && uri.scheme == "https" && !uri.host.isNullOrEmpty() &&
                uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
                (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/")) { "端点 $id 的 host 必须为 HTTPS 源站" }
            val allowed = variables[id].orEmpty()
            val templates = endpoint.params.map { it.second } + endpoint.headers.values +
                listOfNotNull(endpoint.body, endpoint.extract)
            require(templates.all { template -> variablePattern.findAll(template).all { it.groupValues[1] in allowed } }) {
                "端点 $id 引用了宿主未提供的变量"
            }
        }
        return Recipe(
            version = version.toInt(),
            updated = root.optString("updated"),
            hosts = hosts,
            userAgents = userAgents,
            endpoints = endpoints
        )
    }

    private fun parseEndpoint(id: String, obj: JSONObject): Endpoint {
        val path = obj.optString("path")
        require(path.startsWith("/") && !path.startsWith("//") &&
            path.none { it.isWhitespace() || it in "?#" }) { "端点 $id 的 path 无效" }
        val hostKey = obj.optString("host")
        require(hostKey.isNotEmpty()) { "端点 $id 缺少 host" }
        require(obj.optString("versionCode").matches(Regex("[0-9]{1,9}"))) { "端点 $id 缺少有效 versionCode" }
        for (key in listOf("params", "sign")) {
            require(!obj.has(key) || obj.optJSONArray(key) != null) { "端点 $id 的 $key 必须为数组" }
        }
        require(!obj.has("retry") || obj.optJSONObject("retry") != null) { "端点 $id 的 retry 必须为对象" }
        require(!obj.has("headers") || obj.optJSONObject("headers") != null) { "端点 $id 的 headers 必须为对象" }

        val params = ArrayList<Pair<String, String>>()
        val arr = obj.optJSONArray("params")
        if (arr != null) {
            for (i in 0 until arr.length()) {
                val pair = arr.optJSONArray(i) ?: throw IllegalArgumentException("端点 $id 的参数必须为键值对")
                require(pair.length() == 2 && pair.get(0) is String && pair.get(1) is String &&
                    pair.getString(0).matches(Regex("[A-Za-z_][A-Za-z0-9_]*"))) { "端点 $id 的参数无效" }
                params.add(pair.optString(0) to pair.optString(1))
            }
        }

        val retryJson = obj.optJSONObject("retry")
        for (key in listOf("on", "backoffMs")) {
            require(retryJson == null || !retryJson.has(key) || retryJson.optJSONArray(key) != null) { "端点 $id 的 retry.$key 必须为数组" }
        }
        val backoff = ArrayList<Long>()
        retryJson?.optJSONArray("backoffMs")?.let { list ->
            for (i in 0 until list.length()) backoff.add(integer(list.get(i), "$id.retry.backoffMs"))
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
        val method = obj.optString("method", "GET").uppercase()
        val body = obj.optJSONObject("body")?.toString()
        require(!obj.has("body") || body != null) { "端点 $id 的 body 必须为 JSON 对象" }
        require(method in setOf("GET", "POST")) { "端点 $id 不支持方法 $method" }
        require(body == null || method == "POST") { "端点 $id 只有 POST 可带 body" }
        // long: 旧签名只实现了空请求体摘要，不能把带 body 的请求签成看似成功但实际无效。
        require(body == null || signs.all { it == "none" }) { "端点 $id 的签名暂不支持请求体" }
        require(signs.size in 1..3 && signs.all { it in setOf("none", "legacy") } && signs.distinct() == signs) {
            "端点 $id 的签名档位不受支持或重复"
        }
        val attempts = if (retryJson?.has("attempts") == true) integer(retryJson.get("attempts"), "$id.retry.attempts") else 3L
        require(attempts in 1L..5L && backoff.size <= 4 && backoff.all { it in 0L..10000L }) {
            "端点 $id 的重试预算超限"
        }
        require(retryOn.all { it in retryKinds || it.matches(Regex("code:[0-9]{1,9}")) }) { "端点 $id 的重试条件不受支持" }
        val extract = obj.optString("extract").takeIf { it.isNotBlank() }
        require(extract != null && extract.split('.').all { it.isNotBlank() }) { "端点 $id 缺少有效 extract" }
        val headers = obj.optJSONObject("headers").toStringMap()
        require(headers.all { (key, value) -> key.matches(Regex("[A-Za-z0-9-]+")) &&
            !value.contains('\r') && !value.contains('\n') &&
            key.lowercase() !in setOf("host", "content-length", "transfer-encoding") &&
            !key.startsWith("X-Gorgon", true) && !key.startsWith("X-Argus", true) &&
            !key.startsWith("X-Ladon", true) && !key.startsWith("X-Khronos", true) && !key.startsWith("X-SS-", true) }) {
            "端点 $id 的请求头无效或覆盖签名头"
        }

        return Endpoint(
            id = id,
            desc = obj.optString("desc"),
            method = method,
            host = hostKey,
            path = path,
            versionCode = obj.optString("versionCode"),
            signModes = signs,
            params = params,
            retry = RetryPolicy(
                attempts = attempts.toInt(),
                backoffMs = if (backoff.isEmpty()) listOf(600L, 1200L) else backoff,
                on = retryOn
            ),
            extract = extract,
            headers = headers,
            body = body
        )
    }

    private fun integer(value: Any, field: String): Long {
        // long: 配方版本和请求预算不能被 optInt/optLong 截断或默认化，否则坏配置可能伪装成兼容版本。
        require(value is Number && value.toString().matches(Regex("-?(0|[1-9][0-9]*)"))) { "配方 $field 必须为整数" }
        return value.toString().toLongOrNull() ?: throw IllegalArgumentException("配方 $field 超出整数范围")
    }

    private fun JSONObject?.toStringMap(): Map<String, String> {
        if (this == null) return emptyMap()
        val out = LinkedHashMap<String, String>()
        for (key in keys()) {
            require(get(key) is String) { "配方 $key 必须为字符串" }
            out[key] = getString(key)
        }
        return out
    }
}
