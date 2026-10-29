package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestBuildICNS(t *testing.T) {
	data, err := buildICNS()
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "icns" {
		t.Fatalf("bad magic %q", data[:4])
	}
	if total := binary.BigEndian.Uint32(data[4:8]); int(total) != len(data) {
		t.Fatalf("declared length %d, actual %d", total, len(data))
	}
	// Walk the entries and decode each PNG payload.
	sizes := map[string]int{"ic09": 512, "ic08": 256, "ic07": 128, "ic05": 32, "ic04": 16}
	off := 8
	seen := 0
	for off < len(data) {
		typ := string(data[off : off+4])
		n := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		img, err := png.Decode(bytes.NewReader(data[off+8 : off+n]))
		if err != nil {
			t.Fatalf("entry %s: %v", typ, err)
		}
		if want := sizes[typ]; img.Bounds().Dx() != want || img.Bounds().Dy() != want {
			t.Errorf("entry %s: %dx%d, want %d", typ, img.Bounds().Dx(), img.Bounds().Dy(), want)
		}
		seen++
		off += n
	}
	if seen != len(sizes) {
		t.Errorf("entries = %d, want %d", seen, len(sizes))
	}
}

func TestPsQuote(t *testing.T) {
	if got := psQuote(`C:\Users\O'Brien\x.exe`); got != `'C:\Users\O''Brien\x.exe'` {
		t.Errorf("psQuote = %s", got)
	}
}
