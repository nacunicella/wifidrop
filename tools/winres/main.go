// winres writes the Windows resource objects (rsrc_windows_*.syso) that give
// wifidrop.exe its icon, version information and application manifest. The
// Go linker picks the .syso files up automatically when building for Windows.
//
//	go run ./tools/winres [version]
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	ver := "0.0.0"
	if len(os.Args) > 1 && os.Args[1] != "" {
		ver = os.Args[1]
	}
	if err := run(ver); err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
}

func run(ver string) error {
	data, err := os.ReadFile("web/icon-512.png")
	if err != nil {
		return err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	icon, err := winres.NewIconFromResizedImage(img, []int{16, 24, 32, 48, 64, 128, 256})
	if err != nil {
		return err
	}

	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APP"), icon); err != nil {
		return err
	}
	rs.SetManifest(winres.AppManifest{
		Description:   "WiFiDrop: transferencia de archivos por Wi-Fi",
		Compatibility: winres.Win7AndAbove,
		DPIAwareness:  winres.DPIPerMonitorV2,
		LongPathAware: true,
	})
	vi := version.Info{}
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	for k, v := range map[string]string{
		version.ProductName:      "WiFiDrop",
		version.FileDescription:  "WiFiDrop",
		version.OriginalFilename: "wifidrop.exe",
		version.LegalCopyright:   "MIT",
		version.InternalName:     "wifidrop",
	} {
		if err := vi.Set(0, k, v); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)

	for name, arch := range map[string]winres.Arch{
		"rsrc_windows_amd64.syso": winres.ArchAMD64,
		"rsrc_windows_arm64.syso": winres.ArchARM64,
	} {
		f, err := os.Create(name)
		if err != nil {
			return err
		}
		if err := rs.WriteObject(f, arch); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Println("escrito", name)
	}
	return nil
}
