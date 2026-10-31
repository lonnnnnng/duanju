package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.Fragment
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.annotation.OptIn
import androidx.media3.common.util.UnstableApi

class HistoryFragment : Fragment() {

    private lateinit var adapter: HistoryAdapter

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_history, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val empty = view.findViewById<View>(R.id.history_empty)
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
        view?.let { refresh(it.findViewById(R.id.history_empty)) }
    }

    /** 底部 tab 用 hide/show 切换，不触发 onResume，这里补刷新。 */
    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (!hidden) view?.let { refresh(it.findViewById(R.id.history_empty)) }
    }

    private fun refresh(empty: View) {
        val entries = HistoryStore.list(requireContext())
        adapter.submit(entries)
        empty.visibility = if (entries.isEmpty()) View.VISIBLE else View.GONE
    }

    @OptIn(UnstableApi::class)
    private fun openPlayer(entry: HistoryEntry) {
        startActivity(
            Intent(requireContext(), PlayerActivity::class.java)
                .putExtra("dramaId", entry.id)
                .putExtra("title", entry.title)
                .putExtra("episodes", entry.episodes)
                .putExtra("cover", entry.cover)
        )
    }

    private fun confirmDelete(entry: HistoryEntry) {
        AlertDialog.Builder(requireContext())
            .setTitle("删除播放记录")
            .setMessage("确定删除《${entry.title}》的播放记录吗？")
            .setNegativeButton("取消", null)
            .setPositiveButton("删除") { _, _ ->
                HistoryStore.remove(requireContext(), entry.id)
                view?.let { refresh(it.findViewById(R.id.history_empty)) }
            }
            .show()
    }
}
