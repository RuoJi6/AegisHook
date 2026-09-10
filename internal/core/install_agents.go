package core

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(s), "'", "'\"'\"'") + "'"
}
func hookCommand(executable, connection, agent, platform string) string {
	if platform != "windows" {
		return shellQuote(executable) + " hook --agent " + agent + " --connection " + shellQuote(connection)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$ErrorActionPreference='Stop'; try { & " + quote(executable) + " hook --agent " + agent + " --connection " + quote(connection) + "; exit $LASTEXITCODE } catch { [Console]::Error.WriteLine('AegisHook: hook launch failed; blocked'); exit 2 }"
	words := utf16.Encode([]rune(script))
	b := make([]byte, len(words)*2)
	for j, w := range words {
		binary.LittleEndian.PutUint16(b[j*2:], w)
	}
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}
func (e *Engine) PrepareIntegrations(plugin []byte, binary string) error {
	e.IntegrationBinary = binary
	if err := AtomicFile(filepath.Join(e.Dir, "adapter", "opencode.mjs"), plugin, 0600); err != nil {
		return err
	}
	return AtomicFile(filepath.Join(e.Dir, "adapter", "runtime.json"), mustJSON(map[string]string{"binary": binary}), 0600)
}
func hookEvents(agent string) []string {
	events := []string{"SessionStart", "PreToolUse", "PostToolUse", "SessionEnd", "UserPromptSubmit"}
	if agent != "codex" {
		events = append(events, "PostToolUseFailure")
	}
	return events
}
func hookGroup(i Installation, event string) map[string]any {
	timeout := 86520
	if event != "PreToolUse" {
		timeout = 10
	}
	if event == "SessionEnd" && i.Agent == "codex" {
		timeout = 3
	}
	command := i.ManagedCommand
	h := map[string]any{"type": "command", "command": command, "timeout": timeout}
	if i.Agent == "codex" && runtime.GOOS == "windows" {
		h["commandWindows"] = command
	}
	if i.Agent == "claude" && runtime.GOOS == "windows" {
		h["shell"] = "powershell"
	}
	return map[string]any{"hooks": []any{h}}
}
func loadHookDocument(path string) (map[string]any, []byte, os.FileMode, error) {
	mode := os.FileMode(0600)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil, mode, nil
	}
	if err != nil {
		return nil, nil, mode, err
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil, mode, errors.New("配置必须是普通文件，拒绝替换符号链接")
	}
	mode = st.Mode().Perm()
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil || doc == nil {
		return nil, nil, mode, errors.New("现有配置不是有效 JSON 对象，未修改")
	}
	return doc, b, mode, nil
}
func modifyHookDocument(i Installation, install bool) ([]byte, []byte, os.FileMode, error) {
	doc, old, mode, err := loadHookDocument(i.Entry)
	if err != nil {
		return nil, nil, mode, err
	}
	hooks := map[string]any{}
	if value, ok := doc["hooks"]; ok {
		var valid bool
		hooks, valid = value.(map[string]any)
		if !valid {
			return nil, nil, mode, errors.New("现有 hooks 格式异常")
		}
	}
	for _, event := range hookEvents(i.Agent) {
		list := []any{}
		if value, ok := hooks[event]; ok {
			var valid bool
			list, valid = value.([]any)
			if !valid {
				return nil, nil, mode, errors.New("现有事件配置格式异常")
			}
		}
		want := hookGroup(i, event)
		wantBytes, _ := json.Marshal(want)
		next := []any{}
		found := false
		for _, group := range list {
			b, _ := json.Marshal(group)
			if bytes.Equal(b, wantBytes) {
				found = true
				continue
			}
			// A modified owned handler must be resolved by its owner, never silently overwritten.
			if bytes.Contains(b, []byte(strings.Trim(string(mustJSON(i.ManagedCommand)), "\""))) {
				return nil, nil, mode, fmt.Errorf("%s 中 AegisHook 入口已被修改，请先检查配置", event)
			}
			next = append(next, group)
		}
		if install {
			next = append(next, want)
		} else if !found && i.Installed && len(old) > 0 {
			return nil, nil, mode, fmt.Errorf("%s 中自有入口缺失或已更改，未删除其它配置", event)
		}
		if len(next) > 0 {
			hooks[event] = next
		} else {
			delete(hooks, event)
		}
	}
	if len(hooks) > 0 {
		doc["hooks"] = hooks
	} else {
		delete(doc, "hooks")
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	return append(b, '\n'), old, mode, err
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func (e *Engine) InstallAgent(agent, scope, project, piDir string) (Installation, error) {
	agent = AgentID(agent)
	if agent == "pi" {
		return e.Install(scope, project, piDir)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out Installation
	if AgentName(agent) == "" {
		return out, errors.New("未知 Agent 类型")
	}
	if scope != "global" && scope != "project" {
		return out, errors.New("安装范围必须是 global 或 project")
	}
	base := agentHome(agent)
	if scope == "project" {
		if !filepath.IsAbs(project) {
			return out, errors.New("项目须使用绝对路径")
		}
		real, err := filepath.EvalSymlinks(project)
		if err != nil {
			return out, err
		}
		st, err := os.Stat(real)
		if err != nil || !st.IsDir() {
			return out, errors.New("项目目录无效")
		}
		project = real
		base = filepath.Join(real, "."+agent)
	}
	if !filepath.IsAbs(base) {
		return out, errors.New("Agent 配置目录须为绝对路径")
	}
	entry := filepath.Join(base, "hooks.json")
	switch agent {
	case "claude":
		entry = filepath.Join(base, "settings.json")
	case "grok":
		entry = filepath.Join(base, "hooks", "aegishook.json")
	case "opencode":
		entry = filepath.Join(base, "plugins", "aegishook.js")
	}
	if e.IntegrationBinary == "" {
		return out, errors.New("请先启动服务准备适配器")
	}
	entries, err := list[Installation](e, "installations")
	if err != nil {
		return out, err
	}
	for _, v := range entries {
		if v.Entry == entry {
			out = v
		}
		if agent == "grok" && v.Agent == agent && v.Installed && v.Entry != entry {
			return out, errors.New("Grok 未保证原生调用 ID，暂不同时安装多个范围；请先卸载另一范围")
		}
	}
	if out.ID == "" {
		out = Installation{ID: ID(), Agent: agent, Scope: scope, Project: project, Entry: entry, CreatedAt: e.clock()}
	}
	oldCommand := out.ManagedCommand
	out.ManagedCommand = hookCommand(e.IntegrationBinary, filepath.Join(e.Dir, "adapter", "connection.json"), agent, runtime.GOOS)
	if out.Installed && oldCommand != "" && oldCommand != out.ManagedCommand {
		return out, errors.New("程序路径已变化，请先用原程序卸载该入口再安装")
	}
	if err = os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
		return out, err
	}
	var next, old []byte
	mode := os.FileMode(0600)
	if agent == "opencode" {
		// Both scopes import exactly one module URL; OpenCode/module caching deduplicates the adapter.
		moduleURL := fileURL(filepath.Join(e.Dir, "adapter", "opencode.mjs"))
		out.ManagedContent = "// AegisHook managed entry\nexport { default } from " + string(mustJSON(moduleURL)) + ";\n"
		old, err = os.ReadFile(entry)
		if err == nil && string(old) != out.ManagedContent {
			return out, errors.New("同名插件非自有或已修改，拒绝覆盖")
		}
		if err != nil && !os.IsNotExist(err) {
			return out, err
		}
		if st, er := os.Lstat(entry); er == nil && !st.Mode().IsRegular() {
			return out, errors.New("插件入口须为普通文件")
		}
		next = []byte(out.ManagedContent)
	} else {
		next, old, mode, err = modifyHookDocument(out, true)
		if err != nil {
			return out, err
		}
	}
	if err = AtomicFile(entry, next, mode); err != nil {
		return out, err
	}
	out.Installed = true
	out.Status = "waiting_load"
	out.Version = Version
	if err = e.change("installations", out.ID, out, "hook.install", AgentName(agent)+" "+entry); err != nil {
		if old == nil {
			os.Remove(entry)
		} else {
			AtomicFile(entry, old, mode)
		}
	}
	return out, err
}
func (e *Engine) uninstallAgent(i Installation) error {
	var old, next []byte
	var err error
	mode := os.FileMode(0600)
	if i.Agent == "opencode" {
		old, err = os.ReadFile(i.Entry)
		if os.IsNotExist(err) {
			old = nil
			err = nil
		} else if err == nil && string(old) != i.ManagedContent {
			err = errors.New("插件入口已更改，拒绝删除")
		}
		if err != nil {
			return err
		}
		if old != nil {
			err = os.Remove(i.Entry)
		}
	} else {
		next, old, mode, err = modifyHookDocument(i, false)
		if err == nil && old != nil {
			err = AtomicFile(i.Entry, next, mode)
		}
	}
	if err != nil {
		return err
	}
	i.Installed = false
	i.Status = "uninstalled"
	if err = e.change("installations", i.ID, i, "hook.uninstall", AgentName(i.Agent)+" "+i.Entry); err != nil && old != nil {
		AtomicFile(i.Entry, old, mode)
	}
	return err
}
func (e *Engine) agentEntryOK(i Installation) bool {
	if i.Agent == "opencode" {
		b, err := os.ReadFile(i.Entry)
		return err == nil && string(b) == i.ManagedContent
	}
	doc, _, _, err := loadHookDocument(i.Entry)
	if err != nil {
		return false
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return false
	}
	for _, event := range hookEvents(i.Agent) {
		groups, ok := hooks[event].([]any)
		if !ok {
			return false
		}
		found := false
		for _, g := range groups {
			if bytes.Equal(mustJSON(g), mustJSON(hookGroup(i, event))) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
