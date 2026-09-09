package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These fixtures never open a listener or contact a model provider.
func retryResponse(protocol, reply, finish string, usageKnown bool) *http.Response {
	var payload map[string]any
	if protocol == "anthropic" {
		payload = map[string]any{"stop_reason": finish, "content": []any{map[string]string{"type": "text", "text": reply}}}
		if usageKnown {
			payload["usage"] = map[string]int{"input_tokens": 80, "output_tokens": 5, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 10}
		}
	} else {
		payload = map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": map[string]string{"content": reply}}}}
		if usageKnown {
			payload["usage"] = map[string]any{"prompt_tokens": 100, "completion_tokens": 5, "prompt_tokens_details": map[string]int{"cached_tokens": 20}}
		}
	}
	data, _ := json.Marshal(payload)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}
}

func TestModelJSONRetry(t *testing.T) {
	reject := strings.Replace(strings.Replace(unknownApprove, "approve", "reject", 1), "D1", "R4", 1)
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, tc := range []struct {
			name         string
			replies      []string
			wantCalls    int
			wantDecision string
		}{
			{"valid", []string{unknownApprove}, 1, "approve"},
			{"valid_reject", []string{reject}, 1, "reject"},
			{"markdown", []string{"```json\n" + unknownApprove + "\n```", unknownApprove}, 2, "approve"},
			{"empty", []string{"", unknownApprove}, 2, "approve"},
			{"syntax", []string{`{"decision":`, unknownApprove}, 2, "approve"},
			{"extra_field", []string{strings.TrimSuffix(unknownApprove, "}") + `,"reason":"extra"}`, unknownApprove}, 2, "approve"},
			{"wrong_type", []string{`{"decision":true,"comment":"text"}`, unknownApprove}, 2, "approve"},
			{"missing_field", []string{`{"decision":"approve"}`, unknownApprove}, 2, "approve"},
			{"null_field", []string{`{"decision":"approve","comment":null}`, unknownApprove}, 2, "approve"},
			{"null", []string{"null", unknownApprove}, 2, "approve"},
			{"array", []string{"[]", unknownApprove}, 2, "approve"},
			{"trailing", []string{unknownApprove + " explanation", unknownApprove}, 2, "approve"},
			{"multiple_objects", []string{unknownApprove + unknownApprove, unknownApprove}, 2, "approve"},
			{"third_retry_success", []string{"bad", "bad", "bad", reject}, 4, "reject"},
			{"exhausted", []string{"bad"}, 4, ""},
			{"invalid_decision", []string{askReply}, 1, ""},
			{"invalid_comment", []string{`{"decision":"approve","comment":"text"}`}, 1, ""},
			{"invalid_rule", []string{strings.Replace(unknownApprove, "D1", "H1", 1)}, 1, ""},
			{"conflicting_rule", []string{strings.Replace(unknownApprove, "D1", "R4", 1)}, 1, ""},
			{"edited_arguments", []string{strings.TrimSuffix(unknownApprove, "}") + `,"editedArguments":{}}`}, 1, ""},
			{"semantic_error_after_retry", []string{"bad", askReply}, 2, ""},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				original := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = original })
				calls := 0
				var firstInput string
				var deadline time.Time
				http.DefaultTransport = reviewRoundTrip(func(req *http.Request) (*http.Response, error) {
					var payload struct {
						System   string
						Messages []struct{ Role, Content string }
					}
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					messages := payload.Messages
					system := payload.System
					finish := "end_turn"
					if protocol == "openai" {
						system = messages[0].Content
						messages = messages[1:]
						finish = "stop"
					}
					if system != "original policy" || len(messages) != 1+2*calls {
						t.Fatalf("policy/history changed: %+v", payload)
					}
					if calls == 0 {
						firstInput = messages[0].Content
						deadline, _ = req.Context().Deadline()
					}
					currentDeadline, ok := req.Context().Deadline()
					if !ok || !currentDeadline.Equal(deadline) {
						t.Fatal("retry reset total deadline")
					}
					if messages[0].Role != "user" || messages[0].Content != firstInput {
						t.Fatal("original input changed")
					}
					if calls > 0 {
						last := messages[len(messages)-1]
						previous := messages[len(messages)-2]
						if last.Role != "user" || !strings.Contains(last.Content, "JSON 格式校验失败：") || previous.Role != "assistant" || previous.Content == "" {
							t.Fatal("missing correction feedback/history")
						}
						_, parseErr := ParseModelDecision(tc.replies[min(calls-1, len(tc.replies)-1)])
						var formatErr *modelJSONError
						if !errors.As(parseErr, &formatErr) || !strings.Contains(last.Content, formatErr.detail) {
							t.Fatal("missing specific format diagnosis")
						}
					}
					reply := tc.replies[min(calls, len(tc.replies)-1)]
					calls++
					return retryResponse(protocol, reply, finish, true), nil
				})
				settings := Settings{Prompt: "original policy", ModelSeconds: 5, Model: ModelConfig{Protocol: protocol, BaseURL: "https://fixture.invalid/v1", Model: "fixture"}}
				var usage *TokenUsage
				decision, err := callModelWithUsage(context.Background(), settings, map[string]string{"toolName": "read"}, &usage)
				if calls != tc.wantCalls || decision.Decision != tc.wantDecision || (err == nil) != (tc.wantDecision != "") {
					t.Fatalf("calls=%d decision=%+v err=%v", calls, decision, err)
				}
				if tc.name == "exhausted" && !strings.Contains(err.Error(), "重试 3 次") {
					t.Fatal(err)
				}
				if usage == nil || usage.Input != int64(calls*80) || usage.Output != int64(calls*5) || usage.CacheRead != int64(calls*20) {
					t.Fatalf("lost retry usage: %+v", usage)
				}
				if protocol == "anthropic" && usage.CacheWrite != int64(calls*10) {
					t.Fatal(usage)
				}
			})
		}
	}
}

func TestModelJSONRetryStopsOnProviderError(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, kind := range []string{"network", "http", "envelope", "incomplete", "cancel"} {
			t.Run(protocol+"/"+kind, func(t *testing.T) {
				original := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = original })
				calls := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				http.DefaultTransport = reviewRoundTrip(func(req *http.Request) (*http.Response, error) {
					calls++
					finish := "stop"
					if protocol == "anthropic" {
						finish = "end_turn"
					}
					if kind == "cancel" {
						cancel()
						return retryResponse(protocol, "bad", finish, true), nil
					}
					if calls == 1 {
						return retryResponse(protocol, "bad", finish, true), nil
					}
					switch kind {
					case "network":
						return nil, errors.New("fixture connection failed")
					case "http":
						return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
					case "envelope":
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not API JSON"))}, nil
					default:
						return retryResponse(protocol, "bad", "length", true), nil
					}
				})
				_, err := callModel(ctx, Settings{ModelSeconds: 5, Model: ModelConfig{Protocol: protocol, BaseURL: "https://fixture.invalid", Model: "fixture"}}, nil)
				want := 2
				if kind == "cancel" {
					want = 1
				}
				var formatErr *modelJSONError
				if err == nil || errors.As(err, &formatErr) || calls != want {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			})
		}
	}
}

func TestModelJSONRetryUnknownUsage(t *testing.T) {
	for _, missing := range []int{1, 2} {
		t.Run(string(rune('0'+missing)), func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			http.DefaultTransport = reviewRoundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				reply := "bad"
				if calls == 2 {
					reply = unknownApprove
				}
				return retryResponse("openai", reply, "stop", calls != missing), nil
			})
			var usage *TokenUsage
			_, err := callModelWithUsage(context.Background(), Settings{ModelSeconds: 5, Model: ModelConfig{BaseURL: "https://fixture.invalid", Model: "fixture"}}, nil, &usage)
			if err != nil || calls != 2 || usage != nil {
				t.Fatalf("calls=%d usage=%+v err=%v", calls, usage, err)
			}
		})
	}
}
