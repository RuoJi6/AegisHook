package core

import (
	"path/filepath"
	"testing"
)

func TestSessionDisconnectReason(t *testing.T) {
	for _, agent := range []string{"pi", "claude", "codex", "opencode", "grok"} {
		t.Run(agent, func(t *testing.T) {
			dir := t.TempDir()
			e, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			active := Instance{ID: "active", Agent: agent, SessionID: "same-session", Cwd: filepath.Clean(dir), HookVersion: Version, ConnectionMode: "events"}
			ended := active
			ended.ID = "ended"
			for _, i := range []Instance{active, ended} {
				if err := e.Register(i); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.Disconnect(ended.ID); err != nil {
				t.Fatal(err)
			}
			e.Close()
			e, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			for _, id := range []string{"active", "ended"} {
				var i Instance
				if err := e.get("instances", id, &i); err != nil {
					t.Fatal(err)
				}
				want := "service_restart"
				if id == "ended" {
					want = "session_end"
				}
				if i.Agent != agent || i.State != "disconnected" || i.DisconnectReason != want || i.Online {
					t.Fatalf("unexpected state for %s: %+v", id, i)
				}
			}
			if err := e.Register(active); err != nil {
				t.Fatal(err)
			}
			var resumed Instance
			if err := e.get("instances", active.ID, &resumed); err != nil {
				t.Fatal(err)
			}
			if resumed.DisconnectReason != "" || resumed.State != "idle" || resumed.Agent != agent {
				t.Fatalf("resumed state: %+v", resumed)
			}
		})
	}
}
