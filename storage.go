package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Storage saves received files into a single folder and lists them for
// download. File names are sanitised so a remote client can never write
// outside of the folder.

type Storage struct {
	Dir string
}

type FileInfo struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func NewStorage(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Storage{Dir: abs}, nil
}

// safeName strips directories and control characters from a client supplied
// file name and returns something safe to create on every platform.
func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = name[strings.LastIndex(name, "/")+1:]
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32 || r == 127:
			continue
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimFunc(b.String(), func(r rune) bool { return unicode.IsSpace(r) || r == '.' })
	if out == "" {
		out = "archivo"
	}
	if len(out) > 200 {
		ext := filepath.Ext(out)
		out = out[:200-len(ext)] + ext
	}
	return out
}

// uniquePath returns a path in the storage folder that does not exist yet,
// appending " (n)" before the extension when needed.
func (s *Storage) uniquePath(name string) string {
	base := safeName(name)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	candidate := filepath.Join(s.Dir, base)
	for i := 1; ; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = filepath.Join(s.Dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

// Save streams r into a new file and returns the final file name. It writes to
// a temporary ".part" file first so an interrupted transfer never leaves a
// half file that looks complete.
func (s *Storage) Save(name string, r io.Reader) (FileInfo, error) {
	final := s.uniquePath(name)
	tmp := final + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return FileInfo{}, err
	}
	buf := make([]byte, 1<<20) // 1 MiB copy buffer for fast LAN transfers
	n, err := io.CopyBuffer(f, r, buf)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return FileInfo{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return FileInfo{}, err
	}
	return FileInfo{Name: filepath.Base(final), Size: n, Modified: time.Now()}, nil
}

// Open returns the file with the given name if it lives in the folder.
func (s *Storage) Open(name string) (*os.File, os.FileInfo, error) {
	clean := safeName(name)
	if clean != name {
		return nil, nil, os.ErrNotExist
	}
	path := filepath.Join(s.Dir, clean)
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		return nil, nil, os.ErrNotExist
	}
	return f, st, nil
}

func (s *Storage) Delete(name string) error {
	clean := safeName(name)
	if clean != name {
		return os.ErrNotExist
	}
	return os.Remove(filepath.Join(s.Dir, clean))
}

// List returns the files in the folder, newest first.
func (s *Storage) List() ([]FileInfo, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".part") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileInfo{Name: e.Name(), Size: info.Size(), Modified: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}
