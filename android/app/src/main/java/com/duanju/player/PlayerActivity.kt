package com.duanju.player

import android.content.pm.ActivityInfo
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.view.ViewGroup
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
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerControlView
import androidx.media3.ui.PlayerView
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit

@UnstableApi
class PlayerActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var videoContainer: View
    private lateinit var statusView: TextView
    private lateinit var loading: ProgressBar
    private lateinit var episodeAdapter: EpisodeAdapter
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
    private val prefetchExecutor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    @Volatile private var prefetchTarget = -1
    @Volatile private var prefetchSessionId = 0L
    private var prefetchFuture: Future<*>? = null
    /** 当前播放器正在读取的缓存文件，LRU 清理时需跳过。 */
    @Volatile private var playingFile: File? = null

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
                applyVideoAspect()
            }
        })

        findViewById<TextView>(R.id.title).text = title
        playerView = findViewById(R.id.player_view)
        playerView.resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
        playerView.player = player
        // 播放、切集都不主动弹出控制器，只有触摸播放器区域时显示。
        playerView.controllerAutoShow = false
        videoContainer = findViewById(R.id.video_container)
        applyVideoAspect()
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
        cancelPrefetch()
        prefetchExecutor.shutdownNow()
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

    /**
     * 让视频容器贴合画面比例：横屏视频不再在上下留大块黑边，
     * 竖屏视频受屏高上限约束，尽量增大画面宽度。
     */
    private fun applyVideoAspect() {
        if (!::videoContainer.isInitialized) return
        val metrics = resources.displayMetrics
        val params = videoContainer.layoutParams
        params.height = if (fullscreen) {
            ViewGroup.LayoutParams.MATCH_PARENT
        } else {
            val ratio = if (videoWidth > 0 && videoHeight > 0) {
                videoHeight.toFloat() / videoWidth
            } else {
                9f / 16f
            }
            val maxHeight = (metrics.heightPixels * 0.60f).toInt()
            minOf(maxHeight, (metrics.widthPixels * ratio).toInt())
        }
        videoContainer.layoutParams = params
    }

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
        applyVideoAspect()
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
        playerView.hideController()
        // 要切的正是预取中的那一集：保留任务并等它完成；否则取消无用的预取。
        val waiting = if (prefetchTarget == episode) prefetchFuture else null
        if (prefetchTarget != -1 && prefetchTarget != episode) cancelPrefetch()
        updateFullscreenEpisode()
        episodeAdapter.setSelected(episode)
        syncGroup(episode)
        updateEpisodeButtons()
        pendingSeekMs = startMs
        loading.visibility = View.VISIBLE
        status("准备播放…")
        executor.execute {
            // 预取写的是 .tmp，完成才 rename；这里等它落盘后直接命中缓存。
            if (waiting != null) runCatching { waiting.get(60, TimeUnit.SECONDS) }
            try {
                playDirect(episode, startMs)
            } catch (e: Exception) {
                main.post {
                    loading.visibility = View.GONE
                    status("失败：${e.message}")
                }
            }
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
            android.util.Log.d("PlayerActivity", "第${episode}集下载完成 ${System.currentTimeMillis() - dlStart}ms")
        } else {
            android.util.Log.d("PlayerActivity", "第${episode}集命中缓存 ${outFile.length()}B，跳过下载")
        }
        val dlMs = System.currentTimeMillis() - dlStart
        // 播放缓存按 LRU 限制总量；正在播放的上一集与当前集不能删，否则解码会读不到文件。
        val keep = buildSet {
            add(outFile.name)
            playingFile?.let { add(it.name) }
        }
        val freed = com.duanju.player.core.VideoCache.trim(
            cacheDir, com.duanju.player.core.VideoCache.mbToBytes(AppSettings.cacheLimitMb(this)), keep
        )
        if (freed > 0L) {
            android.util.Log.d("PlayerActivity", "缓存超限，清理 ${com.duanju.player.core.VideoCache.format(freed)}")
        }
        com.duanju.player.core.VideoCache.touch(outFile)
        main.post {
            playingFile = outFile
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
            startPrefetch(episode)
        }
    }

    /** 后台预取下一集到播放缓存，切集时可直接命中，减少等待。 */
    private fun startPrefetch(episode: Int) {
        if (!AppSettings.prefetchNext(this)) return
        val next = episode + 1
        if (next > episodes) return
        if (prefetchTarget == next) return
        cancelPrefetch()
        val seriesId = dramaId.substringAfter(':')
        val context = applicationContext
        prefetchTarget = next
        prefetchFuture = prefetchExecutor.submit {
            try {
                val dir = File(cacheDir, "direct").apply { mkdirs() }
                val outFile = File(dir, "${seriesId}_$next.mp4")
                if (outFile.exists() && outFile.length() > 0L) return@submit
                val (deviceId, installId) = DirectClient.identity(context)
                val chapters = DirectClient.chapters(seriesId, deviceId, installId)
                if (prefetchTarget != next || next > chapters.size) return@submit
                val media = DirectClient.media(chapters[next - 1], deviceId, installId)
                if (prefetchTarget != next) return@submit
                DirectClient.decryptTo(media.url, media.keyHex, outFile) { id -> prefetchSessionId = id }
                val keep = buildSet {
                    add(outFile.name)
                    playingFile?.let { add(it.name) }
                }
                com.duanju.player.core.VideoCache.trim(
                    dir, com.duanju.player.core.VideoCache.mbToBytes(AppSettings.cacheLimitMb(context)), keep
                )
                android.util.Log.d("PlayerActivity", "预取第${next}集完成")
            } catch (e: Exception) {
                android.util.Log.d("PlayerActivity", "预取第${next}集结束: ${e.message}")
            }
        }
    }

    private fun cancelPrefetch() {
        val id = prefetchSessionId
        if (id != 0L) runCatching { com.arthenica.ffmpegkit.FFmpegKit.cancel(id) }
        prefetchSessionId = 0L
        prefetchTarget = -1
        prefetchFuture?.cancel(false)
        prefetchFuture = null
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