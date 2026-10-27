package com.duanju.player

import android.content.pm.ActivityInfo
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.util.Log
import android.os.Looper
import android.view.View
import android.view.WindowManager
import android.widget.Button
import android.widget.ProgressBar
import android.widget.Switch
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import androidx.media3.extractor.ExtractorsFactory
import androidx.media3.extractor.mp4.FragmentedMp4Extractor
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerControlView
import androidx.media3.ui.PlayerView
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import com.duanju.player.core.Fmp4Streamer
import com.duanju.player.core.HlsStreamer
import com.duanju.player.core.StreamDataSource
import java.io.File
import java.util.concurrent.Executors

@UnstableApi
class PlayerActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var statusView: TextView
    private lateinit var loading: ProgressBar
    private lateinit var episodeAdapter: EpisodeAdapter
    private var streamer: Fmp4Streamer? = null
    private var hls: HlsStreamer? = null
    private lateinit var groupsBox: android.widget.LinearLayout
    private lateinit var groupScroll: View
    private lateinit var groupLabel: TextView
    private var activeGroup: IntRange? = null
    private lateinit var speedButton: Button
    private lateinit var muteButton: Button
    private lateinit var fullscreenButton: Button
    private lateinit var header: View
    private lateinit var episodePanel: View
    private lateinit var root: View
    private lateinit var fullscreenTopBar: View
    private lateinit var fullscreenTitle: TextView

    private var dramaId = ""
    private var title = ""
    private var cover = ""
    private var episodes = 1
    private var autoNext = true
    private var selectedEpisode = 1
    private var pendingSeekMs = 0L
    private var currentSpeed = 1f
    private var muted = false
    private var fullscreen = false

    private var videoWidth = 0
    private var videoHeight = 0
    private var videoRotation = 0

    private val speeds = listOf(1f, 1.25f, 1.5f, 1.75f, 2f, 3f, 4f)

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        dramaId = intent.getStringExtra("dramaId") ?: ""
        title = intent.getStringExtra("title") ?: "短剧"
        cover = intent.getStringExtra("cover").orEmpty()
        episodes = intent.getIntExtra("episodes", 1).coerceAtLeast(1)

        autoNext = AppSettings.autoNext(this)
        val history = HistoryStore.find(this, dramaId)
        val savedEpisode = history?.episode ?: -1
        val savedPosition = history?.positionMs ?: 0L
        val startEpisode = intent.getIntExtra("startEpisode", 0)
        selectedEpisode = when {
            startEpisode in 1..episodes -> startEpisode
            savedEpisode in 1..episodes -> savedEpisode
            else -> 1
        }
        pendingSeekMs = if (selectedEpisode == savedEpisode) savedPosition else 0L
        currentSpeed = AppSettings.speed(this)

        setContentView(R.layout.activity_player)

        player = ExoPlayer.Builder(this).build()
        player.setPlaybackSpeed(currentSpeed)
        player.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                when (playbackState) {
                    Player.STATE_READY -> {
                        if (pendingSeekMs > 0) {
                            player.seekTo(pendingSeekMs)
                            pendingSeekMs = 0
                        }
                        if (fullscreen) playerView.hideController()
                    }
                    Player.STATE_ENDED -> main.post { onEpisodeEnded() }
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                main.post { status("播放错误：${error.message}") }
            }

            override fun onVideoSizeChanged(videoSize: VideoSize) {
                videoWidth = videoSize.width
                videoHeight = videoSize.height
                videoRotation = videoSize.unappliedRotationDegrees
            }
        })

        findViewById<TextView>(R.id.title).text = title
        playerView = findViewById(R.id.player_view)
        playerView.resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
        playerView.player = player
        header = findViewById(R.id.header)
        episodePanel = findViewById(R.id.episode_panel)
        root = findViewById(R.id.root)
        fullscreenTopBar = findViewById(R.id.fullscreen_top_bar)
        fullscreenTitle = findViewById(R.id.fullscreen_title)
        updateFullscreenEpisode()
        findViewById<View>(R.id.fullscreen_back).setOnClickListener { toggleFullscreen() }
        playerView.setControllerVisibilityListener(object : PlayerControlView.VisibilityListener {
            override fun onVisibilityChange(visibility: Int) {
                if (fullscreen) fullscreenTopBar.visibility = visibility
            }
        })

statusView = findViewById(R.id.status)
        loading = findViewById(R.id.loading)
        speedButton = playerView.findViewById(R.id.custom_speed)
        muteButton = playerView.findViewById(R.id.custom_mute)
        fullscreenButton = playerView.findViewById(R.id.custom_fullscreen)
        speedButton.text = speedLabel(currentSpeed)
        findViewById<Switch>(R.id.auto_next).apply {
            isChecked = autoNext
            setOnCheckedChangeListener { _, checked ->
                autoNext = checked
                AppSettings.setAutoNext(this@PlayerActivity, checked)
            }
        }

        applyControlDispatcher()
        updateEpisodeButtons()
        speedButton.setOnClickListener { cycleSpeed() }
        muteButton.setOnClickListener {
            muted = !muted
            player.volume = if (muted) 0f else 1f
            muteButton.text = if (muted) "已静音" else "静音"
        }
        fullscreenButton.setOnClickListener { toggleFullscreen() }

        val list = findViewById<RecyclerView>(R.id.episode_list)
        list.layoutManager = GridLayoutManager(this, 6)
        groupsBox = findViewById(R.id.episode_groups)
        groupScroll = findViewById(R.id.group_scroll)
        groupLabel = findViewById(R.id.episode_group_label)
        episodeAdapter = EpisodeAdapter { episode -> play(episode, 0L) }
        list.adapter = episodeAdapter
        renderGroups(selectedEpisode)
        startPositionTicker()

        play(selectedEpisode, pendingSeekMs)
    }

    override fun onPause() {
        super.onPause()
        saveProgress()
    }

    override fun onDestroy() {
        super.onDestroy()
        saveProgress()
        streamer?.cancel()
        hls?.cancel()
        player.release()
    }

    /** 选集按 30 集分组；切换剧集时自动跳到所在分组。 */
    private fun renderGroups(selected: Int) {
        val ranges = EpisodeGroups.ranges(episodes)
        groupsBox.removeAllViews()
        if (ranges.isEmpty()) {
            groupScroll.visibility = View.GONE
            return
        }
        if (ranges.size <= 1) {
            groupScroll.visibility = View.GONE
            activeGroup = ranges.first()
            groupLabel.text = "共 ${episodes} 集"
            episodeAdapter.submit((1..episodes).toList(), selected)
            return
        }
        ranges.forEach { range ->
            val chip = layoutInflater.inflate(R.layout.item_group_chip, groupsBox, false) as TextView
            chip.text = EpisodeGroups.label(range)
            chip.setOnClickListener { selectGroup(range, selected) }
            groupsBox.addView(chip)
        }
        selectGroup(EpisodeGroups.groupOf(selected, episodes) ?: ranges.first(), selected)
    }

    private fun selectGroup(range: IntRange, selected: Int) {
        activeGroup = range
        for (i in 0 until groupsBox.childCount) {
            val chip = groupsBox.getChildAt(i) as TextView
            val isSelected = chip.text.toString() == EpisodeGroups.label(range)
            chip.setBackgroundResource(if (isSelected) R.drawable.bg_chip_selected else R.drawable.bg_chip_normal)
            chip.setTextColor(
                if (isSelected) 0xFFFFFFFF.toInt()
                else {
                    val typed = theme.obtainStyledAttributes(intArrayOf(android.R.attr.textColorPrimary))
                    val color = try { typed.getColor(0, 0xFF1A1A1A.toInt()) } finally { typed.recycle() }
                    color
                }
            )
        }
        groupLabel.text = EpisodeGroups.label(range)
        episodeAdapter.submit(range.toList(), selected)
    }

    private fun syncGroup(episode: Int) {
        if (episodes <= EpisodeGroups.SIZE) return
        val group = EpisodeGroups.groupOf(episode, episodes) ?: return
        val selectedChip = activeGroup
        if (selectedChip == group) {
            episodeAdapter.setSelected(episode)
            return
        }
        selectGroup(group, episode)
        // 让当前分组 chip 滚入可视区
        val index = groupsBox.indexOfChild(
            (0 until groupsBox.childCount).map { groupsBox.getChildAt(it) }
                .firstOrNull { (it as TextView).text.toString() == EpisodeGroups.label(group) }
        )
        if (index >= 0) groupScroll.post { (groupScroll as android.widget.HorizontalScrollView).smoothScrollTo(groupsBox.getChildAt(index).left, 0) }
    }

    /**
     * 自定义控制器按钮：快退/快进 15 秒，上一集/下一集切换剧集。
     * 不用 exo_prev/exo_next 原生按钮——PlayerView 每次刷新控制器都会重建它们的点击监听，
     * 直接 setOnClickListener 会被覆盖（这就是“下一集点不动”的原因）。
     */
    private fun applyControlDispatcher() {
        playerView.findViewById<View>(R.id.custom_rewind)?.setOnClickListener {
            player.seekTo((player.currentPosition - 15000).coerceAtLeast(0))
        }
        playerView.findViewById<View>(R.id.custom_forward)?.setOnClickListener {
            val duration = if (player.duration > 0) player.duration else Long.MAX_VALUE
            player.seekTo((player.currentPosition + 15000).coerceAtMost(duration))
        }
        playerView.findViewById<View>(R.id.custom_prev)?.setOnClickListener {
            if (selectedEpisode > 1) play(selectedEpisode - 1, 0L)
        }
        playerView.findViewById<View>(R.id.custom_next)?.setOnClickListener {
            if (selectedEpisode < episodes) play(selectedEpisode + 1, 0L)
        }
    }

    /** 首集/末集时置灰上一集/下一集按钮。 */
    private fun updateEpisodeButtons() {
        if (!::playerView.isInitialized) return
        playerView.findViewById<View>(R.id.custom_prev)?.alpha = if (selectedEpisode > 1) 1f else 0.35f
        playerView.findViewById<View>(R.id.custom_next)?.alpha = if (selectedEpisode < episodes) 1f else 0.35f
    }

    private fun cycleSpeed() {
        val next = (speeds.indexOfFirst { it == currentSpeed } + 1) % speeds.size
        currentSpeed = speeds[next]
        player.setPlaybackSpeed(currentSpeed)
        speedButton.text = speedLabel(currentSpeed)
        AppSettings.setSpeed(this, currentSpeed)
    }

    private fun speedLabel(value: Float): String =
        if (value == value.toInt().toFloat()) "${value.toInt()}×" else "${value}×"

    private fun toggleFullscreen() {
        fullscreen = !fullscreen
        playerView.resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
        header.visibility = if (fullscreen) View.GONE else View.VISIBLE
        episodePanel.visibility = if (fullscreen) View.GONE else View.VISIBLE
        if (fullscreen) {
            fullscreenTopBar.visibility = View.VISIBLE
        } else {
            fullscreenTopBar.visibility = View.GONE
        }
        if (fullscreen) {
            root.setPadding(0, 0, 0, 0)
            WindowCompat.setDecorFitsSystemWindows(window, false)
            requestedOrientation = if (isLandscapeVideo()) {
                ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            } else {
                ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            }
            WindowCompat.getInsetsController(window, window.decorView).apply {
                hide(WindowInsetsCompat.Type.systemBars())
                systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            }
        } else {
            val padding = (8 * resources.displayMetrics.density).toInt()
            root.setPadding(padding, padding, padding, padding)
            WindowCompat.setDecorFitsSystemWindows(window, true)
            requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            WindowCompat.getInsetsController(window, window.decorView).show(WindowInsetsCompat.Type.systemBars())
        }
        fullscreenButton.text = if (fullscreen) "退出全屏" else "全屏"
    }

    private fun isLandscapeVideo(): Boolean {
        var width = videoWidth
        var height = videoHeight
        var rotation = videoRotation
        if (width <= 0 || height <= 0) {
            val size = player.videoSize
            width = size.width
            height = size.height
            rotation = size.unappliedRotationDegrees
        }
        if (rotation % 180 != 0) {
            val swap = width
            width = height
            height = swap
        }
        return width > height
    }

    private fun updateFullscreenEpisode() {
        if (::fullscreenTitle.isInitialized) {
            fullscreenTitle.text = "$title - 第${selectedEpisode}集"
        }
    }

    

    private fun play(episode: Int, startMs: Long) {
        selectedEpisode = episode
        updateFullscreenEpisode()
        episodeAdapter.setSelected(episode)
        syncGroup(episode)
        updateEpisodeButtons()
        pendingSeekMs = startMs
        loading.visibility = View.VISIBLE
        status("准备播放…")
        executor.execute {
            streamer?.cancel()
            hls?.cancel()
            try {
                playHls(episode, startMs)
            } catch (e: Exception) {
                main.post { status("HLS 流式失败，回退整集解密：${e.message}") }
                try {
                    playDirect(episode, startMs)
                } catch (fallback: Exception) {
                    main.post {
                        loading.visibility = View.GONE
                        status("失败：${fallback.message}")
                    }
                }
            }
        }
    }

    /**
     * 方案二：ffmpeg 输出 HLS 分片，本地 HTTP 服务交给 ExoPlayer。
     * 首片就绪即起播，且保留分片时长与 seek 能力。
     */
    private fun playHls(episode: Int, startMs: Long) {
        val (deviceId, installId) = DirectClient.identity(this)
        val seriesId = dramaId.substringAfter(':')
        val started = System.currentTimeMillis()
        val chapters = DirectClient.chapters(seriesId, deviceId, installId)
        if (episode > chapters.size) throw DirectClient.DirectException("集数超出范围")
        val media = DirectClient.media(chapters[episode - 1], deviceId, installId)
        val resolveMs = System.currentTimeMillis() - started
        val ca = DirectClient.exportSystemCa(File(cacheDir, "system-ca.pem"))
        val baseDir = File(cacheDir, "hls").apply { mkdirs() }
        val current = HlsStreamer(media.url, media.keyHex, ca, startMs / 1000.0, baseDir)
        hls = current
        current.start()
        if (!current.awaitReady(15_000)) {
            current.cancel()
            throw DirectClient.DirectException(current.failure ?: "HLS 播放清单未就绪")
        }
        Log.d("PlayerActivity", "HLS 就绪 " + current.playlistUrl + " 解析=${resolveMs}ms")
        main.post {
            loading.visibility = View.GONE
            status("第${selectedEpisode}集 ${media.quality}p · HLS 流式解析${resolveMs}ms")
            val source = androidx.media3.exoplayer.hls.HlsMediaSource.Factory(
                DefaultHttpDataSource.Factory()
            ).createMediaSource(MediaItem.fromUri(current.playlistUrl))
            player.setMediaSource(source)
            player.prepare()
            player.playWhenReady = true
            player.setPlaybackSpeed(currentSpeed)
        }
    }

    /**
     * 流式播放：ffmpeg 解密并转封装为 fMP4，经本地 TCP 交给 ExoPlayer，
     * 首片到达即起播；续播位置通过 ffmpeg 输入侧 -ss 完成。
     */
    private fun playStream(episode: Int, startMs: Long) {
        val (deviceId, installId) = DirectClient.identity(this)
        val seriesId = dramaId.substringAfter(':')
        val started = System.currentTimeMillis()
        val chapters = DirectClient.chapters(seriesId, deviceId, installId)
        if (episode > chapters.size) throw DirectClient.DirectException("集数超出范围")
        val media = DirectClient.media(chapters[episode - 1], deviceId, installId)
        val resolveMs = System.currentTimeMillis() - started
        val ca = DirectClient.exportSystemCa(File(cacheDir, "system-ca.pem"))
        val current = Fmp4Streamer(media.url, media.keyHex, ca, startMs / 1000.0)
        streamer = current
        current.start()
        if (!current.awaitClient(15_000)) {
            current.cancel()
            throw DirectClient.DirectException(current.failure ?: "ffmpeg 未能建立本地流")
        }
        android.util.Log.d("PlayerActivity", "流式连接已建立 port=" + current.port)
        main.post {
            loading.visibility = View.GONE
            status("第${selectedEpisode}集 ${media.quality}p · 流式解析${resolveMs}ms")
            val factory = DataSource.Factory { StreamDataSource(current) }
            val extractors = ExtractorsFactory { arrayOf(FragmentedMp4Extractor()) }
            val source = ProgressiveMediaSource.Factory(factory, extractors)
                .createMediaSource(MediaItem.fromUri(Uri.parse("duanju-stream://$seriesId/$episode")))
            player.setMediaSource(source)
            player.prepare()
            player.playWhenReady = true
            player.setPlaybackSpeed(currentSpeed)
        }
    }


    /** 直连播放：手机直接调官方接口 + 本地 ffmpeg 解密，全程不经服务器。 */
    private fun playDirect(episode: Int, startMs: Long) {
        val (deviceId, installId) = com.duanju.player.core.DirectClient.identity(this)
        val seriesId = dramaId.substringAfter(':')
        val start = System.currentTimeMillis()
        val chapters = com.duanju.player.core.DirectClient.chapters(seriesId, deviceId, installId)
        if (episode > chapters.size) throw com.duanju.player.core.DirectClient.DirectException("集数超出范围")
        val videoId = chapters[episode - 1]
        val media = com.duanju.player.core.DirectClient.media(videoId, deviceId, installId)
        val resolveMs = System.currentTimeMillis() - start
        main.post { status("已解析(${resolveMs}ms) ${media.quality}p，本地解密中…") }
        val cacheDir = File(cacheDir, "direct").apply { mkdirs() }
        com.duanju.player.core.DirectClient.cleanPartial(cacheDir)
        val outFile = File(cacheDir, "${seriesId}_$episode.mp4")
        val dlStart = System.currentTimeMillis()
        if (!outFile.exists() || outFile.length() == 0L) {
            com.duanju.player.core.DirectClient.decryptTo(media.url, media.keyHex, outFile)
        }
        val dlMs = System.currentTimeMillis() - dlStart
        main.post {
            loading.visibility = View.GONE
            status("第${selectedEpisode}集 ${media.quality}p · 解析${resolveMs}ms 下载${dlMs}ms")
            val source = androidx.media3.exoplayer.source.ProgressiveMediaSource.Factory(
                androidx.media3.datasource.DefaultDataSource.Factory(this)
            ).createMediaSource(androidx.media3.common.MediaItem.fromUri(android.net.Uri.fromFile(outFile)))
            pendingSeekMs = startMs
            player.setMediaSource(source)
            player.prepare()
            player.playWhenReady = true
            player.setPlaybackSpeed(currentSpeed)
        }
    }

    private fun onEpisodeEnded() {
        if (!autoNext) {
            status("本集播放完毕，已暂停自动连播")
            return
        }
        if (selectedEpisode < episodes) {
            play(selectedEpisode + 1, 0L)
        } else {
            status("已播放至最后一集")
        }
    }

    private fun saveProgress() {
        if (!::player.isInitialized) return
        val position = player.currentPosition.coerceAtLeast(0)
        HistoryStore.upsert(
            this,
            HistoryEntry(
                id = dramaId,
                title = title,
                episodes = episodes,
                episode = selectedEpisode,
                positionMs = position,
                updatedAt = System.currentTimeMillis(),
                cover = cover
            )
        )
    }

    private fun status(message: String) {
        statusView.text = message
    }

    private val ticker = object : Runnable {
        override fun run() {
            if (::player.isInitialized && player.isPlaying) {
                val position = player.currentPosition / 1000
                val duration = if (player.duration > 0) player.duration / 1000 else 0
                status("第${selectedEpisode}集 ${position}s / ${duration}s")
                saveProgress()
            }
            main.postDelayed(this, 1000)
        }
    }

    private fun startPositionTicker() {
        main.postDelayed(ticker, 1000)
    }
}