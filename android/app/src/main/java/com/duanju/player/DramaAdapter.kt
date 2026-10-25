package com.duanju.player

import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView

class DramaAdapter(
    @Suppress("unused") private val base: String,
    private val onClick: (Drama) -> Unit
) : RecyclerView.Adapter<DramaAdapter.Holder>() {

    private val items = mutableListOf<Drama>()

    fun submit(list: List<Drama>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder {
        val view = LayoutInflater.from(parent.context).inflate(R.layout.item_drama, parent, false)
        return Holder(view)
    }

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val drama = items[position]
        holder.title.text = drama.title
        holder.episodes.text = if (drama.episodes > 0) "共${drama.episodes}集" else ""
        val cover = drama.cover
        if (cover.isNullOrBlank()) {
            holder.cover.setImageBitmap(null)
        } else {
            BitmapLoader.load(cover, holder.cover)
        }
        holder.itemView.setOnClickListener { onClick(drama) }
    }

    override fun getItemCount(): Int = items.size

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val cover: ImageView = view.findViewById(R.id.cover)
        val title: TextView = view.findViewById(R.id.title)
        val episodes: TextView = view.findViewById(R.id.episodes)
    }
}