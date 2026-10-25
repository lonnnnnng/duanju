package com.duanju.player.core


import android.content.Context
import org.json.JSONArray
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

    data class DirectDrama(
        val id: String,        // 纯数字 seriesId
        val title: String,
        val episodes: Int,
        val coverUrl: String?  // 绝对地址
    )

    data class CatalogPage(
        val dramas: List<DirectDrama>,
        val nextOffset: Int,
        val hasMore: Boolean,
        val sessionId: String
    )

    private val executor = Executors.newSingleThreadExecutor()
    private val random = SecureRandom()

    fun deviceId(): String {
        // 与服务器一致：19 位正数，落在 [1e18, 9e18)
        val bytes = ByteArray(8)
        random.nextBytes(bytes)
        var v = 0L
        for (b in bytes) v = (v shl 8) or (b.toLong() and 0xFF)
        val positive = (v and Long.MAX_VALUE) % 8_000_000_000_000_000_000L
        return (1_000_000_000_000_000_000L + positive).toString()
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
        // 优先 ffmpeg 直接拉流（网络好时一步到位且不落加密原片）；
        // 设备代理/VPN 等导致 ffmpeg 网络 I/O 失败时，自动回退 Java 下载 + 本地解密。
        val started = System.currentTimeMillis()
        try {
            decryptViaFfmpeg(sourceUrl, keyHex, outFile)
            android.util.Log.d("DirectClient", "路径=ffmpeg直连 耗时=${System.currentTimeMillis() - started}ms 大小=${outFile.length()}")
        } catch (e: Exception) {
            android.util.Log.d("DirectClient", "路径=Java回退 原因=${e.message?.take(80)}")
            val local = File(outFile.parentFile, outFile.name + ".src")
            local.delete()
            javaDownload(sourceUrl, local)
            try {
                decryptLocal(local, keyHex, outFile)
                android.util.Log.d("DirectClient", "路径=Java回退成功 总耗时=${System.currentTimeMillis() - started}ms 大小=${outFile.length()}")
            } finally {
                local.delete()
            }
        }
    }

    /** ffmpeg 直接拉流并解密（源是 http/https）。 */
    private fun decryptViaFfmpeg(sourceUrl: String, keyHex: String, outFile: File) {
        val args = arrayOf(
            "-y", "-hide_banner", "-loglevel", "error", "-nostdin",
            "-rw_timeout", "20000000",
            "-protocol_whitelist", "file,http,https,tcp,tls,crypto",
            "-decryption_key", keyHex,
            "-i", sourceUrl,
            "-map", "0:v:0", "-map", "0:a:0?",
            "-c", "copy",
            "-avoid_negative_ts", "make_zero",
            "-movflags", "+faststart",
            "-f", "mp4", File(outFile.parentFile, outFile.name + ".tmp").absolutePath
        )
        finishFfmpeg(args, File(outFile.parentFile, outFile.name + ".tmp"), outFile)
    }

    /** ffmpeg 解密本地加密原片。 */
    private fun decryptLocal(local: File, keyHex: String, outFile: File) {
        val tmp = File(outFile.parentFile, outFile.name + ".tmp")
        val args = arrayOf(
            "-y", "-hide_banner", "-loglevel", "error", "-nostdin",
            "-protocol_whitelist", "file,pipe",
            "-decryption_key", keyHex,
            "-i", local.absolutePath,
            "-map", "0:v:0", "-map", "0:a:0?",
            "-c", "copy",
            "-avoid_negative_ts", "make_zero",
            "-movflags", "+faststart",
            "-f", "mp4", tmp.absolutePath
        )
        finishFfmpeg(args, tmp, outFile)
    }

    private fun finishFfmpeg(args: Array<String>, tmp: File, outFile: File) {
        val session = com.arthenica.ffmpegkit.FFmpegKit.executeWithArguments(args)
        val rc = session.getReturnCode()
        if (!com.arthenica.ffmpegkit.ReturnCode.isSuccess(rc) || !tmp.exists() || tmp.length() == 0L) {
            tmp.delete()
            throw DirectException("解密失败: rc=" + rc?.value + " " + session.getOutput().takeLast(300))
        }
        if (outFile.exists()) outFile.delete()
        if (!tmp.renameTo(outFile)) {
            tmp.delete()
            throw DirectException("缓存写入失败")
        }
    }

    /** Java 层直连下载（带 Referer，支持断点不需要）。 */
    private fun javaDownload(url: String, target: File) {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 60000
            setRequestProperty("User-Agent", HongguoSign.UA_MODEL)
            setRequestProperty("Referer", "https://novel.snssdk.com/")
        }
        if (conn.responseCode !in 200..299) {
            conn.disconnect()
            throw DirectException("下载失败 HTTP " + conn.responseCode)
        }
        target.outputStream().use { out -> conn.inputStream.copyTo(out, 64 * 1024) }
        conn.disconnect()
        if (target.length() == 0L) throw DirectException("下载为空")
    }

    /** 清理残缺的临时文件。 */
    fun cleanPartial(cacheDir: File) {
        cacheDir.listFiles()?.forEach { if (it.name.endsWith(".tmp") || it.name.endsWith(".src")) it.delete() }
    }

    /** 剧库列表（App 官方接口，旧版签名，分页）。 */
    fun catalog(
        genreKey: String, genreScene: String, offset: Int, sessionId: String,
        deviceId: String, installId: String
    ): CatalogPage {
        val query = HongguoSign.encodeQuery(
            HongguoSign.baseQuery(deviceId, installId) + listOf("_rticket" to System.currentTimeMillis().toString())
        )
        val headers = HongguoSign.signLegacyHeaders(query, "8662")
        val payload = JSONObject().apply {
            put("req_scene", genreScene)
            put("offset", offset)
            put("limit", 18)
            put("req_type", "only_content")
            put("need_selector_panel", false)
            put("client_req_type", if (offset > 0) 2 else 3)
            put("session_id", sessionId)
            put("filter_ids", "")
            put("select_items", JSONObject().apply {
                put("genre", JSONArray().put(genreKey))
                put("sort", JSONArray().put("online_time"))
                put("gender", JSONArray())
                put("category_dim_theme", JSONArray())
                put("category_dim_role", JSONArray())
                put("category_dim_epoch", JSONArray())
                put("online_time", JSONArray())
                put("creation_status", JSONArray())
            })
        }
        val text = httpPost("$API_HOST/reading/distribution/category/landpage/v/", query, payload.toString(), headers)
        val root = JSONObject(text)
        val data = root.optJSONObject("data") ?: throw DirectException("分类数据为空")
        val rows = data.optJSONArray("video_data") ?: throw DirectException("分类无剧集")
        val dramas = ArrayList<DirectDrama>(rows.length())
        for (i in 0 until rows.length()) {
            val row = rows.optJSONObject(i) ?: continue
            // 分类接口行内字段：title / cover / episode_cnt / series_id（与 detail、搜索接口的 series_title/series_cover 不同）
            val id = row.optString("series_id")
            if (id.isEmpty()) continue
            dramas.add(
                DirectDrama(
                    id = id,
                    title = row.optString("title").ifEmpty { row.optString("series_title").ifEmpty { "短剧" } },
                    episodes = row.optInt("episode_cnt", 0),
                    coverUrl = row.optString("cover").ifEmpty { row.optString("series_cover") }.ifEmpty { null }
                )
            )
        }
        return CatalogPage(
            dramas = dramas,
            nextOffset = data.optInt("next_offset", offset + rows.length()),
            hasMore = data.optBoolean("has_more", false),
            sessionId = data.optString("session_id", "")
        )
    }

    fun dumpCatalogRaw(
        genreKey: String, genreScene: String, offset: Int, sessionId: String,
        deviceId: String, installId: String
    ): String {
        val query = HongguoSign.encodeQuery(
            HongguoSign.baseQuery(deviceId, installId) + listOf("_rticket" to System.currentTimeMillis().toString())
        )
        val headers = HongguoSign.signLegacyHeaders(query, "8662")
        val payload = JSONObject().apply {
            put("req_scene", genreScene)
            put("offset", offset)
            put("limit", 18)
            put("req_type", "only_content")
            put("need_selector_panel", false)
            put("client_req_type", if (offset > 0) 2 else 3)
            put("session_id", sessionId)
            put("filter_ids", "")
            put("select_items", JSONObject().apply {
                put("genre", JSONArray().put(genreKey))
                put("sort", JSONArray().put("online_time"))
                put("gender", JSONArray())
                put("category_dim_theme", JSONArray())
                put("category_dim_role", JSONArray())
                put("category_dim_epoch", JSONArray())
                put("online_time", JSONArray())
                put("creation_status", JSONArray())
            })
        }
        return httpPost("$API_HOST/reading/distribution/category/landpage/v/", query, payload.toString(), headers)
    }

    /** 官网搜索（无签名，GET hongguoduanju.com/api/search/result）。 */
    fun searchOfficial(keyword: String): List<DirectDrama> {
        val url = "https://hongguoduanju.com/api/search/result?query=" +
            java.net.URLEncoder.encode(keyword, "UTF-8")
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
            setRequestProperty("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
            setRequestProperty("Referer", "https://hongguoduanju.com/")
        }
        val code = conn.responseCode
        val text = (if (code in 200..299) conn.inputStream else conn.errorStream)?.bufferedReader()?.use { it.readText() } ?: ""
        conn.disconnect()
        if (code != 200) throw DirectException("搜索 HTTP $code")
        val root = JSONObject(text)
        val rows = root.optJSONArray("searchList") ?: return emptyList()
        val out = ArrayList<DirectDrama>(rows.length())
        for (i in 0 until rows.length()) {
            val row = rows.optJSONObject(i) ?: continue
            val vd = row.optJSONObject("video_data") ?: continue
            val id = vd.optString("series_id")
            if (id.isEmpty()) continue
            out.add(
                DirectDrama(
                    id = id,
                    title = vd.optString("series_title").ifEmpty { row.optString("name").ifEmpty { "短剧" } },
                    episodes = vd.optInt("episode_cnt", 0),
                    coverUrl = vd.optString("series_cover").ifEmpty { null }
                )
            )
        }
        return out
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
        if (text.isEmpty()) throw DirectException("空响应(HTTP $code)")
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