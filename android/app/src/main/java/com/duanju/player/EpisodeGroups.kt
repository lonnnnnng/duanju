package com.duanju.player

/** 选集分组：每 30 集一组，长剧（几百集）不再整屏平铺。 */
object EpisodeGroups {
    const val SIZE = 30

    fun ranges(total: Int): List<IntRange> {
        if (total <= 0) return emptyList()
        val out = ArrayList<IntRange>((total + SIZE - 1) / SIZE)
        var start = 1
        while (start <= total) {
            out.add(start..minOf(start + SIZE - 1, total))
            start += SIZE
        }
        return out
    }

    fun label(range: IntRange): String =
        if (range.first == range.last) "${range.first}" else "${range.first}-${range.last}"

    fun groupOf(episode: Int, total: Int): IntRange? =
        ranges(total).firstOrNull { episode in it }
}
