package com.duanju.player

import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.ProgressBar
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient.HomeDrama
import java.util.Locale

/** 首页瀑布流：卡片 + 底部状态行（加载中 / 没有更多了）。 */
class HomeAdapter(private val onClick: (HomeDrama) -> Unit) :
    RecyclerView.Adapter<RecyclerView.ViewHolder>() {

    private val items = ArrayList<HomeDrama>()
    private var footerLoading = false
    private var footerEnd = false
    private var showRank = false

    /** 榜单筛选下才显示名次（默认推荐流的 rank 等于位置，无意义）。 */
    fun setShowRank(show: Boolean) {
        if (showRank == show) return
        showRank = show
        if (items.isNotEmpty()) notifyItemRangeChanged(0, items.size)
    }

    fun submit(list: List<HomeDrama>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    fun append(list: List<HomeDrama>): Int {
        val known = items.map { it.id }.toHashSet()
        val fresh = list.filter { !known.contains(it.id) }
        if (fresh.isEmpty()) return 0
        val start = items.size
        items.addAll(fresh)
        notifyItemRangeInserted(start, fresh.size)
        return fresh.size
    }

    fun setFooter(loading: Boolean, end: Boolean) {
        val changed = footerLoading != loading || footerEnd != end
        footerLoading = loading
        footerEnd = end
        if (changed && items.isNotEmpty()) notifyItemChanged(items.size)
    }

    fun clear() {
        val size = items.size
        items.clear()
        if (size > 0) notifyItemRangeRemoved(0, size)
    }

    fun size(): Int = items.size

    fun isFooter(position: Int): Boolean = position >= items.size

    override fun getItemCount(): Int = items.size + if (footerEnd || footerLoading) 1 else 0

    override fun getItemViewType(position: Int): Int = if (position >= items.size) TYPE_FOOTER else TYPE_CARD

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): RecyclerView.ViewHolder {
        val inflater = LayoutInflater.from(parent.context)
        return if (viewType == TYPE_FOOTER) {
            FooterHolder(inflater.inflate(R.layout.item_home_footer, parent, false))
        } else {
            val holder = CardHolder(inflater.inflate(R.layout.item_home_drama, parent, false))
            val margin = (4 * holder.itemView.resources.displayMetrics.density).toInt()
            holder.itemView.layoutParams = androidx.recyclerview.widget.GridLayoutManager.LayoutParams(
                androidx.recyclerview.widget.GridLayoutManager.LayoutParams.WRAP_CONTENT,
                androidx.recyclerview.widget.GridLayoutManager.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(margin, margin, margin, margin + margin / 2) }
            holder
        }
    }

    override fun onBindViewHolder(holder: RecyclerView.ViewHolder, position: Int) {
        if (holder is FooterHolder) {
            holder.progress.visibility = if (footerLoading) View.VISIBLE else View.GONE
            holder.text.visibility = if (footerLoading) View.GONE else View.VISIBLE
            return
        }
        holder as CardHolder
        val item = items[position]
        holder.title.text = item.title
        holder.cover.setImageBitmap(null)
        item.cover?.takeIf { it.isNotBlank() }?.let { BitmapLoader.load(it, holder.cover) }

        if (item.episodes > 0) {
            holder.episodes.visibility = View.VISIBLE
            holder.episodes.text = "全${item.episodes}集"
        } else {
            holder.episodes.visibility = View.GONE
        }

        if (showRank && item.rank > 0) {
            holder.rank.visibility = View.VISIBLE
            holder.rank.text = "No.${item.rank}"
        } else {
            holder.rank.visibility = View.GONE
        }

        val meta = buildList {
            if (item.hotValue.isNotBlank()) {
                add(if (item.hotValue.endsWith("热度")) "\uD83D\uDD25 ${item.hotValue}" else "\uD83D\uDD25 ${item.hotValue}热度")
            }
            if (item.category.isNotBlank()) add(item.category)
        }.joinToString(" · ")
        if (meta.isNotEmpty()) {
            holder.meta.visibility = View.VISIBLE
            holder.meta.text = meta
        } else {
            holder.meta.visibility = View.GONE
        }

        val tag = item.tag
        if (!tag.isNullOrBlank()) {
            holder.tag.visibility = View.VISIBLE
            holder.tag.text = tag
            val fallback = runCatching { Color.parseColor(item.tagColor) }.getOrNull()
            holder.tag.background = if (fallback != null) {
                GradientDrawable().apply {
                    cornerRadius = dp(holder.itemView, 6f)
                    setColor(fallback)
                }
            } else {
                androidx.core.content.ContextCompat.getDrawable(holder.itemView.context, R.drawable.bg_tag_badge)?.mutate()
            }
        } else {
            holder.tag.visibility = View.GONE
        }

        val score = item.score
        if (!score.isNullOrBlank() && score != "0") {
            holder.score.visibility = View.VISIBLE
            holder.score.text = "$score 分"
        } else {
            holder.score.visibility = View.GONE
        }
        holder.plays.text = if (item.playCount > 0) formatCount(item.playCount) + "播放" else ""

        holder.itemView.setOnClickListener { onClick(item) }
    }

    private fun dp(view: View, value: Float): Float =
        value * view.resources.displayMetrics.density

    private class CardHolder(view: View) : RecyclerView.ViewHolder(view) {
        val cover: ImageView = view.findViewById(R.id.cover)
        val title: TextView = view.findViewById(R.id.title)
        val episodes: TextView = view.findViewById(R.id.episodes)
        val tag: TextView = view.findViewById(R.id.tag)
        val rank: TextView = view.findViewById(R.id.rank)
        val meta: TextView = view.findViewById(R.id.meta)
        val score: TextView = view.findViewById(R.id.score)
        val plays: TextView = view.findViewById(R.id.plays)
    }

    private class FooterHolder(view: View) : RecyclerView.ViewHolder(view) {
        val progress: ProgressBar = view.findViewById(R.id.footer_progress)
        val text: TextView = view.findViewById(R.id.footer_text)
    }

    companion object {
        private const val TYPE_CARD = 0
        private const val TYPE_FOOTER = 1

        fun formatCount(n: Long): String = when {
            n >= 100_000_000 -> String.format(Locale.CHINA, "%.1f亿", n / 100_000_000.0)
            n >= 10_000 -> String.format(Locale.CHINA, "%.1f万", n / 10_000.0)
            else -> n.toString()
        }
    }
}
