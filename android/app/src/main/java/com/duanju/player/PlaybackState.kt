package com.duanju.player

/** long: 选中集与实际媒体分别记账，准备失败或旧回调迟到时都不能污染续播记录。 */
class PlaybackState {
    var selectedEpisode = 1
        private set
    var generation = 0L
        private set
    var playingEpisode: Int? = null
        private set

    fun select(episode: Int): Long {
        require(episode > 0)
        selectedEpisode = episode
        playingEpisode = null
        return ++generation
    }

    fun isCurrent(episode: Int, request: Long): Boolean =
        selectedEpisode == episode && generation == request

    fun ready(episode: Int, request: Long): Boolean {
        if (!isCurrent(episode, request)) return false
        playingEpisode = episode
        return true
    }

    fun progress(positionMs: Long, durationMs: Long): EpisodeProgress? =
        playingEpisode?.let { EpisodeProgress(it, positionMs.coerceAtLeast(0L), durationMs.coerceAtLeast(0L)) }
}

data class EpisodeProgress(val episode: Int, val positionMs: Long, val durationMs: Long)
