package com.duanju.player

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Switch
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment

class SettingsFragment : Fragment() {

    private val speeds = listOf(1.0f, 1.25f, 1.5f, 1.75f, 2.0f, 3.0f, 4.0f)
    private val speedLabels = listOf("1.0 倍", "1.25 倍", "1.5 倍", "1.75 倍", "2.0 倍", "3.0 倍", "4.0 倍")

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
}
