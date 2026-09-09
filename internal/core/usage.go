package core

import (
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Token categories are mutually exclusive, including for Anthropic cache usage.
type TokenUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}
type ModelPricing struct {
	Enabled    bool    `json:"enabled"`
	Currency   string  `json:"currency"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}
type UsageRecord struct {
	ID        string       `json:"id"`
	ReviewID  string       `json:"reviewId,omitempty"`
	Purpose   string       `json:"purpose"`
	Model     string       `json:"model"`
	Protocol  string       `json:"protocol"`
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"createdAt"`
	Usage     *TokenUsage  `json:"usage"`
	Pricing   ModelPricing `json:"pricing"`
	Cost      *float64     `json:"cost"`
	Failed    bool         `json:"failed"`
}

func (p ModelPricing) validate() error {
	if p.Currency != "" && !hasExact(p.Currency, "CNY", "USD") {
		return errors.New("费用币种须为 CNY 或 USD")
	}
	if p.Enabled && p.Currency == "" {
		return errors.New("请设置费用币种")
	}
	for _, v := range []float64{p.Input, p.Output, p.CacheRead, p.CacheWrite} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e9 {
			return errors.New("模型单价须为有效的非负数")
		}
	}
	return nil
}
func parseUsage(raw json.RawMessage, protocol string) *TokenUsage {
	var v struct {
		Input      *int64 `json:"input_tokens"`
		Output     *int64 `json:"output_tokens"`
		Prompt     *int64 `json:"prompt_tokens"`
		Completion *int64 `json:"completion_tokens"`
		Read       int64  `json:"cache_read_input_tokens"`
		Write      int64  `json:"cache_creation_input_tokens"`
		Details    struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	u := &TokenUsage{}
	if protocol == "anthropic" {
		if v.Input == nil || v.Output == nil {
			return nil
		}
		u.Input, u.Output, u.CacheRead, u.CacheWrite = *v.Input, *v.Output, v.Read, v.Write
	} else {
		if v.Prompt == nil || v.Completion == nil {
			return nil
		}
		u.Input, u.Output, u.CacheRead = *v.Prompt-v.Details.Cached, *v.Completion, v.Details.Cached
	}
	for _, n := range []int64{u.Input, u.Output, u.CacheRead, u.CacheWrite} {
		if n < 0 || n > 1e12 {
			return nil
		}
	}
	return u
}
func (e *Engine) recordUsage(id, reviewID, purpose string, s Settings, u *TokenUsage, failed bool) error {
	r := UsageRecord{ID: id, ReviewID: reviewID, Purpose: purpose, Model: s.Model.Model, Protocol: s.Model.Protocol, Version: s.Version, CreatedAt: e.clock(), Usage: u, Pricing: s.Model.Pricing, Failed: failed}
	if u != nil && r.Pricing.Enabled {
		p := r.Pricing
		cost := (float64(u.Input)*p.Input + float64(u.Output)*p.Output + float64(u.CacheRead)*p.CacheRead + float64(u.CacheWrite)*p.CacheWrite) / 1e6
		r.Cost = &cost
	}
	if err := e.put("usage", id, r); err != nil {
		return err
	}
	if e.Notify != nil {
		e.Notify()
	}
	return nil
}
func (e *Engine) Usage() ([]UsageRecord, error) { return list[UsageRecord](e, "usage") }
