package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView

class HistoryAdapter(
    private val onPick: (HistoryEntry) -> Unit,
    private val onDelete: (HistoryEntry) -> Unit
) : RecyclerView.Adapter<HistoryAdapter.Holder>() {

    private val items = mutableListOf<HistoryEntry>()

    fun submit(list: List<HistoryEntry>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_history, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val entry = items[position]
        holder.title.text = entry.title
        holder.subtitle.text = if (entry.episodes > 0 && entry.episode in 1..entry.episodes) {
            "看到第${entry.episode}集 · 共${entry.episodes}集"
        } else {
            "看到第${entry.episode}集"
        }
        holder.badge.text = "第${entry.episode}集"
        holder.updated.text = relativeTime(entry.updatedAt)

        holder.cover.setImageBitmap(null)
        if (entry.cover.isNotBlank()) {
            BitmapLoader.load(entry.cover, holder.cover)
        }
        holder.itemView.setOnClickListener { onPick(entry) }
        holder.itemView.setOnLongClickListener {
            onDelete(entry)
            true
        }
    }

    override fun getItemCount(): Int = items.size

    /** 相对时间：刚刚 / N分钟前 / N小时前 / 昨天 / M月d日 */
    private fun relativeTime(time: Long): String {
        if (time <= 0) return ""
        val diff = System.currentTimeMillis() - time
        val minute = 60_000L
        val hour = 60 * minute
        val day = 24 * hour
        return when {
            diff < minute -> "刚刚"
            diff < hour -> "${diff / minute}分钟前"
            diff < day -> "${diff / hour}小时前"
            diff < 2 * day -> "昨天"
            diff < 7 * day -> "${diff / day}天前"
            else -> {
                val cal = java.util.Calendar.getInstance().apply { timeInMillis = time }
                "${cal.get(java.util.Calendar.MONTH) + 1}月${cal.get(java.util.Calendar.DAY_OF_MONTH)}日"
            }
        }
    }

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val cover: ImageView = view.findViewById(R.id.cover)
        val badge: TextView = view.findViewById(R.id.badge)
        val title: TextView = view.findViewById(R.id.title)
        val subtitle: TextView = view.findViewById(R.id.subtitle)
        val updated: TextView = view.findViewById(R.id.updated)
    }
}
