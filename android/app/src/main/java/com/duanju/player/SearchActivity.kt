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
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.duanju.player.core.DirectClient
import com.duanju.player.core.TaskCancellation
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * 搜索页：空态展示搜索历史（本地最近 10 条），结果以列表展示。
 * 只搜短剧/漫剧，不搜电影、听书、小说。
 */
class SearchActivity : AppCompatActivity() {

    private lateinit var input: EditText
    private lateinit var clearButton: ImageButton
    private lateinit var loadingView: ProgressBar
    private lateinit var emptyView: TextView
    private lateinit var resultList: RecyclerView
    private lateinit var suggest: View
    private lateinit var historyBox: com.google.android.flexbox.FlexboxLayout
    private lateinit var historyClear: TextView
    private lateinit var historyEmpty: TextView
    private lateinit var adapter: SearchResultAdapter

    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var searching = false
    private var searchCancellation: TaskCancellation? = null
    private var searchFuture: Future<*>? = null
    private var searchKeyword = ""
    internal var searchItems: (String, TaskCancellation) -> List<DirectClient.HomeDrama> = { keyword, cancellation ->
        val (deviceId, installId) = DirectClient.identity(this)
        DirectClient.searchHomeItems(keyword, deviceId, installId, cancellation)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_search)

        input = findViewById(R.id.search_input)
        clearButton = findViewById(R.id.search_clear)
        loadingView = findViewById(R.id.search_loading)
        emptyView = findViewById(R.id.search_empty)
        resultList = findViewById(R.id.search_list)
        suggest = findViewById(R.id.search_suggest)
        historyBox = findViewById(R.id.history_words)
        historyClear = findViewById(R.id.history_clear)
        historyEmpty = findViewById(R.id.history_empty)

        adapter = SearchResultAdapter { openDetail(it) }
        resultList.layoutManager = LinearLayoutManager(this)
        resultList.adapter = adapter

        findViewById<ImageButton>(R.id.search_back).setOnClickListener { finish() }
        findViewById<Button>(R.id.search_go).setOnClickListener { search() }
        clearButton.setOnClickListener { input.setText("") }
        historyClear.setOnClickListener {
            SearchHistoryStore.clear(this)
            renderHistory()
        }

        input.addTextChangedListener(object : TextWatcher {
            override fun afterTextChanged(s: Editable?) {
                val empty = s.isNullOrEmpty()
                clearButton.visibility = if (empty) View.GONE else View.VISIBLE
                // long: 修改关键词即使尚未提交，也不能让旧关键词的回复重新填回结果列表。
                if (s.toString().trim() != searchKeyword) {
                    searchCancellation?.cancel()
                    searchFuture?.cancel(true)
                    searching = false
                    loadingView.visibility = View.GONE
                    adapter.submit(emptyList())
                    resultList.visibility = View.GONE
                    emptyView.visibility = View.GONE
                    suggest.visibility = View.VISIBLE
                }
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
    }

    private fun renderHistory() {
        val history = SearchHistoryStore.load(this)
        historyBox.removeAllViews()
        historyClear.visibility = if (history.isEmpty()) View.GONE else View.VISIBLE
        historyEmpty.visibility = if (history.isEmpty()) View.VISIBLE else View.GONE
        history.take(10).forEach { word -> historyBox.addView(wordRow(word)) }
    }

    private fun wordRow(word: String): View {
        val view = LayoutInflater.from(this).inflate(R.layout.item_search_history_word, historyBox, false)
        (view.layoutParams as? com.google.android.flexbox.FlexboxLayout.LayoutParams)
            ?.setMargins(0, 0, dp(9), dp(9))
        view.findViewById<TextView>(R.id.word).text = word
        view.setOnClickListener {
            input.setText(word)
            input.setSelection(word.length)
            search()
        }
        return view
    }

    private fun search() {
        val keyword = input.text.toString().trim()
        if (keyword.isEmpty() || (searching && searchKeyword == keyword)) return
        searchCancellation?.cancel()
        searchFuture?.cancel(true)
        val cancellation = TaskCancellation()
        searchCancellation = cancellation
        searchKeyword = keyword
        (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager).hideSoftInputFromWindow(input.windowToken, 0)
        SearchHistoryStore.add(this, keyword)
        renderHistory()
        searching = true
        loadingView.visibility = View.VISIBLE
        suggest.visibility = View.GONE
        emptyView.visibility = View.GONE
        resultList.visibility = View.GONE
        searchFuture = executor.submit {
            try {
                val items = searchItems(keyword, cancellation)
                main.post {
                    if (!acceptsSearch(keyword, cancellation)) return@post
                    searching = false
                    loadingView.visibility = View.GONE
                    adapter.submit(items)
                    if (items.isEmpty()) {
                        resultList.visibility = View.GONE
                        emptyView.text = "没有找到相关短剧"
                        emptyView.visibility = View.VISIBLE
                    } else {
                        resultList.visibility = View.VISIBLE
                        resultList.scrollToPosition(0)
                    }
                }
            } catch (e: Exception) {
                main.post {
                    if (!acceptsSearch(keyword, cancellation)) return@post
                    searching = false
                    loadingView.visibility = View.GONE
                    resultList.visibility = View.GONE
                    emptyView.text = "搜索失败：${e.message}"
                    emptyView.visibility = View.VISIBLE
                }
            }
        }
    }

    private fun acceptsSearch(keyword: String, cancellation: TaskCancellation): Boolean =
        !isFinishing && !isDestroyed && !cancellation.isCancelled &&
            searchCancellation === cancellation && input.text.toString().trim() == keyword

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

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
        searchCancellation?.cancel()
        searchFuture?.cancel(true)
        executor.shutdownNow()
        main.removeCallbacksAndMessages(null)
    }
}
