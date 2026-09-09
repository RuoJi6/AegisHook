package core

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const previousVerdict = "实际操作：遍历工单；成功后的后果：读取业务数据；命中规则：R7"

func TestModelContextRemovesReviewFeedback(t *testing.T) {
	for _, raw := range []string{
		"toolResult: " + previousVerdict + "\nassistant: 改为本机命令\ntoolResult: " + previousVerdict,
		"assistant: " + previousVerdict,
		"toolResult: 实际操作：遍历工单\n；成功后的后果：读取业务数据\n；命中规则：R7",
		"toolResult: AegisHook 拒绝执行：服务不可用。工具未执行。",
		previousVerdict,
	} {
		if got := modelContext(raw); got != "" {
			t.Errorf("review feedback retained: %q", got)
		}
	}
	good := "toolResult: HTTP 200，返回测试订单一条"
	if got := modelContext(good + "\nassistant: 开始全量导出\ntoolResult: " + previousVerdict); got != good {
		t.Fatal("lost actual result or kept speculation", got)
	}
	ordinaryJSON := `[{"id":1,"title":"fixture record"}]`
	if modelContext(ordinaryJSON) != ordinaryJSON {
		t.Fatal("ordinary JSON evidence was discarded")
	}
	structured := `[{"toolCallId":"ok","toolName":"http","argumentsPreview":"GET /fixture","result":"HTTP 200","status":"succeeded"},{"toolCallId":"blocked","toolName":"http","result":"denied","status":"not_executed"}]`
	if got := modelContext(structured); !strings.Contains(got, "HTTP 200") || strings.Contains(got, "denied") {
		t.Fatal(got)
	}
}

type reviewRoundTrip func(*http.Request) (*http.Response, error)

func (f reviewRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// No listener or live model: inspect exactly what leaves the review boundary.
func TestCurrentCallIsolatedFromRejectedHistory(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			e := engine(t)
			transport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = transport })
			http.DefaultTransport = reviewRoundTrip(func(req *http.Request) (*http.Response, error) {
				var payload struct {
					System   string                           `json:"system"`
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				system := payload.System
				if protocol == "openai" {
					system = payload.Messages[0].Content
				}
				if !strings.Contains(system, reviewInputBoundary) {
					t.Error("missing input boundary")
				}
				body := payload.Messages[len(payload.Messages)-1].Content
				var input struct {
					Context   string         `json:"context"`
					ToolName  string         `json:"toolName"`
					Arguments map[string]any `json:"argumentsObj"`
				}
				if err := json.Unmarshal([]byte(body), &input); err != nil {
					t.Error(err)
				}
				if input.Context != "" || input.ToolName != "bash" || input.Arguments["command"] != "pwd && echo hello" {
					t.Error("polluted or changed current call", body)
				}
				if strings.Index(body, `"context"`) > strings.Index(body, `"argumentsObj"`) {
					t.Error("current call should follow history")
				}
				verdict := `{"decision":"approve","comment":"实际操作：输出当前目录及 hello；成功后的后果：返回本机文本；命中规则：A6"}`
				var response any = map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": verdict}}}}
				if protocol == "anthropic" {
					response = map[string]any{"stop_reason": "end_turn", "content": []any{map[string]string{"type": "text", "text": verdict}}}
				}
				data, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})
			e.mu.Lock()
			e.Settings.Mode = "model"
			e.Settings.Prompt = "Existing custom policy, retain this text without replacing it."
			e.Settings.Model = ModelConfig{Protocol: protocol, BaseURL: "https://fixture.invalid", Model: "fixture", Tested: true}
			e.mu.Unlock()
			in := input("current-only", "bash", map[string]any{"command": "pwd && echo hello"})
			in.Context = "toolResult: " + previousVerdict + "\nassistant: 上次被拦截，尝试本机命令"
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
			if r.Decision != "approve" || r.RuleID != "A6" || r.Context != in.Context || r.ModelContext == nil || *r.ModelContext != "" || !strings.Contains(r.Prompt, reviewInputBoundary) {
				t.Fatal("lost decision or audit evidence", r)
			}
			if strings.Contains(e.Config().Prompt, reviewInputBoundary) {
				t.Fatal("mutated saved user policy")
			}
		})
	}
}
