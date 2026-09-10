package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func clientFixture(t *testing.T, agent string) ClientOptions {
	t.Helper()
	dir := t.TempDir()
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ClientOptions{Dir: filepath.Join(dir, "client"), Binary: filepath.Join(dir, "runner"), Agent: agent, Scope: "project", Project: project, Endpoint: "https://review.example", Token: strings.Repeat("a", 64), Resources: map[string][]byte{"index.ts": []byte("fixture"), "opencode.mjs": []byte("fixture")}}
}
func TestClientProjectLifecycle(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "opencode", "grok", "pi"} {
		t.Run(agent, func(t *testing.T) {
			if runtime.GOOS == "windows" && agent == "pi" {
				t.Skip("Pi requires Windows symlink privileges")
			}
			o := clientFixture(t, agent)
			first, err := ClientOperation("install", o)
			if err != nil {
				t.Fatal(err)
			}
			second, err := ClientOperation("install", o)
			if err != nil || first.Entry != second.Entry || second.Remaining != 1 {
				t.Fatalf("not idempotent: %v", err)
			}
			if agent == "claude" || agent == "codex" || agent == "grok" {
				b, _ := os.ReadFile(first.Entry)
				var doc map[string]any
				json.Unmarshal(b, &doc)
				doc["foreign"] = true
				hooks := doc["hooks"].(map[string]any)
				hooks["PreToolUse"] = append(hooks["PreToolUse"].([]any), map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo foreign"}}})
				if err = AtomicFile(first.Entry, mustJSON(doc), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := ClientOperation("uninstall", o)
			if err != nil || result.Remaining != 0 {
				t.Fatal(err)
			}
			if agent == "claude" || agent == "codex" || agent == "grok" {
				b, _ := os.ReadFile(first.Entry)
				if !bytes.Contains(b, []byte("echo foreign")) || !bytes.Contains(b, []byte("foreign")) {
					t.Fatal("foreign settings removed")
				}
			} else if _, err = os.Lstat(first.Entry); !os.IsNotExist(err) {
				t.Fatal("owned entry retained")
			}
			if _, err = ClientOperation("uninstall", o); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"client.json", "adapter/connection.json", "adapter/runtime.json", "adapter/index.ts", "adapter/opencode.mjs"} {
				if _, err = os.Stat(filepath.Join(o.Dir, name)); !os.IsNotExist(err) {
					t.Fatalf("residual %s", name)
				}
			}
		})
	}
}
func TestClientScopeSwitchAndConflict(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	o := clientFixture(t, "claude")
	project := o.Project
	o.Scope = "global"
	o.Project = ""
	global, err := ClientOperation("install", o)
	if err != nil {
		t.Fatal(err)
	}
	o.Scope = "project"
	o.Project = project
	o.Switch = true
	// A malformed target must leave the global Hook intact.
	target, _ := clientTarget(o)
	os.MkdirAll(filepath.Dir(target.Entry), 0700)
	os.WriteFile(target.Entry, []byte("malformed"), 0600)
	before, _ := os.ReadFile(global.Entry)
	if _, err = ClientOperation("install", o); err == nil {
		t.Fatal("invalid target accepted")
	}
	after, _ := os.ReadFile(global.Entry)
	if !bytes.Equal(before, after) {
		t.Fatal("previous scope changed on failed switch")
	}
	os.Remove(target.Entry)
	p, err := ClientOperation("install", o)
	if err != nil || p.Remaining != 1 {
		t.Fatal(err)
	}
	if _, err = os.Stat(global.Entry); !os.IsNotExist(err) {
		t.Fatal("global Hook retained")
	}
	o.Scope = "global"
	o.Project = ""
	if _, err = ClientOperation("install", o); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(p.Entry); !os.IsNotExist(err) {
		t.Fatal("project Hook retained")
	}
	if _, err = ClientOperation("uninstall", o); err != nil {
		t.Fatal(err)
	}
}
func TestClientProtectModifiedHook(t *testing.T) {
	o := clientFixture(t, "claude")
	r, err := ClientOperation("install", o)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(r.Entry)
	var doc map[string]any
	json.Unmarshal(b, &doc)
	doc["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["matcher"] = "Read"
	modified := mustJSON(doc)
	os.WriteFile(r.Entry, modified, 0600)
	if _, err = ClientOperation("uninstall", o); err == nil {
		t.Fatal("modified Hook removed")
	}
	after, _ := os.ReadFile(r.Entry)
	if !bytes.Equal(modified, after) {
		t.Fatal("modified Hook changed")
	}
}
func TestClientUninstallDeletedProjectAndPending(t *testing.T) {
	o := clientFixture(t, "claude")
	if _, err := ClientOperation("install", o); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(o.Project); err != nil {
		t.Fatal(err)
	}
	if r, err := ClientOperation("uninstall", o); err != nil || r.Remaining != 0 {
		t.Fatal("cannot uninstall deleted project", err)
	}
	ticket := ClientRequestTicket{ID: ID(), Secret: ID() + ID()}
	p := filepath.Join(o.Dir, "request.json")
	if err := AtomicFile(p, mustJSON(ticket), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClientOperation("uninstall", o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("pending credential retained after uninstall")
	}
}

func TestClientEnrollmentApprovalAndRevocation(t *testing.T) {
	e := engine(t)
	ticket, err := e.CreateClientRequest("device", "linux", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.PollClientRequest(ticket.ID, ticket.Secret, "192.0.2.1")
	if err != nil || result.Connection != nil || result.State != "pending" {
		t.Fatal("pending request has access")
	}
	if _, err = e.PollClientRequest(ticket.ID, strings.Repeat("0", 64), "192.0.2.1"); err == nil {
		t.Fatal("wrong secret accepted")
	}
	if err = e.DecideClientRequest(ticket.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	connections := make(chan ClientConnection, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.PollClientRequest(ticket.ID, ticket.Secret, "192.0.2.1")
			if err == nil && r.Connection != nil {
				connections <- *r.Connection
			}
		}()
	}
	wg.Wait()
	close(connections)
	if len(connections) != 1 {
		t.Fatal("credential issued more or less than once")
	}
	connection := <-connections
	if _, ok := e.AuthenticateClient(connection.Token); !ok {
		t.Fatal("credential rejected")
	}
	nodes, _ := e.ClientNodes()
	requests, _ := e.ClientRequests()
	encoded := string(mustJSON([]any{nodes, requests}))
	if strings.Contains(encoded, connection.Token) || strings.Contains(encoded, ticket.Secret) || strings.Contains(encoded, "Hash") {
		t.Fatal("credential leaked")
	}
	if err = e.RevokeClient(connection.NodeID); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.AuthenticateClient(connection.Token); ok {
		t.Fatal("revoked credential accepted")
	}
}
func TestClientRequestRejectionExpiryAndIPBlock(t *testing.T) {
	e := engine(t)
	now := time.Now()
	e.clock = func() time.Time { return now }
	ticket, _ := e.CreateClientRequest("reject", "windows", "192.0.2.1")
	e.DecideClientRequest(ticket.ID, "reject")
	r, _ := e.PollClientRequest(ticket.ID, ticket.Secret, "192.0.2.1")
	if r.State != "rejected" || r.Connection != nil {
		t.Fatal("rejection ignored")
	}
	ticket, _ = e.CreateClientRequest("blocked", "darwin", "192.0.2.2")
	if err := e.DecideClientRequest(ticket.ID, "block_ip"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateClientRequest("again", "linux", "192.0.2.2"); err == nil {
		t.Fatal("blocked IP can request")
	}
	if _, err := e.PollClientRequest(ticket.ID, ticket.Secret, "192.0.2.3"); err == nil {
		t.Fatal("blocked request moved IP")
	}
	blocks, _ := e.ClientIPBlocks()
	e.UnblockClientIP(blocks[0].ID)
	ticket, _ = e.CreateClientRequest("expires", "linux", "192.0.2.2")
	now = now.Add(11 * time.Minute)
	if err := e.DecideClientRequest(ticket.ID, "approve"); err == nil {
		t.Fatal("expired request approved")
	}
	r, _ = e.PollClientRequest(ticket.ID, ticket.Secret, "192.0.2.2")
	if r.State != "expired" {
		t.Fatal("expiry ignored")
	}
	code, _ := e.CreateEnrollment("optional")
	if _, err := e.EnrollClient(code.Code, "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EnrollClient(code.Code, "linux"); err == nil {
		t.Fatal("code reused")
	}
	for i := 0; i < 5; i++ {
		if _, err := e.CreateClientRequest("limit", "linux", "192.0.2.9"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.CreateClientRequest("limit", "linux", "192.0.2.9"); err == nil {
		t.Fatal("rate limit ignored")
	}
}
func TestClientRemotePathsAndScopeIsolation(t *testing.T) {
	for _, c := range []struct {
		platform, root, path string
		inside               bool
	}{
		{"windows", `C:\Work`, `c:\work\a.txt`, true}, {"windows", `C:\Work`, `C:\Work-other\a`, false}, {"windows", `C:\Work`, `C:\Work\..\secret`, false}, {"windows", `\\server\share`, `\\server\share\x`, true}, {"linux", "/work", "/work/../secret", false},
	} {
		if agentWithin(c.root, c.path, c.platform) != c.inside {
			t.Fatal(c)
		}
	}
	e := engine(t)
	node := ID()
	cwd := `C:\remote\project`
	if err := e.Register(Instance{ID: "remote", SessionID: "remote", NodeID: node, Platform: "windows", Cwd: cwd, HookVersion: Version}); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveScope(Scope{Name: "remote", NodeID: node, Platform: "windows", Project: cwd, Paths: []string{cwd}}); err != nil {
		t.Fatal(err)
	}
	r, err := e.Submit(ReviewInput{InstanceID: "remote", CallID: ID(), ToolName: "read", Arguments: map[string]any{"path": `C:\outside\secret.txt`}})
	if err != nil || r.Decision != "reject" {
		t.Fatal("remote scope did not reject", err)
	}
	local, err := e.Submit(input(ID(), "read", map[string]any{"path": "fixture.txt"}))
	if err != nil || local.Decision != "approve" {
		t.Fatal("remote scope affected local node", err)
	}
}
