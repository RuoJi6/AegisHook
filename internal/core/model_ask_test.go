package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const askReply = `{"decision":"ask","comment":"实际操作：执行未知脚本；成功后的后果：无法确认副作用；命中规则：H1"}`
const unknownApprove = `{"decision":"approve","comment":"实际操作：执行未知脚本；成功后的后果：当前参数未显示明确高危操作；命中规则：D1"}`

func TestModelBinarySchema(t *testing.T) {
	for _, raw := range []string{askReply, strings.Replace(askReply, "H1", "D1", 1), strings.Replace(askReply, `"ask"`, `"approve"`, 1)} {
		if _, err := ParseModelDecision(raw); err == nil {
			t.Fatal("accepted referral", raw)
		}
	}
	if _, err := ParseModelDecision(unknownApprove); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(DefaultPrompt, "H1") || strings.Contains(DefaultPrompt, `"decision":"ask"`) {
		t.Fatal("default prompt still requests referral")
	}
	for _, example := range []string{"13. mysql", "14. 删除"} {
		if !strings.Contains(DefaultPrompt, example) {
			t.Fatal("lost requested example", example)
		}
	}
}

func TestModelNeverRefersToHuman(t *testing.T) {
	for _, raw := range []string{askReply, unknownApprove} {
		t.Run(raw, func(t *testing.T) {
			e := engine(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}}}})
			}))
			defer up.Close()
			e.mu.Lock()
			e.Settings.Mode = "model"
			e.Settings.Model = ModelConfig{BaseURL: up.URL, Model: "fixture", Tested: true}
			e.mu.Unlock()
			r, err := e.Submit(input("binary", "fixture_tool", map[string]any{"command": "sh unknown-fixture.sh"}))
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
			if r.Decision == "pending" || r.NeedsHuman {
				t.Fatal("model left human work", r)
			}
			if raw == askReply && (r.Decision != "reject" || r.RuleID != "E_MODEL" || r.Execution != "not_executed") {
				t.Fatal(r)
			}
			if raw == unknownApprove && (r.Decision != "approve" || r.RuleID != "D1" || r.Execution != "awaiting_execution") {
				t.Fatal(r)
			}
			if _, err = e.Decide(r.ID, r.Digest, "approve", ""); err == nil {
				t.Fatal("manual decision accepted for model review")
			}
			audits, _ := e.Audits()
			for _, a := range audits {
				if a.Action == "review.model.ask" {
					t.Fatal("referral audit created")
				}
			}
		})
	}
}

func TestModelAskRestartInvalidates(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { e.Close() }()
	e.Register(Instance{ID: "instance", SessionID: "session", Cwd: t.TempDir(), HookVersion: Version})
	r, err := e.Submit(input("ask-restart", "fixture_tool", map[string]any{"path": "unknown"}))
	if err != nil {
		t.Fatal(err)
	}
	r.Mode = "model"
	r.NeedsHuman = true
	r.RuleID = "H1"
	r.ModelVerdict = &Decision{Decision: "ask", RuleID: "H1", Comment: "needs clarification"}
	if err = e.commitReview(r, "review.model.ask"); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := e.Review(r.ID)
	if result.Decision != "reject" || result.RuleID != "E_RESTART" || result.NeedsHuman || result.ModelVerdict.Decision != "ask" {
		t.Fatal(result)
	}
	if _, err = e.Decide(r.ID, r.Digest, "approve", ""); err == nil {
		t.Fatal("restarted ask resumed")
	}
}
