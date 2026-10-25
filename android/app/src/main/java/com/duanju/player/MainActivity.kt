package com.duanju.player

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.ProgressBar
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.media3.common.MediaItem
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.PlayerView
import org.json.JSONObject
import java.net.CookieHandler
import java.net.CookieManager
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

@UnstableApi
class MainActivity : AppCompatActivity() {

    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var baseField: EditText
    private lateinit var dramaField: EditText
    private lateinit var episodeField: EditText
    private lateinit var statusView: TextView
    private lateinit var progress: ProgressBar

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        CookieHandler.setDefault(CookieManager())
        setContentView(R.layout.activity_main)

        player = ExoPlayer.Builder(this).build()
        playerView = findViewById(R.id.player_view)
        playerView.player = player

        baseField = findViewById(R.id.base_url)
        dramaField = findViewById(R.id.drama_id)
        episodeField = findViewById(R.id.episode)
        statusView = findViewById(R.id.status)
        progress = findViewById(R.id.progress)
        findViewById<Button>(R.id.play_button).setOnClickListener { start() }
    }

    override fun onDestroy() {
        super.onDestroy()
        player.release()
    }

    private fun start() {
        val base = baseField.text.toString().trim().trimEnd('/')
        val drama = dramaField.text.toString().trim()
        val episode = episodeField.text.toString().trim().toIntOrNull()
        if (drama.isEmpty() || episode == null || episode < 1) {
            status("请输入剧集 ID 和集数")
            return
        }
        progress.visibility = View.VISIBLE
        status("准备播放（服务器解密封装中）…")
        executor.execute {
            try {
                val viewer = ensureViewer(base)
                val hls = requestHls(base, drama, episode, viewer)
                main.post { play(base, hls) }
            } catch (e: Exception) {
                main.post {
                    progress.visibility = View.GONE
                    status("失败：${e.message}")
                }
            }
        }
    }

    private fun ensureViewer(base: String): String {
        // 第一次请求仅设置身份 Cookie，第二次确认并返回 viewer id。
        httpGet("$base/api/ui/viewer")
        val body = httpGet("$base/api/ui/viewer?confirm=1")
        return JSONObject(body).getString("id")
    }

    private fun requestHls(base: String, drama: String, episode: Int, viewer: String): JSONObject {
        val payload = """{"dramaId":"$drama","episode":$episode}"""
        val body = httpPost("$base/api/ui/playback/native/hls", payload, mapOf("X-Duanju-Viewer" to viewer))
        return JSONObject(body)
    }

    private fun play(base: String, hls: JSONObject) {
        progress.visibility = View.GONE
        val relative = hls.getString("url")
        val url = if (relative.startsWith("http")) relative else base + relative
        val title = hls.optString("title", "")
        status("播放中 $title")
        val source = HlsMediaSource.Factory(DefaultHttpDataSource.Factory())
            .createMediaSource(MediaItem.fromUri(url))
        player.setMediaSource(source)
        player.prepare()
        player.playWhenReady = true
    }

    private fun status(message: String) {
        statusView.text = message
    }

    private fun httpGet(url: String): String {
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15000
            readTimeout = 30000
        }
        return readConnection(connection)
    }

    private fun httpPost(url: String, body: String, headers: Map<String, String>): String {
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 15000
            readTimeout = 180000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
            headers.forEach { (key, value) -> setRequestProperty(key, value) }
        }
        connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
        return readConnection(connection)
    }

    private fun readConnection(connection: HttpURLConnection): String {
        val code = connection.responseCode
        val stream = if (code in 200..299) connection.inputStream else connection.errorStream
        val text = stream?.bufferedReader()?.use { it.readText() } ?: ""
        connection.disconnect()
        if (code !in 200..299) {
            val message = runCatching {
                JSONObject(text).optString("error").ifBlank { text }
            }.getOrDefault(text)
            throw RuntimeException("HTTP $code：${message.ifBlank { "请求失败" }}")
        }
        return text
    }
}