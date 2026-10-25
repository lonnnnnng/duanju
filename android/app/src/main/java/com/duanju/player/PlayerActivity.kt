package com.duanju.player

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.ProgressBar
import android.widget.Switch
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.PlayerView
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import org.json.JSONObject
import java.util.concurrent.Executors

@UnstableApi
class PlayerActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var statusView: TextView
    private lateinit var progress: ProgressBar
    private lateinit var episodeAdapter: EpisodeAdapter

    private var base = "http://192.168.1.2:3333"
    private var dramaId = ""
    private var title = ""
    private var episodes = 1
    private var autoNext = true
    private var selectedEpisode = 1
    private var pendingSeekMs = 0L

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        base = intent.getStringExtra("base") ?: base
        dramaId = intent.getStringExtra("dramaId") ?: ""
        title = intent.getStringExtra("title") ?: "短剧"
        episodes = intent.getIntExtra("episodes", 1).coerceAtLeast(1)

        val prefs = getSharedPreferences("duanju", MODE_PRIVATE)
        val savedEpisode = prefs.getInt("ep_$dramaId", -1)
        val savedPosition = prefs.getLong("pos_$dramaId", 0L)
        selectedEpisode = if (savedEpisode in 1..episodes) savedEpisode else 1
        pendingSeekMs = if (selectedEpisode == savedEpisode) savedPosition else 0L

        setContentView(R.layout.activity_player)

        player = ExoPlayer.Builder(this).build()
        player.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                when (playbackState) {
                    Player.STATE_READY -> {
                        if (pendingSeekMs > 0) {
                            player.seekTo(pendingSeekMs)
                            pendingSeekMs = 0
                        }
                    }
                    Player.STATE_ENDED -> main.post { onEpisodeEnded() }
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                main.post { status("播放错误：${error.message}") }
            }
        })

        findViewById<TextView>(R.id.title).text = title
        playerView = findViewById(R.id.player_view)
        playerView.player = player

        statusView = findViewById(R.id.status)
        progress = findViewById(R.id.progress)
        findViewById<Switch>(R.id.auto_next).setOnCheckedChangeListener { _, checked -> autoNext = checked }

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

    private fun play(episode: Int, startMs: Long) {
        selectedEpisode = episode
        episodeAdapter.setSelected(episode)
        pendingSeekMs = startMs
        progress.visibility = View.VISIBLE
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
                    progress.visibility = View.GONE
                    status("失败：${e.message}")
                }
            }
        }
    }

    private fun playHls(hls: JSONObject) {
        progress.visibility = View.GONE
        val relative = hls.getString("url")
        val url = if (relative.startsWith("http")) relative else base + relative
        val source = HlsMediaSource.Factory(DefaultHttpDataSource.Factory())
            .createMediaSource(MediaItem.fromUri(url))
        player.setMediaSource(source)
        player.prepare()
        player.playWhenReady = true
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
        getSharedPreferences("duanju", MODE_PRIVATE)
            .edit()
            .putInt("ep_$dramaId", selectedEpisode)
            .putLong("pos_$dramaId", position)
            .apply()
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