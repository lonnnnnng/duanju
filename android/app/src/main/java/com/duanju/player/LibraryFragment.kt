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
import androidx.core.content.ContextCompat
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
    private var filterGroups: List<DirectClient.HomeFilterGroup> = emptyList()
    private var selectedFilterName: String? = null

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
                    filterGroups = page.filterGroups
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
        addFilterEntry()
        addChip("推荐", null)
        filters.forEach { addChip(it.name, it.id) }
    }

    /** 末尾的「筛选」入口：展开完整分组筛选面板。 */
    private fun addFilterEntry() {
        val chip = TextView(requireContext()).apply {
            text = selectedFilterName ?: "筛选"
            textSize = 13f
            gravity = Gravity.CENTER
            setPadding(dp(14), dp(6), dp(14), dp(6))
            isClickable = true
            isFocusable = true
            setOnClickListener { showFilterSheet() }
        }
        chip.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { marginEnd = dp(8) }
        chip.tag = "__sheet__"
        filterChips.addView(chip)
        styleChip(chip, selectedFilterName != null)
    }

    /** 两级筛选：左侧分类，右侧该分类下的选项。 */
    private fun showFilterSheet() {
        val groups = filterGroups.filter { it.name != "综合" }
        if (groups.isEmpty()) return
        val sheet = com.google.android.material.bottomsheet.BottomSheetDialog(requireContext())
        val content = layoutInflater.inflate(R.layout.sheet_filters, null)
        val groupBox = content.findViewById<LinearLayout>(R.id.filter_groups)
        val itemBox = content.findViewById<LinearLayout>(R.id.filter_items)
        // 面板高度按可用屏幕计算，横屏/小屏也不会被裁掉
        val metrics = resources.displayMetrics
        content.findViewById<View>(R.id.filter_body).layoutParams = content.findViewById<View>(R.id.filter_body).layoutParams.apply {
            height = minOf(dp(360), (metrics.heightPixels * 0.62f).toInt())
        }

        content.findViewById<View>(R.id.filter_reset).setOnClickListener {
            sheet.dismiss()
            applyFilter(null, null)
        }

        fun renderItems(group: DirectClient.HomeFilterGroup) {
            itemBox.removeAllViews()
            val title = TextView(requireContext()).apply {
                text = group.name
                textSize = 12f
                setTextColor(resolveChipColor())
                setPadding(dp(4), 0, 0, dp(10))
            }
            itemBox.addView(title)
            group.items.chunked(3).forEach { chunk ->
                val row = LinearLayout(requireContext()).apply {
                    orientation = LinearLayout.HORIZONTAL
                }
                chunk.forEach { item ->
                    val selected = item.id == selectedFilter
                    val chip = TextView(requireContext()).apply {
                        text = item.name
                        textSize = 13f
                        gravity = Gravity.CENTER
                        maxLines = 1
                        ellipsize = android.text.TextUtils.TruncateAt.END
                        isClickable = true
                        isFocusable = true
                        setPadding(dp(6), dp(9), dp(6), dp(9))
                        setBackgroundResource(if (selected) R.drawable.bg_chip_selected else R.drawable.bg_fopt)
                        setTextColor(if (selected) 0xFFFFFFFF.toInt() else resolveChipColor())
                        setOnClickListener {
                            sheet.dismiss()
                            applyFilter(item.id, item.name)
                        }
                    }
                    val params = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
                    params.marginEnd = dp(6)
                    params.bottomMargin = dp(8)
                    row.addView(chip, params)
                }
                repeat(3 - chunk.size) {
                    val filler = View(requireContext())
                    val params = LinearLayout.LayoutParams(0, 1, 1f)
                    params.marginEnd = dp(6)
                    row.addView(filler, params)
                }
                itemBox.addView(row)
            }
        }

        fun selectGroup(index: Int) {
            for (i in 0 until groupBox.childCount) {
                val tab = groupBox.getChildAt(i) as TextView
                val active = i == index
                tab.setTextColor(if (active) 0xFFFFFFFF.toInt() else resolveChipColor())
                tab.background = if (active) {
                    ContextCompat.getDrawable(requireContext(), R.drawable.bg_filter_group_active)?.mutate()
                } else {
                    null
                }
                tab.setTypeface(null, if (active) android.graphics.Typeface.BOLD else android.graphics.Typeface.NORMAL)
            }
            renderItems(groups[index])
        }

        var initial = 0
        groups.forEachIndexed { index, group ->
            val tab = TextView(requireContext()).apply {
                text = group.name
                textSize = 13f
                gravity = Gravity.CENTER_VERTICAL
                setPadding(dp(12), dp(12), dp(12), dp(12))
                isClickable = true
                setOnClickListener { selectGroup(index) }
            }
            groupBox.addView(tab)
            if (group.items.any { it.id == selectedFilter }) initial = index
        }

        content.findViewById<View>(R.id.filter_items).post { selectGroup(initial) }
        selectGroup(initial)
        sheet.setContentView(content)
        sheet.show()
    }

    private fun applyFilter(id: String?, name: String?) {
        if (id == selectedFilter) return
        selectedFilter = id
        selectedFilterName = name
        for (i in 0 until filterChips.childCount) {
            val chip = filterChips.getChildAt(i) as TextView
            styleChip(chip, chip.tag == (id ?: ""))
        }
        // 「筛选」入口文案随选择变化
        val entry = (0 until filterChips.childCount)
            .map { filterChips.getChildAt(it) as TextView }
            .firstOrNull { it.tag == "__sheet__" }
        if (entry != null) {
            entry.text = name ?: "筛选"
            styleChip(entry, name != null)
        }

        adapter.setShowRank(id == "ranklist_hot")
        adapter.clear()
        grid.scrollToPosition(0)
        load(reset = true)
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
        applyFilter(id, null)
    }

    private fun styleChip(chip: TextView, selected: Boolean) {
        chip.setTextColor(if (selected) 0xFFFFFFFF.toInt() else resolveChipColor())
        chip.setBackgroundResource(if (selected) R.drawable.bg_chip_selected else R.drawable.bg_chip_normal)
    }

    private fun resolveChipColor(): Int =
        androidx.core.content.ContextCompat.getColor(requireContext(), R.color.app_on_surface_variant)

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()
}
