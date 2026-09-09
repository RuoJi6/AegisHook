package core

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestMultiAgentInstallLifecycle(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "opencode", "grok"} {
		t.Run(agent, func(t *testing.T) {
			e := engine(t)
			home := t.TempDir()
			project := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("OPENCODE_CONFIG_DIR", "")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			if err := e.PrepareAdapter([]byte("fixture"), "http://127.0.0.1:1", "fixture"); err != nil {
				t.Fatal(err)
			}
			if err := e.PrepareIntegrations([]byte("export default ()=>({})"), filepath.Join(home, "with space's", "aegis")); err != nil {
				t.Fatal(err)
			}
			g, err := e.InstallAgent(agent, "global", "", "")
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(g.Entry)
			second, err := e.InstallAgent(agent, "global", "", "")
			if err != nil || second.ID != g.ID {
				t.Fatal(second, err)
			}
			after, _ := os.ReadFile(g.Entry)
			if !bytes.Equal(before, after) {
				t.Fatal("not idempotent")
			}
			if !e.agentEntryOK(g) {
				t.Fatal("invalid owned entry")
			}
			if agent == "grok" {
				if _, err = e.InstallAgent(agent, "project", project, ""); err == nil {
					t.Fatal("ambiguous Grok scopes accepted")
				}
				if err = e.Uninstall(g.ID); err != nil {
					t.Fatal(err)
				}
			}
			p, err := e.InstallAgent(agent, "project", project, "")
			if err != nil {
				t.Fatal(err)
			}
			bindings := e.BindAgentInstallations(agent, project)
			expected := 2
			if agent == "grok" {
				expected = 1
			}
			if len(bindings) != expected {
				t.Fatal(bindings)
			}
			if len(e.BindAgentInstallations("pi", project)) != 0 {
				t.Fatal("cross-agent binding")
			}
			if err = e.Register(Instance{ID: "native", Agent: agent, SessionID: "s", Cwd: project, HookVersion: Version, ConnectionMode: "events", Installations: bindings}); err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			var inst Instance
			e.get("instances", "native", &inst)
			inst.Heartbeat = time.Now().Add(-time.Minute)
			e.put("instances", "native", inst)
			e.mu.Unlock()
			entries, _ := e.Installations()
			for _, i := range entries {
				if i.ID == p.ID && i.Status != "observed" {
					t.Fatal("event client falsely offline/online", i)
				}
			}
			if agent != "opencode" {
				doc, _, mode, err := loadHookDocument(p.Entry)
				if err != nil {
					t.Fatal(err)
				}
				doc["foreign_setting"] = "keep"
				hooks := doc["hooks"].(map[string]any)
				hooks["PreToolUse"] = append(hooks["PreToolUse"].([]any), map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo foreign"}}})
				if err = AtomicFile(p.Entry, mustJSON(doc), mode); err != nil {
					t.Fatal(err)
				}
				if err = e.Uninstall(p.ID); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(p.Entry)
				if !bytes.Contains(b, []byte("echo foreign")) || !bytes.Contains(b, []byte("foreign_setting")) {
					t.Fatal("foreign config lost")
				}
			} else {
				if err = e.Uninstall(p.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(p.Entry); !os.IsNotExist(err) {
					t.Fatal("entry left")
				}
			}
			if err = e.Uninstall(p.ID); err != nil {
				t.Fatal("second uninstall", err)
			}
			if agent != "grok" && !e.agentEntryOK(g) {
				t.Fatal("other scope changed")
			}
			if _, err = os.Stat(filepath.Join(e.Dir, "adapter", "connection.json")); err != nil {
				t.Fatal("connection deleted")
			}
		})
	}
}
func TestMultiAgentConflictAndRollback(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "opencode", "grok"} {
		t.Run(agent, func(t *testing.T) {
			e := engine(t)
			e.PrepareAdapter([]byte("test"), "http://127.0.0.1:1", "test")
			e.PrepareIntegrations([]byte("test"), "/fixture/aegis")
			project := t.TempDir()
			i, err := e.InstallAgent(agent, "project", project, "")
			if err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(i.Entry)
			var altered []byte
			if agent == "opencode" {
				altered = []byte("export default ()=>({}); // not ours")
			} else {
				doc, _, _, _ := loadHookDocument(i.Entry)
				hooks := doc["hooks"].(map[string]any)
				hooks["PreToolUse"].([]any)[0].(map[string]any)["matcher"] = "Bash"
				altered = mustJSON(doc)
			}
			os.WriteFile(i.Entry, altered, 0600)
			if _, err = e.InstallAgent(agent, "project", project, ""); err == nil {
				t.Fatal("modified hook overwritten")
			}
			if err = e.Uninstall(i.ID); err == nil {
				t.Fatal("modified hook removed")
			}
			now, _ := os.ReadFile(i.Entry)
			if !bytes.Equal(now, altered) {
				t.Fatal("foreign content changed")
			}
			os.WriteFile(i.Entry, original, 0600)
			if err = e.Uninstall(i.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = e.DB.Exec(`CREATE TRIGGER reject_agent_audit BEFORE INSERT ON records WHEN NEW.kind='audit' BEGIN SELECT RAISE(FAIL, 'fixture'); END;`); err != nil {
				t.Fatal(err)
			}
			other := t.TempDir()
			failedInstall, installErr := e.InstallAgent(agent, "project", other, "")
			err = installErr
			if err == nil {
				t.Fatal("audit failure accepted")
			}
			if _, statErr := os.Stat(failedInstall.Entry); !os.IsNotExist(statErr) {
				t.Fatal("filesystem not rolled back", statErr)
			}
		})
	}
}
func TestNativeToolAliases(t *testing.T) {
	for _, tc := range []struct{ tool, key, value, rule string }{
		{"Read", "file_path", "README.md", "A7"}, {"glob", "path", ".", "A7"}, {"Write", "file_path", "/etc/fixture", "R2"}, {"edit", "filePath", "/etc/fixture", "R2"},
		{"Bash", "command", "passwd fixture", "R1"}, {"powershell", "command", "Remove-LocalUser -Name fixture", "R3"}, {"powershell", "command", "Restart-Service fixture", "R5"}, {"shell", "cmd", "Register-ScheduledTask -TaskName fixture", "R2"},
	} {
		d := Evaluate(ReviewInput{ToolName: tc.tool, Arguments: map[string]any{tc.key: tc.value}}, BuiltinRules())
		if d == nil || d.RuleID != tc.rule {
			t.Fatalf("%+v: %+v", tc, d)
		}
	}
	if !protectedPath(`C:\Windows\System32\drivers\etc\hosts`) {
		t.Fatal("Windows protected path")
	}
	dir := t.TempDir()
	allowed := filepath.Join(dir, "allowed")
	os.Mkdir(allowed, 0700)
	for _, field := range []string{"path", "file_path", "filePath"} {
		if d := checkScope(ReviewInput{ToolName: "Read", Arguments: map[string]any{field: filepath.Join(dir, "outside")}}, dir, []Scope{{Project: dir, Paths: []string{allowed}}}); d == nil {
			t.Fatal(field, "scope bypass")
		}
	}
}
func TestWindowsHookCommand(t *testing.T) {
	command := hookCommand(`C:\Users\中文 O'Brien\aegis.exe`, `C:\private\connection.json`, "claude", "windows")
	data, err := base64.StdEncoding.DecodeString(command[strings.LastIndex(command, " ")+1:])
	if err != nil {
		t.Fatal(err)
	}
	words := make([]uint16, len(data)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	script := string(utf16.Decode(words))
	if !strings.Contains(script, "O''Brien") || !strings.Contains(script, "exit $LASTEXITCODE") || !strings.Contains(script, "blocked'); exit 2") {
		t.Fatal(script)
	}
	var doc map[string]any
	if json.Unmarshal(mustJSON(map[string]string{"command": command}), &doc) != nil {
		t.Fatal("invalid JSON")
	}
}
