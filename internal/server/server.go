package server

import (
	"aegishook/internal/core"
	"aegishook/internal/localaddr"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Engine                          *core.Engine
	AdminToken, HookToken, AgentDir string
	UI                              fs.FS
	mu                              sync.Mutex
	sessions                        map[string]time.Time
	subscribers                     map[chan struct{}]struct{}
}

func New(e *core.Engine, ui fs.FS, admin, hook, agentDir string) *Server {
	s := &Server{Engine: e, UI: ui, AdminToken: admin, HookToken: hook, AgentDir: agentDir, sessions: map[string]time.Time{}, subscribers: map[chan struct{}]struct{}{}}
	e.Notify = s.publish
	return s
}
func (s *Server) publish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": core.RedactText(err.Error())})
}
func read(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		fail(w, 400, errors.New("JSON 请求无效或超过 256 KB"))
		return false
	}
	return true
}
func eq(a, b string) bool { return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !read(w, r, &in) {
			return
		}
		if !eq(in.Token, s.AdminToken) {
			fail(w, 401, errors.New("管理令牌无效"))
			return
		}
		token := core.ID() + core.ID()
		s.mu.Lock()
		s.sessions[token] = time.Now().Add(12 * time.Hour)
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "aegis_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/logout", func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("aegis_session")
		if c != nil {
			s.mu.Lock()
			delete(s.sessions, c.Value)
			s.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "aegis_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]string{"name": "管理员", "version": core.Version})
	})
	mux.HandleFunc("GET /api/v1/overview", func(w http.ResponseWriter, r *http.Request) {
		rs, err := s.Engine.Reviews()
		if err != nil {
			fail(w, 500, err)
			return
		}
		instances, err := s.Engine.Instances()
		if err != nil {
			fail(w, 500, err)
			return
		}
		counts := map[string]int{"total": 0, "approve": 0, "reject": 0, "pending": 0}
		today := time.Now().Format("2006-01-02")
		for _, v := range rs {
			if v.CreatedAt.Local().Format("2006-01-02") == today {
				counts["total"]++
				counts[v.Decision]++
			}
		}
		write(w, map[string]any{"counts": counts, "instances": instances, "recent": rs, "diagnostic": s.Engine.LocalDiagnostic()})
	})
	mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) { write(w, s.Engine.Config()) })
	mux.HandleFunc("GET /api/v1/settings/versions", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.ConfigVersions()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/settings/default-prompt", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]string{
			"prompt": core.DefaultPrompt, "dataGuardPrompt": core.DataGuardPrompt,
			"dataGuardStart": core.DataGuardStart, "dataGuardEnd": core.DataGuardEnd,
			"dataGuardAnchor": core.DataGuardAnchor,
		})
	})
	mux.HandleFunc("PUT /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			core.Settings
			APIKey *string `json:"apiKey"`
		}
		if !read(w, r, &in) {
			return
		}
		if err := s.Engine.SaveSettings(in.Settings, in.APIKey); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, s.Engine.Config())
	})
	mux.HandleFunc("POST /api/v1/model/test", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.TestModel(r.Context()); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/agents", func(w http.ResponseWriter, r *http.Request) { write(w, core.DetectAgents()) })
	mux.HandleFunc("GET /api/v1/pi", func(w http.ResponseWriter, r *http.Request) {
		v := core.DetectPi()
		v["agentDir"] = s.AgentDir
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/installations", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Installations()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("POST /api/v1/installations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Agent   string `json:"agent"`
			Scope   string `json:"scope"`
			Project string `json:"project"`
		}
		if !read(w, r, &in) {
			return
		}
		v, err := s.Engine.InstallAgent(in.Agent, in.Scope, in.Project, s.AgentDir)
		if err != nil {
			fail(w, 409, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("DELETE /api/v1/installations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.Uninstall(r.PathValue("id")); err != nil {
			fail(w, 409, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/instances", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Instances()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Instances()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("POST /api/v1/instances", func(w http.ResponseWriter, r *http.Request) {
		var in core.Instance
		if !read(w, r, &in) {
			return
		}
		in.Installations = s.Engine.BindAgentInstallations(in.Agent, in.Cwd)
		if err := s.Engine.Register(in); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/instances/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			State string `json:"state"`
		}
		if !read(w, r, &in) {
			return
		}
		if err := s.Engine.Heartbeat(r.PathValue("id"), in.State); err != nil {
			fail(w, 409, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/instances/{id}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.Disconnect(r.PathValue("id")); err != nil {
			fail(w, 409, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/reviews", func(w http.ResponseWriter, r *http.Request) {
		var in core.ReviewInput
		if !read(w, r, &in) {
			return
		}
		v, err := s.Engine.Submit(in)
		if err != nil {
			fail(w, 409, err)
			return
		}
		write(w, hookView(v))
	})
	mux.HandleFunc("GET /api/v1/reviews/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Review(r.PathValue("id"))
		if err != nil {
			fail(w, 404, err)
			return
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if r.URL.Query().Get("instanceId") != v.InstanceID {
				fail(w, 403, errors.New("实例不匹配"))
				return
			}
			write(w, hookView(v))
			return
		}
		write(w, v)
	})
	mux.HandleFunc("POST /api/v1/reviews/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			InstanceID string `json:"instanceId"`
			State      string `json:"state"`
			Result     string `json:"result"`
		}
		if !read(w, r, &in) {
			return
		}
		if err := s.Engine.Result(r.PathValue("id"), in.InstanceID, in.State, in.Result); err != nil {
			fail(w, 409, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Usage()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/calls", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Reviews()
		if err != nil {
			fail(w, 500, err)
			return
		}
		out := []core.Review{}
		for _, review := range v {
			if q := r.URL.Query().Get("sessionId"); q != "" && review.SessionID != q {
				continue
			}
			if q := r.URL.Query().Get("decision"); q != "" && review.Decision != q {
				continue
			}
			out = append(out, review)
		}
		write(w, out)
	})
	mux.HandleFunc("GET /api/v1/calls/export", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Engine.ExportCalls()
		if err != nil {
			fail(w, 500, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="aegishook-calls.json"`)
		w.Write(b)
	})
	mux.HandleFunc("POST /api/v1/approvals/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Digest   string `json:"digest"`
			Decision string `json:"decision"`
			Comment  string `json:"comment"`
		}
		if !read(w, r, &in) {
			return
		}
		v, err := s.Engine.Decide(r.PathValue("id"), in.Digest, in.Decision, in.Comment)
		if err != nil {
			fail(w, 409, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Rules()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/rules/defaults", func(w http.ResponseWriter, r *http.Request) {
		write(w, core.BuiltinRules())
	})
	mux.HandleFunc("POST /api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
		var v core.Rule
		if !read(w, r, &v) {
			return
		}
		if err := s.Engine.SaveRule(v); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("DELETE /api/v1/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.DeleteRule(r.PathValue("id")); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/rules/test", func(w http.ResponseWriter, r *http.Request) {
		var v core.ReviewInput
		if !read(w, r, &v) {
			return
		}
		rules, err := s.Engine.Rules()
		if err != nil {
			fail(w, 500, err)
			return
		}
		d := core.Evaluate(v, rules)
		if d == nil {
			write(w, map[string]string{"decision": "pending", "comment": "规则未定，将进入当前审查模式"})
			return
		}
		write(w, d)
	})
	mux.HandleFunc("GET /api/v1/scopes", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Scopes()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("POST /api/v1/scopes", func(w http.ResponseWriter, r *http.Request) {
		var v core.Scope
		if !read(w, r, &v) {
			return
		}
		if err := s.Engine.SaveScope(v); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/v1/scopes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Engine.DeleteScope(r.PathValue("id")); err != nil {
			fail(w, 400, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Audits()
		if err != nil {
			fail(w, 500, err)
			return
		}
		write(w, v)
	})
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, errors.New("接口不存在")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "." || p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(s.UI, p); err != nil {
			p = "index.html"
		}
		data, err := fs.ReadFile(s.UI, p)
		if err != nil {
			http.Error(w, "UI resource unavailable", 500)
			return
		}
		http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(data))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !hasHost(host) {
			fail(w, 403, errors.New("仅允许本机主机名"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || u.Scheme != "http" {
					fail(w, 403, errors.New("请求来源不匹配"))
					return
				}
			}
			if r.URL.Path != "/api/v1/login" {
				auth := r.Header.Get("Authorization")
				if auth != "" {
					if !strings.HasPrefix(auth, "Bearer ") || !eq(strings.TrimPrefix(auth, "Bearer "), s.HookToken) || !hookAllowed(r) {
						fail(w, 403, errors.New("Hook 凭据无权访问此接口"))
						return
					}
				} else {
					cookie, err := r.Cookie("aegis_session")
					ok := false
					if err == nil {
						s.mu.Lock()
						until, exists := s.sessions[cookie.Value]
						ok = exists && time.Now().Before(until)
						s.mu.Unlock()
					}
					if !ok {
						fail(w, 401, errors.New("请使用本机管理令牌登录"))
						return
					}
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func hasHost(h string) bool { return localaddr.IsLoopback(h) }
func hookAllowed(r *http.Request) bool {
	p := r.URL.Path
	return (r.Method == "POST" && (p == "/api/v1/instances" || p == "/api/v1/reviews" || strings.HasPrefix(p, "/api/v1/instances/") || strings.HasPrefix(p, "/api/v1/reviews/"))) || (r.Method == "GET" && strings.HasPrefix(p, "/api/v1/reviews/"))
}
func hookView(r core.Review) map[string]any {
	return map[string]any{"id": r.ID, "decision": r.Decision, "comment": r.Comment, "ruleId": r.RuleID, "deadline": r.Deadline, "execution": r.Execution}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, errors.New("不支持事件流"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, ch); s.mu.Unlock() }()
	fmt.Fprint(w, "event: update\ndata: {}\n\n")
	f.Flush()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: update\ndata: {}\n\n")
			f.Flush()
		case <-t.C:
			fmt.Fprint(w, "event: update\ndata: {}\n\n")
			f.Flush()
		}
	}
}
