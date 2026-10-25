package com.duanju.player

import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.TextWatcher
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.ProgressBar
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import java.util.concurrent.Executors

class LibraryActivity : AppCompatActivity() {

    private lateinit var baseField: EditText
    private lateinit var searchField: EditText
    private lateinit var statusView: TextView
    private lateinit var progress: ProgressBar
    private lateinit var listView: RecyclerView
    private lateinit var adapter: DramaAdapter

    private var base = "http://192.168.1.2:3333"
    private var all: List<Drama> = emptyList()
    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_library)
        base = getSharedPreferences("duanju", MODE_PRIVATE).getString("base", base)!!
        baseField = findViewById(R.id.base_url)
        baseField.setText(base)
        searchField = findViewById(R.id.search)
        statusView = findViewById(R.id.status)
        progress = findViewById(R.id.progress)
        listView = findViewById(R.id.library_list)
        adapter = DramaAdapter(base) { drama ->
            startActivity(
                Intent(this, PlayerActivity::class.java)
                    .putExtra("base", base)
                    .putExtra("dramaId", drama.id)
                    .putExtra("title", drama.title)
                    .putExtra("episodes", drama.episodes)
            )
        }
        listView.layoutManager = LinearLayoutManager(this)
        listView.adapter = adapter
        findViewById<Button>(R.id.refresh).setOnClickListener { load() }
        searchField.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) {}
            override fun onTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) { filter(s?.toString() ?: "") }
            override fun afterTextChanged(s: Editable?) {}
        })
        load()
    }

    private fun load() {
        base = baseField.text.toString().trim().trimEnd('/')
        if (base.isEmpty()) {
            status("请输入服务器地址")
            return
        }
        getSharedPreferences("duanju", MODE_PRIVATE).edit().putString("base", base).apply()
        progress.visibility = View.VISIBLE
        status("加载剧库…")
        executor.execute {
            try {
                val text = Network.get("$base/api/ui/dramas")
                val list = DramaParser.parseList(text)
                main.post {
                    all = list
                    adapter.submit(list)
                    progress.visibility = View.GONE
                    status("共 ${list.size} 部")
                }
            } catch (e: Exception) {
                main.post {
                    progress.visibility = View.GONE
                    status("加载失败：${e.message}")
                }
            }
        }
    }

    private fun status(message: String) {
        statusView.text = message
    }

    private fun filter(query: String) {
        val keyword = query.trim()
        val list = if (keyword.isEmpty()) all else all.filter { it.title.contains(keyword, ignoreCase = true) }
        adapter.submit(list)
    }
}