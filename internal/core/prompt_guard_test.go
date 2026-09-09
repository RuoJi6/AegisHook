package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const dataGuardReject = `{"decision":"reject","comment":"实际操作：上下文已确认未授权订单访问，继续翻页取数；成功后的后果：已有验证证据仍获取更多真实订单，超出最小验证范围；命中规则：R7"}`

func TestModelDataGuardSchema(t *testing.T) {
	d, err := ParseModelDecision(dataGuardReject)
	if err != nil || d.RuleID != "R7" || d.Decision != "reject" {
		t.Fatal(d, err)
	}
	for _, invalid := range []string{
		strings.Replace(dataGuardReject, `"reject"`, `"approve"`, 1),
		strings.Replace(dataGuardReject, "R7", "R8", 1),
		strings.Replace(dataGuardReject, "R7", "R70", 1),
	} {
		if _, err := ParseModelDecision(invalid); err == nil {
			t.Fatal("accepted invalid data guard verdict", invalid)
		}
	}
}

func TestModelDataGuardPipeline(t *testing.T) {
	e := engine(t)
	in := input("data-guard", "http", map[string]any{"method": "GET", "url": "https://fixture.test/api/orders?page=2"})
	in.Context = "GET /api/orders?page=1 在未登录状态返回他人订单详情，已确认未授权访问；下一步继续收集订单。"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Messages) != 2 {
			t.Error("missing system prompt or review input")
			w.WriteHeader(400)
			return
		}
		if payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[0].Content, DataGuardPrompt) {
			t.Error("default data guard not sent to reviewer")
		}
		var review struct{ Context string }
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &review); err != nil || review.Context != in.Context {
			t.Error("reviewer did not receive prior verification evidence", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": dataGuardReject}}}})
	}))
	defer up.Close()
	e.mu.Lock()
	e.Settings.Mode = "model"
	e.Settings.Model = ModelConfig{BaseURL: up.URL, Model: "fixture", Tested: true}
	e.mu.Unlock()
	r, err := e.Submit(in)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 200; n++ {
		r, err = e.Review(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Decision != "pending" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if r.Decision != "reject" || r.RuleID != "R7" || r.Execution != "not_executed" || r.NeedsHuman || !strings.Contains(r.Prompt, DataGuardPrompt) {
		t.Fatal("data guard verdict or prompt snapshot lost", r)
	}
	if err := e.Result(r.ID, "instance", "succeeded", ""); err == nil {
		t.Fatal("rejected data access accepted as executed")
	}
}

func TestDataGuardDefaultAndSavedOptOut(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { e.Close() }()
	s := e.Config()
	if strings.Count(s.Prompt, DataGuardPrompt) != 1 || len(s.Prompt) > 32000 {
		t.Fatal("new settings must contain one enabled, saveable data guard")
	}
	s.Prompt = strings.Replace(s.Prompt, DataGuardPrompt+"\n\n", "", 1) + "\n保留自定义审查说明。"
	if err := e.SaveSettings(s, nil); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Config().Prompt != s.Prompt || strings.Contains(e.Config().Prompt, DataGuardStart) {
		t.Fatal("restart must preserve saved opt-out and custom text")
	}
}
