package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const dataGuardReject = `{"decision":"reject","comment":"实际操作：通过已确认的未授权订单接口读取 11 条记录；成功后的后果：本次获取 11 条业务记录，超过单次 10 条上限；命中规则：R7"}`

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
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	e := engine(t)
	in := input("data-guard", "http", map[string]any{"method": "GET", "url": "https://fixture.test/api/orders?page=2&limit=11"})
	in.Context = "历史请求已成功获取 49 条订单；本条历史仅作审计留档。"
	http.DefaultTransport = reviewRoundTrip(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		if len(payload.Messages) != 2 {
			t.Error("missing system prompt or review input")
		} else {
			if payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[0].Content, DataGuardPrompt) || !strings.HasSuffix(payload.Messages[0].Content, reviewInputBoundary) {
				t.Error("enabled data guard or current-call boundary not sent to reviewer")
			}
			assertCurrentModelInput(t, []byte(payload.Messages[1].Content), in.ToolName, in.Arguments)
		}
		return retryResponse("openai", dataGuardReject, "stop", true), nil
	})
	e.mu.Lock()
	e.Settings.Mode = "model"
	e.Settings.Prompt = strings.Replace(DefaultPrompt, DataGuardAnchor, DataGuardPrompt+"\n\n"+DataGuardAnchor, 1)
	e.Settings.Model = ModelConfig{BaseURL: "https://fixture.invalid", Model: "fixture", Tested: true}
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
	if r.Decision != "reject" || r.RuleID != "R7" || r.Execution != "not_executed" || r.NeedsHuman || !strings.Contains(r.Prompt, DataGuardPrompt) || r.Context != in.Context || r.ModelContext == nil || *r.ModelContext != "" {
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
	if strings.Contains(s.Prompt, DataGuardStart) || len(s.Prompt) > 32000 {
		t.Fatal("new settings must default to a disabled, saveable data guard")
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
