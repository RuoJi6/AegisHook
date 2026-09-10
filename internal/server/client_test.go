package server

import (
	"aegishook/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestClientRequestAPIAndIsolation(t *testing.T) {
	e, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := New(e, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("app")}}, "admin-fixture", "legacy-fixture", t.TempDir())
	s.PublicURL = "https://review.example"
	s.DownloadID = "distribution-fixture"
	h := s.Handler()
	request := func(method, path, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://review.example"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:12345"
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := request("POST", "/api/v1/login", "", `{"token":"admin-fixture"}`, nil)
	cookie := login.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatal("public cookie not secure")
	}
	w := request("GET", "/install/distribution-fixture/hook.sh", "", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://review.example") || strings.Contains(w.Body.String(), "legacy-fixture") {
		t.Fatal("script delivery failed or leaked credential")
	}
	w = request("POST", "/api/v1/client/requests", "", `{"name":"test","platform":"linux"}`, nil)
	var ticket core.ClientRequestTicket
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ticket) != nil {
		t.Fatal("bootstrap failed")
	}
	if w = request("GET", "/api/v1/client-requests", ticket.Secret, "", nil); w.Code != 403 {
		t.Fatal("request credential has admin access")
	}
	if w = request("POST", "/api/v1/client-requests/"+ticket.ID+"/decision", "", `{"decision":"approve"}`, nil); w.Code != 401 {
		t.Fatal("approval did not require admin")
	}
	w = request("POST", "/api/v1/client-requests/"+ticket.ID+"/decision", "", `{"decision":"approve"}`, cookie)
	if w.Code != 200 {
		t.Fatal("admin approval failed")
	}
	w = request("GET", "/api/v1/client/requests/"+ticket.ID, ticket.Secret, "", nil)
	var result core.ClientRequestResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Connection == nil {
		t.Fatal("approved credential not issued")
	}
	conn := result.Connection
	register := `{"id":"remote","sessionId":"session","cwd":"/remote/project","agent":"claude","hookVersion":"` + core.Version + `","nodeId":"` + conn.NodeID + `"}`
	if w = request("POST", "/api/v1/instances", conn.Token, register, nil); w.Code != 200 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	otherCode, _ := e.CreateEnrollment("other")
	other, _ := e.EnrollClient(otherCode.Code, "linux")
	if w = request("POST", "/api/v1/instances/remote/heartbeat", other.Token, `{}`, nil); w.Code != 403 {
		t.Fatalf("cross-node access: %d", w.Code)
	}
	if w = request("GET", "/api/v1/settings", conn.Token, "", nil); w.Code != 403 {
		t.Fatal("node has admin access")
	}
	if w = request("POST", "/api/v1/instances", "legacy-fixture", register, nil); w.Code != 403 {
		t.Fatal("shared local token accepted publicly")
	}
	e.RevokeClient(conn.NodeID)
	if w = request("POST", "/api/v1/instances/remote/heartbeat", conn.Token, `{}`, nil); w.Code != 403 {
		t.Fatal("revoked node accepted")
	}
}
func TestClientTrustedProxyIP(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "http://127.0.0.1:18790/api/v1/client/requests", nil)
	r.RemoteAddr = "192.0.2.2:4567"
	r.Header.Set("X-Real-IP", "203.0.113.1")
	if s.clientIP(r) != "192.0.2.2" {
		t.Fatal("untrusted header used")
	}
	s.TrustedProxies = []string{"192.0.2.2/32"}
	if s.clientIP(r) != "203.0.113.1" {
		t.Fatal("trusted proxy not used")
	}
	r.Header.Set("X-Real-IP", "203.0.113.1, 203.0.113.2")
	if s.clientIP(r) != "192.0.2.2" {
		t.Fatal("ambiguous IP accepted")
	}
}
