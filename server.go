package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed web
var webFS embed.FS

// Server exposes the HTTP API used by the browser UI (on this machine and on
// phones) and by other WiFiDrop instances on the network.
type Server struct {
	cfg   *Config
	store *Storage
	disc  *Discovery
	port  int
	open  bool // accept uploads without PIN
	// trustLoopback lets the browser on this same machine skip the PIN. It is
	// only disabled by tests, which have to talk over loopback.
	trustLoopback bool
	version       string
	startedAt     time.Time

	mu       sync.Mutex
	failures map[string]*failureState // brute force protection per IP
	events   *eventHub
}

type failureState struct {
	count int
	until time.Time
}

const (
	keyHeader   = "X-WifiDrop-Key"
	maxFailures = 5
	lockout     = 15 * time.Second
)

func NewServer(cfg *Config, store *Storage, disc *Discovery, port int, open bool, version string) *Server {
	return &Server{
		cfg:           cfg,
		store:         store,
		disc:          disc,
		port:          port,
		open:          open,
		trustLoopback: true,
		version:       version,
		startedAt:     time.Now(),
		failures:      map[string]*failureState{},
		events:        newEventHub(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("POST /api/auth", s.handleAuth)
	mux.HandleFunc("GET /api/peers", s.auth(s.handlePeers))
	mux.HandleFunc("GET /api/files", s.auth(s.handleFiles))
	mux.HandleFunc("GET /api/files/{name}", s.auth(s.handleDownload))
	mux.HandleFunc("DELETE /api/files/{name}", s.auth(s.handleDelete))
	mux.HandleFunc("POST /api/upload", s.auth(s.handleUpload))
	mux.HandleFunc("POST /api/send", s.auth(s.handleSend))
	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))
	mux.HandleFunc("GET /api/qr", s.auth(s.handleQR))
	mux.Handle("/", s.staticHandler())
	return s.hostCheck(mux)
}

// hostCheck rejects requests whose Host header is not an IP address or a
// local name. This defeats DNS rebinding attacks from malicious websites.
func (s *Server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) == nil && host != "localhost" && !strings.EqualFold(host, hostnameOrEmpty()) {
			http.Error(w, "host no permitido", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostnameOrEmpty() string {
	h, _ := hostnameCached()
	return h
}

var (
	hostOnce sync.Once
	hostVal  string
	hostErr  error
)

func hostnameCached() (string, error) {
	hostOnce.Do(func() { hostVal, hostErr = osHostname() })
	return hostVal, hostErr
}

func isLoopback(remoteAddr string) bool {
	h, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		h = remoteAddr
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// keyOK reports whether the request carries a valid PIN. Requests from the
// browser on this very machine are trusted without a PIN; the PIN is then
// delivered to that page through /api/info and used in subsequent calls.
func (s *Server) keyOK(r *http.Request) bool {
	if s.open || (s.trustLoopback && isLoopback(r.RemoteAddr)) {
		return true
	}
	key := r.Header.Get(keyHeader)
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	return s.checkPIN(clientIP(r), key)
}

func (s *Server) checkPIN(ip, pin string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.failures[ip]
	if st != nil && time.Now().Before(st.until) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(pin), []byte(s.cfg.PIN)) == 1 {
		delete(s.failures, ip)
		return true
	}
	if pin == "" {
		return false
	}
	if st == nil {
		st = &failureState{}
		s.failures[ip] = st
	}
	st.count++
	if st.count >= maxFailures {
		st.until = time.Now().Add(lockout)
		st.count = 0
	}
	return false
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.keyOK(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "PIN incorrecto o ausente"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// --- handlers -------------------------------------------------------------

type infoResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	OS       string   `json:"os"`
	Version  string   `json:"version"`
	Port     int      `json:"port"`
	Dir      string   `json:"dir,omitempty"`
	Authed   bool     `json:"authed"`
	Local    bool     `json:"local"`
	Open     bool     `json:"open"`
	PIN      string   `json:"pin,omitempty"`
	URLs     []string `json:"urls,omitempty"`
	Uptime   int64    `json:"uptime"`
	Hostname string   `json:"hostname"`
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	local := s.trustLoopback && isLoopback(r.RemoteAddr)
	resp := infoResponse{
		ID:       s.cfg.ID,
		Name:     s.cfg.Name,
		OS:       runtime.GOOS,
		Version:  s.version,
		Port:     s.port,
		Authed:   s.keyOK(r),
		Local:    local,
		Open:     s.open,
		Uptime:   int64(time.Since(s.startedAt).Seconds()),
		Hostname: hostnameOrEmpty(),
	}
	if resp.Authed {
		resp.Dir = s.store.Dir
		resp.URLs = s.shareURLs()
	}
	if local {
		// Only the browser on this machine learns the PIN, so it can show it
		// and so phones that scan the QR get it embedded in the URL.
		resp.PIN = s.cfg.PIN
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) shareURLs() []string {
	var urls []string
	for _, ip := range localIPv4s() {
		urls = append(urls, fmt.Sprintf("http://%s:%d/", ip, s.port))
	}
	return urls
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "petición inválida"})
		return
	}
	if s.open || (s.trustLoopback && isLoopback(r.RemoteAddr)) || s.checkPIN(clientIP(r), body.PIN) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "PIN incorrecto"})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers := s.disc.Peers()
	if peers == nil {
		peers = []Peer{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": peers})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, st, err := s.store.Open(name)
	if err != nil {
		http.Error(w, "archivo no encontrado", http.StatusNotFound)
		return
	}
	defer f.Close()
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename*=UTF-8''%s`, disposition, urlPathEscape(name)))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.Delete(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "archivo no encontrado"})
		return
	}
	s.events.publish(event{Type: "deleted", Name: name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUpload receives one or more files as multipart/form-data and streams
// each part straight to disk, so memory use stays flat regardless of size.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "se esperaba multipart/form-data"})
		return
	}
	from := r.Header.Get("X-WifiDrop-From")
	if from == "" {
		from = clientIP(r)
	}
	var saved []FileInfo
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "saved": saved})
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		info, err := s.store.Save(part.FileName(), part)
		part.Close()
		if err != nil {
			log.Printf("upload: error saving %q: %v", part.FileName(), err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "saved": saved})
			return
		}
		log.Printf("recibido %q (%s) de %s", info.Name, humanSize(info.Size), from)
		saved = append(saved, info)
		s.events.publish(event{Type: "received", Name: info.Name, Size: info.Size, From: from})
	}
	if saved == nil {
		saved = []FileInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": saved})
}

// handleSend forwards the uploaded files to another WiFiDrop device. The
// browser never talks to the other device directly, which avoids CORS and
// keeps a single pairing step per device.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	target := q.Get("addr")
	if id := q.Get("peer"); id != "" {
		p, ok := s.disc.Peer(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "el dispositivo ya no está en la red"})
			return
		}
		target = p.Addr
	}
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "falta el destino"})
		return
	}
	target = normalizeAddr(target, s.port)
	resp, err := forwardUpload(r.Context(), target, q.Get("pin"), s.cfg.Name, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "no se pudo conectar con " + target + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		urls := s.shareURLs()
		if len(urls) == 0 {
			http.Error(w, "sin red", http.StatusServiceUnavailable)
			return
		}
		url = urls[0]
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		http.Error(w, "url inválida", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("pin") == "1" && s.trustLoopback && isLoopback(r.RemoteAddr) {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "k=" + s.cfg.PIN
	}
	png, err := qrcode.Encode(url, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// --- events (server-sent) --------------------------------------------------

type event struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	Size int64  `json:"size,omitempty"`
	From string `json:"from,omitempty"`
}

type eventHub struct {
	mu   sync.Mutex
	subs map[chan event]struct{}
}

func newEventHub() *eventHub { return &eventHub{subs: map[chan event]struct{}{}} }

func (h *eventHub) subscribe() chan event {
	ch := make(chan event, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan event) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *eventHub) publish(e event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default: // slow client, drop
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming no soportado", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	fmt.Fprintf(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			flusher.Flush()
		}
	}
}

// --- static files ----------------------------------------------------------

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sw.js" || r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if r.URL.Path == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// --- helpers ---------------------------------------------------------------

func normalizeAddr(addr string, defaultPort int) string {
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	addr = strings.TrimSuffix(addr, "/")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(defaultPort))
	}
	return addr
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func fmtInt(i int) string { return strconv.Itoa(i) }
