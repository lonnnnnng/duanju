package com.duanju.player

import android.content.Context

/** 搜索历史：本地缓存最近 10 条，去重，最新在前。 */
object SearchHistoryStore {

    private const val PREFS = "duanju_search"
    private const val KEY = "history"
    private const val MAX = 10

    fun load(context: Context): List<String> {
        val raw = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY, null)
        if (raw.isNullOrEmpty()) return emptyList()
        return raw.split("\u0001").filter { it.isNotBlank() }.take(MAX)
    }

    fun add(context: Context, keyword: String) {
        // 分隔符本身出现时截断，避免 load 时把一条记录拆成多条
        val word = keyword.trim().replace("\u0001", "")
        if (word.isEmpty()) return
        val list = ArrayList(load(context))
        list.removeAll { it == word }
        list.add(0, word)
        save(context, list.take(MAX))
    }

    fun clear(context: Context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove(KEY).apply()
    }

    private fun save(context: Context, list: List<String>) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString(KEY, list.joinToString("\u0001")).apply()
    }
}
