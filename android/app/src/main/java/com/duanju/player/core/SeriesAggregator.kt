package com.duanju.player.core

/**
 * 系列剧（分季）手动聚合。
 *
 * 详情接口不返回任何季/合集字段，同一部剧的每一季都是独立的 series_id。
 * 规则来自 hongguo-ref/docs/API.md §6.7.5：
 *  1. 按标题搜索，用正则剥离「第X季」后缀得到基名；
 *  2. 以基名为 key 聚合；
 *  3. `episode_cnt > 0` 且不是预约剧壳 → 可播，否则视为预约/未上线。
 *
 * 短剧与漫剧属于不同 product line，同名也不算同一季，因此分组键带上类型。
 */
object SeriesAggregator {

    // 兼容「第五季」「第六季！」「（第一季）」这几种写法。
    private val SEASON_SUFFIX = Regex(
        "[（(【\\[「《]?\\s*第[一二三四五六七八九十百千零两\\d]+\\s*季\\s*[）)】\\]」》]?\\s*$"
    )
    private val SEASON_NUMBER = Regex(
        "第([一二三四五六七八九十百千零两\\d]+)\\s*季\\s*[）)】\\]」》]?\\s*$"
    )
    private val TRAILING_PUNCT = Regex("[\\s！!。.、,，:：;；·…~～\\-_（(【\\[「《]+$")

    private val DIGITS = mapOf(
        '零' to 0, '一' to 1, '二' to 2, '两' to 2, '三' to 3, '四' to 4,
        '五' to 5, '六' to 6, '七' to 7, '八' to 8, '九' to 9
    )
    private val UNITS = mapOf('十' to 10, '百' to 100, '千' to 1000)

    /** 一条候选（搜索命中或剧集页里的某一季）。 */
    data class Hit(
        val id: String,
        val title: String,
        val type: String,
        val episodes: Int,
        val cover: String? = null
    )

    /** 聚合后的一季。 */
    data class Season(
        val id: String,
        val title: String,
        val baseTitle: String,
        val season: Int,
        val episodes: Int,
        val type: String,
        val playable: Boolean,
        val cover: String? = null
    )

    /** 剥离「第X季」后缀；没有后缀时返回原标题（基础季）。 */
    fun baseTitle(title: String): String =
        title.replace(SEASON_SUFFIX, "").replace(TRAILING_PUNCT, "").trim()

    /** 解析季号；基础季（无后缀）为 1，无法识别时也按 1 处理。 */
    fun seasonNumber(title: String): Int {
        val raw = SEASON_NUMBER.find(title.trim())?.groupValues?.get(1) ?: return 1
        return parseNumber(raw) ?: 1
    }

    private fun parseNumber(raw: String): Int? {
        raw.toIntOrNull()?.let { return it }
        var total = 0
        var current = 0
        for (ch in raw) {
            DIGITS[ch]?.let { current = it }
            UNITS[ch]?.let { unit ->
                total += (if (current == 0) 1 else current) * unit
                current = 0
            }
        }
        val value = total + current
        return value.takeIf { it > 0 }
    }

    /**
     * 聚合与 [title] 同名的所有季，按季号升序。
     *
     * 先按基名 + 类型分组，再挑「与当前剧同组」的那一组；若搜索结果里没有当前剧，
     * 退化为季数最多的分组。这样即使上游标题有细微差异也能给出可用结果。
     */
    fun aggregate(title: String, type: String?, hits: List<Hit>): List<Season> {
        val seasons = hits.map { hit ->
            val base = baseTitle(hit.title)
            Season(
                id = hit.id,
                title = hit.title,
                baseTitle = base,
                season = seasonNumber(hit.title),
                episodes = hit.episodes,
                type = hit.type,
                // 预约剧壳的集数为 0，属于「已公布未上线」。
                playable = hit.episodes > 0 && hit.type != RESERVED_TYPE,
                cover = hit.cover
            )
        }
        if (seasons.isEmpty()) return emptyList()

        // 先按基名分组（预约剧壳也属于同一部剧，不参与 product line 区分）。
        val groups = seasons.groupBy { it.baseTitle }
        val currentBase = baseTitle(title)
        val group = groups[currentBase]?.takeIf { it.isNotEmpty() }
            ?: groups.values.maxByOrNull { it.size }
            ?: return emptyList()

        // 再按 product line 过滤：短剧与漫剧同名也不是同一部剧；预约剧壳跟随可播季。
        val playableTypes = group.filter { it.type != RESERVED_TYPE }.groupingBy { it.type }.eachCount()
        val chosenType = type?.takeIf { playableTypes.containsKey(it) }
            ?: playableTypes.maxByOrNull { it.value }?.key
        val picked = if (chosenType == null) {
            group
        } else {
            group.filter { it.type == chosenType || it.type == RESERVED_TYPE }
        }
        // 同一季号可能既有「基础剧」又有「第一季」，集数多的排在前面。
        return picked.distinctBy { it.id }.sortedWith(compareBy({ it.season }, { -it.episodes }))
    }

    const val RESERVED_TYPE = "预约剧壳"
}
