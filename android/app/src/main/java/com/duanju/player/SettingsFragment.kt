package com.duanju.player

import android.os.Bundle
import android.text.Editable
import android.text.TextWatcher
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.AdapterView
import android.widget.ArrayAdapter
import android.widget.EditText
import android.widget.Spinner
import android.widget.Switch
import android.widget.TextView
import androidx.fragment.app.Fragment

class SettingsFragment : Fragment() {

    private val speeds = listOf(1.0f, 1.25f, 1.5f, 1.75f, 2.0f, 3.0f, 4.0f)
    private val speedLabels = listOf("1.0 倍", "1.25 倍", "1.5 倍", "1.75 倍", "2.0 倍", "3.0 倍", "4 倍")

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_settings, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val baseField = view.findViewById<EditText>(R.id.base_url)
        baseField.setText(AppSettings.base(requireContext()))
        baseField.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) {}
            override fun onTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) {}
            override fun afterTextChanged(s: Editable?) {
                AppSettings.setBase(requireContext(), s?.toString()?.trim()?.trimEnd('/') ?: "")
            }
        })

        val autoNext = view.findViewById<Switch>(R.id.auto_next)
        autoNext.isChecked = AppSettings.autoNext(requireContext())
        autoNext.setOnCheckedChangeListener { _, checked -> AppSettings.setAutoNext(requireContext(), checked) }

        val speed = view.findViewById<Spinner>(R.id.speed)
        val adapter = ArrayAdapter(requireContext(), android.R.layout.simple_spinner_item, speedLabels)
        adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
        speed.adapter = adapter
        val current = AppSettings.speed(requireContext())
        val index = speeds.indexOfFirst { it == current }.takeIf { it >= 0 } ?: 0
        speed.setSelection(index)
        speed.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) {
                AppSettings.setSpeed(requireContext(), speeds[position])
            }

            override fun onNothingSelected(parent: AdapterView<*>?) {}
        }

        val direct = view.findViewById<Switch>(R.id.direct_mode)
        direct.isChecked = AppSettings.direct(requireContext())
        direct.setOnCheckedChangeListener { _, checked -> AppSettings.setDirect(requireContext(), checked) }

        view.findViewById<TextView>(R.id.version).text = "版本 ${BuildConfig.VERSION_NAME}"
    }
}