package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.SeriesAggregator

/**
 * 「相关」列表：同一部剧的各季（紧凑单行）。
 * 当前正在看的这一季高亮并标注「当前」，未上线（预约剧壳）标注「待上线」。
 */
class RelatedAdapter(
    private val currentId: String,
    private val onPick: (SeriesAggregator.Season) -> Unit
) : RecyclerView.Adapter<RelatedAdapter.Holder>() {

    private val items = ArrayList<SeriesAggregator.Season>()

    fun submit(list: List<SeriesAggregator.Season>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    override fun getItemCount(): Int = items.size

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_related_season, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val season = items[position]
        val context = holder.itemView.context
        val current = season.id == currentId
        holder.title.text = season.title
        holder.title.setTextColor(
            ContextCompat.getColor(
                context,
                if (current) R.color.accent else R.color.app_on_bg
            )
        )
        holder.meta.text = when {
            !season.playable -> "未上线"
            season.episodes > 0 -> "全${season.episodes}集"
            else -> season.type
        }
        if (!season.cover.isNullOrBlank()) BitmapLoader.load(season.cover, holder.cover)
        holder.badge.visibility = if (current) View.VISIBLE else View.GONE
        holder.badge.text = "当前"
        holder.itemView.setOnClickListener { onPick(season) }
    }

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val cover: ImageView = view.findViewById(R.id.related_cover)
        val title: TextView = view.findViewById(R.id.related_title)
        val meta: TextView = view.findViewById(R.id.related_meta)
        val badge: TextView = view.findViewById(R.id.related_badge)
    }
}
