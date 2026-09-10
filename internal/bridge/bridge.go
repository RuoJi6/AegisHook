// Package bridge implements native command hook protocols without opening the server database.
package bridge

import (
	"aegishook/internal/core"
	"aegishook/internal/localaddr"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Config struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	NodeID   string `json:"nodeId,omitempty"`
}
type client struct {
	cfg  Config
	http *http.Client
}

func (c client) request(ctx context.Context, path string, body any, out any) error {
	method := "GET"
	var data []byte
	if body != nil {
		method = "POST"
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("审查服务连接失败")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("审查服务返回 HTTP %d", res.StatusCode)
	}
	if out != nil {
		decoder := json.NewDecoder(io.LimitReader(res.Body, 2<<20))
		if err := decoder.Decode(out); err != nil {
			return err
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return errors.New("审查服务返回非法 JSON")
		}
	}
	return nil
}
func text(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func hash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}
func eventName(m map[string]any) string { return text(m, "hook_event_name", "hookEventName") }
func decisionOutput(agent, decision, reason string, out io.Writer) {
	if agent == "grok" {
		json.NewEncoder(out).Encode(map[string]any{"decision": map[string]string{"approve": "allow", "reject": "deny"}[decision], "reason": reason})
		return
	}
	if agent == "opencode" {
		json.NewEncoder(out).Encode(map[string]any{"decision": decision, "comment": reason})
		return
	}
	if decision == "approve" {
		// An AegisHook permit must not bypass the client's own permissions.
		fmt.Fprintln(out, "{}")
		return
	}
	json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}})
}

// Run always emits an explicit denial and exit 2 when a pre-tool review cannot complete.
func Run(agent, connection string, in io.Reader, out, stderr io.Writer) (code int) {
	agent = core.AgentID(agent)
	deny := func(err error) int {
		reason := "AegisHook 拒绝执行：" + err.Error()
		decisionOutput(agent, "reject", reason, out)
		fmt.Fprintln(stderr, reason)
		return 2
	}
	defer func() {
		if recover() != nil {
			code = deny(errors.New("Hook 内部异常"))
		}
	}()
	if !strings.Contains("|claude|codex|grok|opencode|", "|"+agent+"|") {
		return deny(errors.New("未知客户端"))
	}
	// Grok also discovers Claude settings. Only the dedicated Grok adapter handles Grok events.
	if agent == "claude" && os.Getenv("GROK_HOOK_EVENT") != "" {
		fmt.Fprintln(out, "{}")
		return 0
	}
	var m map[string]any
	decoder := json.NewDecoder(io.LimitReader(in, 2<<20))
	if err := decoder.Decode(&m); err != nil || m == nil {
		return deny(errors.New("Hook 输入不是 JSON 对象"))
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return deny(errors.New("Hook 输入含多余内容"))
	}
	event := eventName(m)
	pre := event == "PreToolUse"
	fail := func(err error) int {
		if pre || event == "" {
			return deny(err)
		}
		fmt.Fprintln(stderr, "AegisHook："+err.Error())
		return 1
	}
	var cfg Config
	b, err := os.ReadFile(connection)
	if err != nil || json.Unmarshal(b, &cfg) != nil {
		return fail(errors.New("无法读取 Hook 连接配置"))
	}
	endpoint, err := localaddr.Endpoint(cfg.Endpoint)
	if err != nil || cfg.Token == "" {
		return fail(errors.New("Hook 连接地址或凭据无效：远程须使用 HTTPS"))
	}
	cfg.Endpoint = endpoint
	c := client{cfg: cfg, http: &http.Client{Timeout: 2500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("禁止重定向") }}}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour+time.Minute)
	defer cancel()
	session := text(m, "session_id", "sessionId", "sessionID")
	cwd := text(m, "cwd", "workspaceRoot")
	if session == "" || !filepath.IsAbs(cwd) {
		return fail(errors.New("缺少会话 ID 或绝对工作目录"))
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	instance := hash(agent, session, cwd)
	if cfg.NodeID != "" {
		instance = hash(cfg.NodeID, agent, session, cwd)
	}
	if event == "SessionEnd" {
		return passive(c.request(ctx, "/instances/"+instance+"/shutdown", map[string]any{}, nil), stderr)
	}
	if !contains(event, "PreToolUse", "PostToolUse", "PostToolUseFailure", "SessionStart", "UserPromptSubmit") {
		return fail(errors.New("不支持的 Hook 事件"))
	}
	if err = c.request(ctx, "/instances/"+instance+"/heartbeat", map[string]any{"state": "idle"}, nil); err != nil {
		err = c.request(ctx, "/instances", core.Instance{ID: instance, NodeID: cfg.NodeID, Platform: runtime.GOOS, Agent: agent, AgentVersion: text(m, "agent_version"), SessionID: session, Cwd: cwd, HookVersion: core.Version, ConnectionMode: "events"}, nil)
		if err != nil {
			return fail(err)
		}
	}
	contextPath := filepath.Join(filepath.Dir(connection), "context-"+instance+".json")
	if event == "UserPromptSubmit" {
		prompt := core.RedactText(text(m, "prompt", "userMessage"))
		return passive(core.AtomicFile(contextPath, mustJSON(map[string]string{"userMessage": prompt}), 0600), stderr)
	}
	if event == "SessionStart" {
		fmt.Fprintln(out, "{}")
		return 0
	}
	callID := text(m, "tool_use_id", "toolUseId", "toolCallId", "callID")
	tool := text(m, "tool_name", "toolName")
	args, _ := m["tool_input"].(map[string]any)
	if args == nil {
		args, _ = m["toolInput"].(map[string]any)
	}
	if !pre {
		if callID == "" {
			fmt.Fprintln(stderr, "AegisHook：客户端未提供调用 ID，保留执行结果未知")
			return 0
		}
		state, result := resultState(agent, event, m)
		if state == "" {
			fmt.Fprintln(stderr, "AegisHook：没有可确认的执行结果，保留等待状态")
			return 0
		}
		err = c.request(ctx, "/reviews/"+hash(instance, callID)+"/result", map[string]any{"instanceId": instance, "state": state, "result": result}, nil)
		return passive(err, stderr)
	}
	if callID == "" {
		if agent != "grok" {
			return deny(errors.New("客户端未提供工具调用 ID"))
		}
		callID = core.ID()
	}
	if tool == "" || args == nil {
		return deny(errors.New("工具名称或参数对象缺失"))
	}
	input := core.ReviewInput{InstanceID: instance, CallID: callID, ToolName: tool, Arguments: args, UserMessage: text(m, "userMessage"), Context: text(m, "context")}
	if input.UserMessage == "" {
		var saved map[string]string
		if b, err = os.ReadFile(contextPath); err == nil && json.Unmarshal(b, &saved) == nil {
			input.UserMessage = saved["userMessage"]
		}
	}
	var review struct {
		ID        string    `json:"id"`
		Decision  string    `json:"decision"`
		Comment   string    `json:"comment"`
		RuleID    string    `json:"ruleId"`
		Execution string    `json:"execution"`
		Deadline  time.Time `json:"deadline"`
	}
	if err = c.request(ctx, "/reviews", input, &review); err != nil {
		return deny(err)
	}
	expected := hash(instance, callID)
	lastBeat := time.Now()
	for {
		if review.ID != expected || !contains(review.Decision, "approve", "reject", "pending") || review.Deadline.IsZero() {
			return deny(errors.New("服务返回非法裁决"))
		}
		if review.Decision != "pending" {
			if review.Comment == "" || review.RuleID == "" || (review.Decision == "approve" && review.Execution != "awaiting_execution") {
				return deny(errors.New("裁决字段不完整或调用已经执行"))
			}
			break
		}
		if time.Now().After(review.Deadline.Add(time.Second)) {
			return deny(errors.New("审查等待超时"))
		}
		select {
		case <-ctx.Done():
			return deny(errors.New("Hook 等待结束"))
		case <-time.After(500 * time.Millisecond):
		}
		if time.Since(lastBeat) > 8*time.Second {
			if err = c.request(ctx, "/instances/"+instance+"/heartbeat", map[string]any{"state": "awaiting_review"}, nil); err != nil {
				return deny(err)
			}
			lastBeat = time.Now()
		}
		review.ID = ""
		review.Decision = ""
		review.Comment = ""
		review.RuleID = ""
		review.Execution = ""
		review.Deadline = time.Time{}
		if err = c.request(ctx, "/reviews/"+expected+"?instanceId="+instance, nil, &review); err != nil {
			return deny(err)
		}
	}
	if review.Decision == "reject" {
		return deny(errors.New(review.Comment))
	}
	decisionOutput(agent, "approve", review.Comment, out)
	return 0
}
func passive(err error, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintln(stderr, "AegisHook："+err.Error())
		return 1
	}
	return 0
}
func contains(s string, vs ...string) bool {
	for _, v := range vs {
		if s == v {
			return true
		}
	}
	return false
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func resultState(agent, event string, m map[string]any) (string, string) {
	v := m["tool_response"]
	if v == nil {
		v = m["toolResponse"]
	}
	if v == nil {
		v = m["result"]
	}
	result := core.RedactText(string(mustJSON(v)))
	if event == "PostToolUseFailure" {
		return "failed", core.RedactText(text(m, "error"))
	}
	if s := text(m, "executionState"); agent == "opencode" && contains(s, "succeeded", "failed", "not_executed") {
		return s, result
	}
	if agent == "claude" || agent == "grok" {
		return "succeeded", result
	}
	obj, ok := v.(map[string]any)
	if !ok {
		if s, ok := v.(string); ok {
			json.Unmarshal([]byte(s), &obj)
		}
	}
	for _, k := range []string{"isError", "is_error"} {
		if value, ok := obj[k].(bool); ok {
			if value {
				return "failed", result
			}
			return "succeeded", result
		}
	}
	for _, k := range []string{"exit_code", "exitCode"} {
		if value, ok := obj[k].(float64); ok {
			if value != 0 {
				return "failed", result
			}
			return "succeeded", result
		}
	}
	// Codex shell output embeds an explicit exit status; do not infer success from text alone.
	if s, ok := v.(string); ok {
		for _, line := range strings.Split(s, "\n") {
			var status int
			if n, _ := fmt.Sscanf(line, "Process exited with code %d", &status); n == 1 {
				if status != 0 {
					return "failed", result
				}
				return "succeeded", result
			}
		}
	}
	return "", result
}
