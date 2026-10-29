package com.duanju.player

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Switch
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment
import com.duanju.player.core.VideoCache

class SettingsFragment : Fragment() {

    private val speeds = listOf(1.0f, 1.25f, 1.5f, 1.75f, 2.0f, 3.0f, 4.0f)
    private val speedLabels = listOf("1.0 倍", "1.25 倍", "1.5 倍", "1.75 倍", "2.0 倍", "3.0 倍", "4.0 倍")
    private val themeLabels = listOf("跟随系统", "深色模式", "浅色模式")
    private var cacheSize: TextView? = null
    private var cacheLimit: TextView? = null
    private var themeValue: TextView? = null

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_settings, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val autoNext = view.findViewById<Switch>(R.id.auto_next)
        autoNext.isChecked = AppSettings.autoNext(requireContext())
        autoNext.setOnCheckedChangeListener { _, checked ->
            AppSettings.setAutoNext(requireContext(), checked)
        }

        val prefetch = view.findViewById<Switch>(R.id.prefetch_next)
        prefetch.isChecked = AppSettings.prefetchNext(requireContext())
        prefetch.setOnCheckedChangeListener { _, checked ->
            AppSettings.setPrefetchNext(requireContext(), checked)
        }

        val speedValue = view.findViewById<TextView>(R.id.speed_value)
        renderSpeed(speedValue)
        view.findViewById<View>(R.id.row_speed).setOnClickListener {
            val current = speeds.indexOfFirst { it == AppSettings.speed(requireContext()) }.coerceAtLeast(0)
            AlertDialog.Builder(requireContext())
                .setTitle("默认播放倍速")
                .setSingleChoiceItems(speedLabels.toTypedArray(), current) { dialog, which ->
                    AppSettings.setSpeed(requireContext(), speeds[which])
                    renderSpeed(speedValue)
                    dialog.dismiss()
                }
                .setNegativeButton("取消", null)
                .show()
        }

        val version = view.findViewById<TextView>(R.id.version)
        version.text = "v${BuildConfig.VERSION_NAME}"
        view.findViewById<View>(R.id.row_version).setOnClickListener {
            checkUpdate(version)
        }

        themeValue = view.findViewById(R.id.theme_value)
        renderTheme()
        view.findViewById<View>(R.id.row_theme).setOnClickListener { pickTheme() }

        cacheSize = view.findViewById(R.id.cache_size)
        cacheLimit = view.findViewById(R.id.cache_limit_value)
        renderCacheLimit()
        view.findViewById<View>(R.id.row_cache_limit).setOnClickListener { pickCacheLimit() }
        view.findViewById<View>(R.id.row_cache).setOnClickListener { showCacheDialog() }
        refreshCacheSize()
    }

    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (!hidden) refreshCacheSize()
    }

    /** 统计缓存占用（后台线程，避免主线程遍历文件）。 */
    private fun refreshCacheSize() {
        val context = context ?: return
        val target = cacheSize ?: return
        Thread {
            val size = VideoCache.sizeBytes(context)
            activity?.runOnUiThread {
                if (isAdded) target.text = VideoCache.format(size)
            }
        }.start()
    }

    private fun renderCacheLimit() {
        val mb = AppSettings.cacheLimitMb(requireContext())
        cacheLimit?.text = VideoCache.format(VideoCache.mbToBytes(mb))
    }

    /** 缓存上限：常用档位 + 自定义（单位 GB）。 */
    private fun pickCacheLimit() {
        val presets = listOf(512, 1024, 2048, 4096, 8192, 16384)
        val labels = presets.map { VideoCache.format(VideoCache.mbToBytes(it)) } + "自定义…"
        val current = AppSettings.cacheLimitMb(requireContext())
        val checked = presets.indexOf(current).takeIf { it >= 0 } ?: presets.size
        AlertDialog.Builder(requireContext())
            .setTitle("缓存上限")
            .setSingleChoiceItems(labels.toTypedArray(), checked) { dialog, which ->
                dialog.dismiss()
                if (which < presets.size) applyCacheLimit(presets[which]) else inputCacheLimit()
            }
            .setNegativeButton("取消", null)
            .show()
    }

    private fun inputCacheLimit() {
        val context = context ?: return
        val current = AppSettings.cacheLimitMb(context)
        val input = android.widget.EditText(context).apply {
            setText(String.format(java.util.Locale.US, "%.1f", current / 1024f))
            inputType = android.text.InputType.TYPE_CLASS_NUMBER or android.text.InputType.TYPE_NUMBER_FLAG_DECIMAL
            setSelectAllOnFocus(true)
            setPadding(48, 32, 48, 32)
        }
        AlertDialog.Builder(context)
            .setTitle("自定义上限（GB）")
            .setView(input)
            .setPositiveButton("确定") { _, _ ->
                val gb = input.text.toString().trim().toDoubleOrNull()
                if (gb == null || gb <= 0) {
                    toast("请输入有效数字")
                    return@setPositiveButton
                }
                val mb = (gb * 1024).toInt().coerceAtLeast(AppSettings.MIN_CACHE_LIMIT_MB)
                applyCacheLimit(mb)
            }
            .setNegativeButton("取消", null)
            .show()
    }

    /** 写入上限后立即按新阈值清理一次，并刷新占用显示。 */
    private fun applyCacheLimit(mb: Int) {
        val context = context ?: return
        AppSettings.setCacheLimitMb(context, mb)
        renderCacheLimit()
        Thread {
            val freed = VideoCache.trim(VideoCache.dir(context), VideoCache.mbToBytes(mb))
            activity?.runOnUiThread {
                if (isAdded) {
                    if (freed > 0L) toast("已清理 ${VideoCache.format(freed)}")
                    refreshCacheSize()
                }
            }
        }.start()
    }

    private fun showCacheDialog() {
        val context = context ?: return
        AlertDialog.Builder(context)
            .setTitle("播放缓存")
            .setMessage("当前占用 ${cacheSize?.text ?: "-"}，仅统计已解密到本机的剧集。")
            .setPositiveButton("清理缓存") { _, _ -> clearCache() }
            .setNegativeButton("取消", null)
            .show()
    }

    private fun clearCache() {
        val context = context ?: return
        Thread {
            val freed = VideoCache.clear(context)
            activity?.runOnUiThread {
                if (isAdded) {
                    toast("已清理 ${VideoCache.format(freed)}")
                    refreshCacheSize()
                }
            }
        }.start()
    }

    private fun toast(message: String) {
        android.widget.Toast.makeText(requireContext(), message, android.widget.Toast.LENGTH_SHORT).show()
    }

    /** 点击版本行手动检查更新：有新版本弹窗，没有则提示已是最新。 */
    private fun checkUpdate(version: TextView) {
        if (!isAdded) return
        val label = "v${BuildConfig.VERSION_NAME}"
        version.text = "检查中…"
        Thread {
            val release = UpdateChecker.fetchLatest()
            val context = context ?: return@Thread
            activity?.runOnUiThread {
                if (!isAdded) return@runOnUiThread
                version.text = label
                when {
                    release == null -> AlertDialog.Builder(context)
                        .setTitle("检查更新失败")
                        .setMessage("无法获取最新版本，请稍后重试。")
                        .setPositiveButton("知道了", null)
                        .show()
                    UpdateChecker.isNewer(release.version, BuildConfig.VERSION_NAME) ->
                        showUpdateDialog(context, release)
                    else -> AlertDialog.Builder(context)
                        .setTitle("已是最新版本")
                        .setMessage("当前版本 v${BuildConfig.VERSION_NAME}")
                        .setPositiveButton("前往 GitHub 查看") { _, _ ->
                            openUrl(context, release.pageUrl.ifEmpty { UpdateChecker.RELEASES_URL })
                        }
                        .setNegativeButton("知道了", null)
                        .show()
                }
            }
        }.start()
    }

    private fun renderSpeed(target: TextView) {
        val current = AppSettings.speed(requireContext())
        val index = speeds.indexOfFirst { it == current }.takeIf { it >= 0 } ?: 0
        target.text = speedLabels[index]
    }

    private fun renderTheme() {
        themeValue?.text = themeLabels.getOrElse(AppSettings.themeMode(requireContext())) { themeLabels[0] }
    }

    /** 主题模式三选一，选中后立即应用到全局 night mode（Activity 自动重建换肤）。 */
    private fun pickTheme() {
        val context = context ?: return
        val current = AppSettings.themeMode(context)
        AlertDialog.Builder(context)
            .setTitle("主题模式")
            .setSingleChoiceItems(themeLabels.toTypedArray(), current) { dialog, which ->
                dialog.dismiss()
                if (which != AppSettings.themeMode(context)) {
                    AppSettings.setThemeMode(context, which)
                    renderTheme()
                    (context.applicationContext as DuanjuApp).applyThemeMode()
                }
            }
            .setNegativeButton("取消", null)
            .show()
    }
}
