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
	// All attempts share the original time budget, including configuration tests.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.ModelSeconds)*time.Second)
	defer cancel()
	data, _ := json.Marshal(in)
	messages := []map[string]string{{"role": "user", "content": string(data)}}
	total := &TokenUsage{}
	*usage = nil
	allUsageKnown := true
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return Decision{}, errors.New("模型连接失败或请求超时")
		}
		var current *TokenUsage
		reply, err := callModelText(ctx, s, messages, &current)
		if current == nil {
			// A partial total must not be presented as a complete cost estimate.
			allUsageKnown = false
			*usage = nil
		} else {
			total.Input += current.Input
			total.Output += current.Output
			total.CacheRead += current.CacheRead
			total.CacheWrite += current.CacheWrite
			if allUsageKnown {
				*usage = total
			}
		}
		if err != nil {
			return Decision{}, err
		}
		d, err := ParseModelDecision(reply)
		var formatErr *modelJSONError
		if !errors.As(err, &formatErr) {
			return d, err
		}
		if attempt == 3 {
			return Decision{}, fmt.Errorf("%w；JSON 格式纠正已重试 3 次，仍不合格", err)
		}
		if strings.TrimSpace(reply) == "" {
			reply = "（上一条模型响应为空）"
		}
		messages = append(messages,
			map[string]string{"role": "assistant", "content": reply},
			map[string]string{"role": "user", "content": "上一条裁决的 JSON 格式校验失败：" + formatErr.detail + "。请根据最初的审查输入和原有规则重新输出完整裁决，只修正格式，不因格式反馈改变安全判断。仅输出一个 JSON 对象，且仅包含字符串字段 decision 和 comment，不要 Markdown、解释文字或额外字段。decision 只能为 approve 或 reject；comment 使用“实际操作：...；成功后的后果：...；命中规则：...”结构，规则编号须与结论一致。"},
		)
	}
}

// modelJSONError marks only verdict JSON syntax/shape errors as retryable.
// Its diagnostic never includes raw model output or arbitrary field names.
type modelJSONError struct {
	message string
	detail  string
}

func (e *modelJSONError) Error() string { return e.message }

func callModelText(ctx context.Context, s Settings, messages []map[string]string, usage **TokenUsage) (string, error) {
	if err := ValidateModelURL(s.Model.BaseURL); err != nil {
		return "", err
	}
	if s.Model.Model == "" {
		return "", errors.New("模型名称为空")
	}
	endpoint := strings.TrimRight(s.Model.BaseURL, "/")
	payload := map[string]any{"model": s.Model.Model, "stream": false, "messages": append([]map[string]string{{"role": "system", "content": s.Prompt}}, messages...)}
	if s.Model.Protocol == "anthropic" {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/messages"
		} else {
			endpoint += "/v1/messages"
		}
		payload = map[string]any{"model": s.Model.Model, "stream": false, "max_tokens": 2048, "system": s.Prompt, "messages": messages}
	} else {
		endpoint += "/chat/completions"
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
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
		return "", errors.New("模型连接失败或请求超时")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("模型返回 HTTP %d", res.StatusCode)
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
			return "", errors.New("Anthropic 模型响应格式无效")
		}
		*usage = parseUsage(out.Usage, s.Model.Protocol)
		if out.StopReason != "end_turn" {
			return "", errors.New("Anthropic 模型没有完成文本裁决")
		}
		texts := []string{}
		for _, c := range out.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return strings.Join(texts, "\n"), nil
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
		return "", errors.New("模型响应格式无效")
	}
	*usage = parseUsage(out.Usage, s.Model.Protocol)
	if len(out.Choices) != 1 {
		return "", errors.New("模型响应格式无效")
	}
	if reason := out.Choices[0].FinishReason; reason != "" && reason != "stop" {
		return "", errors.New("模型裁决未完整输出")
	}
	return out.Choices[0].Message.Content, nil
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
		detail := "只允许 decision、comment 两个字符串字段，不允许额外字段"
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			detail = fmt.Sprintf("JSON 语法错误（字节位置 %d），请检查引号、逗号及对象外的文本或代码围栏", syntaxErr.Offset)
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			detail = "响应为空或 JSON 不完整，请输出完整的 JSON 对象"
		case errors.As(err, &typeErr):
			detail = "JSON 类型错误：顶层必须为对象，decision 和 comment 必须为字符串"
		}
		return Decision{}, &modelJSONError{message: "模型裁决必须为 JSON，且仅包含 decision、comment", detail: detail}
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Decision{}, &modelJSONError{message: "模型输出包含多余内容", detail: "JSON 对象之后包含多余内容，只能输出一个 JSON 对象"}
	}
	for _, key := range []string{"decision", "comment"} {
		if raw, ok := fields[key]; !ok || string(raw) == "null" {
			return Decision{}, &modelJSONError{message: "模型裁决必须为 JSON，且仅包含 decision、comment", detail: "缺少字符串字段 " + key + "，或该字段为 null"}
		}
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
