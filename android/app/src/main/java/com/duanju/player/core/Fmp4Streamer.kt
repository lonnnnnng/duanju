package com.duanju.player.core

import android.util.Log
import com.arthenica.ffmpegkit.FFmpegKit
import com.arthenica.ffmpegkit.FFmpegSession
import com.arthenica.ffmpegkit.ReturnCode
import java.io.File
import java.io.InputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * 边下边播：ffmpeg 从 CDN 拉流、解密并转封装成 fMP4，通过本地 TCP 输出，
 * ExoPlayer 从 socket 读取，首个分片到达即可起播，不再等整集下载完成。
 *
 * 注意：TCP 是单向流，无法回退读取；seek 由调用方通过 [startSec] 在输入侧完成。
 */
class Fmp4Streamer(
    private val sourceUrl: String,
    private val keyHex: String,
    private val caFile: File?,
    private val startSec: Double
) {
    private val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1"))
    val port: Int get() = server.localPort

    private val accepted = CountDownLatch(1)
    private val closed = AtomicBoolean(false)
    @Volatile private var client: Socket? = null
    @Volatile private var session: FFmpegSession? = null
    @Volatile var failure: String? = null
        private set
    private val log = StringBuilder()

    fun start() {
        Thread({
            try {
                client = server.accept().apply { soTimeout = 0 }
            } catch (e: Exception) {
                if (!closed.get()) failure = failure ?: ("连接失败: " + e.message)
            } finally {
                accepted.countDown()
            }
        }, "fmp4-accept").start()

        val args = buildList {
            addAll(listOf("-y", "-hide_banner", "-loglevel", "error", "-nostdin"))
            addAll(listOf("-rw_timeout", "20000000"))
            addAll(listOf("-protocol_whitelist", "file,http,https,tcp,tls,crypto"))
            if (caFile != null) {
                addAll(listOf("-tls_verify", "1", "-cafile", caFile.absolutePath))
            }
            if (startSec > 0.0) {
                addAll(listOf("-ss", "%.3f".format(startSec)))
            }
            addAll(listOf("-decryption_key", keyHex, "-i", sourceUrl))
            addAll(listOf("-map", "0:v:0", "-map", "0:a:0?", "-c", "copy"))
            addAll(
                listOf(
                    "-movflags", "+frag_keyframe+empty_moov+default_base_moof",
                    "-flush_packets", "1",
                    "-f", "mp4", "tcp://127.0.0.1:$port"
                )
            )
        }.toTypedArray()

        session = FFmpegKit.executeWithArgumentsAsync(
            args,
            { completed ->
                val code = completed.getReturnCode()
                if (!ReturnCode.isSuccess(code) && !closed.get()) {
                    failure = failure ?: ("ffmpeg rc=" + code?.value + " " + log.takeLast(300))
                    Log.e("Fmp4Streamer", "ffmpeg 失败 rc=" + code?.value + " log=" + log.takeLast(500))
                } else {
                    Log.d("Fmp4Streamer", "ffmpeg 结束 rc=" + code?.value + " closed=" + closed.get())
                }
                // 只关闭监听端口，已建立的连接保留到读取方读到 EOF；
                // 否则 ExoPlayer 在缓冲耗尽后重新 open 会拿到已关闭的 socket。
                runCatching { server.close() }
                accepted.countDown()
            },
            { entry ->
                synchronized(log) { log.append(entry.message) }
                Log.d("Fmp4Streamer", "ffmpeg: " + entry.message.trim())
            },
            null
        )
    }

    /** 等待 ffmpeg 连上来；ffmpeg 提前失败或超时都返回 false。 */
    fun awaitClient(timeoutMs: Long): Boolean {
        val ok = accepted.await(timeoutMs, TimeUnit.MILLISECONDS)
        return ok && client != null
    }

    fun openStream(): InputStream {
        val socket = client ?: throw DirectClient.DirectException("流式播放未建立连接：" + (failure ?: "无数据"))
        return socket.getInputStream()
    }

    fun cancel() {
        closed.set(true)
        session?.let { runCatching { FFmpegKit.cancel(it.getSessionId()) } }
        closeQuietly()
    }

    private fun closeQuietly() {
        runCatching { client?.close() }
        runCatching { server.close() }
    }
}
