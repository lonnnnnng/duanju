package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.Button
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import java.util.concurrent.Executors

/** 剧集详情页：封面 + 标签 + 播放按钮 + 简介 + 选集（红果风格）。 */
class DetailActivity : AppCompatActivity() {

    private lateinit var cover: ImageView
    private lateinit var title: TextView
    private lateinit var subtitles: LinearLayout
    private lateinit var meta: TextView
    private lateinit var intro: TextView
    private lateinit var introToggle: TextView
    private var introExpanded = false
    private var introCollapsible = false
    private lateinit var playButton: Button
    private lateinit var episodeView: RecyclerView
    private lateinit var episodeAdapter: EpisodeNumberAdapter
    private lateinit var groupScroll: View
    private lateinit var groupsBox: LinearLayout
    private var activeGroup: IntRange? = null
    private lateinit var loading: ProgressBar
    private lateinit var errorBox: LinearLayout
    private lateinit var errorText: TextView
    private lateinit var scroll: View
    private lateinit var topBar: TopBar
    private lateinit var tabEpisodes: TextView
    private lateinit var tabRelated: TextView
    private lateinit var relatedList: RecyclerView
    private lateinit var relatedStatus: TextView
    private lateinit var relatedAdapter: RelatedAdapter
    private var relatedLoaded = false
    private var showingRelated = false

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    private var seriesId = ""
    private var seriesTitle = ""
    private var detail: DirectClient.SeriesDetail? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_detail)

        seriesId = intent.getStringExtra("dramaId").orEmpty()
        seriesTitle = intent.getStringExtra("title").orEmpty()

        cover = findViewById(R.id.detail_cover)
        title = findViewById(R.id.detail_title)
        subtitles = findViewById(R.id.detail_subtitles)
        meta = findViewById(R.id.detail_meta)
        intro = findViewById(R.id.detail_intro)
        introToggle = findViewById(R.id.detail_intro_toggle)
        introToggle.setOnClickListener { toggleIntro() }
        intro.setOnClickListener { if (introCollapsible) toggleIntro() }
        playButton = findViewById(R.id.detail_play)
        episodeView = findViewById(R.id.detail_episodes)
        loading = findViewById(R.id.detail_loading)
        errorBox = findViewById(R.id.detail_error)
        errorText = findViewById(R.id.detail_error_text)
        scroll = findViewById(R.id.detail_scroll)

        topBar = findViewById<TopBar>(R.id.detail_top_bar).apply {
            setOnBackClickListener { finish() }
        }
        findViewById<Button>(R.id.detail_retry).setOnClickListener { load() }

        groupScroll = findViewById(R.id.detail_group_scroll)
        groupsBox = findViewById(R.id.detail_groups)

        episodeAdapter = EpisodeNumberAdapter { ep -> play(ep.index) }
        episodeView.layoutManager = GridLayoutManager(this, 6)
        episodeView.adapter = episodeAdapter

        tabEpisodes = findViewById(R.id.detail_tab_episodes)
        tabRelated = findViewById(R.id.detail_tab_related)
        relatedList = findViewById(R.id.detail_related)
        relatedStatus = findViewById(R.id.detail_related_status)
        relatedAdapter = RelatedAdapter(seriesId) { season -> openSeason(season) }
        relatedList.layoutManager = androidx.recyclerview.widget.LinearLayoutManager(this)
        relatedList.adapter = relatedAdapter
        tabEpisodes.setOnClickListener { showRelated(false) }
        tabRelated.setOnClickListener { showRelated(true) }

        if (seriesTitle.isNotEmpty()) {
            topBar.setTitle(seriesTitle)
            title.text = seriesTitle
        }
        playButton.setOnClickListener {
            val first = detail?.list?.firstOrNull { !it.disabled } ?: return@setOnClickListener
            play(first.index)
        }
        load()
    }

    private fun load() {
        loading.visibility = View.VISIBLE
        errorBox.visibility = View.GONE
        scroll.visibility = View.INVISIBLE
        executor.execute {
            try {
                val (deviceId, installId) = DirectClient.identity(this)
                val data = DirectClient.seriesDetail(seriesId, deviceId, installId)
                main.post { render(data) }
            } catch (e: Exception) {
                main.post {
                    loading.visibility = View.GONE
                    errorText.text = e.message ?: "加载失败"
                    errorBox.visibility = View.VISIBLE
                }
            }
        }
    }

    private fun render(data: DirectClient.SeriesDetail) {
        detail = data
        loading.visibility = View.GONE
        scroll.visibility = View.VISIBLE
        title.text = data.title
        topBar.setTitle(data.title)
        // 标题可能来自首页卡片（简写），拿到详情后重算「相关」的搜索词。
        relatedLoaded = false
        if (showingRelated) showRelated(true)

        data.cover?.takeIf { it.isNotBlank() }?.let { BitmapLoader.load(it, cover) }

        subtitles.removeAllViews()
        val chips = data.subTitles.ifEmpty { listOfNotNull(data.category.ifBlank { null }) }
        chips.forEach { text ->
            val chip = TextView(this).apply {
                this.text = text
                textSize = 11f
                setTextColor(resolveSecondaryColor())
                setBackgroundResource(R.drawable.bg_chip_normal)
                setPadding(dp(6), dp(2), dp(6), dp(2))
            }
            val params = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { marginEnd = dp(8) }
            subtitles.addView(chip, params)
        }

        meta.text = buildString {
            append("共 ${data.episodes} 集")
            if (data.playCount > 0) append(" · ${HomeAdapter.formatCount(data.playCount)}播放")
            append(if (data.status == 1) " · 完结" else " · 连载中")
        }

        val introText = data.intro.trim()
        introExpanded = false
        introCollapsible = false
        introToggle.visibility = View.GONE
        if (introText.isNotEmpty()) {
            intro.visibility = View.VISIBLE
            intro.maxLines = Int.MAX_VALUE
            intro.text = introText
            // 布局完成后再判断是否超过 8 行，超过就折叠
            intro.post {
                if (intro.lineCount > 8) {
                    introCollapsible = true
                    intro.maxLines = 8
                    intro.ellipsize = android.text.TextUtils.TruncateAt.END
                    introToggle.visibility = View.VISIBLE
                }
            }
        } else {
            intro.visibility = View.GONE
        }

        renderGroups(data)
        val first = data.list.firstOrNull { !it.disabled }
        playButton.isEnabled = first != null
        playButton.text = if (first != null) "播放第 ${first.index} 集" else "暂无可播放剧集"
    }

    /** 每 30 集一组，默认选中包含第 1 集可播放剧集的分组。 */
    private fun renderGroups(data: DirectClient.SeriesDetail) {
        groupsBox.removeAllViews()
        val total = data.list.size
        val ranges = EpisodeGroups.ranges(total)
        if (ranges.size <= 1) {
            groupScroll.visibility = View.GONE
            episodeAdapter.submit(data.list)
            activeGroup = ranges.firstOrNull()
            return
        }
        groupScroll.visibility = View.VISIBLE
        val firstPlayable = data.list.firstOrNull { !it.disabled }?.index ?: 1
        val initial = EpisodeGroups.groupOf(firstPlayable, total) ?: ranges.first()
        activeGroup = initial
        ranges.forEach { range ->
            val chip = layoutInflater.inflate(R.layout.item_group_chip, groupsBox, false) as TextView
            chip.text = EpisodeGroups.label(range)
            chip.setOnClickListener { selectGroup(range, data.list) }
            groupsBox.addView(chip)
        }
        selectGroup(initial, data.list)
    }

    private fun selectGroup(range: IntRange, all: List<DirectClient.SeriesEpisode>) {
        activeGroup = range
        for (i in 0 until groupsBox.childCount) {
            val chip = groupsBox.getChildAt(i) as TextView
            val label = chip.text.toString()
            val selected = label == EpisodeGroups.label(range)
            chip.setBackgroundResource(if (selected) R.drawable.bg_chip_selected else R.drawable.bg_chip_normal)
            chip.setTextColor(if (selected) 0xFFFFFFFF.toInt() else resolvePrimaryColor())
        }
        episodeAdapter.submit(all.filter { it.index in range })
    }

    private fun resolvePrimaryColor(): Int {
        val typed = theme.obtainStyledAttributes(intArrayOf(android.R.attr.textColorPrimary))
        return try {
            typed.getColor(0, 0xFF1A1A1A.toInt())
        } finally {
            typed.recycle()
        }
    }

    private fun toggleIntro() {
        if (!introCollapsible) return
        introExpanded = !introExpanded
        if (introExpanded) {
            intro.maxLines = Int.MAX_VALUE
            intro.ellipsize = null
        } else {
            intro.maxLines = 8
            intro.ellipsize = android.text.TextUtils.TruncateAt.END
        }
        introToggle.text = if (introExpanded) "收起" else "展开"
    }

    /** 选集 / 相关 切换；相关页首次点击时才请求搜索接口。 */
    private fun showRelated(show: Boolean) {
        showingRelated = show
        SectionTabs.apply(if (show) tabRelated else tabEpisodes, if (show) tabEpisodes else tabRelated)
        groupScroll.visibility = if (show) View.GONE else View.VISIBLE
        episodeView.visibility = if (show) View.GONE else View.VISIBLE
        relatedList.visibility = if (show) View.VISIBLE else View.GONE
        if (!show) {
            relatedStatus.visibility = View.GONE
            return
        }
        if (relatedLoaded) return
        loadRelated()
    }

    private fun loadRelated() {
        val current = detail?.title ?: seriesTitle
        if (current.isBlank()) {
            relatedStatus.text = "暂无可关联的剧集"
            relatedStatus.visibility = View.VISIBLE
            return
        }
        relatedStatus.text = "加载中…"
        relatedStatus.visibility = View.VISIBLE
        relatedList.visibility = View.GONE
        Thread {
            val seasons = runCatching {
                val (deviceId, installId) = DirectClient.identity(this)
                val firstVid = detail?.list?.firstOrNull { !it.disabled }?.vid
                DirectClient.relatedSeasons(seriesId, firstVid, deviceId, installId, current, detail?.cover)
            }.getOrElse { emptyList() }
            runOnUiThread {
                if (isFinishing || isDestroyed) return@runOnUiThread
                relatedLoaded = true
                relatedAdapter.submit(seasons)
                relatedStatus.visibility = if (seasons.isEmpty()) View.VISIBLE else View.GONE
                if (seasons.isEmpty()) relatedStatus.text = "未找到相关剧集"
                relatedList.visibility = if (seasons.isEmpty()) View.GONE else View.VISIBLE
            }
        }.start()
    }

    /** 打开某一季的详情；当前这一季直接忽略。 */
    private fun openSeason(season: com.duanju.player.core.SeriesAggregator.Season) {
        if (season.id == seriesId) return
        if (!season.playable) {
            android.widget.Toast.makeText(this, "该季还未上线", android.widget.Toast.LENGTH_SHORT).show()
            return
        }
        startActivity(
            Intent(this, DetailActivity::class.java)
                .putExtra("dramaId", season.id)
                .putExtra("title", season.title)
                .putExtra("episodes", season.episodes)
        )
    }

    private fun play(episode: Int) {
        startActivity(
            Intent(this, PlayerActivity::class.java)
                .putExtra("dramaId", seriesId)
                .putExtra("title", detail?.title ?: seriesTitle)
                .putExtra("episodes", detail?.episodes ?: 0)
                .putExtra("startEpisode", episode)
                .putExtra("cover", detail?.cover.orEmpty())
        )
    }

    private fun resolveSecondaryColor(): Int {
        val typed = theme.obtainStyledAttributes(intArrayOf(android.R.attr.textColorSecondary))
        return try {
            typed.getColor(0, 0xFF606064.toInt())
        } finally {
            typed.recycle()
        }
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    override fun onDestroy() {
        super.onDestroy()
        executor.shutdownNow()
    }
}
