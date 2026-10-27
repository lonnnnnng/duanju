package com.duanju.player

import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient.HomeDrama

/** 搜索结果列表：左侧 3:4 封面，右侧标题 / 标签 / 评分 / 分类 / 播放量。 */
class SearchResultAdapter(private val onClick: (HomeDrama) -> Unit) :
    RecyclerView.Adapter<SearchResultAdapter.Holder>() {

    private val items = ArrayList<HomeDrama>()

    fun submit(list: List<HomeDrama>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    override fun getItemCount(): Int = items.size

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_search_result, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val item = items[position]
        holder.title.text = item.title
        holder.cover.setImageBitmap(null)
        item.cover?.takeIf { it.isNotBlank() }?.let { BitmapLoader.load(it, holder.cover) }

        if (item.episodes > 0) {
            holder.badge.visibility = View.VISIBLE
            holder.badge.text = "全${item.episodes}集"
        } else {
            holder.badge.visibility = View.GONE
        }

        val tag = item.tag
        if (!tag.isNullOrBlank()) {
            holder.tag.visibility = View.VISIBLE
            holder.tag.text = tag
            val color = runCatching { Color.parseColor(item.tagColor) }.getOrDefault(0xFFFF4D4F.toInt())
            holder.tag.background = GradientDrawable().apply {
                cornerRadius = dp(holder.itemView, 4f)
                setColor(color)
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

        if (item.rank > 0) {
            holder.rank.visibility = View.VISIBLE
            holder.rank.text = "No.${item.rank}"
        } else {
            holder.rank.visibility = View.GONE
        }

        val meta = buildList {
            if (item.category.isNotBlank()) add(item.category)
            if (item.hotValue.isNotBlank()) {
                add(if (item.hotValue.endsWith("热度")) item.hotValue else "${item.hotValue}热度")
            }
        }.joinToString(" · ")
        holder.meta.text = meta
        holder.meta.visibility = if (meta.isEmpty()) View.GONE else View.VISIBLE

        holder.plays.text = if (item.playCount > 0) {
            HomeAdapter.formatCount(item.playCount) + "播放"
        } else {
            ""
        }

        holder.itemView.setOnClickListener { onClick(item) }
    }

    private fun dp(view: View, value: Float): Float =
        value * view.resources.displayMetrics.density

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val cover: ImageView = view.findViewById(R.id.cover)
        val title: TextView = view.findViewById(R.id.title)
        val badge: TextView = view.findViewById(R.id.badge)
        val tag: TextView = view.findViewById(R.id.tag)
        val score: TextView = view.findViewById(R.id.score)
        val rank: TextView = view.findViewById(R.id.rank)
        val meta: TextView = view.findViewById(R.id.meta)
        val plays: TextView = view.findViewById(R.id.plays)
    }
}
