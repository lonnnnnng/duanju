package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView

class EpisodeAdapter(
    private val count: Int,
    private val onPick: (Int) -> Unit
) : RecyclerView.Adapter<EpisodeAdapter.Holder>() {

    private var current = 1

    fun setSelected(episode: Int) {
        if (current != episode) {
            current = episode
            notifyDataSetChanged()
        }
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_episode, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val episode = position + 1
        holder.number.text = episode.toString()
        holder.itemView.isSelected = episode == current
        holder.number.setTextColor(if (episode == current) 0xFFFFFFFF.toInt() else 0xFF333333.toInt())
        holder.itemView.setOnClickListener { onPick(episode) }
    }

    override fun getItemCount(): Int = count

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val number: TextView = view.findViewById(R.id.episode_number)
    }
}