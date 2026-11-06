package com.duanju.player

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Switch
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment
import com.duanju.player.core.RecipeStore
import com.duanju.player.core.RecipeUpdater
import com.duanju.player.core.VideoCache

class SettingsFragment : Fragment() {

    private val speeds = listOf(1.0f, 1.25f, 1.5f, 1.75f, 2.0f, 3.0f, 4.0f)
    private val speedLabels = listOf("1.0 倍", "1.25 倍", "1.5 倍", "1.75 倍", "2.0 倍", "3.0 倍", "4.0 倍")
    private val themeLabels = listOf("跟随系统", "深色模式", "浅色模式")
    private var cacheSize: TextView? = null
    private var cacheLimit: TextView? = null
    private var themeValue: TextView? = null
    private var updateChecking = false
    internal var fetchUpdate: () -> UpdateRelease? = { UpdateChecker.fetchLatest() }
    private var updateThread: Thread? = null

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_settings, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val autoNext = view.findViewById<Switch>(R.id.auto_next)
        autoNext.isChecked = AppSettings.autoNext(requireContext())
        autoNext.setOnCheckedChangeListener { _, checked ->
            AppSettings.setAutoNext(requireContext(), checked)
        }
        view.findViewById<View>(R.id.row_auto_next).setOnClickListener { autoNext.performClick() }

        val prefetch = view.findViewById<Switch>(R.id.prefetch_next)
        prefetch.isChecked = AppSettings.prefetchNext(requireContext())
        prefetch.setOnCheckedChangeListener { _, checked ->
            AppSettings.setPrefetchNext(requireContext(), checked)
        }
        view.findViewById<View>(R.id.row_prefetch).setOnClickListener { prefetch.performClick() }

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

        val recipeValue = view.findViewById<TextView>(R.id.recipe_value)
        renderRecipe(recipeValue)
        val recipeRow = view.findViewById<View>(R.id.row_recipe)
        recipeRow.setOnClickListener {
            recipeRow.isEnabled = false
            toast("检查接口配方…")
            Thread {
                val result = runCatching { RecipeUpdater.refresh() }.getOrNull()
                view.post {
                    // long: 检查可能跨越旋转或离开页面，迟到回调不能操作已经销毁的视图。
                    if (!isAdded || this.view !== view) return@post
                    recipeRow.isEnabled = true
                    renderRecipe(recipeValue)
                    if (!isHidden && isResumed) toast(
                        when (result) {
                            true -> "配方已更新"
                            false -> "配方已是最新"
                            null -> "检查失败，继续使用当前配方"
                        }
                    )
                }
            }.start()
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
        if (!hidden) {
            refreshCacheSize()
            view?.findViewById<TextView>(R.id.recipe_value)?.let { renderRecipe(it) }
        }
    }

    /** 统计缓存占用（后台线程，避免主线程遍历文件）。 */
    private fun refreshCacheSize() {
        val context = context ?: return
        val target = cacheSize ?: return
        val owner = view ?: return
        Thread {
            val size = VideoCache.sizeBytes(context)
            activity?.runOnUiThread {
                if (isAdded && view === owner) target.text = VideoCache.format(size)
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
        val input = com.google.android.material.textfield.TextInputEditText(context).apply {
            setText((current / 1024.0).toString())
            inputType = android.text.InputType.TYPE_CLASS_NUMBER or android.text.InputType.TYPE_NUMBER_FLAG_DECIMAL
            setSelectAllOnFocus(true)
            val density = resources.displayMetrics.density
            minHeight = (56 * density).toInt()
            setPadding((12 * density).toInt(), (14 * density).toInt(), (12 * density).toInt(), (14 * density).toInt())
            background = null
        }
        val field = com.google.android.material.textfield.TextInputLayout(context).apply {
            val density = resources.displayMetrics.density
            setPadding((24 * density).toInt(), (8 * density).toInt(), (24 * density).toInt(), 0)
            // long: 弹框标题已包含单位，不再用浮动标签叠压数字；边框和错误文字由同一容器管理。
            isHintEnabled = false
            boxBackgroundMode = com.google.android.material.textfield.TextInputLayout.BOX_BACKGROUND_OUTLINE
            isErrorEnabled = true
            addView(input)
        }
        val dialog = AlertDialog.Builder(context)
            .setTitle("自定义上限（GB）")
            .setView(field)
            .setPositiveButton("确定", null)
            .setNegativeButton("取消", null)
            .create()
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                // long: 非法输入保留原值和弹框，避免默认确定按钮自动关闭，让用户重新从菜单开始。
                val mb = AppSettings.parseCacheLimitGb(input.text.toString())
                if (mb == null) {
                    // long: 错误留在输入框下方，原生 EditText 错误气泡会遮住弹框的取消/确定按钮。
                    field.error = "请输入不小于 0.125 GB 的有效数值"
                } else {
                    applyCacheLimit(mb)
                    dialog.dismiss()
                }
            }
        }
        dialog.show()
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
        if (!isAdded || updateChecking) return
        val owner = view ?: return
        val row = owner.findViewById<View>(R.id.row_version)
        updateChecking = true
        row.isEnabled = false
        val label = "v${BuildConfig.VERSION_NAME}"
        version.text = "检查中…"
        updateThread = Thread {
            val release = runCatching { fetchUpdate() }.getOrNull()
            val context = context ?: return@Thread
            activity?.runOnUiThread {
                if (!isAdded || view !== owner) return@runOnUiThread
                updateChecking = false
                row.isEnabled = true
                version.text = label
                // long: 设置页用 hide/show 保留，后台检查结束时不能在首页或历史页突然弹出更新对话框。
                if (isHidden || !isResumed) return@runOnUiThread
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
        }.also { it.start() }
    }

    private fun renderSpeed(target: TextView) {
        val current = AppSettings.speed(requireContext())
        val index = speeds.indexOfFirst { it == current }.takeIf { it >= 0 } ?: 0
        target.text = speedLabels[index]
    }

    private fun renderRecipe(target: TextView) {
        val (version, updated, source) = RecipeStore.info()
        target.text = "v$version · $updated · ${if (source == "override") "远程" else "内置"}"
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

    override fun onDestroyView() {
        cacheSize = null
        cacheLimit = null
        themeValue = null
        updateChecking = false
        super.onDestroyView()
    }
}
