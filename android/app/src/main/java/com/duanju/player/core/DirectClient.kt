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

    /** long: 搜索只展示可播放内容，过滤小说和相关搜索等其它索引类型。 */
    private val SEARCH_TYPES = setOf("短剧", "漫剧")

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

    class DirectException(message: String, val retryable: Boolean = false) : Exception(message)

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
    private fun detailEntry(
        seriesId: String,
        deviceId: String,
        installId: String,
        cancellation: TaskCancellation? = null
    ): JSONObject {
        val vars = mapOf("seriesId" to seriesId)
        return RecipeHttp.readJson("detail", vars, deviceId, installId, cancellation) { text, endpoint ->
            val container = RecipeHttp.extract(text, endpoint.extract, vars) as? JSONObject
            // long: 批量响应必须精确匹配剧 ID，不能把其它剧的首项当作当前剧播放。
            container?.optJSONObject(seriesId)
                ?: throw RecipeHttp.ResponseFailure("missingData", "详情无数据(series=$seriesId)")
        }
    }

    /**
     * long: 分集列表按 vid_index 排序，签名档位由配方控制。
     * 注意不能用 vid 字符串排序——那是雪花 id 的大小顺序，与集数无关（会打乱集数）。
     */
    fun chapters(dramaId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): List<String> {
        // 与详情页统一用完整参数：详情页刚用这套参数成功过，播放再取分集时
        // 不该换成另一套精简参数去赌上游行为。
        val entry = detailEntry(dramaId, deviceId, installId, cancellation)
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

    /** long: 请求与提取共用一次配方快照，远程更新不能改变在途响应的解析方式。 */
    fun media(videoId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): DirectMedia {
        val vars = mapOf("videoId" to videoId)
        val model = RecipeHttp.readJson("media", vars, deviceId, installId, cancellation) { text, endpoint ->
            when (val raw = RecipeHttp.extract(text, endpoint.extract, vars)) {
                is String -> JSONObject(raw)
                is JSONObject -> raw
                else -> throw RecipeHttp.ResponseFailure("missingData", "未返回该分集")
            }
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

    /** long: 详情复用配方签名与重试策略，避免封面和选集走不同请求规则。 */
    fun seriesDetail(seriesId: String, deviceId: String, installId: String): SeriesDetail {
        val entry = detailEntry(seriesId, deviceId, installId)
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

    /** long: 搜索补充封面与元数据也走同一详情入口，上游切换签名时不会遗漏封面请求。 */
    private fun detailBrief(seriesId: String, deviceId: String, installId: String, cancellation: TaskCancellation? = null): HomeDrama {
        val entry = detailEntry(seriesId, deviceId, installId, cancellation)
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
     * 首页信息流（bookmall/tab，无签名）。
     * 三层结构 tab_item.cell_data[].cell_data[].video_data[]；hasMore 用 next_offset > offset 判断
     * （offset=0 时 has_more 恒为 false，不可信）。
     */
    fun homeTab(
        tabType: Int, offset: Int, sessionId: String, selectedItems: String?,
        deviceId: String, installId: String
    ): HomePage {
        val vars = mapOf(
            "tabType" to tabType.toString(), "offset" to offset.toString(),
            "sessionId" to sessionId, "selectedItems" to (selectedItems ?: "")
        )
        val data = RecipeHttp.readJson(
            "home",
            vars,
            deviceId, installId
        ) { text, endpoint ->
            RecipeHttp.extract(text, endpoint.extract, vars) as? JSONObject
                ?: throw RecipeHttp.ResponseFailure("missingData", "首页无数据")
        }
        val tabs = data.optJSONArray("tab_item")
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
        val filters = LinkedHashMap<String, HomeFilter>()
        val groups = LinkedHashMap<String, HomeFilterGroup>()
        fun readFilters(rows: org.json.JSONArray?): List<HomeFilter> = buildList {
            for (i in 0 until (rows?.length() ?: 0)) {
                val row = rows?.optJSONObject(i) ?: continue
                val id = row.optString("selector_item_id").trim()
                val name = row.optString("show_name").trim()
                if (id.isNotEmpty() && name.isNotEmpty()) add(HomeFilter(id, name))
            }
        }.distinctBy { it.id }
        fun collectSelectors(rows: org.json.JSONArray?) {
            for (i in 0 until (rows?.length() ?: 0)) {
                val cell = rows?.optJSONObject(i) ?: continue
                cell.optJSONObject("cell_selector")?.let { selector ->
                    // long: 上游可能把分类拆到多个 cell；合并同组并按 ID 去重，不能在第一个有效 selector 处停止。
                    readFilters(selector.optJSONObject("outer_row")?.optJSONArray("items")).forEach {
                        filters.putIfAbsent(it.id, it)
                    }
                    val inner = selector.optJSONArray("inner_rows")
                    for (r in 0 until (inner?.length() ?: 0)) {
                        val row = inner?.optJSONObject(r) ?: continue
                        val name = row.optString("row_name").trim().ifEmpty { "其他分类" }
                        val key = row.optString("type") + ":" + name
                        val entries = readFilters(row.optJSONArray("items"))
                        if (entries.isNotEmpty()) {
                            groups[key] = HomeFilterGroup(name, ((groups[key]?.items ?: emptyList()) + entries).distinctBy { it.id })
                        }
                    }
                }
                collectSelectors(cell.optJSONArray("cell_data"))
            }
        }
        collectSelectors(cells)
        val nextOffset = tab.optInt("next_offset", offset + 1)
        return HomePage(
            items = items,
            nextOffset = nextOffset,
            sessionId = tab.optString("session_id"),
            hasMore = nextOffset > offset,
            filters = filters.values.toList(),
            filterGroups = groups.values.toList()
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
        val data = RecipeHttp.exchange(
            "search",
            mapOf("query" to keyword),
            deviceId, installId, cancellation
        ) { conn, endpoint ->
            conn.inputStream.reader(Charsets.UTF_8).use {
                RecipeSearchReader.read(it, RecipeHttp.render(endpoint.extract!!, mapOf("query" to keyword)), cancellation)
            }
        }
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
