package com.duanju.player

import android.content.Context
import android.util.AttributeSet
import android.view.Gravity
import android.view.LayoutInflater
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.TextView

/**
 * 通用顶栏：返回按钮 + 单行标题。详情页、播放页共用同一套外观与点击区域。
 *
 * 用法：
 * ```
 * topBar.setTitle("剧名")
 * topBar.setOnBackClickListener { finish() }
 * ```
 */
class TopBar @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
    defStyleAttr: Int = 0
) : LinearLayout(context, attrs, defStyleAttr) {

    private val back: ImageButton
    private val titleView: TextView

    init {
        orientation = HORIZONTAL
        gravity = Gravity.CENTER_VERTICAL
        LayoutInflater.from(context).inflate(R.layout.view_top_bar, this, true)
        back = findViewById(R.id.bar_back)
        titleView = findViewById(R.id.bar_title)
    }

    fun setTitle(text: CharSequence?) {
        titleView.text = text
    }

    fun setOnBackClickListener(listener: () -> Unit) {
        back.setOnClickListener { listener() }
    }
}
