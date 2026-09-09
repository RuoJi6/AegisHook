package core

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (e *Engine) Register(i Instance) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if real, err := filepath.EvalSymlinks(i.Cwd); err == nil {
		i.Cwd = real
	}
	i.Agent = AgentID(i.Agent)
	if AgentName(i.Agent) == "" {
		return errors.New("未知 Agent")
	}
	if i.ID == "" || i.SessionID == "" || !filepath.IsAbs(i.Cwd) || i.HookVersion != Version {
		return errors.New("会话字段或 Hook 版本无效")
	}
	// Keep historical validation separate from bindings for this registration.
	// Ignore caller-supplied history; only successful registrations establish it.
	i.ObservedInstallations = nil
	var prev Instance
	if e.get("instances", i.ID, &prev) == nil {
		if prev.SessionID != i.SessionID || prev.Cwd != i.Cwd || AgentID(prev.Agent) != i.Agent {
			return errors.New("实例身份冲突")
		}
		bindings := i.Installations
		i = prev
		for _, id := range prev.Installations {
			if !hasExact(id, i.ObservedInstallations...) {
				i.ObservedInstallations = append(i.ObservedInstallations, id)
			}
		}
		i.Installations = bindings
	}
	for _, id := range i.Installations {
		if !hasExact(id, i.ObservedInstallations...) {
			i.ObservedInstallations = append(i.ObservedInstallations, id)
		}
	}
	i.StartedAt = e.clock()
	i.Heartbeat = e.clock()
	i.State = "idle"
	i.DisconnectReason = ""
	i.Online = true
	return e.change("instances", i.ID, i, "instance.register", AgentName(i.Agent)+" 会话事件已接入")
}
func (e *Engine) Heartbeat(id, state string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", id, &i); err != nil {
		return err
	}
	if i.State == "disconnected" {
		return errors.New("实例已注销，请重新注册")
	}
	i.Heartbeat = e.clock()
	if hasExact(state, "idle", "running", "awaiting_review") {
		i.State = state
	}
	return e.put("instances", id, i)
}
func (e *Engine) Disconnect(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", id, &i); err != nil {
		return err
	}
	i.State = "disconnected"
	i.DisconnectReason = "session_end"
	i.Online = false
	if err := e.change("instances", id, i, "instance.disconnect", AgentName(i.Agent)+" 会话已断开"); err != nil {
		return err
	}
	rs, err := e.Reviews()
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.InstanceID == id && r.Decision == "pending" {
			r.Decision = "reject"
			r.NeedsHuman = false
			r.Execution = "not_executed"
			r.RuleID = "E_SESSION"
			r.Comment = "原会话已结束，本次审查已取消。"
			now := e.clock()
			r.DecidedAt = &now
			if err = e.commitReview(r, "review.cancel"); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *Engine) Submit(in ReviewInput) (Review, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", in.InstanceID, &i); err != nil {
		return Review{}, errors.New("Agent 实例尚未注册")
	}
	if i.State == "disconnected" || e.clock().Sub(i.Heartbeat) > 30*time.Second {
		return Review{}, errors.New("Agent 实例已失联")
	}
	if in.CallID == "" || in.ToolName == "" || in.Arguments == nil {
		return Review{}, errors.New("审查参数缺失")
	}
	b, err := json.Marshal(struct {
		Tool string
		Args map[string]any
	}{in.ToolName, in.Arguments})
	if err != nil {
		return Review{}, err
	}
	digest := sha256.Sum256(b)
	key := sha256.Sum256([]byte(in.InstanceID + "\x00" + in.CallID))
	id := hex.EncodeToString(key[:])
	var existing Review
	if err = e.get("reviews", id, &existing); err == nil {
		if existing.Digest != hex.EncodeToString(digest[:]) {
			return Review{}, errors.New("工具调用标识已被不同参数使用")
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Review{}, err
	}
	rules, err := e.Rules()
	if err != nil {
		return Review{}, err
	}
	scopes, err := e.Scopes()
	if err != nil {
		return Review{}, err
	}
	d := checkScope(in, i.Cwd, scopes)
	if d == nil {
		d = evaluateAt(in, rules, i.Cwd)
	}
	s := e.Settings
	if s.Mode == "model" {
		s.Prompt = modelPrompt(s.Prompt)
	}
	in.Arguments = Redact(in.Arguments).(map[string]any)
	in.UserMessage = RedactText(in.UserMessage)
	in.Context = RedactText(in.Context)
	r := Review{Agent: AgentID(i.Agent), ID: id, ReviewInput: in, SessionID: i.SessionID, Cwd: i.Cwd, Digest: hex.EncodeToString(digest[:]), Mode: s.Mode, Version: s.Version, Prompt: s.Prompt, Rules: rules, Scopes: scopes, Decision: "pending", Execution: "not_executed", CreatedAt: e.clock(), Deadline: e.clock().Add(time.Duration(s.ApprovalSeconds) * time.Second)}
	if s.Mode == "model" {
		if d == nil {
			filtered := modelContext(r.Context)
			r.ModelContext = &filtered
		}
		r.Deadline = e.clock().Add(time.Duration(s.ModelSeconds+5) * time.Second)
	}
	if d == nil && s.Mode == "human" {
		r.NeedsHuman = true
	}
	if d != nil {
		r.Decision = d.Decision
		r.RuleID = d.RuleID
		r.Comment = RedactText(d.Comment)
		now := e.clock()
		r.DecidedAt = &now
		if d.Decision == "approve" {
			r.Execution = "awaiting_execution"
		}
	}
	if err = e.commitReview(r, "review.submit"); err != nil {
		return Review{}, err
	}
	if d == nil && s.Mode == "model" {
		go e.modelReview(r, s)
	}
	return r, nil
}
func (e *Engine) Decide(id, digest, decision, comment string) (Review, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.Review(id)
	if err != nil {
		return r, err
	}
	if r.Digest != digest {
		return r, errors.New("参数摘要不匹配")
	}
	if !hasExact(decision, "approve", "reject") {
		return r, errors.New("无效裁决")
	}
	if r.Mode != "human" {
		return r, errors.New("此调用不属于人工审批")
	}
	if r.Decision != "pending" {
		if r.Decision == decision && r.RuleID == "HUMAN" {
			return r, nil
		}
		return r, errors.New("此调用已裁决或已失效")
	}
	if e.clock().After(r.Deadline) {
		return r, errors.New("审批已过期")
	}
	if decision == "reject" && comment == "" {
		return r, errors.New("请填写拒绝原因")
	}
	r.Decision = decision
	r.NeedsHuman = false
	r.RuleID = "HUMAN"
	if comment == "" {
		comment = "人工批准本次工具调用"
	}
	r.Comment = RedactText(comment)
	now := e.clock()
	r.DecidedAt = &now
	if decision == "approve" {
		r.Execution = "awaiting_execution"
	}
	err = e.commitReview(r, "review.human")
	return r, err
}
func (e *Engine) Result(id, instance, state, result string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.Review(id)
	if err != nil {
		return err
	}
	if r.InstanceID != instance {
		return errors.New("实例不匹配")
	}
	if r.Decision == "pending" {
		return errors.New("尚未裁决")
	}
	if r.Decision == "reject" {
		if state != "not_executed" {
			return errors.New("被拒绝的调用不能报告执行成功")
		}
		return nil
	}
	if !hasExact(state, "succeeded", "failed", "not_executed") {
		return errors.New("无效执行结果")
	}
	if r.Execution != "awaiting_execution" {
		if r.Execution == state {
			return nil
		}
		return errors.New("执行结果已终结")
	}
	r.Execution = state
	r.Result = RedactText(result)
	return e.commitReview(r, "review.result")
}
func (e *Engine) LocalDiagnostic() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]any{"version": Version, "mode": e.Settings.Mode, "dataDir": e.Dir, "modelConfigured": e.Settings.Model.Tested, "toolBoundary": "Pi tool_call；不包含脚本内部子调用、其它扩展 pi.exec 或用户 ! 命令"}
}
func (e *Engine) ExportCalls() ([]byte, error) {
	rs, err := e.Reviews()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(rs, "", "  ")
}
func (e *Engine) Secret(name string) (string, error) {
	path := filepath.Join(e.Dir, name+".token")
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	token := ID() + ID()
	if err = AtomicFile(path, []byte(token), 0600); err != nil {
		return "", fmt.Errorf("保存令牌失败: %w", err)
	}
	return token, nil
}
