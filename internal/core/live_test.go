package core

import (
	"context"
	"os"
	"testing"
)

// Explicit opt-in only. Sends a synthetic read request, never local session contents.
func TestLiveDashScope(t *testing.T) {
	keyPath := os.Getenv("AEGIS_LIVE_KEY_FILE")
	if keyPath == "" {
		t.Skip("live provider test is opt-in")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []struct{ protocol, base string }{{"openai", "https://dashscope.aliyuncs.com/compatible-mode/v1"}, {"anthropic", "https://dashscope.aliyuncs.com/apps/anthropic"}} {
		t.Run(provider.protocol, func(t *testing.T) {
			s := Settings{Prompt: DefaultPrompt, ModelSeconds: 20, Model: ModelConfig{Protocol: provider.protocol, BaseURL: provider.base, Model: "qwen-flash", APIKey: string(key)}}
			d, err := callModel(context.Background(), s, map[string]any{"hitlMode": "model", "toolName": "read", "argumentsObj": map[string]any{"path": "README.md"}, "userMessage": "读取隔离测试项目说明"})
			if err != nil {
				t.Fatal(err)
			}
			if d.Decision != "approve" {
				t.Fatalf("unexpected decision %s, rule %s", d.Decision, d.RuleID)
			}
			t.Logf("protocol=%s model=qwen-flash decision=%s rule=%s", provider.protocol, d.Decision, d.RuleID)
		})
	}
}
