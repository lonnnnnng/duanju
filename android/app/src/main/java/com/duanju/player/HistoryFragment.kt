package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView

class HistoryFragment : Fragment() {

    private lateinit var adapter: HistoryAdapter

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_history, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val empty = view.findViewById<TextView>(R.id.empty)
        val list = view.findViewById<RecyclerView>(R.id.history_list)
        list.layoutManager = LinearLayoutManager(requireContext())
        adapter = HistoryAdapter(
            onPick = { entry -> openPlayer(entry) },
            onDelete = { entry -> confirmDelete(entry) }
        )
        list.adapter = adapter
        refresh(empty)
    }

    override fun onResume() {
        super.onResume()
        view?.let { refresh(it.findViewById(R.id.empty)) }
    }

    private fun refresh(empty: TextView) {
        val entries = HistoryStore.list(requireContext())
        adapter.submit(entries)
        empty.visibility = if (entries.isEmpty()) View.VISIBLE else View.GONE
    }

    private fun openPlayer(entry: HistoryEntry) {
        startActivity(
            Intent(requireContext(), PlayerActivity::class.java)
                .putExtra("base", AppSettings.base(requireContext()))
                .putExtra("dramaId", entry.id)
                .putExtra("title", entry.title)
                .putExtra("episodes", entry.episodes)
        )
    }

    private fun confirmDelete(entry: HistoryEntry) {
        AlertDialog.Builder(requireContext())
            .setTitle("删除播放记录")
            .setMessage("确定删除《${entry.title}》的播放记录吗？")
            .setNegativeButton("取消", null)
            .setPositiveButton("删除") { _, _ ->
                HistoryStore.remove(requireContext(), entry.id)
                view?.let { refresh(it.findViewById(R.id.empty)) }
            }
            .show()
    }
}