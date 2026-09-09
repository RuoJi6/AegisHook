package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// No listeners or real client configuration: all installations are temporary projects.
func TestInstallationObservationSurvivesSessionLifecycle(t *testing.T) {
	for _, agent := range []string{"pi", "claude", "codex", "opencode", "grok"} {
		t.Run(agent, func(t *testing.T) {
			dir, project := t.TempDir(), t.TempDir()
			e, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { e.Close() }()
			if err = e.PrepareAdapter([]byte("fixture"), "http://127.0.0.1:18790", "fixture"); err != nil {
				t.Fatal(err)
			}
			if err = e.PrepareIntegrations([]byte("export default ()=>({})"), filepath.Join(dir, "fixture-binary")); err != nil {
				t.Fatal(err)
			}
			installation, err := e.InstallAgent(agent, "project", project, filepath.Join(dir, "pi"))
			if err != nil {
				t.Fatal(err)
			}
			check := func(config string, observed bool) {
				t.Helper()
				entries, err := e.Installations()
				if err != nil {
					t.Fatal(err)
				}
				for _, i := range entries {
					if i.ID == installation.ID {
						if i.ConfigStatus != config || i.Observed != observed {
							t.Fatalf("configuration/verification coupled: %+v", i)
						}
						if agent != "pi" && observed && config == "configured" && i.Status != "observed" {
							t.Fatalf("legacy event status regressed: %s", i.Status)
						}
						return
					}
				}
				t.Fatal("installation missing")
			}
			check("configured", false)
			mode := "events"
			if agent == "pi" {
				mode = ""
			}
			session := Instance{ID: "session", Agent: agent, SessionID: "fixture-session", Cwd: installation.Project, HookVersion: Version, ConnectionMode: mode}
			// A caller cannot assert historical evidence without a current binding.
			session.ObservedInstallations = []string{installation.ID}
			if err = e.Register(session); err != nil {
				t.Fatal(err)
			}
			check("configured", false)
			// Legacy records have current bindings only; no migration should be required.
			session.Installations = []string{installation.ID}
			session.ObservedInstallations = nil
			session.State = "idle"
			session.Heartbeat = time.Now()
			if err = e.put("instances", session.ID, session); err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			session.Heartbeat = time.Now().Add(-time.Minute)
			if err = e.put("instances", session.ID, session); err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			if err = e.Disconnect(session.ID); err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			if err = e.Register(session); err != nil {
				t.Fatal(err)
			}
			if err = e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			var restarted Instance
			if err = e.get("instances", session.ID, &restarted); err != nil {
				t.Fatal(err)
			}
			if restarted.State != "disconnected" || restarted.DisconnectReason != "service_restart" {
				t.Fatal("lost independent session state")
			}
			// Re-registration after bindings disappear must retain historical evidence.
			session.Installations = nil
			if err = e.Register(session); err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			if err = e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			check("configured", true)
			// An invalid or removed entry must not be disguised by successful history.
			if agent != "pi" {
				original, err := os.ReadFile(installation.Entry)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(installation.Entry, []byte("broken fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				check("entry_error", true)
				if err = os.WriteFile(installation.Entry, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = e.Uninstall(installation.ID); err != nil {
				t.Fatal(err)
			}
			check("uninstalled", true)
		})
	}
}

func TestInstallationObservationDoesNotCrossAgents(t *testing.T) {
	e := engine(t)
	project := t.TempDir()
	if err := e.PrepareAdapter([]byte("fixture"), "http://127.0.0.1:18790", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := e.PrepareIntegrations([]byte("export default ()=>({})"), filepath.Join(e.Dir, "fixture-binary")); err != nil {
		t.Fatal(err)
	}
	installation, err := e.InstallAgent("codex", "project", project, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Register(Instance{ID: "other-agent", Agent: "claude", SessionID: "s", Cwd: project, HookVersion: Version, ConnectionMode: "events", Installations: []string{installation.ID}}); err != nil {
		t.Fatal(err)
	}
	entries, err := e.Installations()
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range entries {
		if i.ID == installation.ID && (i.Observed || i.ConfigStatus != "configured") {
			t.Fatal("cross-agent evidence accepted")
		}
	}
}
