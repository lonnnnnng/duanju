package com.duanju.player.core

import android.app.Application
import android.content.ContextWrapper
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.CancellationException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
class RecipeRuntimeTest {
    @get:Rule val temp = TemporaryFolder()
    private lateinit var root: JSONObject
    private val requests = mutableListOf<FakeConnection>()
    private var answer: (FakeConnection) -> Pair<Int, String> = { 200 to "{\"code\":0}" }
    private lateinit var originalConnection: (URL) -> HttpURLConnection

    @Before fun setup() {
        val app = RuntimeEnvironment.getApplication()
        RecipeStore.init(object : ContextWrapper(app) {
            override fun getApplicationContext() = this
            override fun getFilesDir() = temp.root
        })
        root = JSONObject(File("src/main/assets/recipes/hongguo.json").readText()).put("version", 100)
        root.getJSONObject("endpoints").keys().forEach { id ->
            endpoint(id).getJSONObject("retry").put("backoffMs", JSONArray(listOf(0, 0, 0, 0)))
        }
        originalConnection = RecipeHttp.openConnection
        RecipeHttp.openConnection = { url -> FakeConnection(url).also { requests.add(it) } }
    }

    @After fun teardown() { RecipeHttp.openConnection = originalConnection }

    private fun endpoint(id: String) = root.getJSONObject("endpoints").getJSONObject(id)
    private fun activate() { assertTrue(RecipeStore.saveOverride(root.toString())) }
    private fun media() = RecipeHttp.execute("media", mapOf("videoId" to "v1"), "1234567", "7654321")

    private inner class FakeConnection(url: URL) : HttpURLConnection(url) {
        val output = ByteArrayOutputStream()
        private val response by lazy { answer(this) }
        val signed get() = getRequestProperty("X-Gorgon") != null
        override fun getOutputStream() = output
        override fun getResponseCode() = response.first
        override fun getInputStream() = ByteArrayInputStream(response.second.toByteArray())
        override fun connect() {}
        override fun disconnect() {}
        override fun usingProxy() = false
    }

    @Test fun homeHeadersDoNotLeakIntoSearchDetailOrMedia() {
        activate()
        RecipeHttp.execute("home", mapOf("tabType" to "8", "offset" to "0", "sessionId" to "", "selectedItems" to ""), "1234567", "7654321")
        RecipeHttp.execute("search", mapOf("query" to "测试"), "1234567", "7654321")
        RecipeHttp.execute("detail", mapOf("seriesId" to "s1"), "1234567", "7654321")
        media()
        assertEquals(4, requests.size)
        assertTrue(requests[0].getRequestProperty("User-Agent").startsWith("com.phoenix.read/65532 "))
        assertEquals("application/json", requests[0].getRequestProperty("Accept"))
        assertEquals("zh-CN,zh;q=0.9", requests[0].getRequestProperty("Accept-Language"))
        assertNull(requests[1].getRequestProperty("User-Agent"))
        assertEquals(root.getJSONObject("userAgents").getString("model"), requests[2].getRequestProperty("User-Agent"))
        assertEquals(requests[2].getRequestProperty("User-Agent"), requests[3].getRequestProperty("User-Agent"))
        assertTrue(requests.drop(1).all { it.getRequestProperty("Accept-Language") == null })
    }

    @Test fun homeMergesSelectorsAndKeepsNewGeneralAndNestedGroups() {
        activate()
        answer = { 200 to """{"code":0,"data":{"tab_item":[{"tab_type":8,"next_offset":1,"cell_data":[
            {"cell_selector":{"outer_row":{"items":[{"selector_item_id":"modern","show_name":"现代"},{"selector_item_id":"modern","show_name":"重复"}]},"inner_rows":[{"type":"other","row_name":"综合","items":[{"selector_item_id":"new-general","show_name":"新增综合"}]},{"type":"theme","row_name":"题材","items":[{"selector_item_id":"a","show_name":"甲"}]}]}},
            {"cell_data":[{"cell_selector":{"outer_row":{"items":[{"selector_item_id":"urban","show_name":"都市"}]},"inner_rows":[{"type":"theme","row_name":"题材","items":[{"selector_item_id":"a","show_name":"甲"},{"selector_item_id":"b","show_name":"乙"},{"selector_item_id":"","show_name":"缺失ID"}]}]}}]}
        ]}]}}""" }
        val page = DirectClient.homeTab(8, 0, "", null, "1234567", "7654321")
        assertEquals(listOf("modern", "urban"), page.filters.map { it.id })
        assertEquals(listOf("综合", "题材"), page.filterGroups.map { it.name })
        assertEquals(listOf("new-general"), page.filterGroups[0].items.map { it.id })
        assertEquals(listOf("a", "b"), page.filterGroups[1].items.map { it.id })
    }

    @Test fun emptyResponseRetriesSameSignatureBeforeSwitching() {
        activate()
        answer = { if (requests.size == 1) 200 to "" else 200 to "{\"code\":0}" }
        media()
        assertEquals(2, requests.size)
        assertTrue(requests.all { it.signed })
        assertNotEquals(requests[0].getRequestProperty("X-Gorgon"), requests[1].getRequestProperty("X-Gorgon"))
    }

    @Test fun emptyResponseSwitchesOnlyAfterSameModeBudgetIsExhausted() {
        endpoint("media").getJSONObject("retry").put("attempts", 2)
        activate()
        answer = { if (it.signed) 200 to "" else 200 to "{\"code\":0}" }
        media()
        assertEquals(listOf(true, true, false), requests.map { it.signed })
    }

    @Test fun disabledEmptyRetryIsHonored() {
        endpoint("media").getJSONObject("retry").put("on", JSONArray(listOf("network")))
        activate()
        answer = { 200 to "" }
        assertThrows(DirectClient.DirectException::class.java) { media() }
        assertEquals(1, requests.size)
    }

    @Test fun searchUsesRetryModesAndConfiguredExtraction() {
        endpoint("search").put("sign", JSONArray(listOf("none", "legacy"))).put("extract", "payload.results")
        endpoint("search").getJSONObject("retry").put("attempts", 2)
        activate()
        answer = {
            200 to if (it.signed) """{"code":0,"payload":{"results":[{"id":"s","type":"短剧","display":{"title":"测试剧"}}]},"debug_info":""" else ""
        }
        val hits = DirectClient.searchHits("test", "1234567", "7654321")
        assertEquals(listOf("s"), hits.map { it.id })
        // long: 截到目标数组即停止，故意不完整的调试尾部不能拖垮有效搜索结果。
        assertEquals(listOf(false, false, true), requests.map { it.signed })
    }

    @Test fun searchDistinguishesEmptyFailureFromValidEmptyResults() {
        endpoint("search").getJSONObject("retry").put("attempts", 1)
        activate()
        answer = { 200 to "" }
        assertThrows(DirectClient.DirectException::class.java) { DirectClient.searchHits("test", "1234567", "7654321") }
        answer = { 200 to "{\"code\":0, \"data\" : []}" }
        assertTrue(DirectClient.searchHits("test", "1234567", "7654321").isEmpty())
    }

    @Test fun jsonFailuresAndTransientBusinessCodesUseSameBudget() {
        activate()
        answer = { when (requests.size) {
            1 -> 200 to "<html>gateway</html>"
            2 -> 200 to "{\"code\":101116}"
            else -> 200 to "{\"code\":0}"
        } }
        media()
        assertEquals(3, requests.size)
    }

    @Test fun terminalHttpFailureIsNotRetried() {
        activate()
        answer = { 403 to "forbidden" }
        val error = assertThrows(DirectClient.DirectException::class.java) { media() }
        assertFalse(error.retryable)
        assertEquals(1, requests.size)
    }

    @Test fun detailUsesConfiguredPathAndRejectsAnotherSeries() {
        endpoint("detail").put("extract", "data.moved")
        endpoint("detail").getJSONObject("retry").put("attempts", 1)
        activate()
        answer = { 200 to """{"code":0,"data":{"moved":{"s":{"video_data":{"video_list":[{"vid":"v1","vid_index":1}]}}}}}""" }
        assertEquals(listOf("v1"), DirectClient.chapters("s", "1234567", "7654321"))
        assertThrows(DirectClient.DirectException::class.java) { DirectClient.chapters("other", "1234567", "7654321") }
    }

    @Test fun inFlightMediaKeepsOriginalExtractionWhenRecipeChanges() {
        activate()
        answer = {
            val newer = JSONObject(root.toString()).put("version", 101)
            newer.getJSONObject("endpoints").getJSONObject("media").put("extract", "data.changed")
            assertTrue(RecipeStore.saveOverride(newer.toString()))
            val model = JSONObject().put("video_list", JSONArray().put(JSONObject()
                .put("main_url", "https://cdn.example/video")
                .put("video_meta", JSONObject().put("definition", "1080p").put("codec_type", "bytevc1"))
                .put("encrypt_info", JSONObject().put("spade_a", "o7wtxFW+HPdNiwTCf44Bwn+ENcl4gAPJfLE3/XytNtZKkAKjow=="))))
            200 to JSONObject().put("code", 0).put("data", JSONObject().put("video_model_data",
                JSONObject().put("v1", JSONObject().put("video_model", model.toString())))).toString()
        }
        assertEquals(1080, DirectClient.media("v1", "1234567", "7654321").quality)
    }

    @Test fun postSendsRenderedJsonBodyWithoutEscapingIntoOtherFields() {
        endpoint("search").put("method", "POST").put("body", JSONObject().put("keyword", "${'$'}query"))
        activate()
        answer = { 200 to "{\"data\":[]}" }
        DirectClient.searchHits("a\",\"injected\":true", "1234567", "7654321")
        val request = requests.single()
        assertEquals("POST", request.requestMethod)
        val body = JSONObject(request.output.toString("UTF-8"))
        assertEquals(1, body.length())
        assertEquals("a\",\"injected\":true", body.getString("keyword"))
    }

    @Test fun cancellationDuringBackoffStopsBeforeNextRequest() {
        val task = TaskCancellation()
        activate()
        answer = { task.cancel(); 200 to "" }
        assertThrows(CancellationException::class.java) {
            RecipeHttp.execute("media", mapOf("videoId" to "v1"), "1234567", "7654321", task)
        }
        assertEquals(1, requests.size)
    }

    @Test fun cancelledPrefetchCannotPoisonJumpRequestOrItsEmptyResponseRetry() {
        activate()
        val oldTask = TaskCancellation()
        val newTask = TaskCancellation()
        val oldEntered = CountDownLatch(1)
        val releaseOld = CountDownLatch(1)
        val oldFinished = CountDownLatch(1)
        val oldError = AtomicReference<Throwable?>()
        val jumpAttempts = AtomicInteger()
        answer = { connection ->
            if (connection.url.query.contains("video_id=episode3")) {
                oldEntered.countDown()
                check(releaseOld.await(5, TimeUnit.SECONDS))
                200 to "{\"code\":0}"
            } else {
                if (jumpAttempts.incrementAndGet() == 1) 200 to ""
                else 200 to "{\"code\":0,\"selected\":10}"
            }
        }
        val oldWorker = Thread {
            try {
                RecipeHttp.execute("media", mapOf("videoId" to "episode3"), "1234567", "7654321", oldTask)
            } catch (error: Throwable) {
                oldError.set(error)
            } finally { oldFinished.countDown() }
        }.apply { start() }
        try {
            assertTrue(oldEntered.await(5, TimeUnit.SECONDS))
            // long: 模拟自动连播第二集时第三集预取尚未退出，新选第十集的恢复预算必须独立。
            oldTask.cancel()
            val response = RecipeHttp.execute("media", mapOf("videoId" to "episode10"), "1234567", "7654321", newTask)
            assertEquals(10, JSONObject(response).getInt("selected"))
            assertEquals(2, jumpAttempts.get())
            assertFalse(newTask.isCancelled)
            assertTrue(requests.filter { it.url.query.contains("video_id=episode10") }.all { it.signed })
        } finally {
            releaseOld.countDown()
            oldWorker.join(5000)
        }
        assertTrue(oldFinished.await(1, TimeUnit.SECONDS))
        assertTrue(oldError.get() is CancellationException)
        assertEquals(1, requests.count { it.url.query.contains("video_id=episode3") })
    }
}
