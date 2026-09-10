package server

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Only explicitly trusted reverse proxies may supply the original source IP.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	for _, value := range s.TrustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil && prefix.Contains(peer) {
			if source, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
				return source.Unmap().String()
			}
		}
	}
	return peer.String()
}
func clientBootstrap(r *http.Request) bool {
	return (r.Method == "POST" && (r.URL.Path == "/api/v1/client/enroll" || r.URL.Path == "/api/v1/client/requests")) || (r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/client/requests/"))
}
func (s *Server) clientRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/client/requests", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string `json:"name"`
			Platform string `json:"platform"`
		}
		if !read(w, r, &in) {
			return
		}
		ticket, err := s.Engine.CreateClientRequest(in.Name, in.Platform, s.clientIP(r))
		if err != nil {
			fail(w, 429, err)
			return
		}
		write(w, ticket)
	})
	mux.HandleFunc("GET /api/v1/client/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			fail(w, 401, errors.New("缺少申请凭据"))
			return
		}
		result, err := s.Engine.PollClientRequest(r.PathValue("id"), strings.TrimPrefix(auth, "Bearer "), s.clientIP(r))
		if err != nil {
			fail(w, 403, err)
			return
		}
		write(w, result)
	})
	mux.HandleFunc("GET /api/v1/client-requests", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.Engine.ClientRequests()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, result)
	})
	mux.HandleFunc("POST /api/v1/client-requests/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Decision string `json:"decision"`
		}
		if !read(w, r, &in) {
			return
		}
		if err := s.Engine.DecideClientRequest(r.PathValue("id"), in.Decision); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/client-ip-blocks", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.Engine.ClientIPBlocks()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, result)
	})
	mux.HandleFunc("DELETE /api/v1/client-ip-blocks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.UnblockClientIP(r.PathValue("id")); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
}
