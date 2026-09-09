package core

import "testing"

func TestSemanticDescriptionsAndExamples(t *testing.T) {
	for _, r := range BuiltinRules() {
		if r.Semantics == nil || len(r.Semantics.Checks) == 0 || r.Semantics.Limits == "" || len(r.Semantics.Examples) == 0 {
			t.Fatalf("missing description: %s", r.ID)
		}
		for _, example := range r.Semantics.Examples {
			d := Evaluate(ReviewInput{ToolName: example.Tool, Arguments: example.Arguments}, []Rule{r})
			if d == nil || d.RuleID != r.ID || d.Decision != r.Decision {
				t.Fatalf("example disagrees with matcher %s: %+v", r.ID, d)
			}
		}
	}
	e := engine(t)
	r := ruleByID(t, e, "R1")
	r.Semantics = &RuleSemantics{Summary: "untrusted client description"}
	if err := e.SaveRule(r); err != nil {
		t.Fatal(err)
	}
	if ruleByID(t, e, "R1").Semantics.Summary == r.Semantics.Summary {
		t.Fatal("client changed semantic definition")
	}
	r.Matcher, r.Field, r.Pattern = "regex", "command", "fixture"
	if err := e.SaveRule(r); err != nil {
		t.Fatal(err)
	}
	if ruleByID(t, e, "R1").Semantics != nil {
		t.Fatal("custom matcher exposes inactive semantics")
	}
}
