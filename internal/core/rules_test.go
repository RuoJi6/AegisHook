package core

import (
	"strings"
	"testing"
)

func ruleByID(t *testing.T, e *Engine, id string) Rule {
	t.Helper()
	rules, err := e.Rules()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rule %s missing", id)
	return Rule{}
}

func TestEditableBuiltinRulesAndSnapshots(t *testing.T) {
	e := engine(t)
	original := ruleByID(t, e, "R1")
	first, err := e.Submit(input("before-edit", "bash", map[string]any{"command": "passwd fixture"}))
	if err != nil || first.Decision != "reject" {
		t.Fatal(first, err)
	}
	edited := original
	edited.Name, edited.Enabled = "停用密码检查", false
	if err = e.SaveRule(edited); err != nil {
		t.Fatal(err)
	}
	second, err := e.Submit(input("after-disable", "bash", map[string]any{"command": "passwd fixture"}))
	if err != nil || second.Decision != "pending" || second.Version <= first.Version {
		t.Fatal(second, err)
	}
	// A disabled/allowed first shell operation cannot hide a different rejecting rule.
	compound, err := e.Submit(input("compound", "bash", map[string]any{"command": "passwd fixture; userdel fixture"}))
	if err != nil || compound.RuleID != "R3" {
		t.Fatal(compound, err)
	}
	edited.Enabled, edited.Decision, edited.Message = true, "approve", "仅用于隔离测试账号"
	if err = e.SaveRule(edited); err != nil {
		t.Fatal(err)
	}
	approved, err := e.Submit(input("after-allow", "bash", map[string]any{"command": "passwd fixture"}))
	if err != nil || approved.Decision != "approve" || approved.Execution != "awaiting_execution" || !strings.Contains(approved.Comment, edited.Message) {
		t.Fatal(approved, err)
	}
	old, _ := e.Review(first.ID)
	again, err := e.Submit(input("after-disable", "bash", map[string]any{"command": "passwd fixture"}))
	if err != nil || old.Decision != "reject" || old.Version != first.Version || again.Decision != "pending" || again.Version != second.Version {
		t.Fatal(old, again, err)
	}
	for _, r := range old.Rules {
		if r.ID == "R1" && (r.Name != original.Name || !r.Enabled || r.Decision != "reject") {
			t.Fatal("old snapshot changed", r)
		}
	}
	if err = e.SaveRule(original); err != nil {
		t.Fatal(err)
	}
	restored, err := e.Submit(input("after-restore", "bash", map[string]any{"command": "passwd fixture"}))
	if err != nil || restored.RuleID != "R1" || restored.Decision != "reject" {
		t.Fatal(restored, err)
	}
	audits, _ := e.Audits()
	count := 0
	for _, a := range audits {
		if a.Action == "rule.save" && a.Subject == "R1" {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("expected three audited edits, got %d", count)
	}
}

func TestBuiltinReplacementMatchersAndPriority(t *testing.T) {
	e := engine(t)
	r := ruleByID(t, e, "R1")
	r.Matcher, r.Tool, r.Field, r.Pattern = "regex", "bash", "command", `^echo fixture$`
	r.Name = "替换后的规则"
	if err := e.SaveRule(r); err != nil {
		t.Fatal(err)
	}
	rules, _ := e.Rules()
	if d := Evaluate(input("", "bash", map[string]any{"command": "passwd fixture"}), rules); d != nil {
		t.Fatal("hidden hard rule survived", d)
	}
	if d := Evaluate(input("", "bash", map[string]any{"command": "echo fixture"}), rules); d == nil || d.RuleID != "R1" {
		t.Fatal(d)
	}
	r.Target, r.Matcher, r.Pattern, r.Tool, r.Field = "toolName", "contains", "fixture", "", ""
	if err := e.SaveRule(r); err != nil {
		t.Fatal(err)
	}
	rules, _ = e.Rules()
	if d := Evaluate(input("", "fixture_tool", nil), rules); d == nil || d.RuleID != "R1" {
		t.Fatal(d)
	}
	allow := Rule{ID: "U_ALLOW", Name: "high priority allow", Enabled: true, Decision: "approve", Pattern: ".*", Priority: 999}
	deny := Rule{ID: "U_DENY", Name: "highest deny", Enabled: true, Decision: "reject", Pattern: ".*", Priority: 100}
	for _, x := range []Rule{allow, deny} {
		if err := e.SaveRule(x); err != nil {
			t.Fatal(err)
		}
	}
	rules, _ = e.Rules()
	if d := Evaluate(input("", "fixture_tool", nil), rules); d == nil || d.RuleID != "U_DENY" {
		t.Fatal("reject/priority ordering failed", d)
	}
	// A7 also honors an edited decision and its enabled switch.
	e.DeleteRule("U_ALLOW")
	e.DeleteRule("U_DENY")
	a := ruleByID(t, e, "A7")
	a.Decision = "reject"
	if err := e.SaveRule(a); err != nil {
		t.Fatal(err)
	}
	rules, _ = e.Rules()
	if d := Evaluate(input("", "read", map[string]any{"path": "fixture"}), rules); d == nil || d.RuleID != "A7" || d.Decision != "reject" {
		t.Fatal(d)
	}
	a.Enabled = false
	e.SaveRule(a)
	rules, _ = e.Rules()
	if d := Evaluate(input("", "read", map[string]any{"path": "fixture"}), rules); d != nil {
		t.Fatal(d)
	}
}

func TestBuiltinLegacyCompatibilityAndPersistence(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an existing database from before configurable matchers.
	legacy := Rule{ID: "R1", Name: "legacy password rule", Builtin: true, Enabled: true, Decision: "reject"}
	if err = e.put("rules", legacy.ID, legacy); err != nil {
		t.Fatal(err)
	}
	loaded := ruleByID(t, e, "R1")
	if loaded.Matcher != "semantic" || loaded.Target != "arguments" {
		t.Fatal(loaded)
	}
	loaded.Enabled, loaded.Name = false, "saved disabled builtin"
	if err = e.SaveRule(loaded); err != nil {
		t.Fatal(err)
	}
	version := e.Settings.Version
	e.Close()
	e, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	loaded = ruleByID(t, e, "R1")
	if loaded.Enabled || loaded.Name != "saved disabled builtin" || e.Settings.Version != version {
		t.Fatal("restart reset edit", loaded)
	}
	rules, _ := e.Rules()
	if d := Evaluate(input("", "bash", map[string]any{"command": "passwd fixture"}), rules); d != nil {
		t.Fatal(d)
	}
	for _, invalid := range []Rule{
		{ID: "R99", Name: "unknown builtin", Decision: "reject", Builtin: true},
		{ID: "U_FAKE", Name: "fake builtin", Decision: "reject", Builtin: true},
		{ID: "U_BAD", Name: "invalid regexp", Decision: "reject", Pattern: "["},
		{ID: "R1", Name: "bad matcher", Decision: "reject", Matcher: "unsupported"},
		{ID: "R1", Name: "bad decision", Decision: "maybe"},
		{ID: "R1", Name: "ignored fields", Decision: "reject", Matcher: "semantic", Pattern: ".*"},
	} {
		if err := e.SaveRule(invalid); err == nil {
			t.Fatal("accepted invalid rule", invalid)
		}
	}
}
