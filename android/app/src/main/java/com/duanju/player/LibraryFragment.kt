package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.HorizontalScrollView
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView
import androidx.fragment.app.Fragment
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import com.duanju.player.core.DirectClient
import java.util.concurrent.Executors

/**
 * 首页：红果风格信息流（找剧 tab）。
 * 接口 bookmall/tab（无签名），三层结构展开后 3 列网格展示，上滑加载更多。
 */
class LibraryFragment : Fragment() {

    private lateinit var swipe: SwipeRefreshLayout
    private lateinit var grid: RecyclerView
    private lateinit var adapter: HomeAdapter
    private lateinit var loadingView: ProgressBar
    private lateinit var errorBox: LinearLayout
    private lateinit var errorText: TextView
    private lateinit var emptyView: TextView
    private lateinit var filterScroll: HorizontalScrollView
    private lateinit var filterChips: LinearLayout

    private var offset = 0
    private var sessionId = ""
    private var hasMore = true
    private var loading = false
    private var selectedFilter: String? = null

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_library, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        swipe = view.findViewById(R.id.swipe)
        grid = view.findViewById(R.id.home_grid)
        loadingView = view.findViewById(R.id.home_loading)
        errorBox = view.findViewById(R.id.home_error)
        errorText = view.findViewById(R.id.home_error_text)
        emptyView = view.findViewById(R.id.home_empty)
        filterScroll = view.findViewById(R.id.filter_scroll)
        filterChips = view.findViewById(R.id.filter_chips)

        adapter = HomeAdapter { drama -> openDetail(drama) }
        val layoutManager = GridLayoutManager(requireContext(), 3)
        layoutManager.spanSizeLookup = object : GridLayoutManager.SpanSizeLookup() {
            override fun getSpanSize(position: Int): Int = if (adapter.isFooter(position)) 3 else 1
        }
        grid.layoutManager = layoutManager
        grid.adapter = adapter
        swipe.setColorSchemeResources(R.color.accent)

        view.findViewById<View>(R.id.search_pill).setOnClickListener {
            startActivity(Intent(requireContext(), SearchActivity::class.java))
        }
        view.findViewById<Button>(R.id.home_retry).setOnClickListener { load(reset = true) }

        swipe.setOnRefreshListener { load(reset = true) }
        grid.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(recyclerView: RecyclerView, dx: Int, dy: Int) {
                if (dy > 0 && hasMore && !loading && !recyclerView.canScrollVertically(1)) {
                    load(reset = false)
                }
            }
        })

        if (adapter.size() == 0) load(reset = true)
    }

    private fun openDetail(drama: DirectClient.HomeDrama) {
        startActivity(
            Intent(requireContext(), DetailActivity::class.java)
                .putExtra("dramaId", drama.id)
                .putExtra("title", drama.title)
                .putExtra("episodes", drama.episodes)
                .putExtra("cover", drama.cover.orEmpty())
        )
    }

    /** 异步结果回到主线程时，页面可能已被销毁（例如已进入详情/播放页），必须丢弃。 */
    private fun postUi(block: () -> Unit) {
        main.post {
            if (!isAdded || view == null) return@post
            block()
        }
    }

    private fun load(reset: Boolean) {
        if (loading) return
        loading = true
        if (reset) {
            offset = 0
            sessionId = ""
            hasMore = true
            showLoading(adapter.size() == 0)
        } else {
            adapter.setFooter(true, false)
        }
        errorBox.visibility = View.GONE
        executor.execute {
            try {
                val (deviceId, installId) = DirectClient.identity(requireContext())
                val page = DirectClient.homeTab(
                    8, offset, sessionId, selectedFilter, deviceId, installId
                )
                postUi {
                    loading = false
                    swipe.isRefreshing = false
                    showLoading(false)
                    offset = page.nextOffset
                    sessionId = page.sessionId
                    hasMore = page.hasMore
                    if (reset) adapter.submit(page.items) else adapter.append(page.items)
                    adapter.setFooter(false, !hasMore && adapter.size() > 0)
                    if (adapter.size() == 0) {
                        emptyView.visibility = View.VISIBLE
                    } else {
                        emptyView.visibility = View.GONE
                    }
                    if (reset) renderFilters(page.filters)
                }
            } catch (e: Exception) {
                postUi {
                    loading = false
                    swipe.isRefreshing = false
                    showLoading(false)
                    adapter.setFooter(false, false)
                    if (adapter.size() == 0) {
                        errorText.text = e.message ?: "加载失败"
                        errorBox.visibility = View.VISIBLE
                    }
                }
            }
        }
    }

    private fun showLoading(show: Boolean) {
        loadingView.visibility = if (show) View.VISIBLE else View.GONE
        if (show) {
            emptyView.visibility = View.GONE
            errorBox.visibility = View.GONE
        }
    }

    private fun renderFilters(filters: List<DirectClient.HomeFilter>) {
        filterChips.removeAllViews()
        if (filters.isEmpty()) {
            filterScroll.visibility = View.GONE
            return
        }
        filterScroll.visibility = View.VISIBLE
        addChip("推荐", null)
        filters.forEach { addChip(it.name, it.id) }
    }

    private fun addChip(label: String, id: String?) {
        val chip = TextView(requireContext()).apply {
            text = label
            textSize = 13f
            gravity = Gravity.CENTER
            setPadding(dp(14), dp(6), dp(14), dp(6))
            isClickable = true
            isFocusable = true
            setOnClickListener { selectFilter(id) }
        }
        val params = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { marginEnd = dp(8) }
        chip.layoutParams = params
        chip.tag = id ?: ""
        filterChips.addView(chip)
        styleChip(chip, id == selectedFilter)
    }

    private fun selectFilter(id: String?) {
        if (id == selectedFilter) return
        selectedFilter = id
        for (i in 0 until filterChips.childCount) {
            val chip = filterChips.getChildAt(i) as TextView
            styleChip(chip, chip.tag == (id ?: ""))
        }
        adapter.setShowRank(id == "ranklist_hot")
        adapter.clear()
        grid.scrollToPosition(0)
        load(reset = true)
    }

    private fun styleChip(chip: TextView, selected: Boolean) {
        chip.setTextColor(if (selected) 0xFFFFFFFF.toInt() else resolveChipColor())
        chip.setBackgroundResource(if (selected) R.drawable.bg_chip_selected else R.drawable.bg_chip_normal)
    }

    private fun resolveChipColor(): Int {
        val attr = android.R.attr.textColorSecondary
        val typed = requireContext().theme.obtainStyledAttributes(intArrayOf(attr))
        val color = try {
            typed.getColor(0, 0xFF606064.toInt())
        } finally {
            typed.recycle()
        }
        return color
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()
}
