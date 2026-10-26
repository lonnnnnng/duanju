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

        view.findViewById<TextView>(R.id.version).text = "v${BuildConfig.VERSION_NAME}"
    }

    private fun renderSpeed(target: TextView) {
        val current = AppSettings.speed(requireContext())
        val index = speeds.indexOfFirst { it == current }.takeIf { it >= 0 } ?: 0
        target.text = speedLabels[index]
    }
}
