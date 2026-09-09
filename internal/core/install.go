package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func AgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		p, _ := filepath.Abs(d)
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}
func (e *Engine) PrepareAdapter(code []byte, endpoint, token string) error {
	p := filepath.Join(e.Dir, "adapter")
	if err := os.MkdirAll(p, 0700); err != nil {
		return err
	}
	if err := AtomicFile(filepath.Join(p, "index.ts"), code, 0600); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"endpoint": endpoint, "token": token})
	return AtomicFile(filepath.Join(p, "connection.json"), b, 0600)
}
func (e *Engine) Install(scope, project, agentDir string) (Installation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out Installation
	if scope != "global" && scope != "project" {
		return out, errors.New("安装范围必须是 global 或 project")
	}
	base := agentDir
	if scope == "project" {
		if !filepath.IsAbs(project) {
			return out, errors.New("项目须使用绝对路径")
		}
		real, err := filepath.EvalSymlinks(project)
		if err != nil {
			return out, errors.New("项目目录不存在")
		}
		stat, err := os.Stat(real)
		if err != nil || !stat.IsDir() {
			return out, errors.New("项目须为目录")
		}
		project = real
		base = filepath.Join(project, ".pi")
	}
	if !filepath.IsAbs(base) {
		return out, errors.New("Pi 扩展目录须为绝对路径")
	}
	dir := filepath.Join(base, "extensions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	entry := filepath.Join(dir, "aegishook.ts")
	target := filepath.Join(e.Dir, "adapter", "index.ts")
	if _, err := os.Stat(target); err != nil {
		return out, errors.New("适配器资源未准备好，请先启动服务")
	}
	exists := false
	if _, err := os.Lstat(entry); err == nil {
		link, err := os.Readlink(entry)
		if err != nil || link != target {
			return out, errors.New("同名入口不属于 AegisHook，拒绝覆盖")
		}
		exists = true
	} else if !os.IsNotExist(err) {
		return out, err
	}
	installations, err := list[Installation](e, "installations")
	if err != nil {
		return out, err
	}
	for _, i := range installations {
		if i.Entry == entry {
			out = i
		}
	}
	if out.ID == "" {
		out = Installation{Agent: "pi", ID: ID(), Scope: scope, Project: project, Entry: entry, CreatedAt: e.clock()}
	}
	out.Installed = true
	out.Status = "waiting_load"
	out.Version = Version
	if !exists {
		if err = os.Symlink(target, entry); err != nil {
			return out, err
		}
	}
	if err = e.change("installations", out.ID, out, "hook.install", entry); err != nil {
		if !exists {
			os.Remove(entry)
		}
		return out, err
	}
	return out, nil
}
func (e *Engine) Uninstall(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Installation
	if err := e.get("installations", id, &i); err != nil {
		return err
	}
	if AgentID(i.Agent) != "pi" {
		return e.uninstallAgent(i)
	}
	target := filepath.Join(e.Dir, "adapter", "index.ts")
	link, err := os.Readlink(i.Entry)
	removed := false
	if err == nil {
		if link != target {
			return errors.New("入口归属已变化，拒绝删除")
		}
		if err = os.Remove(i.Entry); err != nil {
			return err
		}
		removed = true
	} else if !os.IsNotExist(err) {
		return errors.New("入口不是 AegisHook 符号链接，拒绝删除")
	}
	i.Installed = false
	i.Status = "uninstalled"
	if err = e.change("installations", i.ID, i, "hook.uninstall", i.Entry+"；已加载会话需重载后退出保护"); err != nil {
		if removed {
			os.Symlink(target, i.Entry)
		}
		return err
	}
	return nil
}
func (e *Engine) Installations() ([]Installation, error) {
	is, err := list[Installation](e, "installations")
	if err != nil {
		return nil, err
	}
	instances, err := e.Instances()
	if err != nil {
		return nil, err
	}
	target := filepath.Join(e.Dir, "adapter", "index.ts")
	for j := range is {
		i := &is[j]
		i.Agent = AgentID(i.Agent)
		i.Status = "uninstalled"
		if i.Installed {
			i.Status = "waiting_load"
			link, err := os.Readlink(i.Entry)
			if (i.Agent == "pi" && (err != nil || link != target)) || (i.Agent != "pi" && !e.agentEntryOK(*i)) {
				i.Status = "entry_error"
			}
		}
		for _, instance := range instances {
			if instance.ConnectionMode == "events" && instance.State != "disconnected" && hasExact(i.ID, instance.Installations...) {
				if i.Installed && i.Status != "entry_error" {
					i.Status = "observed"
				}
				continue
			}
			if instance.Online && hasExact(i.ID, instance.Installations...) {
				if !i.Installed {
					i.Status = "waiting_reload"
				} else if i.Status != "entry_error" {
					i.Status = "loaded"
				}
			}
		}
	}
	return is, nil
}
func (e *Engine) BindInstallations(cwd string) []string { return e.BindAgentInstallations("pi", cwd) }
func (e *Engine) BindAgentInstallations(agent, cwd string) []string {
	agent = AgentID(agent)
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	is, err := list[Installation](e, "installations")
	if err != nil {
		return nil
	}
	out := []string{}
	for _, i := range is {
		if AgentID(i.Agent) == agent && i.Installed && (i.Scope == "global" || within(i.Project, cwd)) {
			link, err := os.Readlink(i.Entry)
			if (agent == "pi" && err == nil && link == filepath.Join(e.Dir, "adapter", "index.ts")) || (agent != "pi" && e.agentEntryOK(i)) {
				out = append(out, i.ID)
			}
		}
	}
	return out
}
func DetectPi() map[string]any {
	p, err := exec.LookPath("pi")
	if err != nil {
		return map[string]any{"found": false, "version": "", "agentDir": AgentDir()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, p, "--version").Output()
	return map[string]any{"found": err == nil, "path": p, "version": strings.TrimSpace(string(b)), "agentDir": AgentDir(), "supportedVersion": "0.85.1"}
}
