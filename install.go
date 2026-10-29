package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Installation: WiFiDrop is a single binary, but people expect an app with an
// icon they can open from the desktop or the applications menu. The first run
// (or `wifidrop install`) copies the binary to a stable location and creates
// the platform's launcher pointing at it:
//
//   - Windows: %LocalAppData%\Programs\WiFiDrop\wifidrop.exe plus shortcuts on
//     the Desktop and in the Start Menu (the icon is embedded in the .exe).
//   - macOS:   ~/Applications/WiFiDrop.app with Info.plist and an .icns icon.
//   - Linux:   ~/.local/bin/wifidrop, a .desktop launcher and a PNG icon.

const bundleID = "com.nacunicella.wifidrop"

func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "Programs", "WiFiDrop"), nil
	case "darwin":
		return filepath.Join(home, "Applications", "WiFiDrop.app", "Contents", "MacOS"), nil
	default:
		return filepath.Join(home, ".local", "bin"), nil
	}
}

func installedExePath() (string, error) {
	dir, err := installDir()
	if err != nil {
		return "", err
	}
	name := "wifidrop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name), nil
}

func selfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return self, nil
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// copySelf copies the running binary to the install location unless it is
// already running from there.
func copySelf() (string, error) {
	self, err := selfPath()
	if err != nil {
		return "", err
	}
	dest, err := installedExePath()
	if err != nil {
		return "", err
	}
	if sameFile(self, dest) {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(self)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	// On Windows the destination may be a running copy, which cannot be
	// replaced; keep the existing one in that case.
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		if _, statErr := os.Stat(dest); statErr == nil {
			return dest, nil
		}
		return "", err
	}
	return dest, nil
}

// install creates the launcher and returns a human readable list of what was
// created.
func install() ([]string, error) {
	exe, err := copySelf()
	if err != nil {
		return nil, fmt.Errorf("no se pudo copiar el programa: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		return installWindows(exe)
	case "darwin":
		return installDarwin(exe)
	default:
		return installLinux(exe)
	}
}

func uninstall() ([]string, error) {
	switch runtime.GOOS {
	case "windows":
		return uninstallWindows()
	case "darwin":
		return uninstallDarwin()
	default:
		return uninstallLinux()
	}
}

// --- Windows ---------------------------------------------------------------

// psQuote returns a single-quoted PowerShell string literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func installWindows(exe string) ([]string, error) {
	script := `$ws = New-Object -ComObject WScript.Shell
foreach ($dir in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  if (-not $dir) { continue }
  $lnk = Join-Path $dir 'WiFiDrop.lnk'
  $s = $ws.CreateShortcut($lnk)
  $s.TargetPath = ` + psQuote(exe) + `
  $s.WorkingDirectory = ` + psQuote(filepath.Dir(exe)) + `
  $s.IconLocation = ` + psQuote(exe+",0") + `
  $s.Description = 'WiFiDrop: transferencia de archivos por Wi-Fi'
  $s.Save()
  Write-Output $lnk
}`
	out, err := runPowerShell(script)
	if err != nil {
		return nil, err
	}
	created := []string{exe}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			created = append(created, line)
		}
	}
	return created, nil
}

func uninstallWindows() ([]string, error) {
	script := `foreach ($dir in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  if (-not $dir) { continue }
  $lnk = Join-Path $dir 'WiFiDrop.lnk'
  if (Test-Path $lnk) { Remove-Item $lnk; Write-Output $lnk }
}`
	out, err := runPowerShell(script)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			removed = append(removed, line)
		}
	}
	dir, _ := installDir()
	self, _ := selfPath()
	if dir != "" && !strings.HasPrefix(self, dir) {
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, dir)
		}
	} else if dir != "" {
		removed = append(removed, dir+" (bórralo a mano cuando WiFiDrop esté cerrado)")
	}
	return removed, nil
}

// --- Linux -----------------------------------------------------------------

func linuxPaths() (desktop, icon string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "applications", "wifidrop.desktop"),
		filepath.Join(data, "icons", "hicolor", "512x512", "apps", "wifidrop.png"), nil
}

func installLinux(exe string) ([]string, error) {
	desktop, icon, err := linuxPaths()
	if err != nil {
		return nil, err
	}
	iconData, err := webFS.ReadFile("web/icon-512.png")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(icon, iconData, 0o644); err != nil {
		return nil, err
	}
	entry := "[Desktop Entry]\nType=Application\nName=WiFiDrop\nComment=Transferencia de archivos por Wi-Fi\n" +
		"Exec=" + exe + "\nIcon=wifidrop\nTerminal=false\nCategories=Network;Utility;\nKeywords=wifi;archivos;transferir;\n"
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(desktop, []byte(entry), 0o644); err != nil {
		return nil, err
	}
	created := []string{exe, desktop, icon}
	// Some desktops also show launchers placed on the Desktop folder.
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"Desktop", "Escritorio"} {
			dir := filepath.Join(home, name)
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				link := filepath.Join(dir, "wifidrop.desktop")
				if err := os.WriteFile(link, []byte(entry), 0o755); err == nil {
					created = append(created, link)
				}
				break
			}
		}
	}
	exec.Command("update-desktop-database", filepath.Dir(desktop)).Run() // optional
	return created, nil
}

func uninstallLinux() ([]string, error) {
	desktop, icon, err := linuxPaths()
	if err != nil {
		return nil, err
	}
	exe, _ := installedExePath()
	var removed []string
	paths := []string{desktop, icon}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Desktop", "wifidrop.desktop"), filepath.Join(home, "Escritorio", "wifidrop.desktop"))
	}
	self, _ := selfPath()
	if exe != "" && !sameFile(self, exe) {
		paths = append(paths, exe)
	}
	for _, p := range paths {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed, nil
}

// --- macOS -----------------------------------------------------------------

func installDarwin(exe string) ([]string, error) {
	contents := filepath.Dir(filepath.Dir(exe)) // .../WiFiDrop.app/Contents
	resources := filepath.Join(contents, "Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return nil, err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>WiFiDrop</string>
	<key>CFBundleDisplayName</key><string>WiFiDrop</string>
	<key>CFBundleIdentifier</key><string>` + bundleID + `</string>
	<key>CFBundleExecutable</key><string>wifidrop</string>
	<key>CFBundleIconFile</key><string>wifidrop</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleVersion</key><string>` + version + `</string>
	<key>CFBundleShortVersionString</key><string>` + version + `</string>
	<key>LSMinimumSystemVersion</key><string>10.13</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSHumanReadableCopyright</key><string>MIT</string>
</dict>
</plist>
`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return nil, err
	}
	icns, err := buildICNS()
	if err != nil {
		return nil, err
	}
	iconPath := filepath.Join(resources, "wifidrop.icns")
	if err := os.WriteFile(iconPath, icns, 0o644); err != nil {
		return nil, err
	}
	app := filepath.Dir(contents)
	// Ask Finder/LaunchServices to notice the new bundle and icon.
	exec.Command("touch", app).Run()
	return []string{app}, nil
}

func uninstallDarwin() ([]string, error) {
	exe, err := installedExePath()
	if err != nil {
		return nil, err
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	self, _ := selfPath()
	if strings.HasPrefix(self, app) {
		return []string{app + " (arrástralo a la papelera cuando WiFiDrop esté cerrado)"}, nil
	}
	if err := os.RemoveAll(app); err != nil {
		return nil, err
	}
	return []string{app}, nil
}

// buildICNS packs PNG images of several sizes into Apple's icon container.
// Modern macOS accepts PNG payloads for the ic07..ic14 entry types.
func buildICNS() ([]byte, error) {
	data, err := webFS.ReadFile("web/icon-512.png")
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	entries := []struct {
		typ  string
		size int
	}{{"ic09", 512}, {"ic08", 256}, {"ic07", 128}, {"ic05", 32}, {"ic04", 16}}
	var body bytes.Buffer
	for _, e := range entries {
		var pngData []byte
		if e.size == 512 {
			pngData = data
		} else {
			var buf bytes.Buffer
			if err := png.Encode(&buf, downscale(img, e.size)); err != nil {
				return nil, err
			}
			pngData = buf.Bytes()
		}
		body.WriteString(e.typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(pngData)))
		body.Write(pngData)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// downscale produces a size×size image by averaging blocks of pixels, which
// is good enough for an icon rendered from a 512 px source.
func downscale(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	fx := float64(b.Dx()) / float64(size)
	fy := float64(b.Dy()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, x1 := b.Min.X+int(float64(x)*fx), b.Min.X+int(float64(x+1)*fx)
			y0, y1 := b.Min.Y+int(float64(y)*fy), b.Min.Y+int(float64(y+1)*fy)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r += uint64(cr) * uint64(ca)
					g += uint64(cg) * uint64(ca)
					bl += uint64(cb) * uint64(ca)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 || a == 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = uint8((r / a) >> 8)
			dst.Pix[i+1] = uint8((g / a) >> 8)
			dst.Pix[i+2] = uint8((bl / a) >> 8)
			dst.Pix[i+3] = uint8((a / n) >> 8)
		}
	}
	return dst
}
