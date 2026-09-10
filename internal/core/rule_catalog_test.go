package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDetailedRuleCoverage(t *testing.T) {
	cases := []struct{ id, command string }{
		{"R_SYS_RM", "rm -fr fixture"}, {"R_SYS_RM", "rm -r -f -- fixture"}, {"R_SYS_RM", "sudo -u fixture /bin/rm --recursive fixture"},
		{"R_SYS_RM", "env LANG=C bash -c 'rm -rf fixture'"}, {"R_SYS_RM", "echo $(rm -rf fixture)"},
		{"R_SYS_PATH", "rm /usr/share/fixture"}, {"R_SYS_PATH", "rm /etc/fixture"}, {"R_SYS_PATH", "rmdir /boot/fixture"},
		{"R_SYS_FORMAT", "mkfs.btrfs /dev/sdb"}, {"R_SYS_FORMAT", "diskutil eraseDisk APFS fixture disk9"}, {"R_SYS_FORMAT", "Format-Volume -DriveLetter Z"},
		{"R_SYS_DD", "dd if=fixture of=/dev/nvme0n1"}, {"R_SYS_DD", "echo fixture > /dev/sdb1"}, {"R_SYS_DD", "cat fixture >> /dev/rdisk9"},
		{"R_SYS_FORK", ":(){ :|:& };:"}, {"R_SYS_FORK", "bomb() { bomb | bomb & }; bomb"},
		{"R_SYS_SHUTDOWN", "telinit 0"}, {"R_SYS_SHUTDOWN", "Restart-Computer"},
		{"R_SYS_KILL", "kill -9 -1"}, {"R_SYS_KILL", "kill -- -1"}, {"R_SYS_KILL", "killall -KILL fixture"},
		{"R_SYS_WIPE", "wipe /dev/vdb"}, {"R_SYS_WIPE", "wipefs -a /dev/sdb"},
		{"R_SYS_FIREWALL", "iptables -t nat -F"}, {"R_SYS_FIREWALL", "nft flush ruleset"}, {"R_SYS_FIREWALL", "ufw --force reset"},
		{"R_DB_DROP", "psql -c '/*comment*/ DROP SCHEMA fixture CASCADE'"}, {"R_DB_DROP", "mysql --execute='DROP INDEX fixture'"},
		{"R_DB_DROP", "sqlite3 fixture.db 'SELECT 1; DROP VIEW fixture'"},
		{"R_DB_TRUNCATE", "psql -c 'TRUNCATE \"fixture\"'"}, {"R_DB_TRUNCATE", "mysql -e 'TRUNCATE TABLE fixture'"},
		{"R_DB_DELETE", "mysql -e 'DELETE FROM fixture'"}, {"R_DB_DELETE", "psql -c 'DELETE FROM \"fixture\";'"},
		{"R_DB_MONGO", "mongosh --eval 'db.getCollection(\"fixture\").drop()'"}, {"R_DB_MONGO", "mongo --eval 'db.dropDatabase()'"},
		{"R_DB_REDIS", "redis-cli -h localhost -p 6379 -n 1 FLUSHALL ASYNC"}, {"R_DB_REDIS", "redis-cli --tls --user fixture --pass fixture FLUSHDB"},
		{"R_HTTP_DELETE", "curl -XDELETE https://example.test/fixture"}, {"R_HTTP_DELETE", "wget --method=DELETE https://example.test/fixture"},
		{"R_HTTP_DELETE", "Invoke-RestMethod -Method Delete -Uri https://example.test/fixture"},
		{"R_HTTP_PYTHON", `python3 -c 'import httpx; httpx.delete("https://example.test/fixture")'`},
		{"R_HTTP_SCRIPT", `node -e 'axios.delete("https://example.test/fixture")'`},
		{"R_HTTP_SCRIPT", `python -c 'requests.request("https://example.test/fixture", method="DELETE")'`},
		{"R_HTTP_CLEAR", "curl --data=x https://example.test/api/%63lear"}, {"R_HTTP_CLEAR", "wget --post-data=x https://example.test/flush"},
		{"R_NET_PIPE", "curl https://example.test/data | nc example.test 9000"},
	}
	for _, tc := range cases {
		t.Run(tc.id+"/"+tc.command, func(t *testing.T) {
			r := catalogRule(t, tc.id)
			r.Enabled = true
			if d := Evaluate(ReviewInput{ToolName: "Bash", Arguments: map[string]any{"command": tc.command}}, []Rule{r}); d == nil || d.RuleID != tc.id {
				t.Fatalf("expected %s, got %+v", tc.id, d)
			}
		})
	}
}

// Coverage tests use defaults only; no DB or command execution is necessary.
func catalogRule(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range BuiltinRules() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("missing %s", id)
	return Rule{}
}

func TestDetailedRuleNonMatches(t *testing.T) {
	cases := []string{
		`echo 'rm -rf /; mkfs.ext4 /dev/sdb; curl -X DELETE https://example.test/x'`,
		`printf '%s' ':(){ :|:& };:'`, `echo "redis-cli FLUSHALL"`,
		`rm -- -recursive`, `rm --help`, `rm fixture`, `dd if=/dev/sdb of=fixture.img`, `echo x > /dev/null`,
		`mkfs.ext4 --help`, `wipefs --no-act /dev/sdb`, `iptables -L`, `nft list ruleset`,
		`bomb() { bomb | bomb & }; echo definition`, `kill -9 12345`,
		`mysql -e "SELECT 'DROP TABLE fixture; TRUNCATE fixture; DELETE FROM fixture'"`,
		`mysql -e 'SELECT 1; -- DROP TABLE fixture'`, `mysql -e 'DELETE FROM fixture WHERE id=1'`,
		`mongosh --eval 'print("db.dropDatabase()")'`, `mongosh --eval '// db.fixture.drop()'`,
		`redis-cli GET FLUSHALL`, `redis-cli -a FLUSHALL PING`,
		`curl https://example.test/api/clear`, `curl -X HEAD https://example.test/api/clear`,
		`curl -X POST https://example.test/read?next=/clear`,
		`curl --data https://example.test/clear https://example.test/read`,
		`curl -H --request=DELETE https://example.test/read`,
		`psql -c 'SELECT $$; DROP SCHEMA fixture; $$'`,
		`python -c 'print("requests.delete(123)")'`,
		`node -e 'console.log("axios.delete(123)")'`,
		`node -e 'fetch("https://example.test"); console.log("method: DELETE")'`,
		`node -e '/* axios.delete("x") */ fetch("https://example.test", {method:"GET"})'`,
	}
	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			hits := detailedBuiltinMatches(ReviewInput{ToolName: "Bash", Arguments: map[string]any{"command": command}}, "", policyPath)
			if len(hits) > 0 {
				t.Fatalf("unexpected matches: %v", hits)
			}
		})
	}
}

func TestDetailedStructuredInputs(t *testing.T) {
	cases := []struct {
		id, tool string
		args     map[string]any
	}{
		{"R_DB_DROP", "execute_sql", map[string]any{"query": "DROP SCHEMA fixture"}},
		{"R_DB_MONGO", "mongo_query", map[string]any{"query": "db.fixture.drop()"}},
		{"R_DB_REDIS", "redis_command", map[string]any{"command": "FLUSHDB ASYNC"}},
		{"R_HTTP_DELETE", "http_request", map[string]any{"method": "delete", "url": "https://example.test/x"}},
		{"R_HTTP_CLEAR", "http_request", map[string]any{"method": "POST", "url": "https://example.test/factory_reset"}},
		{"R_HTTP_PYTHON", "execute_python", map[string]any{"code": "requests.delete('https://example.test/x')"}},
	}
	for _, tc := range cases {
		if d := Evaluate(ReviewInput{ToolName: tc.tool, Arguments: tc.args}, []Rule{catalogRule(t, tc.id)}); d == nil || d.RuleID != tc.id {
			t.Fatalf("missing structured match %s: %+v", tc.id, d)
		}
	}
	// A write/print tool containing source examples is not an execution context.
	if hits := detailedBuiltinMatches(ReviewInput{ToolName: "Write", Arguments: map[string]any{"file_path": "fixture.py", "content": "requests.delete('https://example.test/x')"}}, "", policyPath); len(hits) != 0 {
		t.Fatal(hits)
	}
}

func TestDetailedRuleToggleAndReplacement(t *testing.T) {
	e := engine(t)
	r := ruleByID(t, e, "R_DB_REDIS")
	in := input("redis-toggle", "Bash", map[string]any{"command": "redis-cli FLUSHDB"})
	for _, enabled := range []bool{true, false, true} {
		r.Enabled = enabled
		if err := e.SaveRule(r); err != nil {
			t.Fatal(err)
		}
		rules, _ := e.Rules()
		d := Evaluate(in, rules)
		if (d != nil) != enabled {
			t.Fatalf("toggle %v: %+v", enabled, d)
		}
	}
	r.Matcher, r.Field, r.Pattern = "contains", "command", "fixture-only"
	if err := e.SaveRule(r); err != nil {
		t.Fatal(err)
	}
	rules, _ := e.Rules()
	if d := Evaluate(in, rules); d != nil {
		t.Fatalf("hidden matcher: %+v", d)
	}
}

func TestRuleCatalogUpgradePreservesEdits(t *testing.T) {
	for _, mode := range []string{"default", "disabled", "allow", "matcher", "tool"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			e, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate the seven-rule database shipped before this catalog.
			if _, err = e.DB.Exec("DELETE FROM records WHERE kind='rules' AND id GLOB 'R_*'"); err != nil {
				t.Fatal(err)
			}
			parent := ruleByID(t, e, "R4")
			parent.Name = "my data policy"
			parent.Message = "retained message"
			parent.Priority = 321
			switch mode {
			case "disabled":
				parent.Enabled = false
			case "allow":
				parent.Decision = "approve"
			case "matcher":
				parent.Matcher = "contains"
				parent.Field = "command"
				parent.Pattern = "fixture"
			case "tool":
				parent.Tool = "Bash"
			}
			if err = e.SaveRule(parent); err != nil {
				t.Fatal(err)
			}
			custom := Rule{ID: "U_RETAIN", Name: "custom", Enabled: false, Decision: "reject", Matcher: "contains", Pattern: "fixture"}
			if err = e.SaveRule(custom); err != nil {
				t.Fatal(err)
			}
			before := ruleByID(t, e, "R4")
			version := e.Settings.Version
			e.Close()
			e, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			after := ruleByID(t, e, "R4")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("parent overwritten")
			}
			if ruleByID(t, e, "U_RETAIN").Enabled {
				t.Fatal("custom rule re-enabled")
			}
			if r := ruleByID(t, e, "R_DB_REDIS"); r.Enabled != (mode == "default") {
				t.Fatalf("incorrect inherited state %s: %v", mode, r.Enabled)
			}
			if e.Settings.Version != version+1 {
				t.Fatal("upgrade version", e.Settings.Version, version)
			}
			if ruleByID(t, e, "R_NET_PIPE").Enabled {
				t.Fatal("optional rule enabled")
			}
			rules, _ := e.Rules()
			snapshot, _ := json.Marshal(rules)
			e.Close()
			e, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			rules, _ = e.Rules()
			again, _ := json.Marshal(rules)
			if string(snapshot) != string(again) || e.Settings.Version != version+1 {
				t.Fatal("restart modified catalog")
			}
			audits, _ := e.Audits()
			count := 0
			for _, a := range audits {
				if a.Action == "rule.catalog_upgrade" {
					count++
				}
			}
			if count != 1 {
				t.Fatal("upgrade audit count", count)
			}
		})
	}
}

func TestCatalogContainsAllDocumentedRules(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range BuiltinRules() {
		if seen[r.ID] {
			t.Fatal("duplicate", r.ID)
		}
		seen[r.ID] = true
		if strings.HasPrefix(r.ID, "R_") && r.Semantics == nil {
			t.Fatal("missing metadata", r.ID)
		}
	}
	if len(seen) != 26 {
		t.Fatal("unexpected catalog size", len(seen))
	}
}
