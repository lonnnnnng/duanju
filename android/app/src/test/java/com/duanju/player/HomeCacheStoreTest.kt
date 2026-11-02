package com.duanju.player

import android.content.Context
import com.duanju.player.core.DirectClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class HomeCacheStoreTest {

    private lateinit var context: Context

    @Before
    fun setUp() {
        context = RuntimeEnvironment.getApplication()
        HomeCacheStore.clear(context)
    }

    @Test
    fun roundTripKeepsHomePageFieldsAndNullableValues() {
        val page = samplePage()

        HomeCacheStore.save(context, null, page)

        assertEquals(page, HomeCacheStore.load(context, null))
    }

    @Test
    fun filterCachesAreIndependentAndClearRemovesAll() {
        val recommended = samplePage().copy(sessionId = "recommended")
        val ranked = samplePage().copy(sessionId = "ranked")

        HomeCacheStore.save(context, null, recommended)
        HomeCacheStore.save(context, "ranklist_hot", ranked)

        assertEquals("recommended", HomeCacheStore.load(context, null)?.sessionId)
        assertEquals("ranked", HomeCacheStore.load(context, "ranklist_hot")?.sessionId)

        HomeCacheStore.clear(context)
        assertNull(HomeCacheStore.load(context, null))
        assertNull(HomeCacheStore.load(context, "ranklist_hot"))
    }

    private fun samplePage(): DirectClient.HomePage = DirectClient.HomePage(
        items = listOf(
            DirectClient.HomeDrama(
                id = "series-1",
                title = "测试短剧",
                episodes = 12,
                cover = null,
                score = "9.2",
                playCount = 1234,
                tag = "热播",
                tagColor = "#FF0000",
                subTitle = "副标题",
                contentType = 1,
                hotValue = "8602万",
                category = "玄幻",
                rank = 2
            )
        ),
        nextOffset = 8,
        sessionId = "session-1",
        hasMore = true,
        filters = listOf(DirectClient.HomeFilter("全部", "推荐")),
        filterGroups = listOf(
            DirectClient.HomeFilterGroup(
                "题材",
                listOf(DirectClient.HomeFilter("xuanhuan", "玄幻"))
            )
        )
    )
}
