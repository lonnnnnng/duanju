package com.duanju.player.core

import android.util.Log
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.InputStream
import java.io.OutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * 只读的本地 HTTP 服务，仅监听 127.0.0.1，用于把 ffmpeg 产出的 HLS 目录暴露给 ExoPlayer。
 * 只处理 GET/HEAD，支持 Range，拒绝目录穿越。
 */
class LocalHlsServer(private val root: File) : Closeable {

    private val server = ServerSocket(0, 32, InetAddress.getByName("127.0.0.1"))
    private val pool = Executors.newFixedThreadPool(4)
    private val running = AtomicBoolean(true)

    val port: Int get() = server.localPort

    fun start() {
        Thread({
            while (running.get()) {
                val socket = try {
                    server.accept()
                } catch (e: Exception) {
                    break
                }
                pool.execute { serve(socket) }
            }
        }, "hls-http").start()
    }

    private fun serve(socket: Socket) {
        try {
            socket.soTimeout = 10_000
            val input = socket.getInputStream()
            val requestLine = readLine(input) ?: return
            val parts = requestLine.split(' ')
            if (parts.size < 2) return
            val method = parts[0]
            val rawPath = parts[1].substringBefore('?')
            var range: String? = null
            while (true) {
                val header = readLine(input) ?: break
                if (header.isEmpty()) break
                if (header.startsWith("Range:", ignoreCase = true)) range = header.substringAfter(':').trim()
            }
            val output = BufferedOutputStream(socket.getOutputStream())
            if (method != "GET" && method != "HEAD") {
                writeStatus(output, 405, "Method Not Allowed"); return
            }
            val file = resolve(rawPath)
            if (file == null || !file.isFile) {
                writeStatus(output, 404, "Not Found"); return
            }
            writeFile(output, file, if (method == "HEAD") null else range)
            output.flush()
        } catch (e: Exception) {
            Log.d("LocalHlsServer", "请求失败: " + e.message)
        } finally {
            runCatching { socket.close() }
        }
    }

    private fun resolve(rawPath: String): File? {
        val name = rawPath.trimStart('/').substringBefore('?')
        if (name.isEmpty() || name.contains("..")) return null
        val file = File(root, name)
        return if (file.canonicalPath.startsWith(root.canonicalPath)) file else null
    }

    private fun writeFile(out: OutputStream, file: File, range: String?) {
        val total = file.length()
        var start = 0L
        var end = total - 1
        var partial = false
        if (!range.isNullOrEmpty()) {
            val match = Regex("bytes=(\\d*)-(\\d*)").find(range)
            if (match != null) {
                val first = match.groupValues[1]
                val second = match.groupValues[2]
                if (first.isNotEmpty()) start = first.toLong()
                if (second.isNotEmpty()) end = second.toLong()
                if (start > end || start >= total) {
                    writeStatus(out, 416, "Range Not Satisfiable"); return
                }
                end = minOf(end, total - 1)
                partial = true
            }
        }
        val length = end - start + 1
        val header = StringBuilder()
            .append("HTTP/1.1 ").append(if (partial) "206 Partial Content" else "200 OK").append("\r\n")
            .append("Content-Type: ").append(contentType(file.name)).append("\r\n")
            .append("Content-Length: ").append(length).append("\r\n")
            .append("Accept-Ranges: bytes\r\n")
            .append("Cache-Control: no-store\r\n")
            .append("Connection: close\r\n")
        if (partial) header.append("Content-Range: bytes ").append(start).append('-').append(end).append('/').append(total).append("\r\n")
        header.append("\r\n")
        out.write(header.toString().toByteArray())
        file.inputStream().use { input ->
            input.skip(start)
            var remaining = length
            val buffer = ByteArray(64 * 1024)
            while (remaining > 0) {
                val read = input.read(buffer, 0, minOf(buffer.size.toLong(), remaining).toInt())
                if (read < 0) break
                out.write(buffer, 0, read)
                remaining -= read
            }
        }
    }

    private fun writeStatus(out: OutputStream, code: Int, text: String) {
        val body = text.toByteArray()
        out.write(
            ("HTTP/1.1 $code $text\r\nContent-Type: text/plain\r\nContent-Length: ${body.size}\r\nConnection: close\r\n\r\n").toByteArray()
        )
        out.write(body)
        out.flush()
    }

    private fun readLine(input: InputStream): String? {
        val buffer = StringBuilder()
        while (true) {
            val value = input.read()
            if (value < 0) return if (buffer.isEmpty()) null else buffer.toString()
            if (value == '\n'.code) return buffer.toString().trimEnd('\r')
            if (buffer.length > 8192) return null
            buffer.append(value.toChar())
        }
    }

    private fun contentType(name: String): String = when {
        name.endsWith(".m3u8") -> "application/vnd.apple.mpegurl"
        name.endsWith(".ts") -> "video/mp2t"
        name.endsWith(".m4s") -> "video/iso.segment"
        name.endsWith(".mp4") -> "video/mp4"
        else -> "application/octet-stream"
    }

    override fun close() {
        running.set(false)
        runCatching { server.close() }
        pool.shutdownNow()
    }
}
