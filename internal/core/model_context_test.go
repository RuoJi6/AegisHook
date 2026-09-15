package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const previousVerdict = "实际操作：遍历工单；成功后的后果：读取业务数据；命中规则：R7"

func assertCurrentModelInput(t *testing.T, body []byte, tool string, args map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Error(err)
		return
	}
	want := map[string]any{"hitlMode": "model", "toolName": tool, "argumentsObj": args}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected only current call: got %s, want %+v", body, want)
	}
}

func TestModelInputCurrentOnly(t *testing.T) {
	oldModelContext := `[{"toolName":"http","result":"previous successful export","status":"succeeded"}]`
	for _, history := range []string{"", "toolResult: " + previousVerdict, "earlier command succeeded", oldModelContext} {
		for _, savedContext := range []*string{nil, &oldModelContext} {
			// Fields named context/history inside current arguments are real input,
			// and must not be stripped along with the review's background fields.
			args := map[string]any{"command": "pwd && echo hello", "options": map[string]any{"context": "current argument", "history": []any{"current value"}}}
			r := Review{ReviewInput: ReviewInput{ToolName: "bash", Arguments: args, UserMessage: "old user command", Context: history}, ModelContext: savedContext}
			body, err := json.Marshal(modelInput(r))
			if err != nil {
				t.Fatal(err)
			}
			assertCurrentModelInput(t, body, r.ToolName, args)
			if r.Context != history || r.ModelContext != savedContext || r.UserMessage != "old user command" {
				t.Fatal("modified stored audit background")
			}
		}
	}
}

func TestModelPromptCurrentOnly(t *testing.T) {
	for _, policy := range []string{DefaultPrompt, "保留自定义规则；此前根据历史累计判断。", DataGuardPrompt} {
		got := modelPrompt(policy)
		if got != policy+"\n\n"+reviewInputBoundary || modelPrompt(got) != got {
			t.Fatal("lost custom policy or duplicated current-call boundary")
		}
	}
}

type reviewRoundTrip func(*http.Request) (*http.Response, error)

func (f reviewRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// No listener or live model: inspect actual outbound requests, including a
// format retry, for every Agent and protocol. Successful history is excluded too.
func TestCurrentCallIsolatedFromHistory(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, agent := range []string{"pi", "claude", "codex", "opencode", "grok"} {
			t.Run(protocol+"/"+agent, func(t *testing.T) {
				original := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = original })
				e := engine(t)
				if err := e.Register(Instance{ID: agent, Agent: agent, SessionID: agent, Cwd: t.TempDir(), HookVersion: Version}); err != nil {
					t.Fatal(err)
				}
				args := map[string]any{"command": "pwd && echo hello"}
				var requests atomic.Int32
				http.DefaultTransport = reviewRoundTrip(func(req *http.Request) (*http.Response, error) {
					attempt := requests.Add(1)
					data, err := io.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					for _, marker := range []string{"historical-command", "historical-result", "old-user-message", previousVerdict} {
						if strings.Contains(string(data), marker) {
							t.Errorf("historical material reached %s request: %s", protocol, marker)
						}
					}
					var payload struct {
						System   string
						Messages []struct{ Role, Content string }
					}
					if err := json.Unmarshal(data, &payload); err != nil {
						return nil, err
					}
					system, messages, finish := payload.System, payload.Messages, "end_turn"
					if protocol == "openai" && len(messages) > 0 {
						system, messages, finish = messages[0].Content, messages[1:], "stop"
					}
					if !strings.HasSuffix(system, reviewInputBoundary) {
						t.Error("missing current-call boundary")
					}
					if len(messages) != 1+2*int(attempt-1) || len(messages) == 0 || messages[0].Role != "user" {
						return nil, fmt.Errorf("unexpected model messages on attempt %d", attempt)
					}
					assertCurrentModelInput(t, []byte(messages[0].Content), "bash", args)
					verdict := `{"decision":"approve","comment":"实际操作：输出当前目录及 hello；成功后的后果：返回本机文本；命中规则：A6"}`
					if attempt == 1 {
						verdict = "invalid JSON"
					}
					return retryResponse(protocol, verdict, finish, true), nil
				})
				const customPolicy = "Existing custom policy, retain this text without replacing it."
				e.mu.Lock()
				e.Settings.Mode = "model"
				e.Settings.Prompt = customPolicy
				e.Settings.Model = ModelConfig{Protocol: protocol, BaseURL: "https://fixture.invalid", Model: "fixture", Tested: true}
				e.mu.Unlock()
				history := []map[string]string{}
				for i := 0; i < 6; i++ {
					history = append(history, map[string]string{"toolCallId": fmt.Sprint(i), "toolName": "bash", "argumentsPreview": "historical-command", "result": "historical-result", "status": "succeeded"})
				}
				data, _ := json.Marshal(history)
				in := ReviewInput{InstanceID: agent, CallID: "current-only", ToolName: "bash", Arguments: args, UserMessage: "old-user-message " + previousVerdict, Context: string(data)}
				r, err := e.Submit(in)
				if err != nil {
					t.Fatal(err)
				}
				for n := 0; n < 400; n++ {
					r, err = e.Review(r.ID)
					if err != nil {
						t.Fatal(err)
					}
					if r.Decision != "pending" {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if r.Decision != "approve" || r.RuleID != "A6" || r.Context != in.Context || r.UserMessage != in.UserMessage || r.ModelContext == nil || *r.ModelContext != "" || r.Prompt != modelPrompt(customPolicy) || requests.Load() != 2 {
					t.Fatalf("lost current-only decision or audit: decision=%s rule=%s requests=%d", r.Decision, r.RuleID, requests.Load())
				}
				if e.Config().Prompt != customPolicy {
					t.Fatal("mutated saved user policy")
				}
			})
		}
	}
}
