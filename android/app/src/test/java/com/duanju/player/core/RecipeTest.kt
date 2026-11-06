package com.duanju.player.core

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

class RecipeTest {

    private val recipe: Recipe by lazy {
        val file = File("src/main/assets/recipes/hongguo.json")
        assertTrue("内置配方不存在: ${file.absolutePath}", file.isFile)
        RecipeParser.parse(file.readText())
    }

    @Test
    fun bundledRecipe_coversAllEndpoints() {
        assertEquals(setOf("home", "search", "detail", "media"), recipe.endpoints.keys)
        assertTrue(recipe.version >= 1)
    }

    @Test
    fun bundledRecipe_keepsVolatileValuesInData() {
        assertEquals("74232", recipe.endpoint("media").versionCode)
        assertEquals(listOf("legacy", "none"), recipe.endpoint("detail").signModes)
        assertEquals(listOf("legacy", "none"), recipe.endpoint("media").signModes)
        assertEquals(listOf("none"), recipe.endpoint("home").signModes)
        assertEquals("data.video_model_data.\$videoId.video_model", recipe.endpoint("media").extract)
    }

    @Test
    fun buildUrl_encodesValuesAndOmitsEmptyOptionalParams() {
        val url = RecipeHttp.buildUrl(
            recipe.endpoint("home"),
            recipe,
            mapOf("tabType" to "0", "offset" to "0", "sessionId" to "", "selectedItems" to "0,1"),
            "1234567890123456", "1234567890123456"
        )
        assertTrue(url.startsWith("https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/bookmall/tab/v/?"))
        assertTrue(url.contains("tab_type=0"))
        assertTrue(url.contains("offset=0"))
        assertTrue(url.contains("selected_items=0%2C1"))
        assertTrue("空 session_id 应省略", !url.contains("session_id"))
        assertTrue(url.contains("version_code=65532"))
    }

    @Test
    fun extract_followsVarsAndArrayIndex() {
        val body = JSONObject()
            .put("data", JSONObject()
                .put("video_model_data", JSONObject()
                    .put("vid1", JSONObject().put("video_model", """{"status":10}"""))))
            .toString()
        val value = RecipeHttp.extract(body, "data.video_model_data.\$videoId.video_model", mapOf("videoId" to "vid1"))
        assertEquals("""{"status":10}""", value)

        assertNull(RecipeHttp.extract(body, "data.video_model_data.\$videoId.missing", mapOf("videoId" to "vid1")))

        val list = JSONObject().put("data", JSONArray().put(JSONObject().put("id", "a"))).toString()
        assertEquals("a", (RecipeHttp.extract(list, "data.0.id", emptyMap()) as String))
    }

    @Test
    fun parser_rejectsEndpointWithoutHost() {
        val bad = """{"version":1,"hosts":{"open":"https://x"},"endpoints":{"e":{"path":"/p"}}}"""
        val error = runCatching { RecipeParser.parse(bad) }.exceptionOrNull()
        assertTrue(error is IllegalArgumentException)
    }
}
