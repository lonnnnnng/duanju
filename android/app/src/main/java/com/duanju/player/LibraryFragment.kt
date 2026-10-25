package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.fragment.app.Fragment
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import org.json.JSONObject
import java.util.concurrent.Executors

class LibraryFragment : Fragment() {

    private lateinit var searchField: EditText
    private lateinit var statusView: TextView
    private lateinit var swipe: SwipeRefreshLayout
    private lateinit var listView: RecyclerView
    private lateinit var adapter: DramaAdapter

    private var base = "http://192.168.1.2:3333"
    private var all: List<Drama> = emptyList()
    private var hasMore = true
    private var loading = false

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_library, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        base = AppSettings.base(requireContext())
        searchField = view.findViewById(R.id.search)
        statusView = view.findViewById(R.id.status)
        swipe = view.findViewById(R.id.swipe)
        listView = view.findViewById(R.id.library_list)

        adapter = DramaAdapter(base) { drama -> openPlayer(drama) }
        listView.layoutManager = LinearLayoutManager(requireContext())
        listView.adapter = adapter

        view.findViewById<Button>(R.id.search_button).setOnClickListener { search() }
        searchField.setOnEditorActionListener { _, actionId, _ ->
            if (actionId == android.view.inputmethod.EditorInfo.IME_ACTION_SEARCH) {
                search()
                true
            } else {
                false
            }
        }

        swipe.setOnRefreshListener { loadDramas("", append = false) }
        listView.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(recyclerView: RecyclerView, dx: Int, dy: Int) {
                if (dy > 0 && hasMore && !loading && !recyclerView.canScrollVertically(1)) {
                    loadDramas("?more=1", append = true)
                }
            }
        })

        loadDramas("", append = false)
    }

    private fun status(message: String) {
        statusView.text = message
    }

    private fun openPlayer(drama: Drama) {
        startActivity(
            Intent(requireContext(), PlayerActivity::class.java)
                .putExtra("base", base)
                .putExtra("dramaId", drama.id)
                .putExtra("title", drama.title)
                .putExtra("episodes", drama.episodes)
        )
    }

    private fun search() {
        val keyword = searchField.text.toString().trim()
        if (keyword.isEmpty()) {
            loadDramas("", append = false)
            return
        }
        loading = true
        status("搜索中…")
        executor.execute {
            try {
                val text = Network.get("$base/api/ui/search?q=${java.net.URLEncoder.encode(keyword, "UTF-8")}")
                val list = DramaParser.parseList(text)
                main.post {
                    loading = false
                    all = list
                    adapter.submit(list)
                    status("搜索到 ${list.size} 部")
                }
            } catch (e: Exception) {
                main.post {
                    loading = false
                    status("搜索失败：${e.message}")
                }
            }
        }
    }

    private fun loadDramas(path: String, append: Boolean) {
        if (loading) return
        loading = true
        if (!append) {
            status("加载剧库…")
        }
        executor.execute {
            try {
                val result = pollDramas("$base/api/ui/dramas$path")
                val list = DramaParser.parseList(result.toString())
                main.post {
                    loading = false
                    swipe.isRefreshing = false
                    hasMore = result.optBoolean("hasMore", false)
                    if (append) {
                        val known = all.map { it.id }.toHashSet()
                        val merged = ArrayList(all)
                        list.forEach { if (!known.contains(it.id)) merged.add(it) }
                        all = merged
                        adapter.submit(merged)
                    } else {
                        all = list
                        adapter.submit(list)
                    }
                    status("共 ${all.size} 部")
                }
            } catch (e: Exception) {
                main.post {
                    loading = false
                    swipe.isRefreshing = false
                    status("加载失败：${e.message}")
                }
            }
        }
    }

    private fun pollDramas(url: String): JSONObject {
        var result = JSONObject(Network.get(url))
        var polls = 0
        while (result.optBoolean("loading", false) && polls < 60) {
            Thread.sleep(1200)
            val revision = result.optLong("revision", 0)
            result = JSONObject(Network.get("$base/api/ui/dramas?revision=$revision"))
            polls++
        }
        return result
    }
}