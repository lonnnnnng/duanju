package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.RecyclerView

/** 选集数字格：只渲染当前分组，选中集高亮。 */
class EpisodeAdapter(
    private val onPick: (Int) -> Unit
) : RecyclerView.Adapter<EpisodeAdapter.Holder>() {

    private var numbers: List<Int> = emptyList()
    private var current = 1

    fun submit(list: List<Int>, selected: Int) {
        numbers = list
        current = selected
        notifyDataSetChanged()
    }

    fun setSelected(episode: Int) {
        if (current == episode) return
        current = episode
        notifyDataSetChanged()
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_episode_number, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val episode = numbers[position]
        val selected = episode == current
        holder.number.text = episode.toString()
        holder.itemView.setBackgroundResource(
            if (selected) R.drawable.bg_episode_selected else R.drawable.bg_episode_cell
        )
        holder.number.setTextColor(
            if (selected) 0xFFFFFFFF.toInt()
            else ContextCompat.getColor(holder.itemView.context, R.color.app_on_bg)
        )
        holder.itemView.setOnClickListener { onPick(episode) }
    }

    override fun getItemCount(): Int = numbers.size

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val number: TextView = view.findViewById(R.id.num)
    }
}
