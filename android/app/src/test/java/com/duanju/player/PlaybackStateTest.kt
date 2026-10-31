package com.duanju.player

import org.junit.Assert.*
import org.junit.Test

class PlaybackStateTest {
    @Test
    fun switchingCannotAssignOldPositionToNewEpisode() {
        val state = PlaybackState()
        val first = state.select(1)
        state.ready(1, first)
        assertEquals(EpisodeProgress(1, 40_000, 90_000), state.progress(40_000, 90_000))
        state.select(5)
        assertNull(state.progress(40_000, 90_000))
    }

    @Test
    fun preparingOrFailingToResumeDoesNotOverwriteSavedProgressWithZero() {
        val state = PlaybackState()
        state.select(5)
        assertNull(state.progress(0, 0))
    }

    @Test
    fun lateReadyCannotReplaceLatestSelectionEvenWhenReturningToSameEpisode() {
        val state = PlaybackState()
        val old = state.select(2)
        state.select(3)
        val latest = state.select(2)
        assertFalse(state.ready(2, old))
        assertNull(state.progress(15_000, 60_000))
        assertTrue(state.ready(2, latest))
        assertEquals(EpisodeProgress(2, 15_000, 60_000), state.progress(15_000, 60_000))
    }

    @Test
    fun newMediaOwnsProgressOnlyAfterReady() {
        val state = PlaybackState()
        val request = state.select(8)
        assertFalse(state.ready(7, request))
        assertNull(state.playingEpisode)
        assertTrue(state.ready(8, request))
        assertEquals(EpisodeProgress(8, 0, 0), state.progress(-1, -1))
    }
}
