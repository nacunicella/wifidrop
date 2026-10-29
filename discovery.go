package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"time"
)

// Discovery announces this device on the local network via UDP broadcast and
// collects the announcements of other devices. It needs no external services
// and works on any Wi-Fi where broadcast traffic is allowed.

const (
	beaconMagic    = "WIFIDROP1"
	beaconInterval = 2 * time.Second
	peerTTL        = 8 * time.Second
)

type Beacon struct {
	Magic   string `json:"magic"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Port    int    `json:"port"`
	OS      string `json:"os"`
	Version string `json:"version"`
}

type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Addr     string    `json:"addr"` // host:port of the HTTP API
	IP       string    `json:"ip"`
	Port     int       `json:"port"`
	OS       string    `json:"os"`
	Version  string    `json:"version"`
	LastSeen time.Time `json:"lastSeen"`
}

type Discovery struct {
	self      Beacon
	udpPort   int
	mu        sync.Mutex
	peers     map[string]*Peer
	conn      *net.UDPConn
	stopCh    chan struct{}
	announceC *net.UDPConn
}

func NewDiscovery(self Beacon, udpPort int) *Discovery {
	self.Magic = beaconMagic
	return &Discovery{
		self:    self,
		udpPort: udpPort,
		peers:   map[string]*Peer{},
		stopCh:  make(chan struct{}),
	}
}

func (d *Discovery) Start() error {
	// Listening socket: bound to the discovery port on all interfaces.
	lc := net.ListenConfig{Control: reusePort}
	pc, err := lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort("0.0.0.0", itoa(d.udpPort)))
	if err != nil {
		return err
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return fmt.Errorf("unexpected socket type %T", pc)
	}
	d.conn = conn

	// Announcing socket on an ephemeral port so that several instances on one
	// machine (useful for testing) do not fight over the listening port.
	ac, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		conn.Close()
		return err
	}
	d.announceC = ac

	go d.listenLoop()
	go d.announceLoop()
	return nil
}

func (d *Discovery) Stop() {
	close(d.stopCh)
	if d.conn != nil {
		d.conn.Close()
	}
	if d.announceC != nil {
		d.announceC.Close()
	}
}

func (d *Discovery) listenLoop() {
	buf := make([]byte, 2048)
	for {
		n, from, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-d.stopCh:
				return
			default:
				log.Printf("discovery: read error: %v", err)
				time.Sleep(time.Second)
				continue
			}
		}
		b, ok := parseBeacon(buf[:n])
		if !ok || b.ID == d.self.ID {
			continue
		}
		d.mu.Lock()
		d.peers[b.ID] = &Peer{
			ID:       b.ID,
			Name:     b.Name,
			IP:       from.IP.String(),
			Port:     b.Port,
			Addr:     net.JoinHostPort(from.IP.String(), itoa(b.Port)),
			OS:       b.OS,
			Version:  b.Version,
			LastSeen: time.Now(),
		}
		d.mu.Unlock()
	}
}

func parseBeacon(data []byte) (Beacon, bool) {
	var b Beacon
	if err := json.Unmarshal(data, &b); err != nil {
		return b, false
	}
	if b.Magic != beaconMagic || b.ID == "" || b.Port <= 0 || b.Port > 65535 {
		return b, false
	}
	if len(b.Name) > 64 {
		b.Name = b.Name[:64]
	}
	return b, true
}

func (d *Discovery) announceLoop() {
	payload, _ := json.Marshal(d.self)
	t := time.NewTicker(beaconInterval)
	defer t.Stop()
	d.announce(payload)
	for {
		select {
		case <-d.stopCh:
			return
		case <-t.C:
			d.announce(payload)
			d.expire()
		}
	}
}

// announce sends the beacon to the limited broadcast address and to the
// directed broadcast address of every IPv4 interface, which reaches networks
// where 255.255.255.255 is filtered.
func (d *Discovery) announce(payload []byte) {
	targets := map[string]struct{}{"255.255.255.255": {}}
	for _, ip := range broadcastAddrs() {
		targets[ip.String()] = struct{}{}
	}
	for ip := range targets {
		addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: d.udpPort}
		if _, err := d.announceC.WriteToUDP(payload, addr); err != nil {
			// Some interfaces refuse broadcast; that is fine.
			continue
		}
	}
}

func (d *Discovery) expire() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, p := range d.peers {
		if time.Since(p.LastSeen) > peerTTL {
			delete(d.peers, id)
		}
	}
}

func (d *Discovery) Peers() []Peer {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		if time.Since(p.LastSeen) <= peerTTL {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (d *Discovery) Peer(id string) (Peer, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.peers[id]
	if !ok {
		return Peer{}, false
	}
	return *p, true
}

// broadcastAddrs returns the directed broadcast address of each IPv4 network
// this machine is attached to.
func broadcastAddrs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			mask := ipnet.Mask
			if len(mask) == 16 {
				mask = mask[12:]
			}
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}
			out = append(out, bcast)
		}
	}
	return out
}

// localIPv4s lists the non-loopback IPv4 addresses, used to build the URL and
// QR code shown to phones.
func localIPv4s() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					out = append(out, ip4.String())
				}
			}
		}
	}
	// Prefer private ranges first, which is where Wi-Fi networks live.
	sort.SliceStable(out, func(i, j int) bool {
		return net.ParseIP(out[i]).IsPrivate() && !net.ParseIP(out[j]).IsPrivate()
	})
	return out
}

func itoa(i int) string {
	return fmtInt(i)
}
