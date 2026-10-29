package com.duanju.player

import android.graphics.Typeface
import android.view.View
import android.widget.TextView
import androidx.core.content.ContextCompat

/** 「选集 / 相关」这类二级 Tab 的选中样式，详情页与播放页共用。 */
object SectionTabs {

    /** 切换两个 Tab 的文字样式与下方高亮条。 */
    fun apply(
        selected: TextView,
        selectedIndicator: View,
        other: TextView,
        otherIndicator: View
    ) {
        style(selected, true)
        style(other, false)
        selectedIndicator.visibility = View.VISIBLE
        otherIndicator.visibility = View.INVISIBLE
    }

    private fun style(tab: TextView, active: Boolean) {
        val context = tab.context
        // 不能用白色：浅色主题下白字在浅底上完全看不见（表现为 Tab 消失）。
        tab.setTextColor(
            ContextCompat.getColor(
                context,
                if (active) R.color.app_on_bg else R.color.app_on_surface_variant
            )
        )
        tab.typeface = Typeface.create(
            if (active) "sans-serif-medium" else "sans-serif",
            if (active) Typeface.BOLD else Typeface.NORMAL
        )
    }
}
