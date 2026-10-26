package com.duanju.player

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

data class HistoryEntry(
    val id: String,
    val title: String,
    val episodes: Int,
    val episode: Int,
    val positionMs: Long,
    val updatedAt: Long,
    val cover: String = ""
)

object HistoryStore {
    private const val KEY = "history"
    private const val MAX = 100

    fun list(context: Context): List<HistoryEntry> {
        val raw = prefs(context).getString(KEY, null) ?: return emptyList()
        val array = runCatching { JSONArray(raw) }.getOrNull() ?: return emptyList()
        val result = ArrayList<HistoryEntry>(array.length())
        for (index in 0 until array.length()) {
            val item = array.optJSONObject(index) ?: continue
            result.add(
                HistoryEntry(
                    id = item.optString("id"),
                    title = item.optString("title").ifBlank { "短剧" },
                    episodes = item.optInt("episodes"),
                    episode = item.optInt("episode"),
                    positionMs = item.optLong("positionMs"),
                    updatedAt = item.optLong("updatedAt"),
                    cover = item.optString("cover")
                )
            )
        }
        return result
    }

    fun upsert(context: Context, entry: HistoryEntry) {
        val entries = list(context).filter { it.id != entry.id }.toMutableList()
        entries.add(0, entry)
        while (entries.size > MAX) entries.removeAt(entries.size - 1)
        save(context, entries)
    }

    fun remove(context: Context, id: String) {
        save(context, list(context).filter { it.id != id })
    }

    fun find(context: Context, id: String): HistoryEntry? = list(context).firstOrNull { it.id == id }

    private fun save(context: Context, entries: List<HistoryEntry>) {
        val array = JSONArray()
        entries.forEach { entry ->
            array.put(
                JSONObject()
                    .put("id", entry.id)
                    .put("title", entry.title)
                    .put("episodes", entry.episodes)
                    .put("episode", entry.episode)
                    .put("positionMs", entry.positionMs)
                    .put("updatedAt", entry.updatedAt)
                    .put("cover", entry.cover)
            )
        }
        prefs(context).edit().putString(KEY, array.toString()).apply()
    }

    private fun prefs(context: Context) = context.getSharedPreferences("duanju", Context.MODE_PRIVATE)
}