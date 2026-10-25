package com.duanju.player

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import android.widget.ImageView
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

object BitmapLoader {
    private val cache = object : LruCache<String, Bitmap>(16 * 1024) {
        override fun sizeOf(key: String, value: Bitmap): Int = value.byteCount / 1024
    }
    private val executor = Executors.newFixedThreadPool(3)
    private val placeholderUrls = java.util.Collections.newSetFromMap(java.util.concurrent.ConcurrentHashMap<String, Boolean>())

    fun load(url: String, view: ImageView) {
        cache.get(url)?.let {
            view.setImageBitmap(it)
            return
        }
        view.setImageBitmap(null)
        if (!placeholderUrls.add(url)) return
        executor.execute {
            try {
                val connection = (URL(url).openConnection() as HttpURLConnection).apply {
                    connectTimeout = 10000
                    readTimeout = 20000
                }
                val bitmap = BitmapFactory.decodeStream(connection.inputStream)
                connection.disconnect()
                if (bitmap != null) {
                    cache.put(url, bitmap)
                    view.post { view.setImageBitmap(bitmap) }
                }
            } catch (_: Exception) {
            } finally {
                placeholderUrls.remove(url)
            }
        }
    }
}