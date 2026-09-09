package server

import (
	"aegishook/internal/core"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAuthBoundaries(t *testing.T) {
	e, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var ui fs.FS = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("app")}}
	s := New(e, ui, "admin-secret", "hook-secret", t.TempDir())
	handler := s.Handler()
	cases := []struct {
		method, path, auth, origin, host string
		status                           int
	}{{"POST", "/api/v1/rules", "Bearer hook-secret", "", "127.0.0.1:18790", 403}, {"GET", "/api/v1/settings", "", "", "127.0.0.1:18790", 401}, {"GET", "/api/v1/settings", "Bearer hook-secret", "", "127.0.0.1:18790", 403}, {"DELETE", "/api/v1/installations/x", "Bearer hook-secret", "", "127.0.0.1:18790", 403}, {"GET", "/", "", "", "evil.example", 403}, {"POST", "/api/v1/login", "", "http://evil.example", "127.0.0.1:18790", 403}}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://"+c.host+c.path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", c.auth)
		r.Header.Set("Origin", c.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatalf("%s %s got %d", c.method, c.path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:18790/api/v1/login", strings.NewReader(`{"token":"admin-secret"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("weak session cookie")
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:18790/api/v1/settings", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "admin-secret") {
		t.Fatal(w.Body.String())
	}
}

func TestSPADeepLinkNoRedirect(t *testing.T) {
	e, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ui := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("app")}}
	h := New(e, ui, "a", "h", t.TempDir()).Handler()
	for _, path := range []string{"/", "/settings", "/approvals"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:18790"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != "app" {
			t.Fatalf("deep link %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
