package com.duanju.player.core

import android.app.Application
import android.content.ContextWrapper
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.util.concurrent.TimeUnit

/** long: 历史文档不能证明当前接口可用；联网验收必须显式开启，避免普通回归依赖上游。 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
class RecipeLiveTest {
    @get:Rule val temp = TemporaryFolder()

    @Test fun currentRecipeAndHistoricalUnsignedAssumptions() {
        assumeTrue(System.getenv("DUANJU_LIVE_RECIPE") == "1")
        val report = File("build/reports/recipe-live.txt").apply {
            parentFile?.mkdirs()
            writeText("time=${Instant.now()}\n")
        }
        fun record(text: String) { report.appendText(text + "\n") }
        val app = RuntimeEnvironment.getApplication()
        RecipeStore.init(object : ContextWrapper(app) {
            override fun getApplicationContext() = this
            override fun getFilesDir() = temp.root
        })
        val recipe = RecipeStore.recipe()
        val device = DirectClient.deviceId()
        val install = DirectClient.deviceId()
        val original = RecipeHttp.openConnection
        var requests = 0
        val evidence = File("build/reports/home-filter-ui/live-api-${System.currentTimeMillis()}").apply { check(mkdirs()) }
        fun openRecorded(url: URL): HttpURLConnection {
            requests++
            record("request=$requests path=${url.path}")
            return ApiEvidenceConnection(original(url), File(evidence, "%03d".format(requests)))
        }
        RecipeHttp.openConnection = ::openRecorded
        try {
            record("recipe=${recipe.version}; temporary random identity; no login credentials")
            // long: 真机失败必须先用同一分集对照，随机搜索到的另一部剧成功不能证明该样本已恢复。
            val targetVideo = System.getenv("DUANJU_LIVE_VIDEO_ID")?.takeIf { it.matches(Regex("[0-9]+")) }
            if (targetVideo != null) {
                record("targetVideo=$targetVideo; media-only comparison; no CDN/decode request")
                val media = DirectClient.media(targetVideo, device, install)
                record("media=quality=${media.quality} variants=${media.variants.size} keyBytes=${media.keyHex.length / 2}")
                assertEquals(32, media.keyHex.length)
                return
            }
            val home = runCatching { DirectClient.homeTab(8, 0, "", null, device, install) }
            record("home=" + home.fold({ "items=${it.items.size}" }, { "FAIL ${it.javaClass.simpleName}: ${it.message}" }))
            val hits = DirectClient.searchHits("修仙", device, install)
            record("search=hits=${hits.size}")
            assertTrue("真实搜索没有可验收的短剧/漫剧", hits.isNotEmpty())
            val hit = hits.first()
            val detail = DirectClient.seriesDetail(hit.id, device, install)
            record("detail=series=${detail.id} episodes=${detail.list.size} cover=${!detail.cover.isNullOrBlank()}")
            assertTrue(detail.list.isNotEmpty())
            detail.cover?.let { cover ->
                val conn = openRecorded(URL(cover))
                try {
                    conn.connectTimeout = 15000
                    conn.readTimeout = 15000
                    val status = conn.responseCode
                    val bytes = conn.inputStream.use { it.readNBytes(32768).size }
                    record("cover=http$status bytes=$bytes contentType=${conn.contentType}")
                    assertEquals(200, status)
                    assertTrue(bytes > 0)
                } finally { conn.disconnect() }
            }
            val episode = detail.list.first { !it.disabled }
            val media = DirectClient.media(episode.vid, device, install)
            record("media=quality=${media.quality} variants=${media.variants.size} keyBytes=${media.keyHex.length / 2}")
            assertEquals(32, media.keyHex.length)
            // long: 只取一小段 CDN 内容确认直链可读，避免把得到 URL 误当实际可播放。
            val cdn = openRecorded(URL(media.url))
            try {
                cdn.connectTimeout = 15000
                cdn.readTimeout = 15000
                cdn.setRequestProperty("Range", "bytes=0-65535")
                val status = cdn.responseCode
                val bytes = cdn.inputStream.use { it.readNBytes(65536).size }
                record("cdn=http$status bytes=$bytes")
                assertTrue(status == 200 || status == 206)
                assertTrue(bytes > 0)
            } finally { cdn.disconnect() }
            val ffmpeg = System.getenv("DUANJU_LIVE_FFMPEG")
            if (!ffmpeg.isNullOrBlank()) {
                // long: 解密后实际解码一帧，既验证当前密钥信封，也避免整集下载增加上游负担。
                val log = File(temp.root, "decode.log")
                val process = ProcessBuilder(ffmpeg, "-hide_banner", "-loglevel", "error",
                    "-nostdin", "-rw_timeout", "15000000", "-decryption_key", media.keyHex,
                    "-i", media.url, "-map", "0:v:0", "-frames:v", "1", "-f", "null", "-")
                    .redirectErrorStream(true).redirectOutput(log).start()
                val finished = process.waitFor(45, TimeUnit.SECONDS)
                if (!finished) process.destroyForcibly()
                val success = finished && process.exitValue() == 0
                record("decryptAndDecodeFrame=$success")
                assertTrue("真实媒体解密/解码失败（不打印媒体链接与密钥）", success)
            } else record("decryptAndDecodeFrame=NOT_RUN; set DUANJU_LIVE_FFMPEG")
            val sampleFailures = mutableListOf<Int>()
            for (index in listOf(4, 40, 78)) {
                val sample = detail.list.firstOrNull { it.index == index && !it.disabled } ?: continue
                val result = runCatching { DirectClient.media(sample.vid, device, install) }
                record("episode-$index=" + result.fold({ "quality=${it.quality} keyBytes=${it.keyHex.length / 2}" },
                    { "FAIL ${it.javaClass.simpleName}: ${it.message}" }))
                if (result.isFailure) sampleFailures.add(index)
            }
            // long: 文档中的“全部无签名”逐项独立检查；单次失败只能证明本次样本不可用。
            for ((id, vars) in listOf("detail" to mapOf("seriesId" to hit.id), "media" to mapOf("videoId" to episode.vid))) {
                val endpoint = recipe.endpoint(id)
                val conn = openRecorded(URL(RecipeHttp.buildUrl(endpoint, recipe, vars, device, install)))
                try {
                    conn.connectTimeout = 15000
                    conn.readTimeout = 15000
                    val status = conn.responseCode
                    val text = (if (status in 200..299) conn.inputStream else conn.errorStream)
                        ?.bufferedReader()?.use { it.readText() }.orEmpty()
                    val data = runCatching { RecipeHttp.extract(text, endpoint.extract, vars) }.getOrNull()
                    val usable = if (id == "detail") (data as? JSONObject)?.has(hit.id) == true else data is String || data is JSONObject
                    record("unsigned-$id=http$status chars=${text.length} usable=$usable")
                } finally { conn.disconnect() }
            }
            val plan = recipe.endpoint("detail").copy(path = "/reading/bookapi/plan/v/",
                params = listOf("scene" to "10", "source" to "series_short_play_info",
                    "book_id" to hit.id, "filter_ids" to hit.id))
            val planUrl = URL(RecipeHttp.buildUrl(plan, recipe, emptyMap(), device, install))
            val planConn = openRecorded(planUrl)
            try {
                planConn.connectTimeout = 15000
                planConn.readTimeout = 15000
                planConn.setRequestProperty("User-Agent", recipe.userAgents["model"])
                HongguoSign.legacyHeaders(planUrl.query).forEach { (k, v) -> planConn.setRequestProperty(k, v) }
                val status = planConn.responseCode
                val text = (if (status in 200..299) planConn.inputStream else planConn.errorStream)
                    ?.bufferedReader()?.use { it.readText() }.orEmpty()
                val root = runCatching { JSONObject(text) }.getOrNull()
                record("historical-plan=http$status chars=${text.length} code=${root?.optInt("code", -1)} hasData=${root?.isNull("data") == false}")
            } finally { planConn.disconnect() }
            assertTrue("部分抽样分集取流失败: $sampleFailures", sampleFailures.isEmpty())
            assertTrue("首页验收失败", home.isSuccess && home.getOrThrow().items.isNotEmpty())
        } catch (e: Exception) {
            record("FAIL=${e.javaClass.simpleName}: ${e.message}")
            throw e
        } finally {
            RecipeHttp.openConnection = original
            record("recipeRequests=$requests")
            record("fullApiEvidence=${evidence.path}; includes each attempt, explicit headers and raw bodies; FFmpeg transport is separate")
        }
    }
}
