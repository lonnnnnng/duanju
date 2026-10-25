package com.duanju.player

import org.json.JSONObject

data class Drama(
    val id: String,
    val title: String,
    val episodes: Int,
    val cover: String?
)

object DramaParser {
    fun parseList(text: String): List<Drama> {
        val root = JSONObject(text)
        val data = root.optJSONArray("data") ?: return emptyList()
        val list = ArrayList<Drama>(data.length())
        for (index in 0 until data.length()) {
            val item = data.optJSONObject(index) ?: continue
            val id = item.optString("id", "")
            if (id.isEmpty()) continue
            val title = item.optString("title", "").ifBlank { item.optString("name", "短剧") }
            val episodes = intOf(item, "totalEpisode")
                .takeIf { it > 0 }
                ?: intOf(item, "episodeCount")
                .takeIf { it > 0 }
                ?: intOf(item, "total")
            val coverValue = item.opt("cover")
            val cover = (coverValue as? String)?.takeIf { it.isNotBlank() }
            list.add(Drama(id, title.ifBlank { "短剧" }, episodes, cover))
        }
        return list
    }

    private fun intOf(item: JSONObject, key: String): Int = when (val value = item.opt(key)) {
        is Number -> value.toInt()
        is String -> value.toIntOrNull() ?: 0
        else -> 0
    }
}