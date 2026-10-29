package com.nacunicella.wifidrop

import android.content.Context
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.BaseAdapter
import android.widget.TextView

class PeerAdapter(private val context: Context) : BaseAdapter() {

    private var items: List<Discovery.Peer> = emptyList()

    fun update(peers: List<Discovery.Peer>) {
        items = peers
        notifyDataSetChanged()
    }

    override fun getCount(): Int = items.size
    override fun getItem(position: Int): Discovery.Peer = items[position]
    override fun getItemId(position: Int): Long = items[position].id.hashCode().toLong()

    override fun getView(position: Int, convertView: View?, parent: ViewGroup): View {
        val view = convertView ?: LayoutInflater.from(context).inflate(R.layout.item_peer, parent, false)
        val peer = items[position]
        view.findViewById<TextView>(R.id.icon).text = when (peer.os) {
            "windows" -> "🖥️"
            "darwin" -> "💻"
            "linux" -> "🐧"
            else -> "📡"
        }
        view.findViewById<TextView>(R.id.name).text = peer.name
        val addr = "${peer.ip}:${peer.port}"
        view.findViewById<TextView>(R.id.addr).text =
            if (peer.manual) "$addr · ${context.getString(R.string.manual)}" else addr
        return view
    }
}
