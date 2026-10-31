package com.duanju.player.core


import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.security.SecureRandom
import java.util.concurrent.Callable
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit

/**
 * 零服务器直连客户端：手机直接调用红果官方接口（内置签名），
 * 拿到 CDN 直链 + spade_a，再由本地 ffmpeg 解密。全程不经 duanju 服务器。
 */
object DirectClient {

    private const val API_HOST = "https://api5-normal-sinfonlineb.fqnovel.com"
    /** 开放接口域名（搜索/详情无需签名）：sinfonlinec。 */
    private val SEARCH_TYPES = setOf("短剧", "漫剧")

    private const val OPEN_HOST = "https://api5-normal-sinfonlinec.fqnovel.com"
    private const val WEB_HOST = "https://hongguoduanju.com"
    private const val UA_WEB = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
        "(KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

    data class DirectMedia(
        val url: String,
        val keyHex: String,
        val durationSec: Double,
        val quality: Int,
        val variants: List<Pair<Int, String>> // quality -> url
    )

    class DirectException(message: String) : Exception(message)

    /** 首页卡片（字段取自 bookmall/tab 的 video_data）。 */
    data class HomeDrama(
        val id: String,
        val title: String,
        val episodes: Int,
        val cover: String?,
        val score: String,
        val playCount: Long,
        val tag: String?,
        val tagColor: String?,
        val subTitle: String,
        val contentType: Int,
        /** 热度值（sub_title_list data_type=27，如 "8602万"）。 */
        val hotValue: String,
        /** 分类（sub_title_list data_type=3，如 "玄幻"）。 */
        val category: String,
        /** 推荐位名次（recommend_info.rank）。默认推荐流里等于位置，仅在榜单筛选下有意义。 */
        val rank: Int
    )

    data class HomeFilter(val id: String, val name: String)

    data class HomeFilterGroup(val name: String, val items: List<HomeFilter>)

    data class HomePage(
        val items: List<HomeDrama>,
        val nextOffset: Int,
        val sessionId: String,
        val hasMore: Boolean,
        val filters: List<HomeFilter>,
        val filterGroups: List<HomeFilterGroup> = emptyList()
    )

    data class SeriesEpisode(
        val index: Int,
        val vid: String,
        val durationSec: Int,
        val disabled: Boolean
    )

    data class SeriesDetail(
        val id: String,
        val title: String,
        val cover: String?,
        val episodes: Int,
        val playCount: Long,
        val status: Int,
        val intro: String,
        val subTitles: List<String>,
        val category: String,
        val list: List<SeriesEpisode>
    )

    data class SearchHit(
        val id: String,
        val title: String,
        val type: String,
        val episodes: Int,
        val cp: String
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

    /**
     * 详情接口取剧集条目。
     *
     * 实测上游偶发返回 `code=0` 但 `data.video_detail_data` 里没有该剧条目，
     * 同一请求随后几秒再发就恢复正常。这类瞬时降级不能当致命错误，
     * 否则播放/详情页会直接弹「详情无数据」。
     */
    private fun detailEntry(seriesId: String, params: String, cancellation: TaskCancellation? = null): JSONObject {
        var reason = "详情为空"
        for (attempt in 1..3) {
            cancellation?.check()
            val text = try {
                httpGetOpen(
                    "$OPEN_HOST/reading/bookapi/video_tab/multi_video_detail/v/?$params" +
                        "&video_series_id=$seriesId&vs_id_type=1", cancellation
                )
            } catch (e: Exception) {
                cancellation?.check()
                reason = e.message ?: e.toString()
                if (attempt < 3) Thread.sleep(attempt * 600L)
                continue
            }
            val root = runCatching { JSONObject(text) }.getOrNull()
            if (root == null) {
                reason = "响应非 JSON"
                if (attempt < 3) Thread.sleep(attempt * 600L)
                continue
            }
            val code = root.optInt("code", -1)
            val container = root.optJSONObject("data")?.optJSONObject("video_detail_data")
            val entry = container?.optJSONObject(seriesId) ?: container?.keys()?.asSequence()
                ?.mapNotNull { container.optJSONObject(it) }?.firstOrNull()
            if (code == 0 && entry != null) return entry
            reason = if (container == null) "详情为空(code=$code)" else "详情无数据(code=$code)"
            android.util.Log.d("DirectClient", "详情重试$attempt/$3：$reason series=$seriesId")
            if (attempt < 3) Thread.sleep(attempt * 600L)
        }
        throw DirectException(reason)
    }

    /**
     * 分集列表：开放详情接口（无签名），按 vid_index 排序。
     * 注意不能用 vid 字符串排序——那是雪花 id 的大小顺序，与集数无关（会打乱集数）。
     */
    fun chapters(dramaId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): List<String> {
        // 与详情页统一用完整参数：详情页刚用这套参数成功过，播放再取分集时
        // 不该换成另一套精简参数去赌上游行为。
        val entry = detailEntry(dramaId, openCommonFull(deviceId, installId), cancellation)
        val data = entry.optJSONObject("video_data") ?: throw DirectException("详情无 video_data")
        val list = data.optJSONArray("video_list") ?: throw DirectException("分集列表为空")
        val rows = ArrayList<Pair<Int, String>>(list.length())
        for (i in 0 until list.length()) {
            val row = list.optJSONObject(i) ?: continue
            val vid = row.optString("vid")
            if (vid.isEmpty() || row.optBoolean("disable_play", false)) continue
            rows.add(row.optInt("vid_index", i + 1) to vid)
        }
        if (rows.isEmpty()) throw DirectException("分集列表为空")
        rows.sortBy { it.first }
        return rows.map { it.second }
    }

    /** 播放模型（新版签名），返回直链 + 密钥 + 清晰度列表。 */
    /**
     * 取流：开放接口（无签名），video_platform 必须为 1024。
     * 旧的签名接口 novel/player/multi_video_model/v1 现已恒返回 200+空正文，改用此接口。
     */
    fun media(videoId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): DirectMedia {
        val url = "$OPEN_HOST/reading/bookapi/video_tab/multi_video_model/v/?" +
            "${openCommonFull(deviceId, installId, "74232")}&video_id=$videoId" +
            "&video_platform=1024&need_all_video_definition=1"
        val root = JSONObject(httpGetOpenRetry(url, cancellation))
        val code = root.optInt("code", -1)
        if (code != 0) throw DirectException("取流失败($code)")
        val entry = root.optJSONObject("data")?.optJSONObject("video_model_data")?.optJSONObject(videoId)
            ?: throw DirectException("未返回该分集")
        val modelRaw = entry.opt("video_model")
        val model = when (modelRaw) {
            is String -> JSONObject(modelRaw)
            is JSONObject -> modelRaw
            else -> throw DirectException("video_model 无效")
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
        if (best == null) {
            throw DirectException("该剧仅提供 H.266(bytevc2) 编码，暂不支持播放")
        }
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
    fun decryptTo(
        sourceUrl: String, keyHex: String, outFile: File,
        cancellation: TaskCancellation, onTempFile: (File) -> Unit = {}
    ) {
        // 优先 ffmpeg 直接拉流（网络好时一步到位且不落加密原片）。
        decryptViaFfmpeg(sourceUrl, keyHex, outFile, exportSystemCa(File(outFile.parentFile, "system-ca.pem")), cancellation, onTempFile)
    }

    /** ffmpeg 直接拉流并解密（源是 http/https）。
     *  ffmpeg 的 openssl 不带 Android 系统CA，证书验证会失败；
     *  这里把系统 cacerts 合并导出为 PEM，经 -cafile 交给 ffmpeg，正常完成校验。 */
    private fun decryptViaFfmpeg(
        sourceUrl: String, keyHex: String, outFile: File, caFile: File?,
        cancellation: TaskCancellation, onTempFile: (File) -> Unit
    ) {
        val tlsArgs = if (caFile != null) arrayOf("-tls_verify", "1", "-cafile", caFile.absolutePath) else emptyArray()
        CacheDownload.run(outFile, cancellation, onTempFile) { tmp, complete ->
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
                "-f", "mp4", tmp.absolutePath
            )
            val session = com.arthenica.ffmpegkit.FFmpegKit.executeWithArgumentsAsync(
                args,
                { finished ->
                    val rc = finished.getReturnCode()
                    complete(if (com.arthenica.ffmpegkit.ReturnCode.isSuccess(rc)) null else "解密失败: rc=${rc?.value}")
                }, null, null
            )
            val cancel: () -> Unit = { com.arthenica.ffmpegkit.FFmpegKit.cancel(session.getSessionId()) }
            cancel
        }
    }

    /** 合并 Android 系统 CA 为单个 PEM（供 ffmpeg -cafile 使用，一次导出缓存复用）。 */
    @Synchronized
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

    /**
     * 搜索 + 详情补齐，返回与首页同款的卡片数据。
     * 搜索结果本身只有标题/类型，封面、集数、播放量、分类需逐条拉详情（8 线程并行）。
     */
    fun searchHomeItems(keyword: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): List<HomeDrama> {
        val hits = searchHits(keyword, deviceId, installId, cancellation)
        if (hits.isEmpty()) return emptyList()
        val pool = Executors.newFixedThreadPool(8)
        val registration = cancellation?.onCancel { pool.shutdownNow() }
        return try {
            val futures = hits.map { hit ->
                cancellation?.check()
                hit to pool.submit(Callable { runCatching { detailBrief(hit.id, deviceId, installId, cancellation) }.getOrNull() })
            }
            val out = ArrayList<HomeDrama>(hits.size)
            for ((hit, future) in futures) {
                cancellation?.check()
                val detail = runCatching { future.get(20, TimeUnit.SECONDS) }.getOrNull()
                cancellation?.check()
                out.add(
                    detail ?: HomeDrama(
                        id = hit.id, title = hit.title, episodes = hit.episodes, cover = null,
                        score = "", playCount = 0, tag = null, tagColor = null,
                        subTitle = hit.type, contentType = 0, hotValue = "", category = hit.type, rank = 0
                    )
                )
            }
            out
        } finally {
            registration?.close()
            pool.shutdownNow()
        }
    }

    /**
     * 流式读取搜索响应，截取并解析 data 数组后立即断开。
     * 该接口的 debug_info 会膨胀到数十 MB，完整读取会拖慢搜索且浪费流量。
     */
    private fun readSearchData(url: String, cancellation: TaskCancellation? = null): JSONArray? {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
        }
        val registration = cancellation?.onCancel { conn.disconnect() }
        try {
            cancellation?.check()
            if (conn.responseCode != 200) throw DirectException("HTTP ${conn.responseCode}")
            val buffer = StringBuilder(256 * 1024)
            val chunk = CharArray(32 * 1024)
            var length: Int
            var dataStart = -1
            conn.inputStream.bufferedReader().use { reader ->
                while (true) {
                    cancellation?.check()
                    length = reader.read(chunk)
                    if (length <= 0) break
                    buffer.append(chunk, 0, length)
                    if (dataStart < 0) {
                        dataStart = buffer.indexOf("\"data\":")
                        if (dataStart >= 0) dataStart += 7
                    }
                    if (dataStart >= 0 && extractJsonArray(buffer, dataStart) != null) break
                    if (buffer.length > 8 * 1024 * 1024) break
                }
            }
            if (dataStart < 0) return null
            val arrayJson = extractJsonArray(buffer, dataStart) ?: return null
            return JSONArray(arrayJson)
        } finally {
            registration?.close()
            conn.disconnect()
        }
    }

    /** 从上文 text 的 offset 处取一个完整 JSON 数组文本（含方括号）；未读完返回 null。 */
    private fun extractJsonArray(text: CharSequence, offset: Int): String? {
        var start = offset
        while (start < text.length && text[start] != '[') {
            val c = text[start]
            if (c != ' ' && c != ':' && c != '\n' && c != '\r' && c != '\t') return null
            start++
        }
        if (start >= text.length) return null
        var depth = 0
        var inString = false
        var escaped = false
        for (i in start until text.length) {
            val c = text[i]
            if (inString) {
                when {
                    escaped -> escaped = false
                    c == '\\' -> escaped = true
                    c == '"' -> inString = false
                }
                continue
            }
            when (c) {
                '"' -> inString = true
                '[' -> depth++
                ']' -> {
                    depth--
                    if (depth == 0) return text.substring(start, i + 1)
                }
            }
        }
        return null
    }

    /** 详情页完整数据（无签名接口，封面 JPEG）。 */
    fun seriesDetail(seriesId: String, deviceId: String, installId: String): SeriesDetail {
        val entry = detailEntry(seriesId, openCommonFull(deviceId, installId))
        val data = entry.optJSONObject("video_data") ?: throw DirectException("详情无 video_data")

        var category = ""
        val schema = data.optString("category_schema")
        if (schema.isNotEmpty()) {
            runCatching { org.json.JSONArray(schema).optJSONObject(0)?.optString("name")?.let { category = it } }
        }
        val subTitles = ArrayList<String>()
        val subs = data.optJSONArray("series_sub_title_list")
        for (i in 0 until (subs?.length() ?: 0)) {
            val text = subs?.optString(i).orEmpty()
            if (text.isNotEmpty()) subTitles.add(text)
        }
        val rows = data.optJSONArray("video_list") ?: throw DirectException("分集列表为空")
        val list = ArrayList<SeriesEpisode>(rows.length())
        for (i in 0 until rows.length()) {
            val row = rows.optJSONObject(i) ?: continue
            val vid = row.optString("vid")
            if (vid.isEmpty()) continue
            list.add(
                SeriesEpisode(
                    index = row.optInt("vid_index", i + 1),
                    vid = vid,
                    durationSec = row.optInt("duration", 0),
                    disabled = row.optBoolean("disable_play", false)
                )
            )
        }
        list.sortBy { it.index }
        if (list.isEmpty()) throw DirectException("分集列表为空")
        return SeriesDetail(
            id = seriesId,
            title = data.optString("series_title").ifEmpty { "短剧" },
            cover = data.optString("series_cover").ifEmpty { null },
            episodes = data.optInt("episode_cnt", list.size),
            playCount = data.optLong("series_play_cnt", 0),
            status = data.optInt("series_status", 0),
            intro = data.optString("series_intro"),
            subTitles = subTitles,
            category = category,
            list = list
        )
    }

    /** 无签名详情：取剧名/封面/集数/播放量/分类（封面为 JPEG，避免 HEIC 解码失败）。 */
    private fun detailBrief(seriesId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): HomeDrama {
        val entry = detailEntry(seriesId, openCommonFull(deviceId, installId), cancellation)
        val data = entry.optJSONObject("video_data") ?: throw DirectException("详情无 video_data")
        var category = ""
        val schema = data.optString("category_schema")
        if (schema.isNotEmpty()) {
            runCatching {
                org.json.JSONArray(schema).optJSONObject(0)?.optString("name")?.let { category = it }
            }
        }
        var hotValue = ""
        val subs = data.optJSONArray("series_sub_title_list")
        for (i in 0 until (subs?.length() ?: 0)) {
            val content = subs?.optString(i).orEmpty()
            if (content.contains("热度")) hotValue = content.substringAfter("热度值").trim()
        }
        return HomeDrama(
            id = seriesId,
            title = data.optString("series_title").ifEmpty { "短剧" },
            episodes = data.optInt("episode_cnt", 0),
            cover = data.optString("series_cover").ifEmpty { null },
            score = "",
            playCount = data.optLong("series_play_cnt", 0),
            tag = null,
            tagColor = null,
            subTitle = data.optString("episode_right_text"),
            contentType = data.optInt("content_type", 0),
            hotValue = hotValue,
            category = category,
            rank = 0
        )
    }

    private fun cleanHtml(html: String): String =
        html.replace(Regex("<[^>]+>"), "").replace("&nbsp;", " ").replace("&amp;", "&").trim()

    /**
     * 完整真机参数集：首页接口翻页/筛选依赖它（仅 5 个必需参数时 offset>0 会返回重复内容）。
     */
    private fun openCommonFull(
        deviceId: String,
        installId: String,
        versionCode: String = "65532"
    ): String {
        val dev = deviceId.takeIf { it.length >= 6 } ?: "1234567890123456"
        val ins = installId.takeIf { it.length >= 6 } ?: "1234567890123456"
        val versionName = if (versionCode == "65532") "6.5.5.32" else "7.4.2.32"
        return "iid=$ins&device_id=$dev&ac=wifi&channel=hongguo_baidu_pz_kaiping0105_android" +
            "&aid=8662&app_name=novelread&version_code=$versionCode&version_name=$versionName" +
            "&device_platform=android&os=android&ssmix=a&device_type=Redmi+Note+8+Pro&device_brand=Redmi" +
            "&language=zh&os_api=36&os_version=16&manifest_version_code=$versionCode" +
            "&resolution=1080*2198&dpi=440&update_version_code=$versionCode&pv_player=$versionCode&gender=2" +
            "&need_personal_recommend=1&player_so_load=1&is_android_pad_screen=0&host_abi=arm64-v8a" +
            "&dragon_device_type=phone&rom_version=BP4A.251205.006+release-keys&compliance_status=0" +
            "&cdid=1f1d14b3-8c25-4733-9f6b-2fdc01640081"
    }

    /**
     * 首页信息流（bookmall/tab，无签名）。
     * 三层结构 tab_item.cell_data[].cell_data[].video_data[]；hasMore 用 next_offset > offset 判断
     * （offset=0 时 has_more 恒为 false，不可信）。
     */
    fun homeTab(
        tabType: Int, offset: Int, sessionId: String, selectedItems: String?,
        deviceId: String, installId: String
    ): HomePage {
        val url = buildString {
            append("$OPEN_HOST/reading/bookapi/bookmall/tab/v/?")
            append(openCommonFull(deviceId, installId))
            append("&tab_type=$tabType&offset=$offset")
            if (sessionId.isNotEmpty()) append("&session_id=").append(sessionId)
            if (!selectedItems.isNullOrEmpty()) append("&selected_items=").append(selectedItems)
        }
        val root = JSONObject(httpGetOpenRetry(url))
        val code = root.optInt("code", -1)
        if (code != 0) throw DirectException("首页加载失败($code)")
        val tabs = root.optJSONObject("data")?.optJSONArray("tab_item")
            ?: throw DirectException("首页无 tab 数据")
        var target: JSONObject? = null
        for (i in 0 until tabs.length()) {
            val tab = tabs.optJSONObject(i) ?: continue
            if (tab.optString("cell_data").isEmpty() || tab.optJSONArray("cell_data")?.length() == 0) continue
            if (tab.optInt("tab_type", -1) == tabType) { target = tab; break }
            if (target == null) target = tab
        }
        val tab = target ?: return HomePage(emptyList(), offset, sessionId, false, emptyList())
        val items = ArrayList<HomeDrama>()
        val seen = HashSet<String>()
        val cells = tab.optJSONArray("cell_data")
        for (i in 0 until (cells?.length() ?: 0)) {
            val cell = cells?.optJSONObject(i) ?: continue
            val nested = cell.optJSONArray("cell_data")
            for (j in 0 until (nested?.length() ?: 0)) {
                collectHomeDramas(nested?.optJSONObject(j)?.optJSONArray("video_data"), items, seen)
            }
            collectHomeDramas(cell.optJSONArray("video_data"), items, seen)
        }
        var filters = emptyList<HomeFilter>()
        for (i in 0 until (cells?.length() ?: 0)) {
            val selector = cells?.optJSONObject(i)?.optJSONObject("cell_selector") ?: continue
            val rows = selector.optJSONObject("outer_row")?.optJSONArray("items") ?: continue
            val list = ArrayList<HomeFilter>()
            for (j in 0 until rows.length()) {
                val row = rows.optJSONObject(j) ?: continue
                val id = row.optString("selector_item_id")
                val name = row.optString("show_name")
                if (id.isNotEmpty() && name.isNotEmpty()) list.add(HomeFilter(id, name))
            }
            if (list.isNotEmpty()) { filters = list; break }
        }
        val groups = ArrayList<HomeFilterGroup>()
        for (i in 0 until (cells?.length() ?: 0)) {
            val selector = cells?.optJSONObject(i)?.optJSONObject("cell_selector") ?: continue
            val innerRows = selector.optJSONArray("inner_rows") ?: continue
            for (r in 0 until innerRows.length()) {
                val row = innerRows.optJSONObject(r) ?: continue
                val rowName = row.optString("row_name")
                val rowItems = row.optJSONArray("items") ?: continue
                val items = ArrayList<HomeFilter>()
                for (j in 0 until rowItems.length()) {
                    val item = rowItems.optJSONObject(j) ?: continue
                    val id = item.optString("selector_item_id")
                    val name = item.optString("show_name")
                    if (id.isNotEmpty() && name.isNotEmpty()) items.add(HomeFilter(id, name))
                }
                if (rowName.isNotEmpty() && items.isNotEmpty()) groups.add(HomeFilterGroup(rowName, items))
            }
            if (groups.isNotEmpty()) break
        }
        val nextOffset = tab.optInt("next_offset", offset + 1)
        return HomePage(
            items = items,
            nextOffset = nextOffset,
            sessionId = tab.optString("session_id"),
            hasMore = nextOffset > offset,
            filters = filters,
            filterGroups = groups
        )
    }

    private fun collectHomeDramas(array: org.json.JSONArray?, out: MutableList<HomeDrama>, seen: MutableSet<String>) {
        for (i in 0 until (array?.length() ?: 0)) {
            val v = array?.optJSONObject(i) ?: continue
            val id = v.optString("series_id").ifEmpty { v.optString("vid") }
            if (id.isEmpty() || !seen.add(id)) continue
            val title = v.optString("title").ifEmpty { v.optString("video_desc", "短剧") }
            val tagInfo = v.optJSONObject("tag_info")
            val tag = tagInfo?.optString("text")?.takeIf { it.isNotBlank() }
            val tagColor = tagInfo?.optJSONArray("bg_color")?.optString(0)?.takeIf { it.isNotBlank() }
            var hotValue = ""
            var category = ""
            val subList = v.optJSONArray("sub_title_list")
            for (k in 0 until (subList?.length() ?: 0)) {
                val entry = subList?.optJSONObject(k) ?: continue
                val content = entry.optString("content")
                if (content.isEmpty()) continue
                when (entry.optInt("data_type", 0)) {
                    27 -> if (hotValue.isEmpty()) hotValue = content
                    3 -> if (category.isEmpty()) category = content
                }
            }
            if (category.isEmpty()) {
                category = v.optString("sub_title").substringBefore("·").trim()
            }
            val rank = runCatching {
                JSONObject(v.optString("recommend_info")).optString("rank").toIntOrNull() ?: 0
            }.getOrDefault(0)
            out.add(
                HomeDrama(
                    id = id,
                    title = title,
                    episodes = v.optInt("episode_cnt", 0),
                    cover = v.optString("cover").ifEmpty { v.optString("horiz_cover") }.ifEmpty { null },
                    score = v.optString("score"),
                    playCount = v.optLong("play_cnt", 0),
                    tag = tag,
                    tagColor = tagColor,
                    subTitle = v.optString("sub_title"),
                    contentType = v.optInt("content_type", 0),
                    hotValue = hotValue,
                    category = category,
                    rank = rank
                )
            )
        }
    }

    /**
     * 「相关」= 同剧的其他季。
     *
     * 优先读站点剧集页：`hongguoduanju.com/player/<seriesId>/<videoId>` 的
     * `data-fn-args` 里一次性带出各季的 series_id / 名称 / 封面 / 集数（一次请求，无需签名），
     * 比「搜索 + 逐季补详情」省掉 N 次请求。
     * 站点页不可达时退回搜索聚合（搜索结果没有封面，只在本地补当前剧的）。
     */
    fun relatedSeasons(
        seriesId: String,
        videoId: String?,
        deviceId: String,
        installId: String,
        currentTitle: String = "",
        currentCover: String? = null
    ): List<SeriesAggregator.Season> {
        runCatching { seasonsFromPlayerPage(seriesId, videoId, deviceId, installId) }
            .getOrNull()
            ?.takeIf { it.isNotEmpty() }
            ?.let { return it }

        val keyword = SeriesAggregator.baseTitle(currentTitle).ifEmpty { currentTitle }.take(30)
        if (keyword.isEmpty()) return emptyList()
        val hits = searchHits(keyword, deviceId, installId)
            .map { SeriesAggregator.Hit(it.id, it.title, it.type, it.episodes) }
        return SeriesAggregator.aggregate(currentTitle, null, hits)
            .map { if (it.id == seriesId && it.cover == null) it.copy(cover = currentCover) else it }
    }

    /**
     * 站点剧集页 → 各季列表。页面把数据塞在 `data-fn-args='[...]'`，
     * 第三项即 videoList（每项含 series_id / series_name / series_cover / episode_cnt）。
     */
    private fun seasonsFromPlayerPage(
        seriesId: String,
        videoId: String?,
        deviceId: String,
        installId: String
    ): List<SeriesAggregator.Season> {
        val vid = videoId ?: chapters(seriesId, deviceId, installId).firstOrNull() ?: return emptyList()
        val page = httpGetWeb("$WEB_HOST/player/$seriesId/$vid")
        val args = dataFnArgs(page) ?: return emptyList()
        val list = args.optJSONArray(2) ?: return emptyList()
        val hits = ArrayList<SeriesAggregator.Hit>(list.length() + 1)
        // 页面里的 videoList 不含当前这一季，单独从 seriesDetail 补上。
        val current = currentSeason(page)
        if (current != null) hits.add(current)
        for (i in 0 until list.length()) {
            val row = list.optJSONObject(i) ?: continue
            val id = row.optString("series_id")
            val name = row.optString("series_name")
            if (id.isEmpty() || name.isEmpty()) continue
            hits.add(
                SeriesAggregator.Hit(
                    id = id,
                    title = name,
                    type = "",
                    episodes = row.optInt("episode_cnt", 0),
                    cover = row.optString("series_cover").ifEmpty { null }
                )
            )
        }
        return SeriesAggregator.aggregate(current?.title ?: "", null, hits)
    }

    /** 页面 `data-fn-args` 里的 JSON 数组；HTML 实体先还原。 */
    private fun dataFnArgs(page: String): JSONArray? {
        val marker = "data-fn-args='"
        val start = page.indexOf(marker)
        if (start < 0) return null
        val end = page.indexOf('\'', start + marker.length)
        if (end < 0) return null
        val text = page.substring(start + marker.length, end)
            .replace("&quot;", "\"")
            .replace("&#39;", "'")
            .replace("&apos;", "'")
            .replace("&lt;", "<")
            .replace("&gt;", ">")
            .replace("&amp;", "&")
        return runCatching { JSONArray(text) }.getOrNull()
    }

    /** 当前剧的 series_name / series_cover / episode_cnt（页面里的 seriesDetail）。 */
    private fun currentSeason(page: String): SeriesAggregator.Hit? {
        val marker = "\"seriesDetail\":{"
        val start = page.indexOf(marker)
        if (start < 0) return null
        // 整段按 JSON 解析：封面 URL 是 \u002F 转义的，正则截取会拿到非法地址。
        val json = extractJsonObject(page, start + marker.length - 1) ?: return null
        val obj = runCatching { JSONObject(json) }.getOrNull() ?: return null
        val id = obj.optString("series_id")
        val name = obj.optString("series_name")
        if (id.isEmpty() || name.isEmpty()) return null
        return SeriesAggregator.Hit(
            id = id,
            title = name,
            type = "",
            episodes = obj.optInt("episode_cnt", 0),
            cover = obj.optString("series_cover").ifEmpty { null }
        )
    }

    /** 从 start（必须是 '{'）开始按括号配对截取一个完整 JSON 对象。 */
    private fun extractJsonObject(text: String, start: Int): String? {
        if (start !in text.indices || text[start] != '{') return null
        var depth = 0
        var inString = false
        var escaped = false
        for (i in start until text.length) {
            val c = text[i]
            if (inString) {
                when {
                    escaped -> escaped = false
                    c == '\\' -> escaped = true
                    c == '"' -> inString = false
                }
                continue
            }
            when (c) {
                '"' -> inString = true
                '{' -> depth++
                '}' -> {
                    depth--
                    if (depth == 0) return text.substring(start, i + 1)
                }
            }
        }
        return null
    }

    /** 站点页面：浏览器 UA + Referer，失败重试 3 次。 */
    private fun httpGetWeb(url: String): String {
        var last: Exception? = null
        for (attempt in 1..3) {
            try {
                val conn = (URL(url).openConnection() as HttpURLConnection).apply {
                    requestMethod = "GET"
                    connectTimeout = 15000
                    readTimeout = 30000
                    setRequestProperty("User-Agent", UA_WEB)
                    setRequestProperty("Referer", "$WEB_HOST/")
                    setRequestProperty("Accept", "text/html,application/xhtml+xml")
                }
                val code = conn.responseCode
                val text = (if (code in 200..299) conn.inputStream else conn.errorStream)
                    ?.bufferedReader()?.use { it.readText() } ?: ""
                conn.disconnect()
                if (code != 200) throw DirectException("网页 HTTP $code")
                if (text.isEmpty()) throw DirectException("网页空响应")
                return text
            } catch (e: Exception) {
                last = e
                if (attempt < 3) Thread.sleep(attempt * 600L)
            }
        }
        throw last ?: DirectException("网页加载失败")
    }

    /**
     * 搜索命中（无签名 fetch-debug）。返回列表页所需字段：类型/集数/CP方。
     * 搜索结果本身不含封面，这也是官方 App 搜索页不放封面图的原因。
     */
    fun searchHits(keyword: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): List<SearchHit> {
        val url = "$OPEN_HOST/reading/bookapi/search/fetch-debug/v/?${openCommonFull(deviceId, installId)}" +
            "&query=" + URLEncoder.encode(keyword, "UTF-8") + "&count=20"
        var rows: JSONArray? = null
        var last: Exception? = null
        for (attempt in 1..3) {
            cancellation?.check()
            try {
                rows = readSearchData(url, cancellation)
                if (rows != null) break
            } catch (e: Exception) {
                cancellation?.check()
                last = e
                if (attempt < 3) Thread.sleep(attempt * 600L)
            }
        }
        val data = rows ?: run { if (last != null) throw last; return emptyList() }
        val out = ArrayList<SearchHit>(data.length())
        val seen = HashSet<String>()
        for (i in 0 until data.length()) {
            val row = data.optJSONObject(i) ?: continue
            val id = row.optString("id")
            val type = row.optString("type")
            if (id.isEmpty() || type !in SEARCH_TYPES) continue
            if (!seen.add(id)) continue
            val display = row.optJSONObject("display")
            val title = cleanHtml(display?.optString("title").orEmpty())
            if (title.isEmpty()) continue
            val summary = display?.optString("summary").orEmpty()
            val episodes = Regex("\"集数\"\\s*:\\s*\"?\\s*(\\d+)")
                .find(summary)?.groupValues?.get(1)?.toIntOrNull() ?: 0
            val cp = Regex("\"CP方\"\\s*:\\s*\"([^\"]*)\"")
                .find(summary)?.groupValues?.get(1).orEmpty()
            out.add(SearchHit(id, title, type, episodes, cp))
        }
        return out
    }

    /**
     * 取源文件总大小（Content-Length），用于展示预取进度；失败返回 null，
     * 调用方退化为只显示"下载中"。HEAD 很轻，且 CDN 支持。
     */
    fun contentLength(url: String, cancellation: TaskCancellation? = null): Long? {
        val conn = try {
            (URL(url).openConnection() as HttpURLConnection).apply {
                requestMethod = "HEAD"
                connectTimeout = 8000
                readTimeout = 8000
            }
        } catch (e: Exception) {
            return null
        }
        val registration = cancellation?.onCancel { conn.disconnect() }
        return try {
            cancellation?.check()
            if (conn.responseCode !in 200..299) return null
            conn.contentLengthLong.takeIf { it > 0L }
        } catch (e: Exception) {
            cancellation?.check()
            null
        } finally {
            registration?.close()
            conn.disconnect()
        }
    }

    /** 无签名开放接口：偶发 200+空正文，重试 3 次。 */
    private fun httpGetOpenRetry(url: String, cancellation: TaskCancellation? = null): String {
        var last: Exception? = null
        for (attempt in 1..3) {
            cancellation?.check()
            try {
                return httpGetOpen(url, cancellation)
            } catch (e: Exception) {
                cancellation?.check()
                last = e
                if (attempt < 3) Thread.sleep(attempt * 600L)
            }
        }
        throw last ?: DirectException("开放接口失败")
    }

    /** 无签名开放接口：GET，不复用签名 UA。 */
    private fun httpGetOpen(url: String, cancellation: TaskCancellation? = null): String {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
        }
        val registration = cancellation?.onCancel { conn.disconnect() }
        return try {
            cancellation?.check()
            val code = conn.responseCode
            val text = (if (code in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { it.readText() } ?: ""
            cancellation?.check()
            if (code != 200) throw DirectException("HTTP $code")
            if (text.isEmpty()) throw DirectException("空响应(HTTP $code)")
            text
        } finally {
            registration?.close()
            conn.disconnect()
        }
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

    /**
     * 读本地解密后 MP4 的画面宽高（px）。解密用的是 -c copy，尺寸与原始一致。
     * 用途：播放前就知道这一集是横屏还是竖屏，容器高度一次到位，
     * 不用等 ExoPlayer 解出首帧才回调 onVideoSizeChanged（那会导致切集时高度跳一下）。
     * 阻塞调用，需在工作线程执行；解析失败返回 null。
     */
    fun probeSize(file: File): Pair<Int, Int>? = try {
        if (!file.exists() || file.length() == 0L) {
            null
        } else {
            val session = com.arthenica.ffmpegkit.FFprobeKit.getMediaInformation(file.absolutePath)
            val info = session?.getMediaInformation()
            val video = info?.getStreams()?.firstOrNull { it.getType() == "video" }
            val w = video?.getWidth() ?: 0L
            val h = video?.getHeight() ?: 0L
            if (w > 0 && h > 0) w.toInt() to h.toInt() else null
        }
    } catch (e: Exception) {
        android.util.Log.d("DirectClient", "probeSize 失败 ${file.name}: ${e.message}")
        null
    }
}
