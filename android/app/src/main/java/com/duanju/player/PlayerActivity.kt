package com.duanju.player

import android.content.pm.ActivityInfo
import android.os.Bundle
import android.os.Handler
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
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerControlView
import androidx.media3.ui.PlayerView
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors

@UnstableApi
class PlayerActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var statusView: TextView
    private lateinit var loading: ProgressBar
    private lateinit var episodeAdapter: EpisodeAdapter
    private lateinit var speedButton: Button
    private lateinit var muteButton: Button
    private lateinit var fullscreenButton: Button
    private lateinit var header: View
    private lateinit var episodePanel: View
    private lateinit var root: View
    private lateinit var fullscreenTopBar: View
    private lateinit var fullscreenTitle: TextView

    private var base = "http://192.168.1.2:3333"
    private var dramaId = ""
    private var title = ""
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
        base = intent.getStringExtra("base") ?: base
        dramaId = intent.getStringExtra("dramaId") ?: ""
        title = intent.getStringExtra("title") ?: "短剧"
        episodes = intent.getIntExtra("episodes", 1).coerceAtLeast(1)

        autoNext = AppSettings.autoNext(this)
        val history = HistoryStore.find(this, dramaId)
        val savedEpisode = history?.episode ?: -1
        val savedPosition = history?.positionMs ?: 0L
        selectedEpisode = if (savedEpisode in 1..episodes) savedEpisode else 1
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

        speedButton.setOnClickListener { cycleSpeed() }
        muteButton.setOnClickListener {
            muted = !muted
            player.volume = if (muted) 0f else 1f
            muteButton.text = if (muted) "已静音" else "静音"
        }
        fullscreenButton.setOnClickListener { toggleFullscreen() }

        val list = findViewById<RecyclerView>(R.id.episode_list)
        list.layoutManager = GridLayoutManager(this, 6)
        episodeAdapter = EpisodeAdapter(episodes) { episode -> play(episode, 0L) }
        list.adapter = episodeAdapter
        episodeAdapter.setSelected(selectedEpisode)
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
        player.release()
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
        pendingSeekMs = startMs
        loading.visibility = View.VISIBLE
        status("准备播放…")
        executor.execute {
            try {
                val headers = Network.viewerHeaders(base)
                val payload = """{"dramaId":"$dramaId","episode":$episode}"""
                val body = Network.postJson("$base/api/ui/playback/native/hls", payload, headers, readTimeoutMs = 180000)
                val hls = JSONObject(body)
                main.post { playHls(hls) }
            } catch (e: Exception) {
                main.post {
                    loading.visibility = View.GONE
                    status("失败：${e.message}")
                }
            }
        }
    }

    private fun playHls(hls: JSONObject) {
        loading.visibility = View.GONE
        val relative = hls.getString("url")
        val url = if (relative.startsWith("http")) relative else base + relative
        val source = HlsMediaSource.Factory(DefaultHttpDataSource.Factory())
            .createMediaSource(MediaItem.fromUri(url))
        player.setMediaSource(source)
        player.prepare()
        player.playWhenReady = true
        player.setPlaybackSpeed(currentSpeed)
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
                updatedAt = System.currentTimeMillis()
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