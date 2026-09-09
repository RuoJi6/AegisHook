package core

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUsageNormalization(t *testing.T) {
	for _, tc := range []struct {
		protocol, raw string
		want          *TokenUsage
	}{
		{"openai", `{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":200}}`, &TokenUsage{Input: 800, Output: 50, CacheRead: 200}},
		{"anthropic", `{"input_tokens":800,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":100}`, &TokenUsage{Input: 800, Output: 50, CacheRead: 200, CacheWrite: 100}},
		{"openai", `{"prompt_tokens":0,"completion_tokens":0}`, &TokenUsage{}},
		{"openai", `{}`, nil},
		{"openai", `{"prompt_tokens":100,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":101}}`, nil},
		{"anthropic", `{"input_tokens":1,"output_tokens":-1}`, nil},
	} {
		got := parseUsage(json.RawMessage(tc.raw), tc.protocol)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Fatalf("%s: got %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}
func TestUsageIncludesInvalidVerdictsAndPriceSnapshot(t *testing.T) {
	e := engine(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":200}},"choices":[{"message":{"content":"invalid verdict"}}]}`))
	}))
	defer srv.Close()
	s := e.Config()
	s.Model.BaseURL = srv.URL
	s.Model.Model = "fixture"
	s.Model.Pricing = ModelPricing{Enabled: true, Currency: "CNY", Input: 2, Output: 4, CacheRead: 0.5, CacheWrite: 3}
	if err := e.SaveSettings(s, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.TestModel(context.Background()); err == nil {
		t.Fatal("invalid verdict passed")
	}
	rs, err := e.Usage()
	if err != nil || len(rs) != 1 {
		t.Fatal(rs, err)
	}
	if rs[0].Usage.Input != 800 || rs[0].Cost == nil || math.Abs(*rs[0].Cost-0.0019) > 1e-10 || !rs[0].Failed {
		t.Fatal(rs[0])
	}
	s = e.Config()
	s.Model.Pricing.Input = 99
	if err = e.SaveSettings(s, nil); err != nil {
		t.Fatal(err)
	}
	rs, _ = e.Usage()
	if rs[0].Pricing.Input != 2 {
		t.Fatal("historical price changed")
	}
	if err = e.recordUsage("unknown", "", "test", s, nil, true); err != nil {
		t.Fatal(err)
	}
	rs, _ = e.Usage()
	for _, r := range rs {
		if r.ID == "unknown" && r.Cost != nil {
			t.Fatal("unknown usage was priced")
		}
	}
}
