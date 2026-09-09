package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type AgentInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Found        bool   `json:"found"`
	Version      string `json:"version"`
	Path         string `json:"path"`
	Platform     string `json:"platform"`
	Integration  string `json:"integration"`
	Instructions string `json:"instructions"`
	Limitations  string `json:"limitations"`
}

func AgentID(s string) string {
	if s == "" {
		return "pi"
	}
	return s
}
func AgentName(s string) string {
	return map[string]string{"pi": "Pi", "claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode", "grok": "Grok Build"}[AgentID(s)]
}
func DetectAgents() []AgentInfo {
	out := []AgentInfo{}
	for _, id := range []string{"pi", "claude", "codex", "opencode", "grok"} {
		a := AgentInfo{ID: id, Name: AgentName(id), Platform: runtime.GOOS, Integration: "command", Instructions: "安装后重启客户端，并在客户端检查 Hook 已加载。", Limitations: "仅覆盖客户端提供的工具事件；Hook 命令无法启动、被禁用或被客户端超时终止时，客户端可能放行。"}
		switch id {
		case "pi":
			a.Integration = "extension"
			a.Instructions = "在 Pi 空闲时 /reload 或重启，等待实际心跳。"
			a.Limitations = "Pi 0.85.1；不覆盖用户 ! 命令或其它扩展内部执行。Windows 需 Git Bash；符号链接需要开发者模式或相应权限。"
		case "claude":
			a.Instructions = "重启 Claude Code，用 /hooks 检查配置；保留客户端自身权限判断。"
		case "codex":
			a.Instructions = "重启 Codex，使用 /hooks 审阅并信任新 Hook；项目本身也须受信任。"
			a.Limitations += " 托管工具和后续 write_stdin 输入不触发新的工具前审查。"
		case "opencode":
			a.Integration = "plugin"
			a.Instructions = "重启 OpenCode，插件在会话事件或首次工具调用时注册。"
			a.Limitations = "不覆盖插件自身的执行；仅在客户端明确报告完成或失败时更新执行结果。"
		case "grok":
			a.Instructions = "重启 Grok Build，项目安装须在客户端 /hooks-trust，使用 /hooks 检查。"
			a.Limitations += " 无原生调用 ID 时仅记录前置裁决，不推测执行结果；同一数据目录仅允许一个 Grok 安装范围。"
		}
		if p, err := exec.LookPath(id); err == nil {
			a.Path = p
			a.Found = true
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			b, err := exec.CommandContext(ctx, p, "--version").Output()
			cancel()
			if err == nil {
				a.Version = strings.TrimSpace(string(b))
			}
		}
		out = append(out, a)
	}
	return out
}
func agentHome(agent string) string {
	home, _ := os.UserHomeDir()
	switch agent {
	case "claude":
		if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
			return v
		}
		return filepath.Join(home, ".claude")
	case "codex":
		if v := os.Getenv("CODEX_HOME"); v != "" {
			return v
		}
		return filepath.Join(home, ".codex")
	case "grok":
		return filepath.Join(home, ".grok")
	case "opencode":
		if v := os.Getenv("OPENCODE_CONFIG_DIR"); v != "" {
			return v
		}
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, "opencode")
		}
		return filepath.Join(home, ".config", "opencode")
	}
	return AgentDir()
}
