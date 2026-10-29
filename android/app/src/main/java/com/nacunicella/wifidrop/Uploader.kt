package com.nacunicella.wifidrop

import android.content.Context
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import org.json.JSONObject
import java.io.BufferedOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * Sends files chosen on the phone to a computer, streaming them as
 * multipart/form-data so even very large videos never need to fit in memory.
 */
object Uploader {

    class AuthException : IOException("PIN incorrecto")

    fun interface Progress {
        fun onProgress(sentBytes: Long, totalBytes: Long, currentName: String)
    }

    /** Returns the number of files the computer reported as saved. */
    @Throws(IOException::class)
    fun upload(context: Context, baseUrl: String, pin: String, uris: List<Uri>, progress: Progress?): Int {
        val resolver = context.contentResolver
        val boundary = "----WiFiDrop" + System.currentTimeMillis()
        val total = uris.sumOf { size(context, it) }

        val conn = URL("$baseUrl/api/upload").openConnection() as HttpURLConnection
        conn.requestMethod = "POST"
        conn.doOutput = true
        conn.connectTimeout = 5_000
        conn.readTimeout = 0
        conn.setChunkedStreamingMode(1 shl 16)
        conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
        conn.setRequestProperty("X-WifiDrop-Key", pin)
        conn.setRequestProperty("X-WifiDrop-From", Build.MODEL ?: "Android")

        var sent = 0L
        BufferedOutputStream(conn.outputStream, 1 shl 16).use { out ->
            for (uri in uris) {
                val name = displayName(context, uri).replace("\"", "_").replace("\r", "").replace("\n", "")
                out.write(
                    ("--$boundary\r\n" +
                        "Content-Disposition: form-data; name=\"file\"; filename=\"$name\"\r\n" +
                        "Content-Type: application/octet-stream\r\n\r\n").toByteArray(Charsets.UTF_8)
                )
                val input = resolver.openInputStream(uri) ?: throw IOException("No se puede leer $name")
                input.use {
                    val buf = ByteArray(1 shl 16)
                    while (true) {
                        val n = it.read(buf)
                        if (n < 0) break
                        out.write(buf, 0, n)
                        sent += n
                        progress?.onProgress(sent, total, name)
                    }
                }
                out.write("\r\n".toByteArray(Charsets.UTF_8))
            }
            out.write("--$boundary--\r\n".toByteArray(Charsets.UTF_8))
            out.flush()
        }

        val code = conn.responseCode
        if (code == 401) throw AuthException()
        if (code != 200) {
            val msg = try {
                conn.errorStream?.bufferedReader()?.readText()?.let { JSONObject(it).optString("error") }
            } catch (e: Exception) {
                null
            }
            throw IOException(msg?.ifEmpty { null } ?: "Error $code")
        }
        val body = conn.inputStream.bufferedReader().readText()
        return JSONObject(body).optJSONArray("saved")?.length() ?: 0
    }

    fun displayName(context: Context, uri: Uri): String {
        if (uri.scheme == "content") {
            context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst()) {
                    val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    if (idx >= 0) c.getString(idx)?.let { if (it.isNotBlank()) return it }
                }
            }
        }
        return uri.lastPathSegment?.substringAfterLast('/')?.ifBlank { null } ?: "archivo"
    }

    private fun size(context: Context, uri: Uri): Long {
        if (uri.scheme == "content") {
            context.contentResolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { c ->
                if (c.moveToFirst()) {
                    val idx = c.getColumnIndex(OpenableColumns.SIZE)
                    if (idx >= 0 && !c.isNull(idx)) return c.getLong(idx)
                }
            }
        }
        return 0L
    }
}
