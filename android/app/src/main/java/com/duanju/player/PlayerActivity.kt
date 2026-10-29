package com.duanju.player

import android.content.pm.ActivityInfo
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.ImageButton
import androidx.appcompat.widget.AppCompatButton
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
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerControlView
import androidx.media3.ui.PlayerView
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import com.duanju.player.core.VideoCache
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
    private var activeGroup: IntRange? = null
    private lateinit var speedButton: AppCompatButton
    private lateinit var muteButton: ImageButton
    private lateinit var fullscreenButton: ImageButton
    private lateinit var header: View
    private lateinit var episodePanel: View
    private lateinit var root: View
    private lateinit var fullscreenTopBar: View
    private lateinit var fullscreenTitle: TextView
    private lateinit var topBar: TopBar
    private lateinit var tabEpisodes: TextView
    private lateinit var tabEpisodesIndicator: View
    private lateinit var tabRelated: TextView
    private lateinit var tabRelatedIndicator: View
    private lateinit var relatedList: RecyclerView
    private lateinit var relatedStatus: TextView
    private lateinit var relatedAdapter: RelatedAdapter
    private lateinit var episodeList: RecyclerView
    private var relatedLoaded = false
    private var showingRelated = false

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
    /** 播放代次：每次切集递增，旧集的异步回调据此丢弃。 */
    private var playGeneration = 0L
    /** 进度落库节流计数。 */
    private var saveTick = 0

    private var videoWidth = 0
    private var videoHeight = 0
    private var videoRotation = 0
    // 当前生效的画面宽高比（高/宽）。切集时先用该剧上次记录的比例占位，
    // 避免容器先按上一集的横屏比例布局、拿到真实尺寸后又跳一下（闪一下）。
    private var aspectRatio = 2f   // 未知时按竖屏短剧处理：高度取半屏（与重构前行为一致）
    private var aspectLayoutListener: View.OnLayoutChangeListener? = null
    private var initialPadding: IntArray? = null

    private val speeds = listOf(1f, 1.25f, 1.5f, 1.75f, 2f, 3f, 4f)

    private val executor = Executors.newSingleThreadExecutor()
    private val prefetchExecutor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    @Volatile private var prefetchTarget = -1
    @Volatile private var prefetchSessionId = 0L
    private var prefetchFuture: Future<*>? = null
    /** 当前播放器正在读取的缓存文件，LRU 清理时需跳过。 */
    @Volatile private var playingFile: File? = null
    /** 当前集对应的 vid，用于「相关」页拼剧集页 URL。 */
    @Volatile private var currentVideoId: String? = null

    // 状态行展示用的播放信息（集数进度之后的附加文案）。
    @Volatile private var mediaQuality = 0
    @Volatile private var playingBytes = 0L
    @Volatile private var prefetchTmp: File? = null
    @Volatile private var prefetchTotalBytes = 0L
    @Volatile private var prefetchDoneBytes = 0L
    @Volatile private var prefetchPercent = -1
    @Volatile private var prefetchReadyEpisode = -1
    @Volatile private var prefetchReadyBytes = 0L
    @Volatile private var prefetchQuality = 0
    @Volatile private var prefetchElapsedMs = 0L
    @Volatile private var prefetchFailedEpisode = -1

    /** 预取进度轮询：ffmpeg 写 .tmp 期间定期读文件大小算百分比。 */
    private val prefetchTicker = object : Runnable {
        override fun run() {
            if (prefetchTarget <= 0) return
            val tmp = prefetchTmp
            if (tmp != null && tmp.exists()) {
                prefetchDoneBytes = tmp.length()
                if (prefetchTotalBytes > 0L) {
                    prefetchPercent = (prefetchDoneBytes * 100 / prefetchTotalBytes).toInt().coerceIn(0, 99)
                }
            }
            renderStatus()
            main.postDelayed(this, 500)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        dramaId = intent.getStringExtra("dramaId") ?: ""
        title = intent.getStringExtra("title") ?: "短剧"
        cover = intent.getStringExtra("cover").orEmpty()
        episodes = intent.getIntExtra("episodes", 1).coerceAtLeast(1)
        // 同一部剧的各集分辨率/朝向基本一致：先用上次记住的比例占位，
        // 切集时容器高度就不会先按旧比例布局再跳到新比例（消除闪一下）。
        aspectRatio = rememberedAspectRatio() ?: aspectRatio

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

            // 首帧真正上屏后才收起 loading：切集时容器高度可能瞬间变化，
            // 若此时就把上一集的旧画面露出来，看起来就是"闪一下"。
            override fun onRenderedFirstFrame() {
                main.post { loading.visibility = View.GONE }
            }

            override fun onVideoSizeChanged(videoSize: VideoSize) {
                videoWidth = videoSize.width
                videoHeight = videoSize.height
                videoRotation = videoSize.unappliedRotationDegrees
                if (videoWidth > 0 && videoHeight > 0) {
                    aspectRatio = videoHeight.toFloat() / videoWidth
                    rememberAspectRatio(aspectRatio)
                }
                applyVideoAspect()
            }
        })

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
        initialPadding = intArrayOf(root.paddingLeft, root.paddingTop, root.paddingRight, root.paddingBottom)
        fullscreenTopBar = findViewById(R.id.fullscreen_top_bar)
        fullscreenTitle = findViewById(R.id.fullscreen_title)
        topBar = findViewById<TopBar>(R.id.player_top_bar).apply {
            setTitle(title)
            setOnBackClickListener { finish() }
        }
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
            muteButton.setImageResource(if (muted) R.drawable.ic_volume_off else R.drawable.ic_volume_on)
        }
        fullscreenButton.setOnClickListener { toggleFullscreen() }

        tabEpisodes = findViewById(R.id.tab_episodes)
        tabEpisodesIndicator = findViewById(R.id.tab_episodes_indicator)
        tabRelated = findViewById(R.id.tab_related)
        tabRelatedIndicator = findViewById(R.id.tab_related_indicator)
        relatedList = findViewById(R.id.related_list)
        relatedStatus = findViewById(R.id.related_status)
        relatedAdapter = RelatedAdapter(dramaId.substringAfter(':')) { season -> openSeason(season) }
        relatedList.layoutManager = androidx.recyclerview.widget.LinearLayoutManager(this)
        relatedList.adapter = relatedAdapter
        tabEpisodes.setOnClickListener { showRelated(false) }
        tabRelated.setOnClickListener { showRelated(true) }

        episodeList = findViewById(R.id.episode_list)
        val list = episodeList
        list.layoutManager = GridLayoutManager(this, 6)
        groupsBox = findViewById(R.id.episode_groups)
        groupScroll = findViewById(R.id.group_scroll)
        episodeAdapter = EpisodeAdapter { episode -> play(episode, 0L) }
        list.adapter = episodeAdapter
        renderGroups(selectedEpisode)
        startPositionTicker()
        root.post { applyVideoAspect() }

        play(selectedEpisode, pendingSeekMs)
    }

    override fun onPause() {
        super.onPause()
        saveProgress()
    }

    override fun onDestroy() {
        super.onDestroy()
        main.removeCallbacks(ticker)
        main.removeCallbacks(prefetchTicker)
        saveProgress()
        cancelPrefetch()
        executor.shutdownNow()
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
                else androidx.core.content.ContextCompat.getColor(this, R.color.app_on_surface_variant)
            )
        }
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

    private fun aspectPrefs() =
        getSharedPreferences("duanju_aspect", MODE_PRIVATE)

    /** 按剧（seriesId）记录上次已知的画面比例，返回 高/宽。 */
    private fun rememberedAspectRatio(): Float? {
        val prefs = aspectPrefs()
        val key = "ratio_" + dramaId.substringAfter(':')
        val v = prefs.getFloat(key, 0f)
        return if (v > 0.05f && v < 20f) v else null
    }

    private fun rememberAspectRatio(ratio: Float) {
        if (ratio <= 0.05f || ratio >= 20f) return
        aspectPrefs().edit()
            .putFloat("ratio_" + dramaId.substringAfter(':'), ratio)
            .apply()
    }

    /**
     * 同剧已缓存的分集（含预取好的下一集）可以直接读出真实宽高，
     * 这样切集时不必等本集下载完就能把容器定到正确高度。
     */
    private fun probeCachedAspect(seriesId: String): Float? {
        val dir = File(cacheDir, "direct")
        if (!dir.isDirectory) return null
        val files = dir.listFiles { f -> f.isFile && f.name.startsWith("${seriesId}_") && f.name.endsWith(".mp4") }
            ?: return null
        for (f in files.sortedByDescending { it.lastModified() }) {
            val size = com.duanju.player.core.DirectClient.probeSize(f) ?: continue
            if (size.first > 0 && size.second > 0) return size.second.toFloat() / size.first
        }
        return null
    }

    /**
     * 让视频容器贴合画面比例：横屏视频不再在上下留大块黑边，
     * 竖屏视频受屏高上限约束，尽量增大画面宽度。
     */
    private fun applyVideoAspect() {
        if (!::videoContainer.isInitialized) return
        val metrics = resources.displayMetrics
        val params = videoContainer.layoutParams
        val paramsHeight = if (fullscreen) {
            ViewGroup.LayoutParams.MATCH_PARENT
        } else {
            val ratio = aspectRatio
            // 竖屏剧：播放器窗口与下方内容各占一半，避免播放器吃掉整屏；
            // 横屏剧：按画面比例收紧，尽量不留黑边（通常比一半矮）。
            // 用容器实际宽度而非屏幕宽计算，否则比画面多出的宽度会造成上下 letterbox（浅色主题下是白边）。
            val containerWidth = videoContainer.width.takeIf { it > 0 }
                ?: playerView.width.takeIf { it > 0 }
                ?: metrics.widthPixels
            val rootHeight = if (::root.isInitialized && root.height > 0) root.height else metrics.heightPixels
            val barHeight = if (::topBar.isInitialized) topBar.height else 0
            val half = ((rootHeight - barHeight) * 0.5f).toInt().coerceAtLeast(1)
            val h = minOf(half, (containerWidth * ratio).toInt())
            h
        }
        // 高度没变时不要重复赋值：setLayoutParams 会触发 requestLayout，
        // 而 OnLayoutChangeListener 又会在布局后回调本方法，无条件赋值会形成死循环。
        if (params.height != paramsHeight) {
            params.height = paramsHeight
            videoContainer.layoutParams = params
        }
        // 竖屏视频在横向容器里 FIT 会左右留黑边（容器背景为黑色，与原生行为一致）；
        // 不能用 ZOOM：竖屏短剧在接近方形的容器里会被裁掉近一半画面高度。
        playerView.resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
    }

    private fun toggleFullscreen() {
        fullscreen = !fullscreen
        topBar.visibility = if (fullscreen) View.GONE else View.VISIBLE
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
            // 退出全屏时不能凭空加 padding：root 原本没有 padding（XML 未设置），
            // 加了之后播放器左右各缩进 22px，浅色主题下露出底色即“左右白边”。
            // 恢复进全屏前记录的真实 padding 与 inset 行为。
            val p = initialPadding
            if (p != null) {
                root.setPadding(p[0], p[1], p[2], p[3])
            } else {
                root.setPadding(0, 0, 0, 0)
            }
            WindowCompat.setDecorFitsSystemWindows(window, true)
            requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            WindowCompat.getInsetsController(window, window.decorView).show(WindowInsetsCompat.Type.systemBars())
        }
        fullscreenButton.setImageResource(if (fullscreen) R.drawable.ic_fullscreen_exit else R.drawable.ic_fullscreen_enter)
        applyVideoAspect()
        if (!fullscreen) {
            // 退出全屏后旋转/insets 恢复需要多轮布局才能稳定；只算一次拿到的 root.height
            // 仍是中间态（复现为播放器高度 1029/399px、画面两侧露底色）。改为每次布局
            // 都重算：高度与上次相同时不再触发 requestLayout，视图树稳定后自然收敛。
            // 注意：ViewTreeObserver 在布局合并时会换实例导致监听失效，
            // 必须挂在 View 自身的 OnLayoutChangeListener 上。
            if (aspectLayoutListener == null) {
                aspectLayoutListener = View.OnLayoutChangeListener { _, _, _, _, _, _, _, _, _ ->
                    if (!fullscreen && ::videoContainer.isInitialized) applyVideoAspect()
                }
                root.addOnLayoutChangeListener(aspectLayoutListener)
            }
        }
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
        val generation = ++playGeneration
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
        mediaQuality = 0
        playingBytes = 0L
        prefetchPercent = -1
        prefetchDoneBytes = 0L
        prefetchReadyEpisode = -1
        prefetchFailedEpisode = -1
        status("准备播放…")
        executor.execute {
            // 预取写的是 .tmp，完成才 rename；这里等它落盘后直接命中缓存。
            if (waiting != null) runCatching { waiting.get(60, TimeUnit.SECONDS) }
            try {
                playDirect(episode, startMs, generation)
            } catch (e: Exception) {
                main.post {
                    if (playGeneration != generation) return@post
                    loading.visibility = View.GONE
                    status("失败：${e.message}")
                }
            }
        }
    }


    /** 直连播放：手机直接调官方接口 + 本地 ffmpeg 解密，全程不经服务器。 */
    private fun playDirect(episode: Int, startMs: Long, generation: Long) {
        val (deviceId, installId) = com.duanju.player.core.DirectClient.identity(this)
        val seriesId = dramaId.substringAfter(':')
        // 先看同剧已缓存的分集（预取的下一集也在内），几毫秒就能拿到真实比例，
        // 不用等本集下载完成才修正高度。
        val cachedAspect = probeCachedAspect(seriesId)
        if (cachedAspect != null) {
            main.post {
                if (playGeneration != generation) return@post
                aspectRatio = cachedAspect
                applyVideoAspect()
            }
        }
        val start = System.currentTimeMillis()
        val chapters = com.duanju.player.core.DirectClient.chapters(seriesId, deviceId, installId)
        if (episode > chapters.size) throw com.duanju.player.core.DirectClient.DirectException("集数超出范围")
        val videoId = chapters[episode - 1]
        currentVideoId = videoId
        val media = com.duanju.player.core.DirectClient.media(videoId, deviceId, installId)
        val resolveMs = System.currentTimeMillis() - start
        main.post {
            if (playGeneration != generation) return@post
            status("已解析(${resolveMs}ms) ${media.quality}p，本地解密中…")
        }
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
        android.util.Log.d("PlayerActivity", "第${episode}集解析${resolveMs}ms 下载${dlMs}ms")
        // 解密产物就在本地：播放前先读真实宽高，让容器高度一次到位。
        // 否则要等 ExoPlayer 解出首帧回调 onVideoSizeChanged 才修正，切集时会闪一下。
        val probed = com.duanju.player.core.DirectClient.probeSize(outFile)
        if (probed != null && probed.first > 0 && probed.second > 0) {
            android.util.Log.d("PlayerActivity", "第${episode}集尺寸 ${probed.first}x${probed.second}")
        }
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
        val sizeBytes = outFile.length()
        main.post {
            if (playGeneration != generation) return@post
            playingFile = outFile
            mediaQuality = media.quality
            playingBytes = sizeBytes
            // 先按本集的真实尺寸定好容器高度，再挂播放器：首帧出现时布局已经是最终态。
            if (probed != null && probed.first > 0 && probed.second > 0) {
                aspectRatio = probed.second.toFloat() / probed.first
                rememberAspectRatio(aspectRatio)
                applyVideoAspect()
            }
            renderStatus()
            // loading 不在这里关：等 onRenderedFirstFrame，避免旧画面在新尺寸下闪一下。
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
        prefetchReadyEpisode = -1
        prefetchFailedEpisode = -1
        prefetchPercent = -1
        prefetchDoneBytes = 0L
        prefetchTotalBytes = 0L
        prefetchQuality = 0
        prefetchElapsedMs = 0L
        prefetchFuture = prefetchExecutor.submit {
            val startedAt = System.currentTimeMillis()
            try {
                val dir = File(cacheDir, "direct").apply { mkdirs() }
                val outFile = File(dir, "${seriesId}_$next.mp4")
                if (outFile.exists() && outFile.length() > 0L) {
                    prefetchReadyEpisode = next
                    prefetchReadyBytes = outFile.length()
                    prefetchElapsedMs = 0L
                    prefetchTarget = -1
                    main.post { renderStatus() }
                    return@submit
                }
                val (deviceId, installId) = DirectClient.identity(context)
                val chapters = DirectClient.chapters(seriesId, deviceId, installId)
                if (prefetchTarget != next || next > chapters.size) return@submit
                val media = DirectClient.media(chapters[next - 1], deviceId, installId)
                prefetchQuality = media.quality
                if (prefetchTarget != next) return@submit
                // 进度 = 已写入的 .tmp 大小 / 源文件 Content-Length；拿不到总长时只显示"下载中"。
                prefetchTotalBytes = DirectClient.contentLength(media.url) ?: 0L
                prefetchTmp = File(dir, "${seriesId}_$next.mp4.tmp")
                main.post { prefetchTicker.run() }
                DirectClient.decryptTo(media.url, media.keyHex, outFile) { id -> prefetchSessionId = id }
                val keep = buildSet {
                    add(outFile.name)
                    playingFile?.let { add(it.name) }
                }
                com.duanju.player.core.VideoCache.trim(
                    dir, com.duanju.player.core.VideoCache.mbToBytes(AppSettings.cacheLimitMb(context)), keep
                )
                if (prefetchTarget == next) {
                    prefetchReadyEpisode = next
                    prefetchReadyBytes = outFile.length()
                    prefetchElapsedMs = System.currentTimeMillis() - startedAt
                    prefetchTarget = -1
                    prefetchTmp = null
                }
                android.util.Log.d("PlayerActivity", "预取第${next}集完成")
            } catch (e: Exception) {
                if (prefetchTarget == next) {
                    prefetchFailedEpisode = next
                    prefetchTarget = -1
                }
                android.util.Log.d("PlayerActivity", "预取第${next}集结束: ${e.message}")
            } finally {
                main.post { renderStatus() }
            }
        }
    }

    private fun cancelPrefetch() {
        val id = prefetchSessionId
        if (id != 0L) runCatching { com.arthenica.ffmpegkit.FFmpegKit.cancel(id) }
        prefetchSessionId = 0L
        prefetchTarget = -1
        prefetchTmp = null
        prefetchPercent = -1
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
                cover = cover,
                durationMs = if (player.duration > 0) player.duration else 0L
            )
        )
    }

    /** 临时状态（准备播放/已解析/失败…）也只占第一行，第二行保持预取状态，高度恒定。 */
    private fun status(message: String) {
        statusView.text = message + "\n" + prefetchLine()
    }

    private fun qualitySuffix(): String = if (prefetchQuality > 0) " · ${prefetchQuality}p" else ""

    /** 预取耗时：不足 1 秒显示毫秒，其余保留一位小数秒。 */
    private fun formatElapsed(ms: Long): String = when {
        ms <= 0L -> "-"
        ms < 1000L -> "${ms}ms"
        else -> String.format(java.util.Locale.US, "%.1fs", ms / 1000.0)
    }

    /** 选集 / 相关 切换；相关页首次点击时才请求搜索接口。 */
    private fun showRelated(show: Boolean) {
        showingRelated = show
        if (show) {
            SectionTabs.apply(tabRelated, tabRelatedIndicator, tabEpisodes, tabEpisodesIndicator)
        } else {
            SectionTabs.apply(tabEpisodes, tabEpisodesIndicator, tabRelated, tabRelatedIndicator)
        }
        groupScroll.visibility = if (show) View.GONE else View.VISIBLE
        episodeList.visibility = if (show) View.GONE else View.VISIBLE
        relatedList.visibility = if (show) View.VISIBLE else View.GONE
        if (!show) {
            relatedStatus.visibility = View.GONE
            return
        }
        if (relatedLoaded) return
        loadRelated()
    }

    private fun loadRelated() {
        val current = title
        relatedStatus.text = "加载中…"
        relatedStatus.visibility = View.VISIBLE
        relatedList.visibility = View.GONE
        Thread {
            val seasons = runCatching {
                val (deviceId, installId) = DirectClient.identity(this)
                DirectClient.relatedSeasons(
                    dramaId.substringAfter(':'), currentVideoId, deviceId, installId, current,
                    cover.ifBlank { null }
                )
            }.getOrElse { emptyList() }
            runOnUiThread {
                if (isFinishing || isDestroyed) return@runOnUiThread
                relatedLoaded = true
                relatedAdapter.submit(seasons)
                relatedStatus.text = "未找到相关剧集"
                relatedStatus.visibility = if (seasons.isEmpty()) View.VISIBLE else View.GONE
                relatedList.visibility = if (seasons.isEmpty()) View.GONE else View.VISIBLE
            }
        }.start()
    }

    /** 点击「相关」里的某一季，进入它的详情页。 */
    private fun openSeason(season: com.duanju.player.core.SeriesAggregator.Season) {
        if (!season.playable) {
            android.widget.Toast.makeText(this, "该季还未上线", android.widget.Toast.LENGTH_SHORT).show()
            return
        }
        startActivity(
            android.content.Intent(this, DetailActivity::class.java)
                .putExtra("dramaId", season.id)
                .putExtra("title", season.title)
                .putExtra("episodes", season.episodes)
        )
    }

    /**
     * 状态行两行：第一行当前集进度/清晰度/大小，第二行下一集预取状态。
     * 预取信息只在开启预加载且存在下一集时出现。
     */
    private fun renderStatus() {
        val position = if (::player.isInitialized) player.currentPosition / 1000 else 0
        val duration = if (::player.isInitialized && player.duration > 0) player.duration / 1000 else 0
        // 第一行：集数 + 清晰度 + 已下载大小，播放进度放到最后。
        val text = StringBuilder("第${selectedEpisode}集")
        if (mediaQuality > 0) text.append(" · ${mediaQuality}p")
        if (playingBytes > 0L) text.append(" · ${VideoCache.formatSize(playingBytes)}")
        text.append(" · ${position}s / ${duration}s")
        text.append("\n").append(prefetchLine())
        statusView.text = text.toString()
    }

    /**
     * 状态行第二行（预取状态）。必须始终返回非空文本：
     * lineSpacingExtra 只在真的有两行时才计入高度，单行文本会让状态行矮一行间距（约 2dp），
     * 表现为切集时下方选集面板轻微跳动。
     */
    private fun prefetchLine(): String = when {
        !AppSettings.prefetchNext(this) -> "未开启预加载下一集"
        prefetchTarget > 0 && prefetchPercent >= 0 ->
            "下一集预取中" + qualitySuffix() + " · ${prefetchPercent}%"
        prefetchTarget > 0 -> "下一集预取中" + qualitySuffix() + "…"
        prefetchReadyEpisode > 0 ->
            "下一集已预载" + qualitySuffix() + " · ${VideoCache.formatSize(prefetchReadyBytes)}" +
                // 命中磁盘缓存时没有下载耗时，别显示"耗时-"
                if (prefetchElapsedMs > 0L) " · 耗时${formatElapsed(prefetchElapsedMs)}" else " · 已缓存"
        prefetchFailedEpisode > 0 -> "第${prefetchFailedEpisode}集预取失败"
        selectedEpisode >= episodes -> "已是最后一集"
        else -> "下一集预取待命中…"
    }

    private val ticker = object : Runnable {
        override fun run() {
            if (::player.isInitialized && player.isPlaying) {
                renderStatus()
                // 状态行每秒刷新，进度每 5 秒落库一次，避免每秒重写历史 JSON
                if (++saveTick % 5 == 0) saveProgress()
            }
            main.postDelayed(this, 1000)
        }
    }

    private fun startPositionTicker() {
        main.postDelayed(ticker, 1000)
    }
}