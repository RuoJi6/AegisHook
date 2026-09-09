package bridge

import (
	"aegishook/internal/core"
	"aegishook/internal/server"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func fixture(t *testing.T) (*core.Engine, string, string) {
	t.Helper()
	e, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	srv := httptest.NewServer(server.New(e, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("fixture")}}, "admin-fixture", "hook-fixture", t.TempDir()).Handler())
	t.Cleanup(srv.Close)
	path := filepath.Join(e.Dir, "connection.json")
	if err = os.WriteFile(path, mustJSON(Config{Endpoint: srv.URL, Token: "hook-fixture"}), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	return e, path, cwd
}
func invoke(agent, path string, v any) (int, string, string) {
	var out, log bytes.Buffer
	code := Run(agent, path, bytes.NewReader(mustJSON(v)), &out, &log)
	return code, out.String(), log.String()
}
func native(agent, cwd, call, tool, event string, args map[string]any) map[string]any {
	if agent == "grok" {
		return map[string]any{"hookEventName": event, "sessionId": "session", "cwd": cwd, "toolUseId": call, "toolName": tool, "toolInput": args}
	}
	return map[string]any{"hook_event_name": event, "session_id": "session", "cwd": cwd, "tool_use_id": call, "tool_name": tool, "tool_input": args}
}
func TestNativeProtocolsDenyAllowAndResults(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "grok", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			e, path, cwd := fixture(t)
			code, _, log := invoke(agent, path, native(agent, cwd, "deny", "Bash", "PreToolUse", map[string]any{"command": "passwd fixture"}))
			if code != 2 || !strings.Contains(log, "R1") {
				t.Fatal(code, log)
			}
			allowed := native(agent, cwd, "allow", "Read", "PreToolUse", map[string]any{"file_path": "README.md"})
			code, out, log := invoke(agent, path, allowed)
			if code != 0 {
				t.Fatal(code, out, log)
			}
			if (agent == "claude" || agent == "codex") && strings.TrimSpace(out) != "{}" {
				t.Fatal("native permission bypass", out)
			}
			if c, _, l := invoke(agent, path, allowed); c != 0 {
				t.Fatal(l)
			}
			reviews, _ := e.Reviews()
			if len(reviews) != 2 {
				t.Fatal("duplicate review", len(reviews))
			}
			for _, r := range reviews {
				if r.Agent != agent {
					t.Fatal("wrong source", r)
				}
				if r.CallID == "allow" && r.Execution != "awaiting_execution" {
					t.Fatal("false success", r)
				}
			}
			post := native(agent, cwd, "allow", "Read", "PostToolUse", nil)
			post["tool_response"] = map[string]any{"exit_code": float64(0)}
			post["executionState"] = "succeeded"
			if c, _, l := invoke(agent, path, post); c != 0 {
				t.Fatal(l)
			}
			r, _ := e.Review(hash(hash(agent, "session", cwd), "allow"))
			if r.Execution != "succeeded" {
				t.Fatal(r)
			}
			changed := native(agent, cwd, "allow", "Read", "PreToolUse", map[string]any{"file_path": "different"})
			if c, _, _ := invoke(agent, path, changed); c != 2 {
				t.Fatal("changed args allowed")
			}
			if c, _, l := invoke(agent, path, native(agent, cwd, "", "", "SessionEnd", nil)); c != 0 {
				t.Fatal(l)
			}
			instances, _ := e.Instances()
			if len(instances) != 1 || instances[0].State != "disconnected" || instances[0].ConnectionMode != "events" {
				t.Fatal(instances)
			}
		})
	}
}
func TestHumanReviewBlocksAndDisconnectRejects(t *testing.T) {
	for _, end := range []bool{false, true} {
		t.Run(map[bool]string{false: "approve", true: "disconnect"}[end], func(t *testing.T) {
			e, path, cwd := fixture(t)
			prompt := native("codex", cwd, "", "", "UserPromptSubmit", nil)
			prompt["prompt"] = "fixture token=private-value"
			if c, _, l := invoke("codex", path, prompt); c != 0 {
				t.Fatal(l)
			}
			done := make(chan int, 1)
			go func() {
				code, _, _ := invoke("codex", path, native("codex", cwd, "pending", "Bash", "PreToolUse", map[string]any{"command": "echo fixture"}))
				done <- code
			}()
			var r core.Review
			for n := 0; n < 150; n++ {
				rs, _ := e.Reviews()
				if len(rs) > 0 {
					r = rs[0]
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if r.ID == "" {
				t.Fatal("no pending review")
			}
			if strings.Contains(r.UserMessage, "private-value") || !strings.Contains(r.UserMessage, "REDACTED") {
				t.Fatal("context unredacted", r.UserMessage)
			}
			select {
			case <-done:
				t.Fatal("did not wait")
			default:
			}
			if end {
				if err := e.Disconnect(r.InstanceID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := e.Decide(r.ID, r.Digest, "approve", ""); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case code := <-done:
				if (!end && code != 0) || (end && code != 2) {
					t.Fatal(code)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("bridge stuck")
			}
		})
	}
}
func TestOfflineMalformedAndUnavailableAuditFailClosed(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "grok", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			e, path, cwd := fixture(t)
			e.DB.Close()
			if code, _, _ := invoke(agent, path, native(agent, cwd, "call", "Read", "PreToolUse", map[string]any{"path": "fixture"})); code != 2 {
				t.Fatal("audit unavailable allowed")
			}
			for _, input := range []string{"not json", "null", `{} {}`} {
				var out, log bytes.Buffer
				if code := Run(agent, path, strings.NewReader(input), &out, &log); code != 2 {
					t.Fatal(input, code)
				}
			}
			os.WriteFile(path, mustJSON(Config{Endpoint: "http://127.0.0.1:1", Token: "test"}), 0600)
			if code, _, _ := invoke(agent, path, native(agent, cwd, "call", "Read", "PreToolUse", map[string]any{"path": "fixture"})); code != 2 {
				t.Fatal("offline allowed")
			}
		})
	}
}
func TestInvalidServerVerdict(t *testing.T) {
	for _, decision := range []string{"ask", "", "allow"} {
		t.Run(decision, func(t *testing.T) {
			cwd := t.TempDir()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"id": hash(hash("codex", "session", cwd), "call"), "decision": decision, "deadline": time.Now().Add(time.Minute)})
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "connection.json")
			os.WriteFile(path, mustJSON(Config{Endpoint: srv.URL, Token: "test"}), 0600)
			if code, _, _ := invoke("codex", path, native("codex", cwd, "call", "Read", "PreToolUse", map[string]any{"path": "fixture"})); code != 2 {
				t.Fatal("invalid decision allowed")
			}
		})
	}
}
func TestGrokWithoutCallIDAndUnknownCodexResult(t *testing.T) {
	e, path, cwd := fixture(t)
	pre := native("grok", cwd, "", "Read", "PreToolUse", map[string]any{"path": "fixture"})
	if c, _, l := invoke("grok", path, pre); c != 0 {
		t.Fatal(l)
	}
	post := native("grok", cwd, "", "Read", "PostToolUse", nil)
	if c, _, l := invoke("grok", path, post); c != 0 {
		t.Fatal(l)
	}
	reviews, _ := e.Reviews()
	if len(reviews) != 1 || reviews[0].Execution != "awaiting_execution" {
		t.Fatal("fabricated result", reviews)
	}
	state, _ := resultState("codex", "PostToolUse", map[string]any{"tool_response": "looks okay"})
	if state != "" {
		t.Fatal("unknown result claimed success")
	}
	state, _ = resultState("codex", "PostToolUse", map[string]any{"tool_response": "Process exited with code 1\nerror"})
	if state != "failed" {
		t.Fatal(state)
	}
}
