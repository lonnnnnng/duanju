package com.duanju.player.core

import android.net.Uri
import androidx.media3.common.C
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import java.io.IOException
import java.io.InputStream

/**
 * 从 [Fmp4Streamer] 的 TCP 流读取 fMP4 字节。
 * TCP 单向，position > 0 时只能丢弃已读部分，无法真正回退；续播由 ffmpeg 输入侧 -ss 完成。
 */
class StreamDataSource(private val streamer: Fmp4Streamer) : DataSource {

    private var input: InputStream? = null
    private var uri: Uri = Uri.EMPTY

    override fun addTransferListener(transferListener: TransferListener) = Unit

    override fun open(dataSpec: DataSpec): Long {
        val stream = streamer.openStream()
        var skip = dataSpec.position
        if (skip > 0) {
            val buffer = ByteArray(64 * 1024)
            while (skip > 0) {
                val n = stream.read(buffer, 0, minOf(buffer.size.toLong(), skip).toInt())
                if (n < 0) break
                skip -= n
            }
        }
        input = stream
        uri = dataSpec.uri
        return C.LENGTH_UNSET.toLong()
    }

    override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
        if (length == 0) return 0
        val stream = input ?: return C.RESULT_END_OF_INPUT
        return try {
            stream.read(buffer, offset, length)
        } catch (e: IOException) {
            throw e
        }
    }

    override fun getUri(): Uri = uri

    override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

    // TCP 无法重开，关闭输入流会连带关闭 socket，导致 ExoPlayer 重新 open 失败；
    // 生命周期由 Fmp4Streamer 统一管理。
    override fun close() {
        input = null
    }
}
