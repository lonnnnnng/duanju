package com.duanju.player.core

import android.content.Context
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.SecureRandom
import java.util.concurrent.Callable
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * 零服务器直连客户端：手机直接调用红果官方接口（内置签名），
 * 拿到 CDN 直链 + spade_a，再由本地 ffmpeg 解密。全程不经 duanju 服务器。
 */
object DirectClient {

    private const val API_HOST = "https://api5-normal-sinfonlineb.fqnovel.com"

    data class DirectMedia(
        val url: String,
        val keyHex: String,
        val durationSec: Double,
        val quality: Int,
        val variants: List<Pair<Int, String>> // quality -> url
    )

    class DirectException(message: String) : Exception(message)

    private val executor = Executors.newSingleThreadExecutor()
    private val random = SecureRandom()

    fun deviceId(): String {
        val bytes = ByteArray(8)
        random.nextBytes(bytes)
        var v = 0L
        for (b in bytes) v = (v shl 8) or (b.toLong() and 0xFF)
        return (1_000_000_000_000_000_000L + v % 8_000_000_000_000_000_000L).toString()
    }

    fun <T> submit(task: Callable<T>): Future<T> = executor.submit(task)

    /** 分集列表（detail 接口，旧版签名）。返回 videoID 列表（按集序）。 */
    fun chapters(dramaId: String, deviceId: String, installId: String): List<String> {
        val query = HongguoSign.encodeQuery(
            HongguoSign.baseQuery(deviceId, installId) + listOf("_rticket" to System.currentTimeMillis().toString())
        )
        val headers = HongguoSign.signLegacyHeaders(query, "8662")
        val body = """{"series_id":"$dramaId"}"""
        val text = httpPost("$API_HOST/novel/player/video_detail/v1/", query, body, headers)
        val root = JSONObject(text)
        val data = root.optJSONObject("data")?.optJSONObject("video_data") ?: throw DirectException("详情无分集数据")
        val list = data.optJSONArray("video_list") ?: throw DirectException("分集列表为空")
        val ids = ArrayList<String>(list.length())
        for (i in 0 until list.length()) {
            val row = list.optJSONObject(i) ?: continue
            val vid = row.optString("vid")
            if (vid.isNotEmpty()) ids.add(vid)
        }
        ids.sortBy { vid -> vid }
        return ids
    }

    /** 播放模型（新版签名），返回直链 + 密钥 + 清晰度列表。 */
    fun media(videoId: String, deviceId: String, installId: String): DirectMedia {
        val query = HongguoSign.encodeQuery(
            HongguoSign.baseQuery(deviceId, installId) + listOf(
                "_rticket" to System.currentTimeMillis().toString(),
                "ts" to (System.currentTimeMillis() / 1000).toString()
            )
        )
        val body = """{"mixed_video_id_map":{"1004":["$videoId"]},"biz_param":{"detail_page_version":0,"device_level":3,"disable_digg_stat":false,"need_all_video_definition":true,"need_mp4_align":false,"use_os_player":false,"use_server_dns":false,"video_platform":1024}}"""
        val headers = HongguoSign.signModelHeaders(query, body.toByteArray(Charsets.UTF_8))
        val text = httpPost("$API_HOST/novel/player/multi_video_model/v1/", query, body, headers)
        val root = JSONObject(text)
        if (root.optInt("code", 0) != 0) {
            throw DirectException("取流失败(${root.optInt("code", 0)})")
        }
        val entry = root.optJSONObject("data")?.optJSONObject(videoId)
            ?: throw DirectException("未返回该分集")
        val modelRaw = entry.opt("video_model")
        val model = when (modelRaw) {
            is String -> JSONObject(modelRaw)
            else -> modelRaw as? JSONObject ?: throw DirectException("video_model 无效")
        }
        val videoList = model.optJSONArray("video_list") ?: throw DirectException("无清晰度列表")
        var best: JSONObject? = null
        var bestQuality = -1
        val variants = ArrayList<Pair<Int, String>>()
        var spadeA: String? = null
        for (i in 0 until videoList.length()) {
            val row = videoList.optJSONObject(i) ?: continue
            val codec = row.optJSONObject("video_meta")?.optString("codec_type") ?: ""
            if (codec.contains("bytevc2")) continue // 不兼容编码
            val url = row.optString("main_url")
            if (!url.startsWith("https://")) continue
            val def = row.optJSONObject("video_meta")?.optString("definition") ?: ""
            val quality = Regex("(\\d{3,4})p").find(def)?.groupValues?.get(1)?.toIntOrNull() ?: 0
            val sa = row.optJSONObject("encrypt_info")?.optString("spade_a") ?: ""
            if (sa.isNotEmpty()) spadeA = sa
            variants.add(quality to url)
            if (quality > bestQuality) {
                bestQuality = quality
                best = row
            }
        }
        if (best == null) throw DirectException("无兼容媒体")
        val key = HongguoSign.contentKey(spadeA ?: "") ?: throw DirectException("密钥派生失败")
        val duration = model.optDouble("video_duration", 0.0)
        return DirectMedia(
            url = best.optString("main_url"),
            keyHex = key.joinToString("") { "%02x".format(it) },
            durationSec = duration,
            quality = bestQuality,
            variants = variants
        )
    }

    /** 本地 ffmpeg 解密下载：CDN 直链 → -c copy -decryption_key → 清晰 MP4。
     *  写临时文件，成功后原子重命名，避免进程被杀留下无 moov 的坏文件。 */
    fun decryptTo(sourceUrl: String, keyHex: String, outFile: File) {
        val tmp = File(outFile.parentFile, outFile.name + ".tmp")
        val args = arrayOf(
            "-y", "-hide_banner", "-loglevel", "error", "-nostdin",
            "-rw_timeout", "20000000",
            "-protocol_whitelist", "http,https,tcp,tls,crypto",
            "-decryption_key", keyHex,
            "-i", sourceUrl,
            "-map", "0:v:0", "-map", "0:a:0?",
            "-c", "copy",
            "-avoid_negative_ts", "make_zero",
            "-movflags", "+faststart",
            "-f", "mp4", tmp.absolutePath
        )
        val session = com.arthenica.ffmpegkit.FFmpegKit.executeWithArguments(args)
        val rc = session.getReturnCode()
        if (!com.arthenica.ffmpegkit.ReturnCode.isSuccess(rc) || !tmp.exists() || tmp.length() == 0L) {
            tmp.delete()
            throw DirectException("解密失败: rc=${rc?.value} ${session.getOutput().takeLast(300)}")
        }
        if (outFile.exists()) outFile.delete()
        if (!tmp.renameTo(outFile)) {
            tmp.delete()
            throw DirectException("缓存写入失败")
        }
    }

    /** 清理残缺的临时文件。 */
    fun cleanPartial(cacheDir: File) {
        cacheDir.listFiles()?.forEach { if (it.name.endsWith(".tmp")) it.delete() }
    }

    private fun httpPost(url: String, query: String, body: String, headers: Map<String, String>): String {
        val conn = (URL("$url?$query").openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 15000
            readTimeout = 60000
            doOutput = true
        }
        conn.setRequestProperty("User-Agent", HongguoSign.UA_MODEL)
        conn.setRequestProperty("Accept", "application/json")
        conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")
        headers.forEach { (k, v) -> conn.setRequestProperty(k, v) }
        conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
        val code = conn.responseCode
        val stream = if (code in 200..299) conn.inputStream else conn.errorStream
        val text = stream?.bufferedReader()?.use { it.readText() } ?: ""
        conn.disconnect()
        if (code != 200) throw DirectException("HTTP $code")
        return text
    }

    // 设备身份持久化（与服务器无关，App 自己的随机身份）
    fun identity(context: Context): Pair<String, String> {
        val prefs = context.getSharedPreferences("duanju", Context.MODE_PRIVATE)
        var dev = prefs.getString("direct_device", null)
        var ins = prefs.getString("direct_install", null)
        if (dev == null || ins == null) {
            dev = deviceId()
            ins = deviceId()
            prefs.edit().putString("direct_device", dev).putString("direct_install", ins).apply()
        }
        return dev to ins
    }
}