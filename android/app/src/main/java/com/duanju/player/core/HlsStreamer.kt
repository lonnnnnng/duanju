package com.duanju.player.core

import android.util.Log
import com.arthenica.ffmpegkit.FFmpegKit
import com.arthenica.ffmpegkit.FFmpegSession
import com.arthenica.ffmpegkit.ReturnCode
import java.io.File

/**
 * 边下边播（HLS 版）：ffmpeg 解密并转封装为 HLS 分片写入本地目录，
 * 同时由 [LocalHlsServer] 通过 127.0.0.1 暴露给 ExoPlayer。
 * 相比 TCP 裸流，HLS 自带分片时长、索引与 ENDLIST，播放器可获得正确总时长并支持 seek。
 */
class HlsStreamer(
    private val sourceUrl: String,
    private val keyHex: String,
    private val caFile: File?,
    private val startSec: Double,
    parentDir: File
) {
    private val dir = File(parentDir, "hls-" + System.currentTimeMillis())
    private val server = LocalHlsServer(dir)
    private val log = StringBuilder()
    @Volatile private var session: FFmpegSession? = null
    @Volatile var failure: String? = null
        private set

    val playlistUrl: String get() = "http://127.0.0.1:${server.port}/index.m3u8"

    fun start() {
        dir.mkdirs()
        server.start()
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
                    "-f", "hls",
                    "-hls_time", "2",
                    "-hls_list_size", "0",
                    "-hls_playlist_type", "event",
                    "-hls_flags", "independent_segments",
                    "-hls_segment_filename", File(dir, "seg%05d.ts").absolutePath,
                    File(dir, "index.m3u8").absolutePath
                )
            )
        }.toTypedArray()
        session = FFmpegKit.executeWithArgumentsAsync(
            args,
            { completed ->
                val code = completed.getReturnCode()
                if (!ReturnCode.isSuccess(code)) {
                    failure = failure ?: ("ffmpeg rc=" + code?.value + " " + log.takeLast(300))
                    Log.e("HlsStreamer", "ffmpeg 失败 rc=" + code?.value + " log=" + log.takeLast(500))
                } else {
                    Log.d("HlsStreamer", "ffmpeg 完成 rc=" + code?.value)
                }
            },
            { entry ->
                synchronized(log) { log.append(entry.message) }
                Log.d("HlsStreamer", "ffmpeg: " + entry.message.trim())
            },
            null
        )
    }

    /**
     * 等播放清单写完（#EXT-X-ENDLIST），此时分片时长已完整，ExoPlayer 的时长不会随后续分片增长。
     * 超过 timeoutMs 仍未完成时，只要已有分片就先起播，避免慢网下一直黑屏。
     */
    fun awaitReady(timeoutMs: Long): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            failure?.let { return false }
            val playlist = File(dir, "index.m3u8")
            if (playlist.isFile && playlist.length() > 0) {
                val text = runCatching { playlist.readText() }.getOrDefault("")
                if (text.contains("#EXT-X-ENDLIST")) return true
                if (session == null) return false
            }
            Thread.sleep(40)
        }
        val text = runCatching { File(dir, "index.m3u8").readText() }.getOrDefault("")
        return text.contains("#EXTINF")
    }

    /** 停止 ffmpeg、关闭本地服务并删除分片目录。 */
    fun cancel() {
        session?.let { runCatching { FFmpegKit.cancel(it.getSessionId()) } }
        server.close()
        dir.deleteRecursively()
    }
}
