package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, open bool) (*Server, *httptest.Server) {
	t.Helper()
	store, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ID: "test01", Name: "Test", PIN: "123456"}
	disc := NewDiscovery(Beacon{ID: cfg.ID, Name: cfg.Name, Port: 1}, 0) // never started
	srv := NewServer(cfg, store, disc, 8750, open, "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

// remoteRequest builds a request that looks like it came from another host,
// so the loopback trust shortcut does not apply.
func remoteRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.RemoteAddr = "192.168.1.50:40000"
	r.Host = "192.168.1.10:8750"
	return r
}

func multipartBody(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		w, err := mw.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, content)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestAuthRequiredForRemote(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("GET", "/api/files", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no PIN: status %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	r := remoteRequest("GET", "/api/files", nil)
	r.Header.Set(keyHeader, "123456")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct PIN: status %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("GET", "/api/files?key=123456", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("PIN in query: status %d, want 200", rec.Code)
	}
}

func TestLoopbackTrustedAndInfoHidesPINRemotely(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/info", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Host = "localhost:8750"
	h.ServeHTTP(rec, r)
	var local infoResponse
	json.NewDecoder(rec.Body).Decode(&local)
	if !local.Local || !local.Authed || local.PIN != "123456" {
		t.Fatalf("loopback info = %+v", local)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("GET", "/api/info", nil))
	var remote infoResponse
	json.NewDecoder(rec.Body).Decode(&remote)
	if remote.Local || remote.Authed || remote.PIN != "" || remote.Dir != "" {
		t.Fatalf("remote info leaks data: %+v", remote)
	}
}

func TestBruteForceLockout(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()
	for i := 0; i < maxFailures; i++ {
		rec := httptest.NewRecorder()
		r := remoteRequest("GET", "/api/files", nil)
		r.Header.Set(keyHeader, "000000")
		h.ServeHTTP(rec, r)
	}
	rec := httptest.NewRecorder()
	r := remoteRequest("GET", "/api/files", nil)
	r.Header.Set(keyHeader, "123456")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("correct PIN during lockout: status %d, want 401", rec.Code)
	}
	// A different client is not affected.
	rec = httptest.NewRecorder()
	r = remoteRequest("GET", "/api/files", nil)
	r.RemoteAddr = "192.168.1.51:40000"
	r.Header.Set(keyHeader, "123456")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("other client blocked: status %d", rec.Code)
	}
}

func TestHostHeaderCheck(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()
	for host, want := range map[string]int{
		"evil.example.com": http.StatusForbidden,
		"localhost:8750":   http.StatusOK,
		"192.168.1.10":     http.StatusOK,
		"[::1]:8750":       http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/info", nil)
		r.RemoteAddr = "127.0.0.1:5000"
		r.Host = host
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("host %q: status %d, want %d", host, rec.Code, want)
		}
	}
}

func TestUploadListDownloadDelete(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()

	body, ctype := multipartBody(t, map[string]string{"../../hack.txt": "hola", "foto.jpg": "jpegdata"})
	rec := httptest.NewRecorder()
	r := remoteRequest("POST", "/api/upload", body)
	r.Header.Set("Content-Type", ctype)
	r.Header.Set(keyHeader, "123456")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var up struct{ Saved []FileInfo }
	json.NewDecoder(rec.Body).Decode(&up)
	if len(up.Saved) != 2 {
		t.Fatalf("saved %d files, want 2", len(up.Saved))
	}
	for _, f := range up.Saved {
		if strings.Contains(f.Name, "..") || strings.Contains(f.Name, "/") {
			t.Errorf("unsafe name saved: %q", f.Name)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("GET", "/api/files/hack.txt?key=123456", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hola" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("GET", "/api/files/..%2Fgo.mod?key=123456", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("traversal download: status %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteRequest("DELETE", "/api/files/foto.jpg?key=123456", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	files, _ := srv.store.List()
	if len(files) != 1 || files[0].Name != "hack.txt" {
		t.Fatalf("after delete: %+v", files)
	}
}

func TestOpenModeSkipsPIN(t *testing.T) {
	srv, _ := newTestServer(t, true)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, remoteRequest("GET", "/api/files", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("open mode: status %d", rec.Code)
	}
}

func TestSendToPeerEndToEnd(t *testing.T) {
	// "Remote" device that receives the files.
	remote, remoteTS := newTestServer(t, false)
	remote.cfg.PIN = "654321"
	remote.trustLoopback = false // the test client connects over loopback
	// Local device whose /api/send forwards to the remote one.
	local, _ := newTestServer(t, false)
	h := local.Handler()

	addr := strings.TrimPrefix(remoteTS.URL, "http://")
	body, ctype := multipartBody(t, map[string]string{"doc.pdf": "pdf"})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/send?addr="+addr+"&pin=000000", body)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Host = "localhost"
	r.Header.Set("Content-Type", ctype)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong peer PIN: status %d, want 401", rec.Code)
	}

	body, ctype = multipartBody(t, map[string]string{"doc.pdf": "pdf"})
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/send?addr="+addr+"&pin=654321", body)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Host = "localhost"
	r.Header.Set("Content-Type", ctype)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	files, _ := remote.store.List()
	if len(files) != 1 || files[0].Name != "doc.pdf" || files[0].Size != 3 {
		t.Fatalf("remote files = %+v", files)
	}
}

func TestStaticAndManifest(t *testing.T) {
	srv, _ := newTestServer(t, false)
	h := srv.Handler()
	for path, ctype := range map[string]string{
		"/":                     "text/html",
		"/manifest.webmanifest": "application/manifest+json",
		"/sw.js":                "text/javascript",
		"/icon-192.png":         "image/png",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, remoteRequest("GET", path, nil))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestHumanSizeAndNormalizeAddr(t *testing.T) {
	if got := humanSize(1536); got != "1.5 KB" {
		t.Errorf("humanSize = %q", got)
	}
	if got := normalizeAddr("http://192.168.1.2/", 8750); got != "192.168.1.2:8750" {
		t.Errorf("normalizeAddr = %q", got)
	}
	if got := normalizeAddr("192.168.1.2:9000", 8750); got != "192.168.1.2:9000" {
		t.Errorf("normalizeAddr = %q", got)
	}
}
