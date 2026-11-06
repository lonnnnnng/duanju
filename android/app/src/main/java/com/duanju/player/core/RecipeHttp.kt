package com.duanju.player.core

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.util.concurrent.CancellationException

/**
 * long: 普通 JSON 与流式搜索共用同一请求状态机，避免两个入口的签名和重试策略漂移。
 */
internal object RecipeHttp {
    internal var openConnection: (URL) -> HttpURLConnection = { it.openConnection() as HttpURLConnection }

    class ResponseFailure(val kind: String, message: String) : IOException(message)

    // ==================== 公共参数（稳定，留在代码里） ====================

    fun commonParams(deviceId: String, installId: String, versionCode: String): String {
        val dev = deviceId.takeIf { it.length >= 6 } ?: "1234567890123456"
        val ins = installId.takeIf { it.length >= 6 } ?: "1234567890123456"
        val versionName = when (versionCode) {
            "74232" -> "7.4.2.32"
            "73532" -> "7.3.5.32"
            else -> "6.5.5.32"
        }
        return "iid=$ins&device_id=$dev&ac=wifi&channel=hongguo_baidu_pz_kaiping0105_android" +
            "&aid=8662&app_name=novelread&version_code=$versionCode&version_name=$versionName" +
            "&device_platform=android&os=android&ssmix=a&device_type=Redmi+Note+8+Pro&device_brand=Redmi" +
            "&language=zh&os_api=36&os_version=16&manifest_version_code=$versionCode" +
            "&resolution=1080*2198&dpi=440&update_version_code=$versionCode&pv_player=$versionCode&gender=2" +
            "&need_personal_recommend=1&player_so_load=1&is_android_pad_screen=0&host_abi=arm64-v8a" +
            "&dragon_device_type=phone&rom_version=BP4A.251205.006+release-keys&compliance_status=0" +
            "&cdid=1f1d14b3-8c25-4733-9f6b-2fdc01640081"
    }

    // ==================== URL / 参数 ====================

    fun buildUrl(
        endpoint: Endpoint,
        recipe: Recipe,
        vars: Map<String, String>,
        deviceId: String,
        installId: String
    ): String {
        val host = recipe.hosts[endpoint.host] ?: endpoint.host
        val query = LinkedHashMap<String, String>()
        commonParams(deviceId, installId, endpoint.versionCode).split('&').forEach {
            query[it.substringBefore('=')] = it.substringAfter('=')
        }
        for ((key, value) in endpoint.params) {
            val resolved = render(value, vars)
            // long: 配方可覆盖公共参数；重复拼接 version_name 等字段会让上游取值产生歧义。
            if (value.startsWith("$") && resolved.isEmpty()) continue
            query[key] = urlEncode(resolved)
        }
        return host.trimEnd('/') + endpoint.path + "?" + query.entries.joinToString("&") { "${it.key}=${it.value}" }
    }

    fun render(template: String, vars: Map<String, String>): String {
        return Regex("\\$([A-Za-z_][A-Za-z0-9_]*)").replace(template) {
            vars[it.groupValues[1]] ?: throw IllegalArgumentException("缺少配方变量: ${it.value}")
        }
    }

    private fun urlEncode(value: String): String =
        URLEncoder.encode(value, "UTF-8").replace("+", "%20")

    // ==================== L2：具名签名原语 ====================

    private fun applySign(mode: String, query: String, recipe: Recipe, headers: MutableMap<String, String>) {
        when (mode) {
            "none" -> Unit
            "legacy" -> {
                recipe.userAgents["model"]?.let { headers["User-Agent"] = it }
                    ?: headers.put("User-Agent", HongguoSign.UA_MODEL)
                headers.putAll(HongguoSign.legacyHeaders(query))
            }
            else -> throw IllegalStateException("未知签名档位: $mode")
        }
    }

    // ==================== 执行 ====================

    fun execute(
        endpointId: String,
        vars: Map<String, String>,
        deviceId: String,
        installId: String,
        cancellation: TaskCancellation? = null
    ): String = readJson(endpointId, vars, deviceId, installId, cancellation) { text, _ -> text }

    fun <T> readJson(
        endpointId: String,
        vars: Map<String, String>,
        deviceId: String,
        installId: String,
        cancellation: TaskCancellation? = null,
        parse: (String, Endpoint) -> T
    ): T = exchange(endpointId, vars, deviceId, installId, cancellation) { conn, endpoint ->
        val text = conn.inputStream.bufferedReader(Charsets.UTF_8).use { it.readText() }
        if (text.isBlank()) throw ResponseFailure("empty", "空响应(HTTP ${conn.responseCode})")
        checkCode(JSONObject(text))
        parse(text, endpoint)
    }

    fun checkCode(root: JSONObject) {
        if (root.has("code") && root.optInt("code", -1) != 0) {
            val code = root.optInt("code", -1)
            throw ResponseFailure("code:$code", "接口业务错误(code=$code)")
        }
    }

    fun <T> exchange(
        endpointId: String,
        vars: Map<String, String>,
        deviceId: String,
        installId: String,
        cancellation: TaskCancellation? = null,
        read: (HttpURLConnection, Endpoint) -> T
    ): T {
        // long: 更新可能与请求并行；URL、签名和解析路径必须属于同一份配方。
        val recipe = RecipeStore.recipe()
        val endpoint = recipe.endpoint(endpointId)
        val url = buildUrl(endpoint, recipe, vars, deviceId, installId)
        val body = endpoint.body?.let { renderBody(it, vars).toByteArray(Charsets.UTF_8) }
        var last: ResponseFailure? = null
        for (mode in endpoint.signModes) {
            for (attempt in 1..endpoint.retry.attempts) {
                checkCancellation(cancellation)
                val startedAt = System.nanoTime()
                try {
                    return request(url, endpoint, recipe, mode, body, vars, cancellation, read).also {
                        trace("endpoint=$endpointId mode=$mode attempt=$attempt result=ok elapsedMs=${(System.nanoTime() - startedAt) / 1000000}")
                    }
                } catch (e: Exception) {
                    checkCancellation(cancellation)
                    if (e is CancellationException) throw e
                    val failure = when (e) {
                        is ResponseFailure -> e
                        is JSONException -> ResponseFailure("nonJson", "响应 JSON 无效")
                        is IOException -> ResponseFailure("network", e.message ?: "网络请求失败")
                        else -> throw e
                    }
                    last = failure
                    trace("endpoint=$endpointId mode=$mode attempt=$attempt result=${failure.kind} elapsedMs=${(System.nanoTime() - startedAt) / 1000000}")
                    if (failure.kind !in endpoint.retry.on) {
                        throw DirectClient.DirectException(failure.message ?: "接口失败")
                    }
                    // long: 空正文与格式错误也先在当前档位退避；用尽预算后才换下一档。
                    if (attempt < endpoint.retry.attempts) pause(endpoint.retry.backoffFor(attempt), cancellation)
                }
            }
        }
        throw DirectClient.DirectException(last?.message ?: "接口失败", retryable = true)
    }

    private fun trace(message: String) {
        // long: 仅 Debug 输出端点、档位和耗时，便于区分上游空响应与取消竞态，禁止记录请求身份和签名。
        if (com.duanju.player.BuildConfig.DEBUG) android.util.Log.d("RecipeHttp", message)
    }

    private fun <T> request(
        url: String,
        endpoint: Endpoint,
        recipe: Recipe,
        mode: String,
        body: ByteArray?,
        vars: Map<String, String>,
        cancellation: TaskCancellation?,
        read: (HttpURLConnection, Endpoint) -> T
    ): T {
        val headers = LinkedHashMap<String, String>()
        endpoint.headers.forEach { (key, value) ->
            headers[key] = render(value, vars).also { require(!it.contains('\r') && !it.contains('\n')) }
        }
        // long: 每次实际发送都重新生成时间戳和随机数，退避后不能重放旧签名。
        applySign(mode, URL(url).query.orEmpty(), recipe, headers)
        val conn = openConnection(URL(url)).apply {
            requestMethod = endpoint.method
            connectTimeout = 15000
            readTimeout = 30000
            instanceFollowRedirects = false
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json; charset=utf-8")
                setFixedLengthStreamingMode(body.size)
            }
            headers.forEach { (k, v) -> setRequestProperty(k, v) }
        }
        val registration = cancellation?.onCancel { conn.disconnect() }
        return try {
            checkCancellation(cancellation)
            if (body != null) conn.outputStream.use { it.write(body) }
            val code = conn.responseCode
            if (code !in 200..299) {
                val kind = when {
                    code >= 500 -> "http5xx"
                    code == 429 -> "http429"
                    else -> "http"
                }
                throw ResponseFailure(kind, "HTTP $code")
            }
            read(conn, endpoint).also { checkCancellation(cancellation) }
        } finally {
            registration?.close()
            conn.disconnect()
        }
    }

    private fun checkCancellation(cancellation: TaskCancellation?) {
        cancellation?.check()
        if (Thread.currentThread().isInterrupted) throw CancellationException("任务已取消")
    }

    private fun pause(ms: Long, cancellation: TaskCancellation?) {
        // long: 取消无需等完整退避窗口，快速切集时也不能继续发出旧请求。
        var remaining = ms
        while (remaining > 0) {
            checkCancellation(cancellation)
            val step = minOf(remaining, 100L)
            try { Thread.sleep(step) } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                throw CancellationException("任务已取消")
            }
            remaining -= step
        }
        checkCancellation(cancellation)
    }

    private fun renderBody(template: String, vars: Map<String, String>): String {
        fun substitute(value: Any): Any = when (value) {
            is String -> render(value, vars)
            is JSONObject -> JSONObject().also { out -> value.keys().forEach { key -> out.put(key, substitute(value.get(key))) } }
            is JSONArray -> JSONArray().also { out -> for (i in 0 until value.length()) out.put(substitute(value.get(i))) }
            else -> value
        }
        // long: 按 JSON 值替换模板，关键词中的引号不能逃逸成请求体字段。
        return substitute(JSONObject(template)).toString()
    }

    // ==================== 取值 ====================

    /** 按配方 `extract` 路径取值；`$var` 段用变量替换。找不到返回 null。 */
    fun extract(body: String, path: String?, vars: Map<String, String>): Any? {
        if (path.isNullOrEmpty()) return body
        var node: Any = JSONObject(body)
        for (segRaw in path.split('.')) {
            val seg = render(segRaw, vars)
            node = when (val cur = node) {
                is JSONObject -> cur.opt(seg) ?: return null
                is JSONArray -> seg.toIntOrNull()?.let { cur.opt(it) } ?: return null
                else -> return null
            }
        }
        return node
    }
}
