package com.duanju.player.core

import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.File
import java.io.InputStream
import java.net.HttpURLConnection
import java.security.MessageDigest
import java.time.Instant

/** long: 仅显式联网验收使用，保留每次尝试的真实参数与正文，不能用最终成功掩盖中间失败。 */
internal class ApiEvidenceConnection(private val delegate: HttpURLConnection, private val folder: File) : HttpURLConnection(delegate.url) {
    private val started = System.nanoTime()
    private var status = -1
    private var requestSaved = false
    private var responseSaved = false

    init { check(folder.mkdirs()) }

    private fun saveRequest() {
        if (requestSaved) return
        requestSaved = true
        File(folder, "request.json").writeText(JSONObject()
            .put("startedAt", Instant.now().toString()).put("method", delegate.requestMethod)
            .put("url", url.toString()).put("headersExplicit", JSONObject(delegate.requestProperties))
            .put("body", JSONObject.NULL)
            .put("transport", "JVM HttpURLConnection；自动请求头由 HTTP 栈补齐，非抓包")
            .toString(2))
    }

    override fun getResponseCode(): Int {
        saveRequest()
        return try { delegate.responseCode.also { status = it } } catch (e: Exception) {
            File(folder, "error.json").writeText(JSONObject().put("error", e.toString()).toString(2))
            throw e
        }
    }

    private fun record(stream: InputStream?): InputStream? {
        if (stream == null) return null
        // long: 本轮取证保留完整响应，避免流式搜索提前结束后留下无法复查的半份 JSON；不用于生产请求。
        val bytes = stream.use { it.readBytes() }
        File(folder, "response.body").writeBytes(bytes)
        val headers = JSONArray()
        delegate.headerFields.forEach { (key, values) -> headers.put(JSONArray(listOf(key ?: ":status", values))) }
        File(folder, "response.json").writeText(JSONObject().put("status", status).put("headers", headers)
            .put("bytes", bytes.size).put("sha256", MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) })
            .put("finishedAt", Instant.now().toString()).put("elapsedMs", (System.nanoTime() - started) / 1_000_000).toString(2))
        responseSaved = true
        return ByteArrayInputStream(bytes)
    }

    override fun getInputStream(): InputStream = record(delegate.inputStream)!!
    override fun getErrorStream(): InputStream? = record(delegate.errorStream)
    override fun setRequestMethod(method: String) { delegate.requestMethod = method }
    override fun setRequestProperty(key: String, value: String) { delegate.setRequestProperty(key, value) }
    override fun getRequestProperty(key: String): String? = delegate.getRequestProperty(key)
    override fun setConnectTimeout(timeout: Int) { delegate.connectTimeout = timeout }
    override fun setReadTimeout(timeout: Int) { delegate.readTimeout = timeout }
    override fun setInstanceFollowRedirects(follow: Boolean) { delegate.instanceFollowRedirects = follow }
    override fun connect() { saveRequest(); delegate.connect() }
    override fun usingProxy() = delegate.usingProxy()
    override fun getContentType(): String? = delegate.contentType
    override fun disconnect() {
        if (status >= 0 && !responseSaved) runCatching {
            record(if (status >= 400) delegate.errorStream else delegate.inputStream)
        }.onFailure { File(folder, "error.json").writeText(JSONObject().put("error", it.toString()).toString(2)) }
        delegate.disconnect()
    }
}
