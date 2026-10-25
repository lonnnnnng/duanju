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
        decryptViaFfmpeg(sourceUrl, keyHex, outFile, exportSystemCa(File(outFile.parentFile, "system-ca.pem")))
    }

    /** ffmpeg 直接拉流并解密（源是 http/https）。
     *  ffmpeg 的 openssl 不带 Android 系统CA，证书验证会失败；
     *  这里把系统 cacerts 合并导出为 PEM，经 -cafile 交给 ffmpeg，正常完成校验。 */
    private fun decryptViaFfmpeg(sourceUrl: String, keyHex: String, outFile: File, caFile: File?) {
        val tlsArgs = if (caFile != null) arrayOf("-tls_verify", "1", "-cafile", caFile.absolutePath) else emptyArray()
        val args = arrayOf(
            "-y", "-hide_banner", "-loglevel", "verbose", "-nostdin",
            "-rw_timeout", "20000000",
            "-protocol_whitelist", "file,http,https,tcp,tls,crypto",
            *tlsArgs,
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

    private fun finishFfmpeg(args: Array<String>, tmp: File, outFile: File) {
        val session = com.arthenica.ffmpegkit.FFmpegKit.executeWithArguments(args)
        val rc = session.getReturnCode()
        if (!com.arthenica.ffmpegkit.ReturnCode.isSuccess(rc) || !tmp.exists() || tmp.length() == 0L) {
            tmp.delete()
            android.util.Log.d("DirectClient", "ffmpeg失败日志=" + session.getAllLogsAsString(2000))
            throw DirectException("解密失败: rc=" + rc?.value + " " + session.getOutput().takeLast(300))
        }
        if (outFile.exists()) outFile.delete()
        if (!tmp.renameTo(outFile)) {
            tmp.delete()
            throw DirectException("缓存写入失败")
        }
    }

    /** 合并 Android 系统 CA 为单个 PEM（供 ffmpeg -cafile 使用，一次导出缓存复用）。 */
    private fun exportSystemCa(target: File): File? {
        if (target.exists() && target.length() > 0L) return target
        val dirs = arrayOf(
            "/apex/com.android.conscrypt/cacerts",
            "/system/etc/security/cacerts"
        )
        val out = StringBuilder()
        for (dir in dirs) {
            val d = File(dir)
            val files = d.listFiles() ?: continue
            for (f in files) {
                val pem = f.readText()
                if (pem.contains("BEGIN CERTIFICATE")) out.append(pem).append('\n')
            }
            if (out.isNotEmpty()) break
        }
        if (out.isEmpty()) {
            android.util.Log.d("DirectClient", "CA导出失败：两个目录都无证书")
            return null
        }
        return try {
            target.writeText(out.toString())
            android.util.Log.d("DirectClient", "CA导出成功：${target.length()}字节 -> ${target.absolutePath}")
            target
        } catch (e: Exception) {
            android.util.Log.d("DirectClient", "CA导出异常：${e.message}")
            null
        }
    }

    /** 清理残缺的临时文件。 */
    fun cleanPartial(cacheDir: File) {
        cacheDir.listFiles()?.forEach { if (it.name.endsWith(".tmp")) it.delete() }
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

    private const val APP_SEARCH_HOST = "https://api5-normal-sinfonlineb.fqnovel.com"

    /** App 官方搜索（bookapi/search/tab/v，旧版签名）——任何网络直连可用。 */
    fun searchApp(keyword: String, deviceId: String, installId: String, offset: Int = 0): Pair<List<DirectDrama>, Boolean> {
        val query = HongguoSign.encodeQuery(
            HongguoSign.baseQuery(deviceId, installId) + listOf(
                "query" to keyword, "count" to "20", "offset" to offset.toString(),
                "tab_type" to "11", "bookshelf_search_plan" to "4",
                "use_correct" to "true", "user_is_login" to "0",
                "ts" to (System.currentTimeMillis() / 1000).toString(),
                "_rticket" to System.currentTimeMillis().toString()
            )
        )
        val headers = HongguoSign.signLegacyHeaders(query, "8662")
        val text = httpGet("$APP_SEARCH_HOST/reading/bookapi/search/tab/v", query, headers)
        val root = JSONObject(text)
        if (root.optInt("code", -1) != 0) throw DirectException("搜索失败(${root.optInt("code", -1)})")
        val container = root.optJSONObject("data") ?: root
        val tabs = container.optJSONArray("search_tabs") ?: return Pair(emptyList(), false)
        var tab: JSONObject? = null
        for (i in 0 until tabs.length()) {
            val t = tabs.optJSONObject(i) ?: continue
            if (t.optInt("tab_type") == 11) { tab = t; break }
        }
        tab ?: return Pair(emptyList(), false)
        val cells = tab.optJSONArray("data") ?: return Pair(emptyList(), false)
        val out = ArrayList<DirectDrama>(cells.length() * 2)
        for (i in 0 until cells.length()) {
            val cell = cells.optJSONObject(i) ?: continue
            // lynx 卡片内还嵌一层 cell_data
            val candidates = ArrayList<JSONObject>()
            candidates.add(cell)
            val lynx = cell.optString("lynx_data")
            if (lynx.isNotEmpty()) {
                try {
                    val payload = JSONObject(lynx)
                    val inner = payload.optJSONArray("cell_data")
                    for (j in 0 until (inner?.length() ?: 0)) inner?.optJSONObject(j)?.let { candidates.add(it) }
                } catch (_: Exception) {}
            }
            for (c in candidates) {
                val vds = c.optJSONArray("video_data") ?: continue
                var video: JSONObject? = null
                for (j in 0 until vds.length()) {
                    val v = vds.optJSONObject(j) ?: continue
                    video = v; break
                }
                val v = video ?: continue
                val id = v.optString("series_id").ifEmpty { c.optString("book_id") }
                if (id.isEmpty()) continue
                val title = c.optJSONObject("search_high_light")?.optJSONObject("title")?.optString("text")
                    ?.takeIf { it.isNotBlank() } ?: v.optString("title").ifEmpty { c.optString("cell_name") }
                out.add(
                    DirectDrama(
                        id = id,
                        title = title.ifEmpty { "短剧" },
                        episodes = v.optInt("episode_cnt", 0),
                        coverUrl = v.optString("cover").ifEmpty { v.optString("cover_url") }.ifEmpty { null }
                    )
                )
            }
        }
        return Pair(out, tab.optBoolean("has_more", false))
    }

    private fun httpGet(url: String, query: String, headers: Map<String, String>): String {
        val conn = (URL("$url?$query").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
        }
        conn.setRequestProperty("User-Agent", HongguoSign.UA_LEGACY)
        conn.setRequestProperty("Accept", "application/json; charset=utf-8,application/x-protobuf")
        headers.forEach { (k, v) -> if (v.isNotEmpty()) conn.setRequestProperty(k, v) }
        val code = conn.responseCode
        val text = (if (code in 200..299) conn.inputStream else conn.errorStream)?.bufferedReader()?.use { it.readText() } ?: ""
        conn.disconnect()
        if (code != 200) throw DirectException("HTTP $code")
        if (text.isEmpty()) throw DirectException("空响应(HTTP $code)")
        return text
    }

    /**
     * 官网搜索（服务器 web 端 /api/ui/search 的同款逻辑）：
     * GET hongguoduanju.com/api/search/result?query=…，浏览器 UA + 搜索页 Referer，重试 3 次。
     * 该域名仅在代理/透明代理网络可达（家用路由器已接管），移动网络直连不通。
     */
    fun searchOfficial(keyword: String): List<DirectDrama> {
        val url = "https://hongguoduanju.com/api/search/result?query=" +
            java.net.URLEncoder.encode(keyword, "UTF-8")
        var text: String? = null
        var lastError: Exception? = null
        for (attempt in 1..3) {
            try {
                val conn = (URL(url).openConnection() as HttpURLConnection).apply {
                    requestMethod = "GET"
                    connectTimeout = 15000
                    readTimeout = 30000
                    setRequestProperty("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
                    setRequestProperty("Referer", "https://hongguoduanju.com/search/" + java.net.URLEncoder.encode(keyword, "UTF-8"))
                    setRequestProperty("Accept", "application/json, text/plain, */*")
                }
                val code = conn.responseCode
                val body = (if (code in 200..299) conn.inputStream else conn.errorStream)?.bufferedReader()?.use { it.readText() } ?: ""
                conn.disconnect()
                if (code != 200) throw DirectException("搜索 HTTP $code")
                if (body.isEmpty()) throw DirectException("搜索空响应")
                text = body
                break
            } catch (e: Exception) {
                lastError = e
                if (attempt < 3) Thread.sleep(attempt * 1000L)
            }
        }
        if (text == null) throw lastError ?: DirectException("搜索失败")
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