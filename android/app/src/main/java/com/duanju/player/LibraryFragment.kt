package com.duanju.player

import android.content.Context
import android.content.Intent
import android.graphics.drawable.InsetDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.fragment.app.Fragment
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import com.duanju.player.core.DirectClient
import com.google.android.material.bottomsheet.BottomSheetDialog
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
    private lateinit var filterBar: View
    private lateinit var filterEntry: TextView
    private lateinit var filterChips: LinearLayout
    private lateinit var paginationStatus: View
    private lateinit var paginationProgress: ProgressBar
    private lateinit var paginationText: TextView

    private var offset = 0
    private var sessionId = ""
    private var hasMore = true
    private var loading = false
    private var paginationFailed = false
    private var retryReset = false
    private var selectedFilter: String? = null
    private var filterGroups: List<DirectClient.HomeFilterGroup> = emptyList()
    private var quickFilters: List<DirectClient.HomeFilter> = emptyList()
    private var selectedFilterName: String? = null
    private var selectedFilterGroup: String? = null
    private var filterSheet: BottomSheetDialog? = null
    /** 加载期间又发起了 reset 请求（如切筛选）时置位，在途回复结束后补发。 */
    private var pendingReset = false

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    internal var homePageLoader: (Context, Int, String, String?) -> DirectClient.HomePage = { context, pageOffset, pageSessionId, filter ->
        val (deviceId, installId) = DirectClient.identity(context)
        DirectClient.homeTab(8, pageOffset, pageSessionId, filter, deviceId, installId)
    }

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        inflater.inflate(R.layout.fragment_library, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        if (savedInstanceState != null) {
            selectedFilter = savedInstanceState.getString("filter")
            selectedFilterName = savedInstanceState.getString("filterName")
            selectedFilterGroup = savedInstanceState.getString("filterGroup")
        }
        swipe = view.findViewById(R.id.swipe)
        grid = view.findViewById(R.id.home_grid)
        loadingView = view.findViewById(R.id.home_loading)
        errorBox = view.findViewById(R.id.home_error)
        errorText = view.findViewById(R.id.home_error_text)
        emptyView = view.findViewById(R.id.home_empty)
        filterBar = view.findViewById(R.id.filter_bar)
        filterEntry = view.findViewById(R.id.filter_entry)
        filterEntry.setOnClickListener { showFilterSheet() }
        filterChips = view.findViewById(R.id.filter_chips)
        paginationStatus = view.findViewById(R.id.home_pagination_status)
        paginationProgress = view.findViewById(R.id.home_pagination_progress)
        paginationText = view.findViewById(R.id.home_pagination_text)
        paginationStatus.setOnClickListener {
            if (paginationFailed) load(reset = retryReset)
        }

        adapter = HomeAdapter { drama -> openDetail(drama) }
        adapter.setShowRank(selectedFilter == "ranklist_hot")
        val columns = if (resources.configuration.screenWidthDp < 360 || resources.configuration.fontScale > 1.2f) 2 else 3
        val layoutManager = GridLayoutManager(requireContext(), columns)
        layoutManager.spanSizeLookup = object : GridLayoutManager.SpanSizeLookup() {
            override fun getSpanSize(position: Int): Int = if (adapter.isFooter(position)) columns else 1
        }
        grid.layoutManager = layoutManager
        grid.adapter = adapter
        swipe.setColorSchemeResources(R.color.accent)

        view.findViewById<View>(R.id.search_pill).setOnClickListener {
            startActivity(Intent(requireContext(), SearchActivity::class.java))
        }
        view.findViewById<Button>(R.id.home_retry).setOnClickListener { load(reset = true) }

        swipe.setOnRefreshListener {
            load(reset = true)
        }
        grid.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(recyclerView: RecyclerView, dx: Int, dy: Int) {
                if (dy > 0 && hasMore && !loading && !paginationFailed && !recyclerView.canScrollVertically(1)) {
                    load(reset = false)
                }
            }
        })

        // long: 首页视图首次创建时拉取数据；Tab 隐藏和前后台切换会复用此视图，因此不在恢复回调重复请求。
        load(reset = true)
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
    private fun postUi(owner: View, block: () -> Unit) {
        main.post {
            if (!isAdded || view !== owner) return@post
            block()
        }
    }

    private fun load(reset: Boolean) {
        if (loading) {
            if (reset) pendingReset = true
            return
        }
        val owner = view ?: return
        val context = requireContext().applicationContext
        // long: 刷新成功前保留旧游标，失败后继续翻页不能从第一页重复追加。
        val requestOffset = if (reset) 0 else offset
        val requestSession = if (reset) "" else sessionId
        val requestFilter = selectedFilter
        loading = true
        adapter.setFooter(false)
        renderPagination(loading = !reset)
        if (reset) {
            showLoading(adapter.size() == 0)
        }
        errorBox.visibility = View.GONE
        executor.execute {
            try {
                val page = homePageLoader(context, requestOffset, requestSession, requestFilter)
                postUi(owner) {
                    loading = false
                    swipe.isRefreshing = false
                    showLoading(false)
                    renderPagination(loading = false)
                    if (pendingReset) {
                        // 在途回复已过时（期间切了筛选/下拉刷新），直接补发新请求
                        pendingReset = false
                        load(reset = true)
                        return@postUi
                    }
                    offset = page.nextOffset
                    sessionId = page.sessionId
                    hasMore = page.hasMore
                    if (reset) adapter.submit(page.items) else adapter.append(page.items)
                    adapter.setFooter(!hasMore && adapter.size() > 0)
                    if (adapter.size() == 0) {
                        emptyView.visibility = View.VISIBLE
                    } else {
                        emptyView.visibility = View.GONE
                    }
                    filterGroups = if (page.filterGroups.isNotEmpty() || reset) page.filterGroups else filterGroups
                    if (reset) renderFilters(page.filters) else renderFilterEntry()
                }
            } catch (e: Exception) {
                postUi(owner) {
                    loading = false
                    swipe.isRefreshing = false
                    showLoading(false)
                    renderPagination(loading = false)
                    if (pendingReset) {
                        pendingReset = false
                        load(reset = true)
                        return@postUi
                    }
                    adapter.setFooter(!hasMore && adapter.size() > 0)
                    if (adapter.size() == 0) {
                        errorText.text = e.message ?: "加载失败"
                        errorBox.visibility = View.VISIBLE
                    } else {
                        // long: 分页失败保留已加载卡片和游标，等待用户点击重试，避免滚动回调反复请求。
                        retryReset = reset
                        renderPagination(loading = false, failed = true)
                    }
                }
            }
        }
    }

    private fun renderPagination(loading: Boolean, failed: Boolean = false) {
        paginationFailed = failed
        paginationStatus.visibility = if (loading || failed) View.VISIBLE else View.GONE
        paginationStatus.isClickable = failed
        paginationStatus.isFocusable = failed
        paginationProgress.visibility = if (loading) View.VISIBLE else View.GONE
        paginationText.text = if (failed) {
            if (retryReset) "刷新失败，点击重试" else "加载失败，点击重试"
        } else "正在加载更多…"
    }

    private fun showLoading(show: Boolean) {
        loadingView.visibility = if (show) View.VISIBLE else View.GONE
        if (show) {
            emptyView.visibility = View.GONE
            errorBox.visibility = View.GONE
        }
    }

    private fun renderFilters(filters: List<DirectClient.HomeFilter>) {
        quickFilters = filters.distinctBy { it.id }
        filterChips.removeAllViews()
        addChip("推荐", null)
        quickFilters.forEach { addChip(it.name, it.id) }
        renderFilterEntry()
    }

    private fun panelGroups(): List<DirectClient.HomeFilterGroup> = buildList {
        // long: 现代、都市等项目前仅出现在快捷行，面板也保留它们，不能只展示 inner_rows。
        if (quickFilters.isNotEmpty()) add(DirectClient.HomeFilterGroup("快捷分类", quickFilters))
        addAll(filterGroups)
    }

    private fun renderFilterEntry() {
        val hasOptions = panelGroups().any { it.items.isNotEmpty() }
        // long: 上游仅返回内部分组时仍能筛选；已选项暂时消失时也允许清除，防止困在旧条件中。
        filterBar.visibility = if (hasOptions || selectedFilter != null) View.VISIBLE else View.GONE
        filterEntry.isEnabled = hasOptions || selectedFilter != null
        val current = selectedFilter?.let { id ->
            panelGroups().flatMap { it.items }.firstOrNull { it.id == id }?.name
        }
        if (current != null) selectedFilterName = current
        filterEntry.text = selectedFilterName?.let { "筛选 · $it" } ?: "筛选"
        filterEntry.contentDescription = selectedFilterName?.let { "筛选，当前：$it" } ?: "打开全部分类"
        styleChip(filterEntry, selectedFilter != null)
    }

    /** long: 分组只负责导航，点选任意分类都会替换全局选择，保持已验证的单选语义。 */
    private fun showFilterSheet() {
        if (filterSheet?.isShowing == true) return
        val groups = panelGroups()
        if (groups.isEmpty() && selectedFilter == null) return
        val sheet = BottomSheetDialog(requireContext())
        filterSheet = sheet
        sheet.setOnDismissListener { if (filterSheet === sheet) filterSheet = null }
        val content = layoutInflater.inflate(R.layout.sheet_filters, null)
        val groupBox = content.findViewById<LinearLayout>(R.id.filter_groups)
        val itemBox = content.findViewById<LinearLayout>(R.id.filter_items)
        val itemScroll = content.findViewById<ScrollView>(R.id.filter_item_scroll)
        val groupScroll = content.findViewById<ScrollView>(R.id.filter_group_scroll)
        content.findViewById<TextView>(R.id.filter_current).text = getString(R.string.home_filter_current, selectedFilterName ?: "推荐")
        // long: 为标题、当前条件和系统栏预留空间，小屏及大字体时仍能滚动访问所有分类。
        val metrics = resources.displayMetrics
        content.findViewById<View>(R.id.filter_body).layoutParams = content.findViewById<View>(R.id.filter_body).layoutParams.apply {
            height = minOf(dp(360), (metrics.heightPixels - dp(220)).coerceAtLeast(dp(120)))
        }

        content.findViewById<View>(R.id.filter_reset).setOnClickListener {
            sheet.dismiss()
            applyFilter(null, null, null)
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
            val columns = if (resources.configuration.screenWidthDp < 360 || resources.configuration.fontScale > 1.2f) 2 else 3
            group.items.chunked(columns).forEach { chunk ->
                val row = LinearLayout(requireContext()).apply {
                    orientation = LinearLayout.HORIZONTAL
                }
                chunk.forEach { item ->
                    val selected = item.id == selectedFilter
                    val chip = TextView(requireContext()).apply {
                        text = item.name
                        textSize = 13f
                        gravity = Gravity.CENTER
                        maxLines = 2
                        minHeight = dp(48)
                        isSelected = selected
                        tag = item.id
                        contentDescription = item.name + if (selected) "，已选中" else ""
                        isClickable = true
                        isFocusable = true
                        setPadding(dp(6), dp(9), dp(6), dp(9))
                        compactBackground(this, R.drawable.bg_home_filter_option, 6)
                        setTextColor(filterTextColor(selected))
                        setOnClickListener {
                            sheet.dismiss()
                            applyFilter(item.id, item.name, group.name)
                        }
                    }
                    val params = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
                    params.marginEnd = dp(6)
                    params.bottomMargin = dp(2)
                    row.addView(chip, params)
                }
                repeat(columns - chunk.size) {
                    val filler = View(requireContext())
                    val params = LinearLayout.LayoutParams(0, 1, 1f)
                    params.marginEnd = dp(6)
                    row.addView(filler, params)
                }
                itemBox.addView(row)
            }
            itemScroll.scrollTo(0, 0)
            // long: 长分组中的已选项可能在屏幕下方，重开时不仅定位分组，也把具体选项滚入视野。
            val selectedView = selectedFilter?.let { itemBox.findViewWithTag<View>(it) }
            selectedView?.post {
                selectedView.requestRectangleOnScreen(android.graphics.Rect(0, 0, selectedView.width, selectedView.height), false)
            }
        }

        fun selectGroup(index: Int) {
            for (i in 0 until groupBox.childCount) {
                val tab = groupBox.getChildAt(i) as TextView
                val active = i == index
                tab.isSelected = active
                tab.setTextColor(filterTextColor(active))
                tab.setBackgroundResource(R.drawable.bg_home_filter_group)
                tab.setTypeface(null, if (active) android.graphics.Typeface.BOLD else android.graphics.Typeface.NORMAL)
            }
            renderItems(groups[index])
        }

        groups.forEachIndexed { index, group ->
            val tab = TextView(requireContext()).apply {
                text = group.name
                textSize = 13f
                gravity = Gravity.CENTER_VERTICAL
                minHeight = dp(48)
                setPadding(dp(12), dp(12), dp(12), dp(12))
                isClickable = true
                isFocusable = true
                setOnClickListener { selectGroup(index) }
            }
            groupBox.addView(tab)
        }

        val initial = groups.indexOfFirst { it.name == selectedFilterGroup && it.items.any { item -> item.id == selectedFilter } }
            .takeIf { it >= 0 }
            ?: groups.indexOfFirst { it.items.any { item -> item.id == selectedFilter } }.coerceAtLeast(0)
        if (groups.isNotEmpty()) {
            selectGroup(initial)
            groupBox.getChildAt(initial).post { groupScroll.smoothScrollTo(0, groupBox.getChildAt(initial).top) }
        }
        sheet.setContentView(content)
        sheet.show()
        sheet.behavior.state = com.google.android.material.bottomsheet.BottomSheetBehavior.STATE_EXPANDED
        val container = sheet.findViewById<View>(com.google.android.material.R.id.design_bottom_sheet)
        container?.post {
            // long: 圆角由弹层容器统一绘制并裁切；内容不再叠一张圆角背景，避免方形底色和阴影露在角外。
            container.setBackgroundResource(R.drawable.bg_sheet_top)
            container.clipToOutline = true
            container.elevation = 0f
        }
    }

    private fun applyFilter(id: String?, name: String?, group: String? = null) {
        if (id == selectedFilter) return
        selectedFilter = id
        selectedFilterName = name
        selectedFilterGroup = group
        for (i in 0 until filterChips.childCount) {
            val chip = filterChips.getChildAt(i) as TextView
            styleChip(chip, chip.tag == (id ?: ""))
        }
        renderFilterEntry()

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
            minHeight = dp(48)
            setPadding(dp(14), dp(6), dp(14), dp(6))
            isClickable = true
            isFocusable = true
            setOnClickListener { applyFilter(id, if (id == null) null else label) }
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

    private fun styleChip(chip: TextView, selected: Boolean) {
        chip.isSelected = selected
        chip.setTextColor(filterTextColor(selected))
        compactBackground(chip, R.drawable.bg_home_filter_chip, 8)
    }

    private fun compactBackground(view: View, drawable: Int, inset: Int) {
        // long: 48dp 只用于手指命中，顶部胶囊保持约 32dp、面板选项约 36dp 的视觉高度。
        // 背景内缩后恢复原文字内边距，避免 InsetDrawable 自动改变排版而再次撑大按钮。
        val padding = intArrayOf(view.paddingLeft, view.paddingTop, view.paddingRight, view.paddingBottom)
        view.background = InsetDrawable(ContextCompat.getDrawable(requireContext(), drawable)!!.mutate(), 0, dp(inset), 0, dp(inset))
        view.setPadding(padding[0], padding[1], padding[2], padding[3])
    }

    private fun resolveChipColor(): Int =
        filterTextColor(false)

    private fun filterTextColor(selected: Boolean): Int = ContextCompat.getColor(requireContext(),
        if (selected) R.color.home_filter_selected_foreground else R.color.home_filter_foreground)

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    override fun onDestroyView() {
        filterSheet?.dismiss()
        filterSheet = null
        loading = false
        pendingReset = false
        super.onDestroyView()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString("filter", selectedFilter)
        outState.putString("filterName", selectedFilterName)
        outState.putString("filterGroup", selectedFilterGroup)
    }

    override fun onDestroy() {
        executor.shutdownNow()
        main.removeCallbacksAndMessages(null)
        super.onDestroy()
    }
}
