package core

import (
	"os"
	"strings"
	"testing"
)

func TestUpgradeDataGuardPreservesCustomization(t *testing.T) {
	old, err := os.ReadFile("testdata/data_guard_v011.txt")
	if err != nil {
		t.Fatal(err)
	}
	legacy := string(old)
	prompt := "自定义前言\n\n" + legacy + "\n\n自定义输出要求"
	if got := upgradeDataGuard(prompt); got != "自定义前言\n\n"+DataGuardPrompt+"\n\n自定义输出要求" {
		t.Fatal("failed to replace only known legacy block")
	}
	for _, unchanged := range []string{
		"自定义提示词，无附加规则。", DefaultPrompt,
		strings.Replace(prompt, "R7：", "R7：自定义条件：", 1),
		strings.Replace(prompt, DataGuardEnd, "", 1),
		legacy + legacy,
	} {
		if upgradeDataGuard(unchanged) != unchanged {
			t.Fatal("changed opt-out or custom policy")
		}
	}
}

func TestPromptUpgradeVersionAndRestart(t *testing.T) {
	old, err := os.ReadFile("testdata/data_guard_v011.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Config()
	s.Version = 13
	s.Prompt = "custom-prefix\n" + string(old) + "\ncustom-suffix"
	if err = e.put("settings", "current", s); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Config().Version != 14 || e.Config().Prompt != "custom-prefix\n"+DataGuardPrompt+"\ncustom-suffix" {
		t.Fatal("migration failed")
	}
	versions, err := e.ConfigVersions()
	if err != nil || len(versions) != 2 {
		t.Fatal("missing version snapshots", err)
	}
	var previous Settings
	if err = e.get("settings_versions", "13", &previous); err != nil || previous.Prompt != s.Prompt {
		t.Fatal("old policy not preserved", err)
	}
	audits, _ := e.Audits()
	if len(audits) != 1 || audits[0].Action != "settings.prompt.upgrade" {
		t.Fatal("missing upgrade audit")
	}
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Config().Version != 14 {
		t.Fatal("migration repeated")
	}
}
