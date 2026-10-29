package com.nacunicella.wifidrop

import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.view.View
import android.widget.EditText
import android.widget.ListView
import android.widget.ProgressBar
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

/**
 * Home screen: lists the computers running WiFiDrop on the current Wi-Fi.
 * Tapping one opens its interface. When the app is opened from the system
 * "Share" sheet, tapping a computer sends the shared files to it instead.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var discovery: Discovery
    private lateinit var adapter: PeerAdapter
    private lateinit var status: TextView
    private lateinit var hint: TextView
    private val handler = Handler(Looper.getMainLooper())
    private val prefs by lazy { getSharedPreferences("wifidrop", MODE_PRIVATE) }

    /** Files received through ACTION_SEND / ACTION_SEND_MULTIPLE, if any. */
    private var sharedUris: List<Uri> = emptyList()

    private val refresh = object : Runnable {
        override fun run() {
            render()
            handler.postDelayed(this, 1000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        discovery = Discovery(this)
        adapter = PeerAdapter(this)
        status = findViewById(R.id.status)
        hint = findViewById(R.id.hint)
        val list = findViewById<ListView>(R.id.list)
        list.adapter = adapter
        list.setOnItemClickListener { _, _, position, _ -> onPeerTapped(adapter.getItem(position)) }
        list.setOnItemLongClickListener { _, _, position, _ ->
            val peer = adapter.getItem(position)
            if (peer.manual) {
                removeManual(peer.ip, peer.port)
                true
            } else {
                false
            }
        }
        findViewById<View>(R.id.addByIp).setOnClickListener { askManualAddress() }
        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(intent: Intent?) {
        sharedUris = when (intent?.action) {
            Intent.ACTION_SEND -> listOfNotNull(intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM))
            Intent.ACTION_SEND_MULTIPLE -> intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM)?.toList() ?: emptyList()
            else -> emptyList()
        }
        findViewById<TextView>(R.id.title).text =
            if (sharedUris.isEmpty()) getString(R.string.app_name) else getString(R.string.share_title, sharedUris.size)
    }

    override fun onStart() {
        super.onStart()
        discovery.start()
        handler.post(refresh)
    }

    override fun onStop() {
        super.onStop()
        handler.removeCallbacks(refresh)
        discovery.stop()
    }

    // ---- peers --------------------------------------------------------------

    private fun manualPeers(): List<Discovery.Peer> =
        (prefs.getStringSet("manual", emptySet()) ?: emptySet()).sorted().map { addr ->
            val ip = addr.substringBefore(':')
            val port = addr.substringAfter(':', "8750").toIntOrNull() ?: 8750
            Discovery.Peer(id = "manual:$addr", name = ip, ip = ip, port = port, os = "", lastSeen = 0, manual = true)
        }

    private fun render() {
        val found = discovery.peers()
        val manual = manualPeers().filter { m -> found.none { it.ip == m.ip && it.port == m.port } }
        val all = found + manual
        adapter.update(all)
        status.text = if (found.isEmpty()) getString(R.string.searching) else ""
        status.visibility = if (found.isEmpty()) View.VISIBLE else View.GONE
        hint.visibility = if (all.isEmpty()) View.VISIBLE else View.GONE
    }

    private fun askManualAddress() {
        val input = EditText(this).apply {
            hint = getString(R.string.add_by_ip_hint)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        AlertDialog.Builder(this)
            .setTitle(R.string.add_by_ip)
            .setMessage(R.string.add_by_ip_message)
            .setView(input)
            .setNegativeButton(R.string.cancel, null)
            .setPositiveButton(R.string.ok) { _, _ ->
                val addr = input.text.toString().trim()
                    .removePrefix("http://").removePrefix("https://").trimEnd('/')
                if (addr.isNotEmpty()) {
                    val set = (prefs.getStringSet("manual", emptySet()) ?: emptySet()).toMutableSet()
                    set.add(addr)
                    prefs.edit().putStringSet("manual", set).apply()
                    render()
                }
            }
            .show()
    }

    private fun removeManual(ip: String, port: Int) {
        AlertDialog.Builder(this)
            .setTitle(R.string.remove)
            .setMessage("$ip:$port")
            .setNegativeButton(R.string.cancel, null)
            .setPositiveButton(R.string.remove) { _, _ ->
                val set = (prefs.getStringSet("manual", emptySet()) ?: emptySet()).toMutableSet()
                set.remove("$ip:$port")
                set.remove(ip)
                prefs.edit().putStringSet("manual", set).apply()
                render()
            }
            .show()
    }

    // ---- actions ------------------------------------------------------------

    private fun pinKey(peer: Discovery.Peer) = "pin:${peer.ip}:${peer.port}"

    private fun onPeerTapped(peer: Discovery.Peer) {
        if (sharedUris.isEmpty()) {
            openWeb(peer)
            return
        }
        val stored = prefs.getString(pinKey(peer), null)
        if (stored != null) {
            sendShared(peer, stored)
        } else {
            askPin(peer) { pin -> sendShared(peer, pin) }
        }
    }

    private fun openWeb(peer: Discovery.Peer) {
        val pin = prefs.getString(pinKey(peer), null)
        val url = peer.baseUrl + "/" + (if (pin != null) "?k=" + Uri.encode(pin) else "")
        startActivity(
            Intent(this, WebActivity::class.java)
                .putExtra(WebActivity.EXTRA_URL, url)
                .putExtra(WebActivity.EXTRA_NAME, peer.name)
        )
    }

    private fun askPin(peer: Discovery.Peer, onPin: (String) -> Unit) {
        val input = EditText(this).apply {
            inputType = InputType.TYPE_CLASS_NUMBER
            hint = "000000"
        }
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.pin_title, peer.name))
            .setMessage(R.string.pin_message)
            .setView(input)
            .setNegativeButton(R.string.cancel, null)
            .setPositiveButton(R.string.send) { _, _ ->
                val pin = input.text.toString().trim()
                if (pin.length == 6) {
                    prefs.edit().putString(pinKey(peer), pin).apply()
                    onPin(pin)
                } else {
                    Toast.makeText(this, R.string.wrong_pin, Toast.LENGTH_SHORT).show()
                }
            }
            .show()
    }

    private fun sendShared(peer: Discovery.Peer, pin: String) {
        val uris = sharedUris
        if (uris.isEmpty()) return
        val bar = ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal).apply {
            max = 1000
            setPadding(48, 24, 48, 0)
        }
        val dialog = AlertDialog.Builder(this)
            .setTitle(getString(R.string.sending, peer.name))
            .setView(bar)
            .setCancelable(false)
            .show()

        Thread {
            try {
                val count = Uploader.upload(this, peer.baseUrl, pin, uris) { sent, total, _ ->
                    if (total > 0) handler.post { bar.progress = (sent * 1000 / total).toInt() }
                }
                handler.post {
                    dialog.dismiss()
                    Toast.makeText(this, getString(R.string.sent_ok, count, peer.name), Toast.LENGTH_LONG).show()
                    finish()
                }
            } catch (e: Uploader.AuthException) {
                prefs.edit().remove(pinKey(peer)).apply()
                handler.post {
                    dialog.dismiss()
                    Toast.makeText(this, R.string.wrong_pin, Toast.LENGTH_SHORT).show()
                    askPin(peer) { newPin -> sendShared(peer, newPin) }
                }
            } catch (e: Exception) {
                handler.post {
                    dialog.dismiss()
                    Toast.makeText(this, getString(R.string.sent_error, e.message ?: e.javaClass.simpleName), Toast.LENGTH_LONG).show()
                }
            }
        }.start()
    }
}
