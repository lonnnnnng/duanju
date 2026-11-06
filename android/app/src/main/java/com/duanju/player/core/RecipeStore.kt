package com.duanju.player.core

import android.content.Context
import java.io.File

/**
 * 配方仓库（L1）。
 *
 * 加载顺序：
 * 1. APK 内置 `assets/recipes/hongguo.json`（始终可用，作为兜底）；
 * 2. `filesDir/recipes/hongguo.json`（版本号更高时覆盖，可 adb push 或后续远程下发）。
 *
 * 解析失败一律保留上一份可用配方，绝不因为一份坏配置让 App 无法联网。
 */
object RecipeStore {

    private const val ASSET_PATH = "recipes/hongguo.json"
    private const val OVERRIDE_DIR = "recipes"
    private const val OVERRIDE_FILE = "hongguo.json"

    @Volatile private var appContext: Context? = null
    @Volatile private var current: Recipe? = null
    @Volatile private var source: String = "none"

    fun init(context: Context) {
        appContext = context.applicationContext
        reload()
    }

    @Synchronized
    fun reload() {
        val context = appContext ?: return
        val bundled = runCatching {
            context.assets.open(ASSET_PATH).bufferedReader().use { it.readText() }
        }.getOrNull()?.let { runCatching { RecipeParser.parse(it) }.getOrNull() }

        val overrideFile = File(context.filesDir, "$OVERRIDE_DIR/$OVERRIDE_FILE")
        val override = if (overrideFile.isFile) {
            runCatching { RecipeParser.parse(overrideFile.readText()) }.getOrNull()
        } else null

        val chosen = when {
            bundled == null && override == null -> null
            override != null && (bundled == null || override.version >= bundled.version) -> override
            else -> bundled
        } ?: return

        current = chosen
        source = if (chosen === override) "override" else "bundled"
    }

    fun recipe(): Recipe {
        current?.let { return it }
        synchronized(this) {
            current?.let { return it }
            val context = appContext ?: throw IllegalStateException("RecipeStore 未初始化")
            val text = context.assets.open(ASSET_PATH).bufferedReader().use { it.readText() }
            return RecipeParser.parse(text).also {
                current = it
                source = "bundled"
            }
        }
    }

    fun endpoint(id: String): Endpoint = recipe().endpoint(id)

    fun info(): Triple<Int, String, String> {
        val r = recipe()
        return Triple(r.version, r.updated, source)
    }

    /** 保存外部下发的配方（后续远程更新用），版本号必须不倒退。 */
    fun saveOverride(text: String): Boolean {
        val context = appContext ?: return false
        val incoming = runCatching { RecipeParser.parse(text) }.getOrNull() ?: return false
        if (incoming.version < recipe().version) return false
        val dir = File(context.filesDir, OVERRIDE_DIR).apply { mkdirs() }
        val file = File(dir, OVERRIDE_FILE)
        val tmp = File(dir, "$OVERRIDE_FILE.tmp")
        tmp.writeText(text)
        if (!tmp.renameTo(file)) {
            file.delete()
            if (!tmp.renameTo(file)) return false
        }
        reload()
        return true
    }
}
