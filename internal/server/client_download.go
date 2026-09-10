package server

import (
	"aegishook/internal/assets"
	"aegishook/internal/core"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) clientDownloads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/client-installer", func(w http.ResponseWriter, r *http.Request) {
		if s.DownloadID == "" {
			fail(w, 503, errors.New("下载入口尚未配置"))
			return
		}
		base := s.installEndpoint(r) + "/install/" + s.DownloadID
		write(w, map[string]string{"shellUrl": base + "/hook.sh", "powershellUrl": base + "/hook.ps1", "endpoint": s.installEndpoint(r)})
	})
	mux.HandleFunc("GET /install/{id}/{script}", func(w http.ResponseWriter, r *http.Request) {
		if s.DownloadID == "" || r.PathValue("id") != s.DownloadID {
			http.NotFound(w, r)
			return
		}
		filename := r.PathValue("script")
		resource := ""
		switch filename {
		case "hook.sh":
			resource = "hooks.sh"
		case "hook.ps1":
			resource = "hooks.ps1"
		default:
			http.NotFound(w, r)
			return
		}
		b, err := assets.Files.ReadFile(resource)
		if err != nil {
			fail(w, 500, errors.New("安装脚本资源不可用"))
			return
		}
		endpoint := s.installEndpoint(r)
		base := ""
		if s.ClientBinaryDir != "" {
			base = endpoint + "/install/" + s.DownloadID
		}
		if filename == "hook.sh" {
			quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
			b = append([]byte("#!/usr/bin/env bash\nexport AEGIS_INSTALL_ENDPOINT="+quote(endpoint)+"\nexport AEGIS_INSTALL_DOWNLOAD_BASE="+quote(base)+"\n"), b...)
		} else {
			quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
			b = []byte(strings.Replace(string(b), "[string]$Endpoint,", "[string]$Endpoint = "+quote(endpoint)+",", 1))
			b = []byte(strings.Replace(string(b), "[string]$DownloadBase = $env:AEGIS_INSTALL_DOWNLOAD_BASE,", "[string]$DownloadBase = "+quote(base)+",", 1))
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("GET /install/{id}/{kind}/{os}/{arch}", func(w http.ResponseWriter, r *http.Request) {
		if s.DownloadID == "" || r.PathValue("id") != s.DownloadID || s.ClientBinaryDir == "" {
			http.NotFound(w, r)
			return
		}
		platform, arch, kind := r.PathValue("os"), r.PathValue("arch"), r.PathValue("kind")
		if (platform != "linux" && platform != "darwin" && platform != "windows") || (arch != "arm64" && arch != "amd64") || (platform == "windows" && arch != "amd64") || (kind != "bin" && kind != "sha256") {
			http.NotFound(w, r)
			return
		}
		name := "aegishook-" + platform + "-" + arch
		if platform == "windows" {
			name += ".exe"
		}
		p := filepath.Join(s.ClientBinaryDir, name)
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Cache-Control", "no-store")
		if kind == "sha256" {
			h := sha256.New()
			if _, err = io.Copy(h, f); err != nil {
				fail(w, 500, errors.New("程序校验失败"))
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, hex.EncodeToString(h.Sum(nil)))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
}
func (s *Server) installEndpoint(r *http.Request) string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	return "http://" + r.Host
}

// The distribution ID is a stable path, not an authentication credential.
func InitializeClientDownloads(e *core.Engine) (string, error) {
	p := filepath.Join(e.Dir, "installer.id")
	if b, err := os.ReadFile(p); err == nil {
		v := strings.TrimSpace(string(b))
		if decoded, e := hex.DecodeString(v); e == nil && len(decoded) == 16 {
			return v, nil
		}
		return "", errors.New("安装脚本路径配置无效")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id := core.ID()
	return id, core.AtomicFile(p, []byte(id), 0600)
}
