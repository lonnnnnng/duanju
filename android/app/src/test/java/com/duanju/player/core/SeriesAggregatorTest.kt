package com.duanju.player.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SeriesAggregatorTest {

    private fun hit(id: String, title: String, type: String = "漫剧", episodes: Int = 100) =
        SeriesAggregator.Hit(id, title, type, episodes)

    @Test
    fun stripsSeasonSuffix() {
        assertEquals("谁说纨绔不能当状元", SeriesAggregator.baseTitle("谁说纨绔不能当状元第五季"))
        assertEquals("谁说纨绔不能当状元", SeriesAggregator.baseTitle("谁说纨绔不能当状元第十二季"))
        assertEquals("聚宝仙盆", SeriesAggregator.baseTitle("聚宝仙盆 第15季"))
        assertEquals("聚宝仙盆", SeriesAggregator.baseTitle("聚宝仙盆"))
    }

    @Test
    fun parsesSeasonNumbers() {
        assertEquals(1, SeriesAggregator.seasonNumber("聚宝仙盆"))
        assertEquals(2, SeriesAggregator.seasonNumber("聚宝仙盆第二季"))
        assertEquals(7, SeriesAggregator.seasonNumber("聚宝仙盆第七季"))
        assertEquals(10, SeriesAggregator.seasonNumber("聚宝仙盆第十季"))
        assertEquals(13, SeriesAggregator.seasonNumber("聚宝仙盆第十三季"))
        assertEquals(21, SeriesAggregator.seasonNumber("聚宝仙盆第二十一季"))
        assertEquals(15, SeriesAggregator.seasonNumber("聚宝仙盆第15季"))
    }

    @Test
    fun aggregatesSeasonsInOrder() {
        val hits = listOf(
            hit("3", "某剧第三季"),
            hit("1", "某剧"),
            hit("2", "某剧第二季"),
            hit("9", "无关剧")
        )
        val seasons = SeriesAggregator.aggregate("某剧第二季", "漫剧", hits)
        assertEquals(listOf("1", "2", "3"), seasons.map { it.id })
        assertEquals(listOf(1, 2, 3), seasons.map { it.season })
    }

    @Test
    fun marksReservedSeasonsUnplayable() {
        val hits = listOf(
            hit("1", "某剧"),
            hit("2", "某剧第十五季", type = SeriesAggregator.RESERVED_TYPE, episodes = 0),
            hit("3", "某剧第十六季", episodes = 0)
        )
        val seasons = SeriesAggregator.aggregate("某剧", "漫剧", hits)
        assertEquals(listOf(true, false, false), seasons.map { it.playable })
    }

    @Test
    fun keepsShortDramaAndComicSeparate() {
        val hits = listOf(
            hit("1", "某剧", type = "漫剧"),
            hit("2", "某剧第二季", type = "漫剧"),
            hit("3", "某剧", type = "短剧")
        )
        val seasons = SeriesAggregator.aggregate("某剧", "漫剧", hits)
        assertEquals(listOf("1", "2"), seasons.map { it.id })
    }

    @Test
    fun fallsBackToLargestGroup() {
        val hits = listOf(hit("1", "甲剧"), hit("2", "乙剧"), hit("3", "乙剧第二季"))
        val seasons = SeriesAggregator.aggregate("找不到的剧", null, hits)
        assertEquals(listOf("2", "3"), seasons.map { it.id })
    }

    @Test
    fun emptyInputGivesEmptyResult() {
        assertTrue(SeriesAggregator.aggregate("某剧", "漫剧", emptyList()).isEmpty())
    }
}
