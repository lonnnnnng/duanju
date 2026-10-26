package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.TextWatcher
import android.view.LayoutInflater
import android.view.View
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import java.util.concurrent.Executors

/**
 * 搜索页：左热门搜索（接口返回）/ 右搜索历史（本地最近 10 条），结果与首页同款 3 列卡片。
 * 只搜短剧/漫剧，不搜电影、听书、小说。
 */
class SearchActivity : AppCompatActivity() {

    private lateinit var input: EditText
    private lateinit var clearButton: ImageButton
    private lateinit var loadingView: ProgressBar
    private lateinit var emptyView: TextView
    private lateinit var grid: RecyclerView
    private lateinit var suggest: View
    private lateinit var hotBox: LinearLayout
    private lateinit var historyBox: LinearLayout
    private lateinit var historyClear: TextView
    private lateinit var adapter: HomeAdapter

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var searching = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_search)

        input = findViewById(R.id.search_input)
        clearButton = findViewById(R.id.search_clear)
        loadingView = findViewById(R.id.search_loading)
        emptyView = findViewById(R.id.search_empty)
        grid = findViewById(R.id.search_grid)
        suggest = findViewById(R.id.search_suggest)
        hotBox = findViewById(R.id.hot_words)
        historyBox = findViewById(R.id.history_words)
        historyClear = findViewById(R.id.history_clear)

        adapter = HomeAdapter { openDetail(it) }
        val layoutManager = GridLayoutManager(this, 3)
        layoutManager.spanSizeLookup = object : GridLayoutManager.SpanSizeLookup() {
            override fun getSpanSize(position: Int): Int = if (adapter.isFooter(position)) 3 else 1
        }
        grid.layoutManager = layoutManager
        grid.adapter = adapter

        findViewById<ImageButton>(R.id.search_back).setOnClickListener { finish() }
        findViewById<Button>(R.id.search_go).setOnClickListener { search() }
        clearButton.setOnClickListener { input.setText("") }
        historyClear.setOnClickListener {
            SearchHistoryStore.clear(this)
            renderHistory()
        }

        input.addTextChangedListener(object : TextWatcher {
            override fun afterTextChanged(s: Editable?) {
                clearButton.visibility = if (s.isNullOrEmpty()) View.GONE else View.VISIBLE
            }

            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) = Unit
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) = Unit
        })
        input.setOnEditorActionListener { _, actionId, _ ->
            if (actionId == EditorInfo.IME_ACTION_SEARCH) {
                search()
                true
            } else {
                false
            }
        }
        input.requestFocus()
        input.post {
            (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager)
                .showSoftInput(input, InputMethodManager.SHOW_IMPLICIT)
        }
        renderHistory()
        loadHotWords()
    }

    private fun renderHistory() {
        val history = SearchHistoryStore.load(this)
        historyBox.removeAllViews()
        historyClear.visibility = if (history.isEmpty()) View.GONE else View.VISIBLE
        history.take(10).forEach { word -> historyBox.addView(wordRow(word)) }
    }

    private fun renderHotWords(words: List<String>) {
        hotBox.removeAllViews()
        words.take(10).forEach { word -> hotBox.addView(wordRow(word)) }
    }

    private fun wordRow(word: String): TextView {
        val view = LayoutInflater.from(this).inflate(R.layout.item_word_row, hotBox, false) as TextView
        view.text = word
        view.setOnClickListener {
            input.setText(word)
            input.setSelection(word.length)
            search()
        }
        return view
    }

    private fun search() {
        val keyword = input.text.toString().trim()
        if (keyword.isEmpty() || searching) return
        (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager).hideSoftInputFromWindow(input.windowToken, 0)
        SearchHistoryStore.add(this, keyword)
        renderHistory()
        searching = true
        loadingView.visibility = View.VISIBLE
        suggest.visibility = View.GONE
        emptyView.visibility = View.GONE
        executor.execute {
            try {
                val (deviceId, installId) = DirectClient.identity(this)
                val items = DirectClient.searchHomeItems(keyword, deviceId, installId)
                main.post {
                    searching = false
                    loadingView.visibility = View.GONE
                    adapter.submit(items)
                    if (items.isEmpty()) {
                        grid.visibility = View.GONE
                        emptyView.text = "没有找到相关短剧"
                        emptyView.visibility = View.VISIBLE
                    } else {
                        grid.visibility = View.VISIBLE
                        grid.scrollToPosition(0)
                    }
                }
            } catch (e: Exception) {
                main.post {
                    searching = false
                    loadingView.visibility = View.GONE
                    grid.visibility = View.GONE
                    emptyView.text = "搜索失败：${e.message}"
                    emptyView.visibility = View.VISIBLE
                }
            }
        }
    }

    private fun loadHotWords() {
        executor.execute {
            val (deviceId, installId) = DirectClient.identity(this)
            val words = runCatching { DirectClient.hotWords(deviceId, installId) }.getOrDefault(emptyList())
            if (words.isEmpty()) return@execute
            main.post { renderHotWords(words) }
        }
    }

    private fun openDetail(drama: DirectClient.HomeDrama) {
        startActivity(
            Intent(this, DetailActivity::class.java)
                .putExtra("dramaId", drama.id)
                .putExtra("title", drama.title)
                .putExtra("episodes", drama.episodes)
                .putExtra("cover", drama.cover.orEmpty())
        )
    }

    override fun onDestroy() {
        super.onDestroy()
        executor.shutdownNow()
    }
}
