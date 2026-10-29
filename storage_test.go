package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"foto.jpg":                 "foto.jpg",
		"../../etc/passwd":         "passwd",
		`..\..\windows\system.ini`: "system.ini",
		"con:fig?.txt":             "con_fig_.txt",
		"  spaced .pdf  ":          "spaced .pdf",
		"":                         "archivo",
		"...":                      "archivo",
		"nombre\x00nulo.txt":       "nombrenulo.txt",
		"ñandú ⚡.png":              "ñandú ⚡.png",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 300) + ".txt"
	if got := safeName(long); len(got) != 200 || !strings.HasSuffix(got, ".txt") {
		t.Errorf("long name not truncated correctly: len=%d", len(got))
	}
}

func TestSaveUniqueAndList(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStorage(filepath.Join(dir, "recv"))
	if err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"uno", "dos", "tres"} {
		info, err := s.Save("nota.txt", strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"nota.txt", "nota (1).txt", "nota (2).txt"}[i]
		if info.Name != want {
			t.Errorf("save %d: name %q, want %q", i, info.Name, want)
		}
	}
	files, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "nota.txt.part")); !os.IsNotExist(err) {
		t.Errorf("temporary .part file left behind")
	}
	f, st, err := s.Open("nota (1).txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if st.Size() != 3 {
		t.Errorf("size = %d, want 3", st.Size())
	}
	if _, _, err := s.Open("../go.mod"); err == nil {
		t.Errorf("Open must reject paths outside the folder")
	}
	if err := s.Delete("nota.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("../go.mod"); err == nil {
		t.Errorf("Delete must reject paths outside the folder")
	}
}
