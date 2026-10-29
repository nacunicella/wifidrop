package com.nacunicella.wifidrop

import android.content.Context
import android.net.wifi.WifiManager
import android.util.Log
import org.json.JSONException
import org.json.JSONObject
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetSocketAddress
import java.net.SocketException
import java.net.SocketTimeoutException
import java.util.concurrent.ConcurrentHashMap

/**
 * Listens for the UDP beacons that the desktop app broadcasts every two
 * seconds and keeps a list of the computers currently on the network.
 */
class Discovery(context: Context, private val port: Int = 8751) {

    data class Peer(
        val id: String,
        val name: String,
        val ip: String,
        val port: Int,
        val os: String,
        val lastSeen: Long,
        val manual: Boolean = false,
    ) {
        val baseUrl: String get() = "http://$ip:$port"
    }

    private val appContext = context.applicationContext
    private val peers = ConcurrentHashMap<String, Peer>()
    private var socket: DatagramSocket? = null
    private var thread: Thread? = null
    private var multicastLock: WifiManager.MulticastLock? = null

    @Volatile
    private var running = false

    fun start() {
        if (running) return
        running = true
        // Many phones drop broadcast packets unless a multicast lock is held.
        val wifi = appContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        multicastLock = wifi?.createMulticastLock("wifidrop")?.apply {
            setReferenceCounted(false)
            acquire()
        }
        thread = Thread {
            try {
                val s = DatagramSocket(null).apply {
                    reuseAddress = true
                    broadcast = true
                    soTimeout = 1000
                    bind(InetSocketAddress(port))
                }
                socket = s
                val buf = ByteArray(2048)
                while (running) {
                    val packet = DatagramPacket(buf, buf.size)
                    try {
                        s.receive(packet)
                    } catch (e: SocketTimeoutException) {
                        continue
                    } catch (e: SocketException) {
                        break
                    }
                    val ip = packet.address?.hostAddress ?: continue
                    parse(String(packet.data, 0, packet.length, Charsets.UTF_8), ip)
                }
            } catch (e: Exception) {
                Log.w(TAG, "discovery stopped", e)
            }
        }.apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running = false
        socket?.close()
        socket = null
        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null
    }

    private fun parse(json: String, ip: String) {
        try {
            val o = JSONObject(json)
            if (o.optString("magic") != MAGIC) return
            val id = o.optString("id")
            val p = o.optInt("port")
            if (id.isEmpty() || p <= 0 || p > 65535) return
            peers[id] = Peer(
                id = id,
                name = o.optString("name").ifEmpty { ip },
                ip = ip,
                port = p,
                os = o.optString("os"),
                lastSeen = System.currentTimeMillis(),
            )
        } catch (e: JSONException) {
            // Not one of ours.
        }
    }

    /** Computers seen in the last few seconds, sorted by name. */
    fun peers(): List<Peer> {
        val now = System.currentTimeMillis()
        return peers.values.filter { now - it.lastSeen < TTL_MS }.sortedBy { it.name.lowercase() }
    }

    companion object {
        private const val TAG = "WiFiDrop"
        private const val MAGIC = "WIFIDROP1"
        private const val TTL_MS = 8_000L
    }
}
