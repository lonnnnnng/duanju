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
import com.duanju.player.core.DirectClient
import java.util.concurrent.Executors

class LibraryFragment : Fragment() {

    private lateinit var searchField: EditText
    private lateinit var statusView: TextView
    private lateinit var swipe: SwipeRefreshLayout
    private lateinit var listView: RecyclerView
    private lateinit var adapter: DramaAdapter

    private var all: MutableList<Drama> = mutableListOf()
    private var catalogOffset = 0
    private var catalogSession = ""
    private var catalogHasMore = true
    private var loading = false

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_library, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        searchField = view.findViewById(R.id.search)
        statusView = view.findViewById(R.id.status)
        swipe = view.findViewById(R.id.swipe)
        listView = view.findViewById(R.id.library_list)

        adapter = DramaAdapter("") { drama -> openPlayer(drama) }
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

        swipe.setOnRefreshListener { loadFirstPage() }
        listView.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(recyclerView: RecyclerView, dx: Int, dy: Int) {
                if (dy > 0 && catalogHasMore && !loading && !recyclerView.canScrollVertically(1)) {
                    loadMore()
                }
            }
        })

        if (all.isEmpty()) loadFirstPage()
    }

    private fun openPlayer(drama: Drama) {
        startActivity(
            Intent(requireContext(), PlayerActivity::class.java)
                .putExtra("dramaId", drama.id)
                .putExtra("title", drama.title)
                .putExtra("episodes", drama.episodes)
        )
    }

    private fun identity(): Pair<String, String> = DirectClient.identity(requireContext())

    private fun status(message: String) {
        statusView.text = message
    }

    private fun loadFirstPage() {
        if (loading) return
        loading = true
        catalogOffset = 0
        catalogSession = ""
        status("加载剧库…")
        executor.execute {
            try {
                val (deviceId, installId) = identity()
                var page = DirectClient.catalog("short_play", "default", 0, "", deviceId, installId)
                val dramas = page.dramas.map { it.toDrama() }.toMutableList()
                // 首屏至少拉两页，列表不至于太空
                if (page.hasMore) {
                    val page2 = DirectClient.catalog("short_play", "default", page.nextOffset, page.sessionId, deviceId, installId)
                    dramas.addAll(page2.dramas.map { it.toDrama() })
                    page = page2
                }
                val fetched = page
                main.post {
                    loading = false
                    swipe.isRefreshing = false
                    all = dedupe(dramas)
                    adapter.submit(all)
                    catalogOffset = fetched.nextOffset
                    catalogSession = fetched.sessionId
                    catalogHasMore = fetched.hasMore
                    status("共 ${all.size} 部${if (fetched.hasMore) "（上滑加载更多）" else ""}")
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

    private fun loadMore() {
        if (loading) return
        loading = true
        status("加载更多…")
        executor.execute {
            try {
                val (deviceId, installId) = identity()
                var page = DirectClient.catalog("short_play", "default", catalogOffset, catalogSession, deviceId, installId)
                // 分页偶发连接被断开（官方接口不稳定）：退避重试一次
                var attempt = 0
                while (attempt < 2) {
                    try {
                        page = DirectClient.catalog("short_play", "default", catalogOffset, catalogSession, deviceId, installId)
                        break
                    } catch (e: Exception) {
                        attempt++
                        if (attempt >= 2) throw e
                        Thread.sleep(1500)
                    }
                }
                main.post {
                    loading = false
                    val known = all.map { it.id }.toHashSet()
                    val fresh = page.dramas.map { it.toDrama() }.filter { !known.contains(it.id) }
                    all.addAll(fresh)
                    adapter.submit(all)
                    catalogOffset = page.nextOffset
                    catalogSession = page.sessionId
                    catalogHasMore = page.hasMore
                    status("共 ${all.size} 部${if (page.hasMore) "（上滑加载更多）" else "（已全部加载）"}")
                }
            } catch (e: Exception) {
                main.post {
                    loading = false
                    catalogHasMore = false
                    status("加载更多失败：${e.message}")
                }
            }
        }
    }

    private fun search() {
        val keyword = searchField.text.toString().trim()
        if (keyword.isEmpty()) {
            if (all.isNotEmpty()) return
            loadFirstPage()
            return
        }
        loading = true
        status("搜索中…")
        executor.execute {
            try {
                val list = DirectClient.searchOfficial(keyword).map { it.toDrama() }
                main.post {
                    loading = false
                    val known = all.map { it.id }.toHashSet()
                    val fresh = list.filter { !known.contains(it.id) }
                    all.addAll(0, fresh)
                    adapter.submit(all)
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

    private fun com.duanju.player.core.DirectClient.DirectDrama.toDrama() =
        Drama(id, title, episodes, coverUrl)

    private fun dedupe(list: List<Drama>): MutableList<Drama> {
        val seen = HashSet<String>()
        val out = ArrayList<Drama>(list.size)
        for (d in list) {
            if (!seen.contains(d.id)) {
                seen.add(d.id)
                out.add(d)
            }
        }
        return out
    }
}