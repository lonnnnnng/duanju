package com.duanju.player

import android.content.Context
import com.duanju.player.core.DirectClient
import org.json.JSONArray
import org.json.JSONObject

/** 首页分页结果的本地缓存，进程被系统回收后也能恢复首页首屏和分页位置。 */
object HomeCacheStore {

    private const val PREFS = "duanju_home_cache"
    private const val KEY_PREFIX = "page:"

    fun load(context: Context, selectedFilter: String?): DirectClient.HomePage? {
        val raw = prefs(context).getString(key(selectedFilter), null) ?: return null
        return runCatching { decode(JSONObject(raw)) }.getOrNull()
    }

    fun save(context: Context, selectedFilter: String?, page: DirectClient.HomePage) {
        // long: 首页卡片会随分页增长，统一写入聚合结果，恢复后才能继续从正确 offset 加载。
        prefs(context).edit().putString(key(selectedFilter), encode(page).toString()).apply()
    }

    fun clear(context: Context) {
        prefs(context).edit().clear().apply()
    }

    private fun key(selectedFilter: String?): String = KEY_PREFIX + (selectedFilter ?: "default")

    private fun encode(page: DirectClient.HomePage): JSONObject = JSONObject().apply {
        put("items", JSONArray().apply {
            page.items.forEach { item ->
                put(JSONObject().apply {
                    put("id", item.id)
                    put("title", item.title)
                    put("episodes", item.episodes)
                    put("cover", item.cover ?: JSONObject.NULL)
                    put("score", item.score)
                    put("playCount", item.playCount)
                    put("tag", item.tag ?: JSONObject.NULL)
                    put("tagColor", item.tagColor ?: JSONObject.NULL)
                    put("subTitle", item.subTitle)
                    put("contentType", item.contentType)
                    put("hotValue", item.hotValue)
                    put("category", item.category)
                    put("rank", item.rank)
                })
            }
        })
        put("nextOffset", page.nextOffset)
        put("sessionId", page.sessionId)
        put("hasMore", page.hasMore)
        put("filters", JSONArray().apply {
            page.filters.forEach { filter ->
                put(JSONObject().put("id", filter.id).put("name", filter.name))
            }
        })
        put("filterGroups", JSONArray().apply {
            page.filterGroups.forEach { group ->
                put(JSONObject().apply {
                    put("name", group.name)
                    put("items", JSONArray().apply {
                        group.items.forEach { filter ->
                            put(JSONObject().put("id", filter.id).put("name", filter.name))
                        }
                    })
                })
            }
        })
    }

    private fun decode(root: JSONObject): DirectClient.HomePage {
        val itemsJson = root.optJSONArray("items") ?: JSONArray()
        val items = buildList {
            for (index in 0 until itemsJson.length()) {
                val item = itemsJson.optJSONObject(index) ?: continue
                add(
                    DirectClient.HomeDrama(
                        id = item.optString("id"),
                        title = item.optString("title"),
                        episodes = item.optInt("episodes"),
                        cover = item.optNullableString("cover"),
                        score = item.optString("score"),
                        playCount = item.optLong("playCount"),
                        tag = item.optNullableString("tag"),
                        tagColor = item.optNullableString("tagColor"),
                        subTitle = item.optString("subTitle"),
                        contentType = item.optInt("contentType"),
                        hotValue = item.optString("hotValue"),
                        category = item.optString("category"),
                        rank = item.optInt("rank")
                    )
                )
            }
        }
        val filtersJson = root.optJSONArray("filters") ?: JSONArray()
        val filters = buildList {
            for (index in 0 until filtersJson.length()) {
                val item = filtersJson.optJSONObject(index) ?: continue
                add(DirectClient.HomeFilter(item.optString("id"), item.optString("name")))
            }
        }
        val groupsJson = root.optJSONArray("filterGroups") ?: JSONArray()
        val groups = buildList {
            for (index in 0 until groupsJson.length()) {
                val group = groupsJson.optJSONObject(index) ?: continue
                val groupItems = group.optJSONArray("items") ?: JSONArray()
                val itemsInGroup = buildList {
                    for (itemIndex in 0 until groupItems.length()) {
                        val item = groupItems.optJSONObject(itemIndex) ?: continue
                        add(DirectClient.HomeFilter(item.optString("id"), item.optString("name")))
                    }
                }
                add(DirectClient.HomeFilterGroup(group.optString("name"), itemsInGroup))
            }
        }
        return DirectClient.HomePage(
            items = items,
            nextOffset = root.optInt("nextOffset"),
            sessionId = root.optString("sessionId"),
            hasMore = root.optBoolean("hasMore"),
            filters = filters,
            filterGroups = groups
        )
    }

    private fun JSONObject.optNullableString(name: String): String? =
        if (isNull(name)) null else optString(name).takeIf { it.isNotEmpty() }

    private fun prefs(context: Context) =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
}
