package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func engine(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	if err = e.Register(Instance{ID: "instance", SessionID: "session", Cwd: t.TempDir(), HookVersion: Version}); err != nil {
		t.Fatal(err)
	}
	return e
}
func input(call, tool string, args map[string]any) ReviewInput {
	return ReviewInput{InstanceID: "instance", CallID: call, ToolName: tool, Arguments: args}
}
func TestHardRulesAndNoExecution(t *testing.T) {
	e := engine(t)
	cases := []struct{ cmd, rule string }{{"passwd alice", "R1"}, {"sudo userdel alice", "R3"}, {"systemctl restart nginx", "R5"}, {"echo x > /etc/nginx/nginx.conf", "R2"}, {"mysql -e 'DROP TABLE users'", "R4"}, {"hping3 --flood example.test", "R6"}}
	for _, c := range cases {
		r, err := e.Submit(input(ID(), "bash", map[string]any{"command": c.cmd}))
		if err != nil || r.Decision != "reject" || r.RuleID != c.rule || r.Execution != "not_executed" {
			t.Fatalf("%q got %+v / %v", c.cmd, r, err)
		}
		if err = e.Result(r.ID, "instance", "succeeded", ""); err == nil {
			t.Fatal("rejected execution accepted")
		}
	}
}
func TestDenyPrecedenceAndCustomRules(t *testing.T) {
	e := engine(t)
	allow := Rule{ID: "U_ALLOW", Name: "allow all", Enabled: true, Decision: "approve", Pattern: ".*"}
	if err := e.SaveRule(allow); err != nil {
		t.Fatal(err)
	}
	r, err := e.Submit(input(ID(), "bash", map[string]any{"command": "passwd alice"}))
	if err != nil || r.RuleID != "R1" {
		t.Fatal(r, err)
	}
	deny := Rule{ID: "U_DENY", Name: "deny read secret", Enabled: true, Decision: "reject", Tool: "read", Field: "path", Pattern: "secret"}
	e.SaveRule(deny)
	r, err = e.Submit(input(ID(), "read", map[string]any{"path": "secret.txt"}))
	if err != nil || r.RuleID != "U_DENY" || r.Decision != "reject" {
		t.Fatal(r, err)
	}
	if e.DeleteRule("R1") == nil {
		t.Fatal("deleted builtin")
	}
}
func TestHumanBindingAndIdempotence(t *testing.T) {
	e := engine(t)
	r, err := e.Submit(input("call", "bash", map[string]any{"command": "echo hello"}))
	if err != nil || r.Decision != "pending" {
		t.Fatal(r, err)
	}
	same, err := e.Submit(input("call", "bash", map[string]any{"command": "echo hello"}))
	if err != nil || same.ID != r.ID {
		t.Fatal("not idempotent")
	}
	if _, err = e.Submit(input("call", "bash", map[string]any{"command": "echo changed"})); err == nil {
		t.Fatal("changed parameters accepted")
	}
	if _, err = e.Decide(r.ID, "wrong", "approve", ""); err == nil {
		t.Fatal("bad digest accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := e.Decide(r.ID, r.Digest, "approve", ""); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, _ = e.Review(r.ID)
	if r.Execution != "awaiting_execution" {
		t.Fatal(r)
	}
	if err = e.Result(r.ID, "instance", "succeeded", "done"); err != nil {
		t.Fatal(err)
	}
	if err = e.Result(r.ID, "instance", "failed", "late"); err == nil {
		t.Fatal("terminal result overwritten")
	}
}
func TestExpiryAndShutdown(t *testing.T) {
	e := engine(t)
	r, _ := e.Submit(input("timeout", "bash", map[string]any{"command": "unknown"}))
	r.Deadline = time.Now().Add(-time.Second)
	e.put("reviews", r.ID, r)
	e.Expire()
	r, _ = e.Review(r.ID)
	if r.RuleID != "E_TIMEOUT" {
		t.Fatal(r)
	}
	r, _ = e.Submit(input("shutdown", "bash", map[string]any{"command": "unknown"}))
	e.Disconnect("instance")
	r, _ = e.Review(r.ID)
	if r.RuleID != "E_SESSION" {
		t.Fatal(r)
	}
}
func TestRestartAndExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(dir); err == nil {
		t.Fatal("concurrent process accepted")
	}
	e.Register(Instance{ID: "instance", SessionID: "s", Cwd: t.TempDir(), HookVersion: Version})
	r, _ := e.Submit(input("pending", "bash", map[string]any{"command": "unknown"}))
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	r, _ = e.Review(r.ID)
	if r.RuleID != "E_RESTART" {
		t.Fatal(r)
	}
}
func TestLifecycleAndOwnership(t *testing.T) {
	e := engine(t)
	if err := e.PrepareAdapter([]byte("test"), "http://127.0.0.1:1", "token"); err != nil {
		t.Fatal(err)
	}
	agent := t.TempDir()
	project := t.TempDir()
	g, err := e.Install("global", "", agent)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Install("project", project, agent)
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := e.Install("global", "", agent)
	if g.ID != g2.ID {
		t.Fatal("install not idempotent")
	}
	a, _ := filepath.EvalSymlinks(g.Entry)
	b, _ := filepath.EvalSymlinks(p.Entry)
	if a != b {
		t.Fatal("different realpaths")
	}
	e.Register(Instance{ID: "bound", SessionID: "s", Cwd: project, HookVersion: Version, Installations: e.BindInstallations(project)})
	if err = e.Uninstall(p.ID); err != nil {
		t.Fatal(err)
	}
	is, _ := e.Installations()
	for _, i := range is {
		if i.ID == p.ID && i.Status != "waiting_reload" {
			t.Fatal(i)
		}
		if i.ID == g.ID && i.Status != "loaded" {
			t.Fatal(i)
		}
	}
	if _, err = os.Stat(g.Entry); err != nil {
		t.Fatal(err)
	}
	e.Uninstall(g.ID)
	os.WriteFile(g.Entry, []byte("not ours"), 0600)
	if _, err = e.Install("global", "", agent); err == nil {
		t.Fatal("foreign file overwritten")
	}
	if err = e.Uninstall(g.ID); err == nil {
		t.Fatal("foreign file deleted")
	}
}
func TestModelSchema(t *testing.T) {
	valid := `{"decision":"approve","comment":"实际操作：未知脚本；成功后的后果：当前参数未显示明确高危操作；命中规则：D1"}`
	if _, err := ParseModelDecision(valid); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"```json\n" + valid + "\n```", strings.Replace(valid, "D1", "无", 1), strings.Replace(valid, "D1", "R1", 1), strings.TrimSuffix(valid, "}") + `,"editedArguments":{}}`, valid + valid} {
		if _, err := ParseModelDecision(v); err == nil {
			t.Fatalf("invalid accepted %s", v)
		}
	}
}
func TestModelPipelineAndSnapshot(t *testing.T) {
	e := engine(t)
	gate := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error(r.URL)
		}
		<-gate
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"decision":"approve","comment":"实际操作：不透明脚本；成功后的后果：当前参数未显示明确高危操作；命中规则：D1"}`}}}})
	}))
	defer up.Close()
	e.mu.Lock()
	e.Settings.Mode = "model"
	e.Settings.Model = ModelConfig{BaseURL: up.URL + "/v1", Model: "test", Tested: true}
	e.mu.Unlock()
	r, err := e.Submit(input("model", "bash", map[string]any{"command": "opaque-script"}))
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := r.Version
	e.mu.Lock()
	e.Settings.Version++
	e.Settings.Mode = "human"
	e.mu.Unlock()
	close(gate)
	for n := 0; n < 100; n++ {
		r, _ = e.Review(r.ID)
		if r.Decision != "pending" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Decision != "approve" || r.RuleID != "D1" || r.Version != oldVersion || r.Mode != "model" {
		t.Fatal(r)
	}
}
func TestModelFailureAndRedaction(t *testing.T) {
	e := engine(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer up.Close()
	e.mu.Lock()
	e.Settings.Mode = "model"
	e.Settings.Model = ModelConfig{BaseURL: up.URL, Model: "test", Tested: true}
	e.mu.Unlock()
	r, _ := e.Submit(input("failure", "tool", map[string]any{"password": "sensitive-value", "headers": map[string]any{"Authorization": "Bearer private-value"}, "command": "echo token=hidden-value"}))
	for n := 0; n < 100; n++ {
		r, _ = e.Review(r.ID)
		if r.Decision != "pending" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Decision != "reject" || r.RuleID != "E_MODEL" {
		t.Fatal(r)
	}
	b, _ := e.ExportCalls()
	for _, secret := range []string{"sensitive-value", "private-value", "hidden-value"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("leaked secret", secret)
		}
	}
	if err := e.TestModel(context.Background()); err == nil {
		t.Fatal("bad model marked valid")
	}
}
func TestScopeAndAuditFailure(t *testing.T) {
	e := engine(t)
	instances, _ := e.Instances()
	cwd := instances[0].Cwd
	err := e.SaveScope(Scope{Name: "test", Project: cwd, Targets: []string{"staging.example.test"}, Paths: []string{cwd}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Submit(input("out-of-scope", "http", map[string]any{"method": "GET", "url": "https://other.example.test"}))
	if err != nil || r.RuleID != "SCOPE_TARGET" {
		t.Fatal(r, err)
	}
	e.DB.Exec(`CREATE TRIGGER reject_audit BEFORE INSERT ON records WHEN NEW.kind='audit' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END;`)
	_, err = e.Submit(input("audit-failure", "read", map[string]any{"path": "README.md"}))
	if err == nil {
		t.Fatal("allowed without durable audit")
	}
	rs, _ := e.Reviews()
	for _, r := range rs {
		if r.CallID == "audit-failure" {
			t.Fatal("partial transaction persisted")
		}
	}
}
func TestAnthropicProtocol(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/anthropic/v1/messages" {
			t.Error(r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "private-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			t.Error("wrong authentication")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["system"] == nil || body["max_tokens"] == nil {
			t.Error("missing Anthropic fields")
		}
		json.NewEncoder(w).Encode(map[string]any{"stop_reason": "end_turn", "content": []any{map[string]string{"type": "text", "text": `{"decision":"approve","comment":"实际操作：读取；成功后的后果：获得信息；命中规则：A7"}`}}})
	}))
	defer up.Close()
	d, err := callModel(context.Background(), Settings{Prompt: DefaultPrompt, ModelSeconds: 3, Model: ModelConfig{Protocol: "anthropic", BaseURL: up.URL + "/apps/anthropic", Model: "qwen-flash", APIKey: "private-key"}}, map[string]any{"toolName": "read"})
	if err != nil || d.RuleID != "A7" {
		t.Fatal(d, err)
	}
}

func TestLostHeartbeatCancelsPending(t *testing.T) {
	e := engine(t)
	r, err := e.Submit(input("lost-heartbeat", "bash", map[string]any{"command": "opaque"}))
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	var i Instance
	e.get("instances", "instance", &i)
	i.Heartbeat = time.Now().Add(-31 * time.Second)
	e.put("instances", i.ID, i)
	e.mu.Unlock()
	e.Expire()
	r, _ = e.Review(r.ID)
	if r.RuleID != "E_SESSION" || r.Decision != "reject" {
		t.Fatal(r)
	}
	if _, err = e.Decide(r.ID, r.Digest, "approve", ""); err == nil {
		t.Fatal("expired approval accepted")
	}
}
func TestRedactionFormats(t *testing.T) {
	for _, s := range []string{`{"password":"private-value"}`, `curl --token private-value`, `https://user:private-value@example.test`, `Authorization: Bearer private-value`, `token=private-value`} {
		if strings.Contains(RedactText(s), "private-value") {
			t.Fatalf("unredacted format: %s", s)
		}
	}
}
func TestConfigurationVersionAndKeyRollback(t *testing.T) {
	e := engine(t)
	v := e.Config().Version
	if err := e.SaveRule(Rule{ID: "U_TEST", Name: "test", Enabled: true, Decision: "reject", Tool: "test", Pattern: ".*"}); err != nil {
		t.Fatal(err)
	}
	if e.Config().Version != v+1 {
		t.Fatal("rule edit not versioned")
	}
	s := e.Config()
	key := "original-private-key"
	if err := e.SaveSettings(s, &key); err != nil {
		t.Fatal(err)
	}
	e.DB.Exec(`CREATE TRIGGER reject_settings_audit BEFORE INSERT ON records WHEN NEW.kind='audit' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END;`)
	next := "replacement-private-key"
	if e.SaveSettings(e.Config(), &next) == nil {
		t.Fatal("saved config without audit")
	}
	stored, err := os.ReadFile(filepath.Join(e.Dir, "model.key"))
	if err != nil || string(stored) != key {
		t.Fatal("key rollback failed")
	}
	b, _ := json.Marshal(e.Config())
	if strings.Contains(string(b), key) {
		t.Fatal("key exposed")
	}
}
func TestModelTimeoutFailsClosed(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(1500 * time.Millisecond):
		}
	}))
	defer up.Close()
	_, err := callModel(context.Background(), Settings{Prompt: DefaultPrompt, ModelSeconds: 1, Model: ModelConfig{BaseURL: up.URL, Model: "test"}}, map[string]any{})
	if err == nil {
		t.Fatal("timeout approved")
	}
}

func TestCanonicalPathsAndQuotedCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("/etc", filepath.Join(root, "config")); err != nil {
		t.Fatal(err)
	}
	cases := []ReviewInput{input("relative", "write", map[string]any{"path": "config/new-aegis-fixture.conf"}), input("quoted", "bash", map[string]any{"command": `"passwd" 'isolated-user'`}), input("redirect", "bash", map[string]any{"command": `echo fixture > "config/new-aegis-fixture.conf"`})}
	for _, in := range cases {
		d := evaluateAt(in, BuiltinRules(), root)
		if d == nil || d.Decision != "reject" {
			t.Fatal("explicit operation not blocked", in.ToolName)
		}
	}
	// Tests inspect paths and strings only; no writes under the symlink occur.
}
