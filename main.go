// WiFiDrop: transferencia rápida de archivos por Wi-Fi entre el móvil y el
// ordenador, sin internet ni cuentas.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) > 1 && os.Args[1] == "send" {
		os.Exit(runSend(os.Args[2:]))
	}
	os.Exit(runServe(os.Args[1:]))
}

func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "WiFiDrop"
	}
	// Prefer the Downloads folder when it exists; otherwise use the home.
	if st, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && st.IsDir() {
		return filepath.Join(home, "Downloads", "WiFiDrop")
	}
	if st, err := os.Stat(filepath.Join(home, "Descargas")); err == nil && st.IsDir() {
		return filepath.Join(home, "Descargas", "WiFiDrop")
	}
	return filepath.Join(home, "WiFiDrop")
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("wifidrop", flag.ContinueOnError)
	port := fs.Int("port", 8750, "puerto HTTP de la interfaz y la API")
	udpPort := fs.Int("discovery-port", 8751, "puerto UDP para descubrir otros equipos")
	dir := fs.String("dir", defaultDir(), "carpeta donde se guardan y comparten los archivos")
	name := fs.String("name", "", "nombre con el que se muestra este equipo")
	pin := fs.String("pin", "", "fija el PIN de 6 dígitos (por defecto se genera y se guarda)")
	open := fs.Bool("open", false, "acepta archivos de cualquier dispositivo de la red sin PIN")
	noBrowser := fs.Bool("no-browser", false, "no abrir el navegador al iniciar")
	showVersion := fs.Bool("version", false, "muestra la versión")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Uso:\n  wifidrop [opciones]            inicia la aplicación\n  wifidrop send <destino> <archivos...>   envía archivos desde la terminal\n\nOpciones:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Println("wifidrop", version)
		return 0
	}

	cfg, err := loadOrCreateConfig()
	if err != nil {
		log.Printf("error cargando configuración: %v", err)
		return 1
	}
	changed := false
	if *name != "" && *name != cfg.Name {
		cfg.Name = *name
		changed = true
	}
	if *pin != "" {
		if len(*pin) != 6 || strings.Trim(*pin, "0123456789") != "" {
			log.Printf("el PIN debe tener exactamente 6 dígitos")
			return 2
		}
		if *pin != cfg.PIN {
			cfg.PIN = *pin
			changed = true
		}
	}
	if changed {
		// Remember the new name/PIN so the next start and the `send`
		// subcommand use the same identity.
		if path, err := configPath(); err == nil {
			if err := cfg.save(path); err != nil {
				log.Printf("aviso: no se pudo guardar la configuración: %v", err)
			}
		}
	}

	store, err := NewStorage(*dir)
	if err != nil {
		log.Printf("no se pudo crear la carpeta %s: %v", *dir, err)
		return 1
	}

	disc := NewDiscovery(Beacon{ID: cfg.ID, Name: cfg.Name, Port: *port, OS: runtime.GOOS, Version: version}, *udpPort)
	if err := disc.Start(); err != nil {
		log.Printf("aviso: descubrimiento de red desactivado: %v", err)
	} else {
		defer disc.Stop()
	}

	srv := NewServer(cfg, store, disc, *port, *open, version)
	httpSrv := &http.Server{
		Addr:              ":" + strconv.Itoa(*port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		log.Printf("no se pudo escuchar en el puerto %d: %v", *port, err)
		return 1
	}

	fmt.Printf("\n  WiFiDrop %s · %s\n", version, cfg.Name)
	fmt.Printf("  Carpeta:   %s\n", store.Dir)
	fmt.Printf("  PIN:       %s\n", cfg.PIN)
	fmt.Printf("  En este equipo:  http://localhost:%d/\n", *port)
	for _, u := range srv.shareURLs() {
		fmt.Printf("  Desde el móvil:  %s\n", u)
	}
	if *open {
		fmt.Printf("  Modo abierto: cualquier dispositivo de la red puede enviar archivos.\n")
	}
	fmt.Println()

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("servidor detenido: %v", err)
		}
	}()
	if !*noBrowser {
		go openBrowser(fmt.Sprintf("http://localhost:%d/", *port))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	fmt.Println("\nCerrando…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdownCtx)
	return 0
}

func runSend(args []string) int {
	fs := flag.NewFlagSet("wifidrop send", flag.ContinueOnError)
	pin := fs.String("pin", "", "PIN del equipo de destino")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Uso: wifidrop send [-pin 123456] <ip[:puerto]> <archivo> [archivo...]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		return 2
	}
	cfg, err := loadOrCreateConfig()
	if err != nil {
		log.Printf("error cargando configuración: %v", err)
		return 1
	}
	addr := normalizeAddr(rest[0], 8750)
	files := rest[1:]
	for _, f := range files {
		if st, err := os.Stat(f); err != nil || st.IsDir() {
			log.Printf("no se puede leer %s", f)
			return 1
		}
	}
	start := time.Now()
	var total int64
	err = sendFiles(context.Background(), addr, *pin, cfg.Name, files, func(name string, n int64) {
		total += n
		fmt.Printf("  enviado %s (%s)\n", name, humanSize(n))
	})
	if err != nil {
		log.Printf("error: %v", err)
		return 1
	}
	secs := time.Since(start).Seconds()
	if secs > 0 {
		fmt.Printf("Listo: %s en %.1fs (%s/s)\n", humanSize(total), secs, humanSize(int64(float64(total)/secs)))
	}
	return 0
}

func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("abre %s en tu navegador", url)
	}
}
