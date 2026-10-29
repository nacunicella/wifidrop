package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// osHostname is split out so tests can run without touching the system.
var osHostname = os.Hostname

func urlPathEscape(s string) string { return url.PathEscape(s) }

var sendClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		ResponseHeaderTimeout: 0, // large uploads take as long as they take
		DisableCompression:    true,
	},
}

// forwardUpload streams an already encoded multipart body to another device.
func forwardUpload(ctx context.Context, addr, pin, fromName, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/upload", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(keyHeader, pin)
	req.Header.Set("X-WifiDrop-From", fromName)
	return sendClient.Do(req)
}

// sendFiles uploads local files to another device. Used by the `send`
// subcommand of the CLI.
func sendFiles(ctx context.Context, addr, pin, fromName string, paths []string, progress func(name string, n int64)) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		var werr error
		defer func() {
			mw.Close()
			pw.CloseWithError(werr)
		}()
		for _, p := range paths {
			f, err := os.Open(p)
			if err != nil {
				werr = err
				return
			}
			part, err := mw.CreateFormFile("file", filepath.Base(p))
			if err != nil {
				f.Close()
				werr = err
				return
			}
			n, err := io.Copy(part, f)
			f.Close()
			if err != nil {
				werr = err
				return
			}
			if progress != nil {
				progress(filepath.Base(p), n)
			}
		}
	}()
	resp, err := forwardUpload(ctx, addr, pin, fromName, mw.FormDataContentType(), pr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	return nil
}
