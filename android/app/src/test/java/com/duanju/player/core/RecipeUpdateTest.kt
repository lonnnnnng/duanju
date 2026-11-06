package com.duanju.player.core

import android.app.Application
import android.content.ContextWrapper
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
class RecipeUpdateTest {
    @get:Rule val temp = TemporaryFolder()
    private lateinit var template: String
    @Before fun setup() {
        val app = RuntimeEnvironment.getApplication()
        RecipeStore.init(object : ContextWrapper(app) {
            override fun getApplicationContext() = this
            override fun getFilesDir() = temp.root
        })
        template = File("src/main/assets/recipes/hongguo.json").readText()
    }
    private fun recipe(version: Int) = JSONObject(template).put("version", version).toString()

    @Test fun staleMirrorDoesNotHideHigherVersionAndHighestValidSourceWins() {
        val seen = mutableListOf<String>()
        val versions = mapOf("old" to 1, "new" to 101, "newest" to 102)
        val result = RecipeUpdater.refreshFrom(versions.keys.toList(), { url ->
            seen.add(url)
            if (url.contains(".sig?")) byteArrayOf(1)
            else recipe(versions.getValue(url.substringBefore('/'))).toByteArray()
        }, { _, _ -> true })
        assertEquals(true, result)
        assertEquals(102, RecipeStore.recipe().version)
        assertEquals(6, seen.size)
    }

    @Test fun malformedSignatureCannotStopFallbackToNextSource() {
        val seen = mutableListOf<String>()
        val signedJson = File("../../recipes/recipe.json").readBytes()
        val signedSig = File("../../recipes/recipe.json.sig").readBytes()
        val result = RecipeUpdater.refreshFrom(listOf("bad", "good"), { url ->
            seen.add(url)
            if (url.contains(".sig?")) {
                if (url.startsWith("bad/")) "<html>proxy error</html>".toByteArray() else signedSig
            } else signedJson
        })
        assertEquals(false, result)
        assertEquals(4, seen.size)
    }

    @Test fun allInvalidSourcesReturnFailureWithoutChangingLocalRecipe() {
        val version = RecipeStore.recipe().version
        assertNull(RecipeUpdater.refreshFrom(listOf("bad"), { byteArrayOf(1) }))
        assertEquals(version, RecipeStore.recipe().version)
    }

    @Test fun concurrentSavesNeverRollBackVersionOrCorruptFile() {
        val pool = Executors.newFixedThreadPool(8)
        val start = CountDownLatch(1)
        try {
            val futures = (100..131).shuffled().map { version -> pool.submit<Boolean> {
                start.await()
                RecipeStore.saveOverride(recipe(version))
            } }
            start.countDown()
            futures.forEach { it.get(5, TimeUnit.SECONDS) }
            assertEquals(131, RecipeStore.recipe().version)
            RecipeStore.reload()
            assertEquals(131, RecipeStore.recipe().version)
            assertFalse(RecipeStore.saveOverride(recipe(130)))
            assertFalse(RecipeStore.saveOverride(recipe(131)))
            assertEquals(131, RecipeParser.parse(File(temp.root, "recipes/hongguo.json").readText()).version)
        } finally { pool.shutdownNow() }
    }

    @Test fun invalidRecipeCannotReplaceUsableLocalRecipe() {
        assertTrue(RecipeStore.saveOverride(recipe(100)))
        val invalid = JSONObject(recipe(101))
        invalid.getJSONObject("endpoints").remove("search")
        assertFalse(RecipeStore.saveOverride(invalid.toString()))
        assertEquals(100, RecipeStore.recipe().version)
        assertNotNull(RecipeStore.endpoint("search"))
    }

    @Test fun writeFailureRetainsCurrentRecipe() {
        File(temp.root, "recipes").writeText("blocked")
        val version = RecipeStore.recipe().version
        assertFalse(RecipeStore.saveOverride(recipe(100)))
        assertEquals(version, RecipeStore.recipe().version)
    }

    @Test fun unsupportedRecipeFeaturesAreRejectedBeforeActivation() {
        val mutations: List<(JSONObject) -> Unit> = listOf(
            { it.put("schemaVersion", 2) },
            { it.put("schemaVersion", 1.5) },
            { it.put("schemaVersion", "invalid") },
            { it.put("version", 100.5) },
            { it.put("version", "100") },
            { it.getJSONObject("endpoints").remove("media") },
            { it.getJSONObject("hosts").put("open", "http://unsafe.example") },
            { it.getJSONObject("endpoints").getJSONObject("media").put("sign", JSONArray(listOf("unknown"))) },
            { it.getJSONObject("endpoints").getJSONObject("media").put("method", "DELETE") },
            { it.getJSONObject("endpoints").getJSONObject("media").put("body", JSONObject()) },
            { it.getJSONObject("endpoints").getJSONObject("media").put("method", "POST").put("body", JSONObject()) },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("attempts", 0) },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("attempts", 999) },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("attempts", 2.5) },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("attempts", "2") },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("backoffMs", JSONArray(listOf(-1))) },
            { it.getJSONObject("endpoints").getJSONObject("media").getJSONObject("retry").put("backoffMs", JSONArray(listOf("bad"))) },
            { it.getJSONObject("endpoints").getJSONObject("media").put("extract", "${'$'}unsupported") }
        )
        mutations.forEach { change ->
            val root = JSONObject(recipe(100)).also(change)
            assertThrows(root.toString(), IllegalArgumentException::class.java) { RecipeParser.parse(root.toString()) }
        }
    }
}
