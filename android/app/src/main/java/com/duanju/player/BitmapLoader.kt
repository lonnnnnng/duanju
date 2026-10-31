package com.duanju.player

import android.widget.ImageView
import coil.load
import coil.dispose

object BitmapLoader {
    fun load(url: String?, view: ImageView) {
        // long: 未附着的复用控件也要立即清掉旧图，不能等下一次布局或图片请求才取消旧封面。
        view.dispose()
        view.setImageDrawable(null)
        // long: 非空封面由库按控件尺寸解码，并随控件生命周期取消失效请求。
        if (!url.isNullOrBlank()) view.load(url)
    }
}
