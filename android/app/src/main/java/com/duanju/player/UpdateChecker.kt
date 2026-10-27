package com.duanju.player

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

data class UpdateRelease(
    val version: String,
    val pageUrl: String,
    val downloadUrl: String
)

/**
 * 从 GitHub Release 读取最新版本。
 * 统一发版后 latest 同时包含 Web 端程序与安卓 APK，版本号一致。
 */
object UpdateChecker {

    private const val API_URL = "https://api.github.com/repos/lonnnnnng/duanju/releases/latest"
    const val RELEASES_URL = "https://github.com/lonnnnnng/duanju/releases"

    /** 网络或解析失败返回 null，调用方按「检查失败」处理。 */
    fun fetchLatest(): UpdateRelease? {
        val conn = (URL(API_URL).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 8000
            readTimeout = 8000
            setRequestProperty("Accept", "application/vnd.github+json")
            setRequestProperty("User-Agent", "duanju-android")
        }
        return try {
            if (conn.responseCode != 200) return null
            val root = JSONObject(conn.inputStream.bufferedReader().use { it.readText() })
            if (root.optBoolean("draft") || root.optBoolean("prerelease")) return null
            val version = root.optString("tag_name").trim().removePrefix("v").removePrefix("V")
            if (version.isEmpty()) return null
            val pageUrl = root.optString("html_url")
            var downloadUrl = pageUrl
            val assets = root.optJSONArray("assets")
            if (assets != null) {
                for (i in 0 until assets.length()) {
                    val asset = assets.optJSONObject(i) ?: continue
                    if (asset.optString("name").endsWith("-android.apk")) {
                        downloadUrl = asset.optString("browser_download_url").ifEmpty { pageUrl }
                        break
                    }
                }
            }
            UpdateRelease(version, pageUrl, downloadUrl)
        } catch (e: Exception) {
            null
        } finally {
            conn.disconnect()
        }
    }

    /** 逐段比较语义版本，忽略前缀 v 与后缀（如 -beta）。 */
    fun isNewer(remote: String, local: String): Boolean {
        val r = parse(remote) ?: return false
        val l = parse(local) ?: return false
        for (i in 0 until maxOf(r.size, l.size)) {
            val a = r.getOrElse(i) { 0 }
            val b = l.getOrElse(i) { 0 }
            if (a != b) return a > b
        }
        return false
    }

    private fun parse(version: String): List<Int>? {
        val clean = version.trim().removePrefix("v").removePrefix("V").substringBefore('-')
        if (clean.isEmpty()) return null
        val parts = clean.split('.')
        return parts.map { it.toIntOrNull() ?: return null }
    }
}

/** 新版本弹窗：前往下载跳到 APK 直链，系统浏览器/下载器接管。 */
fun showUpdateDialog(context: Context, release: UpdateRelease) {
    AlertDialog.Builder(context)
        .setTitle("发现新版本 v${release.version}")
        .setMessage("当前版本 v${BuildConfig.VERSION_NAME}，是否前往下载？")
        .setPositiveButton("前往下载") { _, _ -> openUrl(context, release.downloadUrl) }
        .setNegativeButton("以后再说", null)
        .show()
}

fun openUrl(context: Context, url: String) {
    try {
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
    } catch (e: Exception) {
        Toast.makeText(context, "没有可用的浏览器", Toast.LENGTH_SHORT).show()
    }
}
