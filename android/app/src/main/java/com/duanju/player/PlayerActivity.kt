package com.duanju.player

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.Button
import android.widget.NumberPicker
import android.widget.ProgressBar
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
import org.json.JSONObject
import java.util.concurrent.Executors

@UnstableApi
class PlayerActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var episodePicker: NumberPicker
    private lateinit var statusView: TextView
    private lateinit var progress: ProgressBar

    private var base = "http://192.168.1.2:3333"
    private var dramaId = ""
    private var title = ""
    private var episodes = 1

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        base = intent.getStringExtra("base") ?: base
        dramaId = intent.getStringExtra("dramaId") ?: ""
        title = intent.getStringExtra("title") ?: "短剧"
        episodes = intent.getIntExtra("episodes", 1).coerceAtLeast(1)

        setContentView(R.layout.activity_player)

        player = ExoPlayer.Builder(this).build()
        player.addListener(object : Player.Listener {
            override fun onPlayerError(error: PlaybackException) {
                main.post { status("播放错误：${error.message}") }
            }
        })

        val titleView = findViewById<TextView>(R.id.title)
        titleView.text = title
        playerView = findViewById(R.id.player_view)
        playerView.player = player

        episodePicker = findViewById(R.id.episode_picker)
        episodePicker.minValue = 1
        episodePicker.maxValue = episodes
        episodePicker.value = 1

        statusView = findViewById(R.id.status)
        progress = findViewById(R.id.progress)
        findViewById<Button>(R.id.play_button).setOnClickListener { play() }
        startPositionTicker()
    }

    override fun onDestroy() {
        super.onDestroy()
        player.release()
    }

    private fun play() {
        val episode = episodePicker.value
        progress.visibility = View.VISIBLE
        status("准备播放（服务器解密封装中）…")
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
        status("播放中 $title 第${episodePicker.value}集")
        val source = HlsMediaSource.Factory(DefaultHttpDataSource.Factory())
            .createMediaSource(MediaItem.fromUri(url))
        player.setMediaSource(source)
        player.prepare()
        player.playWhenReady = true
    }

    private fun status(message: String) {
        statusView.text = message
    }

    private val ticker = object : Runnable {
        override fun run() {
            if (::player.isInitialized && player.isPlaying) {
                val position = player.currentPosition / 1000
                val duration = if (player.duration > 0) player.duration / 1000 else 0
                status("播放中 第${episodePicker.value}集 ${position}s / ${duration}s")
            }
            main.postDelayed(this, 1000)
        }
    }

    private fun startPositionTicker() {
        main.postDelayed(ticker, 1000)
    }
}