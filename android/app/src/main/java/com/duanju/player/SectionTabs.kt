package com.duanju.player

import android.graphics.Typeface
import android.widget.TextView
import androidx.core.content.ContextCompat

/** 「选集 / 相关」这类二级 Tab 的选中样式，详情页与播放页共用。 */
object SectionTabs {

    fun apply(selected: TextView, other: TextView) {
        style(selected, true)
        style(other, false)
    }

    private fun style(tab: TextView, active: Boolean) {
        val context = tab.context
        tab.setTextColor(
            ContextCompat.getColor(
                context,
                if (active) R.color.accent else R.color.app_on_surface_variant
            )
        )
        tab.typeface = Typeface.create(if (active) "sans-serif-medium" else "sans-serif", Typeface.NORMAL)
    }
}
