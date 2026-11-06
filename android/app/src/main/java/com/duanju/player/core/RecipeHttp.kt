package com.duanju.player.core

import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/**
 * 配方执行器（L1）+ 具名签名原语（L2）。
 *
 * 流程：拼 URL → 按 `sign` 顺序尝试签名档位 → 每档按 `retry` 重试 → 返回正文。
 * 「200 + 0 字节」视为网关拦截，直接切下一档（这正是上游在签名/免签之间摆动时的形态）。
 */
internal object RecipeHttp {

    class Prepared(val url: String, val headers: Map<String, String>)

    class GatewayEmptyException(message: String) : Exception(message)
    class HttpStatusException(val code: Int) : Exception("HTTP $code")

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
        val query = StringBuilder(commonParams(deviceId, installId, endpoint.versionCode))
        for ((key, value) in endpoint.params) {
            val resolved = render(value, vars)
            // 变量型参数为空时整项省略（如首页首次加载没有 session_id / selected_items）。
            if (value.startsWith("$") && resolved.isEmpty()) continue
            query.append('&').append(key).append('=').append(urlEncode(resolved))
        }
        return host + endpoint.path + "?" + query
    }

    fun render(template: String, vars: Map<String, String>): String {
        if (!template.contains('$')) return template
        var out = template
        for ((k, v) in vars) out = out.replace("\$$k", v)
        return out
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
            // 预留给「新版带随机数的 X-Gorgon」等 POST 场景；GET 暂不需要。
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
    ): String {
        val recipe = RecipeStore.recipe()
        val endpoint = recipe.endpoint(endpointId)
        var last: Exception? = null

        for (mode in endpoint.signModes) {
            val url = buildUrl(endpoint, recipe, vars, deviceId, installId)
            val rawQuery = URL(url).query.orEmpty()
            val headers = LinkedHashMap<String, String>()
            applySign(mode, rawQuery, recipe, headers)

            var attempt = 1
            while (attempt <= endpoint.retry.attempts) {
                cancellation?.check()
                try {
                    val body = get(url, headers, cancellation)
                    if (body.isEmpty()) {
                        // 空正文可能是瞬时抖动，也可能是网关拦截：
                        // 先在同档内退避重试，仍为空再切下一个签名档位。
                        last = GatewayEmptyException("空响应(HTTP 200) [$mode]")
                        if (attempt < endpoint.retry.attempts) {
                            sleep(endpoint.retry.backoffFor(attempt))
                            attempt++
                            continue
                        }
                        break
                    }
                    return body
                } catch (e: GatewayEmptyException) {
                    last = e
                    break
                } catch (e: HttpStatusException) {
                    last = e
                    val retryable = e.code >= 500 && "http5xx" in endpoint.retry.on
                    if (retryable && attempt < endpoint.retry.attempts) {
                        sleep(endpoint.retry.backoffFor(attempt))
                        attempt++
                        continue
                    }
                    if (retryable) break
                    throw DirectClient.DirectException("HTTP ${e.code}")
                } catch (e: Exception) {
                    cancellation?.check()
                    last = e
                    if ("network" in endpoint.retry.on && attempt < endpoint.retry.attempts) {
                        sleep(endpoint.retry.backoffFor(attempt))
                        attempt++
                        continue
                    }
                    throw DirectClient.DirectException(e.message ?: e.toString())
                }
            }
        }
        throw DirectClient.DirectException(last?.message ?: "接口失败")
    }

    /** 给需要自行流式读取的调用方（搜索）准备 URL 与头。 */
    fun prepare(
        endpointId: String,
        vars: Map<String, String>,
        deviceId: String,
        installId: String,
        signMode: String? = null
    ): Prepared {
        val recipe = RecipeStore.recipe()
        val endpoint = recipe.endpoint(endpointId)
        val url = buildUrl(endpoint, recipe, vars, deviceId, installId)
        val mode = signMode ?: endpoint.signModes.first()
        val headers = LinkedHashMap<String, String>()
        applySign(mode, URL(url).query.orEmpty(), recipe, headers)
        return Prepared(url, headers)
    }

    private fun get(url: String, headers: Map<String, String>, cancellation: TaskCancellation?): String {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
            headers.forEach { (k, v) -> setRequestProperty(k, v) }
        }
        val registration = cancellation?.onCancel { conn.disconnect() }
        return try {
            cancellation?.check()
            val code = conn.responseCode
            if (code !in 200..299) throw HttpStatusException(code)
            val text = conn.inputStream?.bufferedReader()?.use { it.readText() }.orEmpty()
            cancellation?.check()
            if (text.isEmpty()) throw GatewayEmptyException("空响应(HTTP $code)")
            text
        } finally {
            registration?.close()
            conn.disconnect()
        }
    }

    private fun sleep(ms: Long) = Thread.sleep(ms)

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
