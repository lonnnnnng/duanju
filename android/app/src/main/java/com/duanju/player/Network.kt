package com.duanju.player

import org.json.JSONObject
import java.net.CookieHandler
import java.net.CookieManager
import java.net.HttpURLConnection
import java.net.URL

object Network {

    init {
        CookieHandler.setDefault(CookieManager())
    }

    fun httpText(
        method: String,
        url: String,
        body: String? = null,
        headers: Map<String, String> = emptyMap(),
        readTimeoutMs: Int = 30000,
        connectTimeoutMs: Int = 15000
    ): String {
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = connectTimeoutMs
            readTimeout = readTimeoutMs
        }
        headers.forEach { (key, value) -> connection.setRequestProperty(key, value) }
        if (body != null) {
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", "application/json")
            connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
        }
        val code = connection.responseCode
        val stream = if (code in 200..299) connection.inputStream else connection.errorStream
        val text = stream?.bufferedReader()?.use { it.readText() } ?: ""
        connection.disconnect()
        if (code !in 200..299) {
            val message = runCatching {
                JSONObject(text).optString("error").ifBlank { text }
            }.getOrDefault(text)
            throw RuntimeException("HTTP $code：${message.ifBlank { "请求失败" }}")
        }
        return text
    }

    fun get(url: String, readTimeoutMs: Int = 30000) = httpText("GET", url, readTimeoutMs = readTimeoutMs)
    fun postJson(url: String, body: String, headers: Map<String, String> = emptyMap(), readTimeoutMs: Int = 30000) =
        httpText("POST", url, body, headers, readTimeoutMs = readTimeoutMs)

    @Volatile
    private var viewerId: String? = null

    fun ensureViewer(base: String): String {
        viewerId?.let { return it }
        // 第一次请求仅设置身份 Cookie，第二次确认并返回 viewer id。
        kotlin.synchronized(this) {
            viewerId?.let { return it }
            get("$base/api/ui/viewer")
            val body = get("$base/api/ui/viewer?confirm=1")
            viewerId = JSONObject(body).getString("id")
        }
        return viewerId!!
    }

    fun viewerHeaders(base: String): Map<String, String> = mapOf("X-Duanju-Viewer" to ensureViewer(base))
}