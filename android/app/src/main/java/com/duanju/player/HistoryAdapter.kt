package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
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
        val sub = if (entry.episodes > 0 && entry.episode in 1..entry.episodes) "看到第${entry.episode}集 / 共${entry.episodes}集" else "看到第${entry.episode}集"
        holder.subtitle.text = sub
        holder.itemView.setOnClickListener { onPick(entry) }
        holder.itemView.setOnLongClickListener {
            onDelete(entry)
            true
        }
    }

    override fun getItemCount(): Int = items.size

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val title: TextView = view.findViewById(R.id.title)
        val subtitle: TextView = view.findViewById(R.id.subtitle)
    }
}