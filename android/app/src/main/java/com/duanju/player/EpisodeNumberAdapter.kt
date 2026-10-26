package com.duanju.player

import android.graphics.drawable.GradientDrawable
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient.SeriesEpisode

/** 选集数字格（6 列）。 */
class EpisodeNumberAdapter(
    private val onPick: (SeriesEpisode) -> Unit
) : RecyclerView.Adapter<EpisodeNumberAdapter.Holder>() {

    private val items = ArrayList<SeriesEpisode>()

    fun submit(list: List<SeriesEpisode>) {
        items.clear()
        items.addAll(list)
        notifyDataSetChanged()
    }

    fun indexOf(position: Int): SeriesEpisode? = items.getOrNull(position)

    override fun getItemCount(): Int = items.size

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder =
        Holder(LayoutInflater.from(parent.context).inflate(R.layout.item_episode_number, parent, false))

    override fun onBindViewHolder(holder: Holder, position: Int) {
        val ep = items[position]
        holder.num.text = ep.index.toString()
        val ctx = holder.itemView.context
        val density = ctx.resources.displayMetrics.density
        val bg = GradientDrawable().apply {
            cornerRadius = 8 * density
            setColor(
                when {
                    ep.disabled -> 0x14FFFFFF
                    else -> resolve(context = ctx, res = R.color.app_surface_variant)
                }
            )
        }
        holder.itemView.background = bg
        holder.itemView.alpha = if (ep.disabled) 0.45f else 1f
        holder.itemView.setOnClickListener { if (!ep.disabled) onPick(ep) }
    }

    private fun resolve(context: android.content.Context, res: Int): Int =
        androidx.core.content.ContextCompat.getColor(context, res)

    class Holder(view: View) : RecyclerView.ViewHolder(view) {
        val num: TextView = view.findViewById(R.id.num)
    }
}
