package com.duanju.player.core

import android.util.JsonReader
import android.util.JsonToken
import org.json.JSONArray
import org.json.JSONObject
import java.io.FilterReader
import java.io.Reader

/** long: 只读取配方指定的搜索数组，完成后断开，避免下载体积远大于业务数据的调试尾部。 */
internal object RecipeSearchReader {
    fun read(source: Reader, path: String, cancellation: TaskCancellation?): JSONArray {
        val limited = object : FilterReader(source) {
            private var consumed = 0L
            override fun read(buffer: CharArray, offset: Int, length: Int): Int {
                cancellation?.check()
                val count = super.read(buffer, offset, length)
                consumed += maxOf(count, 0)
                if (consumed > 8 * 1024 * 1024) throw RecipeHttp.ResponseFailure("nonJson", "搜索业务数据超出读取上限")
                return count
            }
        }
        val reader = JsonReader(limited)
        try {
            if (reader.peek() == JsonToken.END_DOCUMENT) throw RecipeHttp.ResponseFailure("empty", "搜索空响应")
            val value = find(reader, path.split('.'), 0, cancellation)
            return value as? JSONArray ?: throw RecipeHttp.ResponseFailure("missingData", "搜索响应缺少结果数组")
        } catch (e: android.util.MalformedJsonException) {
            throw RecipeHttp.ResponseFailure("nonJson", "搜索响应 JSON 无效")
        } catch (e: IllegalStateException) {
            throw RecipeHttp.ResponseFailure("nonJson", "搜索响应结构无效")
        }
    }

    private fun find(reader: JsonReader, path: List<String>, depth: Int, cancellation: TaskCancellation?): Any? {
        cancellation?.check()
        if (depth == path.size) return value(reader, cancellation)
        if (reader.peek() == JsonToken.BEGIN_OBJECT) {
            reader.beginObject()
            while (reader.hasNext()) {
                val name = reader.nextName()
                if (depth == 0 && name == "code") {
                    RecipeHttp.checkCode(JSONObject().put("code", value(reader, cancellation)))
                } else if (name == path[depth]) {
                    return find(reader, path, depth + 1, cancellation)
                } else reader.skipValue()
            }
            reader.endObject()
        } else if (reader.peek() == JsonToken.BEGIN_ARRAY) {
            val target = path[depth].toIntOrNull() ?: return null
            reader.beginArray()
            var index = 0
            while (reader.hasNext()) {
                if (index++ == target) return find(reader, path, depth + 1, cancellation)
                reader.skipValue()
            }
            reader.endArray()
        } else reader.skipValue()
        return null
    }

    private fun value(reader: JsonReader, cancellation: TaskCancellation?, depth: Int = 0): Any {
        cancellation?.check()
        if (depth > 64) throw RecipeHttp.ResponseFailure("nonJson", "搜索 JSON 嵌套过深")
        return when (reader.peek()) {
            JsonToken.BEGIN_OBJECT -> JSONObject().also { out ->
                reader.beginObject()
                while (reader.hasNext()) out.put(reader.nextName(), value(reader, cancellation, depth + 1))
                reader.endObject()
            }
            JsonToken.BEGIN_ARRAY -> JSONArray().also { out ->
                reader.beginArray()
                while (reader.hasNext()) out.put(value(reader, cancellation, depth + 1))
                reader.endArray()
            }
            JsonToken.STRING, JsonToken.NUMBER -> reader.nextString()
            JsonToken.BOOLEAN -> reader.nextBoolean()
            JsonToken.NULL -> { reader.nextNull(); JSONObject.NULL }
            else -> throw RecipeHttp.ResponseFailure("nonJson", "搜索 JSON 值无效")
        }
    }
}
