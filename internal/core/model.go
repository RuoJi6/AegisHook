package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func ValidateModelURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("模型地址无效，不允许内嵌凭据、查询参数或片段")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && hasExact(u.Hostname(), "localhost", "127.0.0.1", "::1")) {
		return errors.New("远程模型服务须使用 HTTPS；本机可使用 HTTP")
	}
	return nil
}
func callModel(ctx context.Context, s Settings, in any) (Decision, error) {
	var usage *TokenUsage
	return callModelWithUsage(ctx, s, in, &usage)
}
func callModelWithUsage(ctx context.Context, s Settings, in any, usage **TokenUsage) (Decision, error) {
	if err := ValidateModelURL(s.Model.BaseURL); err != nil {
		return Decision{}, err
	}
	if s.Model.Model == "" {
		return Decision{}, errors.New("模型名称为空")
	}
	data, _ := json.Marshal(in)
	endpoint := strings.TrimRight(s.Model.BaseURL, "/")
	payload := map[string]any{"model": s.Model.Model, "stream": false, "messages": []map[string]string{{"role": "system", "content": s.Prompt}, {"role": "user", "content": string(data)}}}
	if s.Model.Protocol == "anthropic" {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/messages"
		} else {
			endpoint += "/v1/messages"
		}
		payload = map[string]any{"model": s.Model.Model, "stream": false, "max_tokens": 2048, "system": s.Prompt, "messages": []map[string]string{{"role": "user", "content": string(data)}}}
	} else {
		endpoint += "/chat/completions"
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Model.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
		if s.Model.APIKey != "" {
			req.Header.Set("x-api-key", s.Model.APIKey)
		}
	} else if s.Model.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Model.APIKey)
	}

	client := &http.Client{Timeout: time.Duration(s.ModelSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("模型端点不允许重定向") }}
	res, err := client.Do(req)
	if err != nil {
		return Decision{}, errors.New("模型连接失败或请求超时")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Decision{}, fmt.Errorf("模型返回 HTTP %d", res.StatusCode)
	}
	if s.Model.Protocol == "anthropic" {
		var out struct {
			Usage   json.RawMessage `json:"usage"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		}
		if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
			return Decision{}, errors.New("Anthropic 模型响应格式无效")
		}
		*usage = parseUsage(out.Usage, s.Model.Protocol)
		if out.StopReason != "end_turn" {
			return Decision{}, errors.New("Anthropic 模型没有完成文本裁决")
		}
		texts := []string{}
		for _, c := range out.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return ParseModelDecision(strings.Join(texts, "\n"))
	}
	var out struct {
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return Decision{}, errors.New("模型响应格式无效")
	}
	*usage = parseUsage(out.Usage, s.Model.Protocol)
	if len(out.Choices) != 1 {
		return Decision{}, errors.New("模型响应格式无效")
	}
	if reason := out.Choices[0].FinishReason; reason != "" && reason != "stop" {
		return Decision{}, errors.New("模型裁决未完整输出")
	}
	return ParseModelDecision(out.Choices[0].Message.Content)
}
func ParseModelDecision(text string) (Decision, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &fields) == nil {
		if _, ok := fields["editedArguments"]; ok {
			return Decision{}, errors.New("首版不执行模型的 editedArguments；请由 Pi 调整参数后重新提交审查")
		}
	}
	var out struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Decision{}, errors.New("模型裁决必须为 JSON，且仅包含 decision、comment")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Decision{}, errors.New("模型输出包含多余内容")
	}
	if !hasExact(out.Decision, "approve", "reject") || !strings.Contains(out.Comment, "实际操作：") || !strings.Contains(out.Comment, "成功后的后果：") {
		return Decision{}, errors.New("模型裁决缺少有效结论或说明")
	}
	m := regexp.MustCompile(`命中规则[：:]\s*(R[1-7]|A[1-7]|D1)\b`).FindStringSubmatch(out.Comment)
	if len(m) != 2 {
		return Decision{}, errors.New("模型裁决缺少有效规则编号")
	}
	if strings.HasPrefix(m[1], "R") && out.Decision != "reject" {
		return Decision{}, errors.New("模型结论与拒绝规则冲突")
	}
	if (strings.HasPrefix(m[1], "A") || m[1] == "D1") && out.Decision != "approve" {
		return Decision{}, errors.New("模型结论与允许规则冲突")
	}
	return Decision{Decision: out.Decision, Comment: RedactText(out.Comment), RuleID: m[1]}, nil
}
func (e *Engine) modelReview(r Review, s Settings) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.ModelSeconds)*time.Second)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-e.stop:
			cancel()
		case <-done:
		}
	}()
	var usage *TokenUsage
	d, err := callModelWithUsage(ctx, s, modelInput(r), &usage)
	if err != nil {
		d = Decision{Decision: "reject", RuleID: "E_MODEL", Comment: err.Error() + "；本次调用已拒绝，请检查审查模型配置。"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if usageErr := e.recordUsage(r.ID, r.ID, "review", s, usage, err != nil); usageErr != nil {
		return
	}
	current, err := e.Review(r.ID)
	if err != nil || current.Decision != "pending" || current.NeedsHuman {
		return
	}
	if e.clock().After(current.Deadline) {
		return
	}
	current.ModelVerdict = &d
	current.Decision = d.Decision
	current.RuleID = d.RuleID
	current.Comment = d.Comment
	now := e.clock()
	current.DecidedAt = &now
	if d.Decision == "approve" {
		current.Execution = "awaiting_execution"
	}
	e.commitReview(current, "review.model")
}
func (e *Engine) TestModel(ctx context.Context) error {
	e.mu.Lock()
	s := e.Settings
	e.mu.Unlock()
	requestSettings := s
	requestSettings.Prompt = modelPrompt(s.Prompt)
	var usage *TokenUsage
	_, err := callModelWithUsage(ctx, requestSettings, modelInput(Review{ReviewInput: ReviewInput{ToolName: "read", Arguments: map[string]any{"path": "README.md"}, UserMessage: "读取项目说明"}}), &usage)
	e.mu.Lock()
	defer e.mu.Unlock()
	if usageErr := e.recordUsage(ID(), "", "test", s, usage, err != nil); usageErr != nil {
		return usageErr
	}
	if err != nil {
		return err
	}
	if e.Settings.Version != s.Version {
		return errors.New("配置已变化，请重新测试")
	}
	s.Model.Tested = true
	if err = e.change("settings", "current", s, "model.test", "模型连接和结构化裁决测试成功"); err != nil {
		return err
	}
	e.Settings = s
	return nil
}
