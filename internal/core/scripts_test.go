package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureScriptsSnapshotsLiteralFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "check.py")
	content := "print('fixture')\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	files, status := CaptureScripts(dir, "Bash", map[string]any{"command": "sudo python check.py"})
	if status != "complete" || len(files) != 1 || !files[0].Complete || files[0].Content != content || files[0].SHA256 == "" {
		t.Fatalf("unexpected snapshot: status=%s files=%+v", status, files)
	}
}

func TestCaptureScriptsFailsClosedForDynamicAndInteractiveCommands(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, tool, command string
	}{
		{"dynamic", "Bash", "python $SCRIPT"},
		{"interactive", "shell_send", "python check.py"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, status := CaptureScripts(dir, tc.tool, map[string]any{"command": tc.command, "text": tc.command})
			if status != "incomplete" {
				t.Fatalf("expected incomplete, got %q", status)
			}
		})
	}
}

func TestCaptureScriptsLimitsSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.py")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxReviewScriptBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	files, status := CaptureScripts(dir, "Bash", map[string]any{"command": "python large.py"})
	if status != "incomplete" || len(files) != 1 || !files[0].Truncated {
		t.Fatalf("expected truncated snapshot: status=%s files=%+v", status, files)
	}
}

func TestIncompleteScriptOverridesDeterministicAllow(t *testing.T) {
	e := engine(t)
	if err := e.SaveRule(Rule{ID: "U_ALLOW_SCRIPT", Name: "allow bash", Enabled: true, Decision: "approve", Matcher: "regex", Target: "toolName", Pattern: "^Bash$"}); err != nil {
		t.Fatal(err)
	}
	files, status := CaptureScripts(t.TempDir(), "Bash", map[string]any{"command": "python $SCRIPT"})
	r, err := e.Submit(ReviewInput{InstanceID: "instance", CallID: "incomplete-allow", ToolName: "Bash", Arguments: map[string]any{"command": "python $SCRIPT"}, Scripts: files, ScriptCaptureStatus: status})
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != "pending" || r.ReviewPath != "human" || !r.NeedsHuman || !strings.Contains(r.Comment, "脚本未能完整采集") {
		t.Fatalf("incomplete script was auto-allowed: %+v", r)
	}
}

func TestReviewsPageSearchAndPagination(t *testing.T) {
	e := engine(t)
	now := time.Now().UTC()
	e.clock = func() time.Time { return now }
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	for i, command := range []string{"echo alpha", "echo 100% literal", "echo gamma"} {
		if _, err := e.Submit(input("page-"+string(rune('a'+i)), "Bash", map[string]any{"command": command})); err != nil {
			t.Fatal(err)
		}
	}
	page, err := e.ReviewsPage(1, 2, ReviewFilter{})
	if err != nil || page.Total != 3 || len(page.Items) != 2 || page.PageSize != 2 {
		t.Fatalf("unexpected page: %+v err=%v", page, err)
	}
	match, err := e.ReviewsPage(1, 20, ReviewFilter{Query: "%"})
	if err != nil || match.Total != 1 || len(match.Items) != 1 || !strings.Contains(match.Items[0].Arguments["command"].(string), "%") {
		t.Fatalf("literal search failed: %+v err=%v", match, err)
	}
	day, err := e.ReviewsPage(1, 20, ReviewFilter{Query: "gamma", From: today, To: today})
	if err != nil || day.Total != 1 || len(day.Items) != 1 {
		t.Fatalf("combined date and text filters failed: %+v err=%v", day, err)
	}
	outside, err := e.ReviewsPage(1, 20, ReviewFilter{From: yesterday, To: yesterday})
	if err != nil || outside.Total != 0 {
		t.Fatalf("inclusive end date leaked records: %+v err=%v", outside, err)
	}
	pathAndExecution, err := e.ReviewsPage(1, 20, ReviewFilter{ReviewPath: "human", Execution: "not_executed"})
	if err != nil || pathAndExecution.Total != 3 {
		t.Fatalf("combined approval path and execution filters failed: %+v err=%v", pathAndExecution, err)
	}
	modelOnly, err := e.ReviewsPage(1, 20, ReviewFilter{ReviewPath: "model"})
	if err != nil || modelOnly.Total != 0 {
		t.Fatalf("approval path filter leaked records: %+v err=%v", modelOnly, err)
	}
}

func TestModelInputIncludesScriptSnapshot(t *testing.T) {
	r := Review{ReviewInput: ReviewInput{ToolName: "Bash", Arguments: map[string]any{"command": "python check.py"}, ScriptCaptureStatus: "complete", Scripts: []ReviewScript{{Path: "/tmp/check.py", SHA256: "abc", Content: "print(1)", Complete: true}}}}
	b, err := json.Marshal(modelInput(r))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["scriptCaptureStatus"] != "complete" || !strings.Contains(string(b), "print(1)") {
		t.Fatalf("script snapshot missing: %s", b)
	}
}
