import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawn } from "node:child_process";
import { once } from "node:events";

test("OpenCode callbacks, both scopes, parameter binding, outcomes and offline denial", async () => {
  const dir = await mkdtemp(join(tmpdir(), "aegis opencode's-"));
  const project = join(dir, "project");
  await mkdir(project);
  const base = "http://127.0.0.1:18798";
  const server = spawn(
    fileURLToPath(new URL("../bin/aegishook", import.meta.url)),
    ["serve", "--data-dir", join(dir, "data"), "--addr", "127.0.0.1:18798"],
    {
      env: {
        ...process.env,
        XDG_CONFIG_HOME: join(dir, "config"),
        OPENCODE_CONFIG_DIR: join(dir, "config", "opencode"),
      },
      stdio: "pipe",
    },
  );
  let hooks;
  try {
    let ready = false;
    for (let i = 0; i < 100; i++) {
      try {
        if ((await fetch(base)).ok) {
          ready = true;
          break;
        }
      } catch {}
      await new Promise((r) => setTimeout(r, 30));
    }
    assert.equal(ready, true);
    const token = await readFile(join(dir, "data", "admin.token"), "utf8");
    const login = await fetch(base + "/api/v1/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
    const cookie = login.headers.get("set-cookie").split(";")[0];
    async function api(path, body) {
      const r = await fetch(base + "/api/v1" + path, {
        method: body ? "POST" : "GET",
        headers: { Cookie: cookie, "Content-Type": "application/json" },
        body: body ? JSON.stringify(body) : undefined,
      });
      assert.equal(r.status, 200);
      return r.json();
    }
    const global = await api("/installations", {
      agent: "opencode",
      scope: "global",
    });
    const local = await api("/installations", {
      agent: "opencode",
      scope: "project",
      project,
    });
    assert.ok(global.entry.endsWith(".js"));
    const one = await import(pathToFileURL(global.entry));
    const two = await import(pathToFileURL(local.entry));
    assert.equal(one.default, two.default);
    const client = { app: { log: async () => {} } };
    hooks = await one.default({ directory: project, client });
    const duplicate = await two.default({ directory: project, client });
    assert.equal(hooks, duplicate);
    await hooks["chat.message"](
      { sessionID: "fixture" },
      { parts: [{ type: "text", text: "fixture password=secret-value" }] },
    );
    let executed = false;
    await assert.rejects(async () => {
      await hooks["tool.execute.before"](
        { sessionID: "fixture", callID: "deny", tool: "bash" },
        { args: { command: "passwd fixture" } },
      );
      executed = true;
    }, /R1/);
    assert.equal(executed, false);
    const input = { sessionID: "fixture", callID: "read", tool: "read" };
    const args = { filePath: "README.md" };
    await Promise.all([
      hooks["tool.execute.before"](input, { args }),
      duplicate["tool.execute.before"](input, { args }),
    ]);
    await assert.rejects(
      hooks["tool.execute.before"](input, { args: { filePath: "different" } }),
      /parameters changed/,
    );
    let calls = await api("/calls");
    assert.equal(calls.length, 2);
    assert.equal(
      calls.find((c) => c.callId === "read").execution,
      "awaiting_execution",
    );
    assert.equal(
      calls
        .find((c) => c.callId === "read")
        .userMessage.includes("secret-value"),
      false,
    );
    await hooks["tool.execute.after"](
      { ...input, args },
      { title: "fixture", output: "fixture read", metadata: {} },
    );
    const failure = { sessionID: "fixture", callID: "failed", tool: "read" };
    await hooks["tool.execute.before"](failure, { args });
    await hooks.event({
      event: {
        type: "message.part.updated",
        properties: {
          part: {
            type: "tool",
            sessionID: "fixture",
            callID: "failed",
            tool: "read",
            state: { status: "error", error: "fixture tool error" },
          },
        },
      },
    });
    calls = await api("/calls");
    assert.equal(calls.find((c) => c.callId === "read").execution, "succeeded");
    assert.equal(calls.find((c) => c.callId === "failed").execution, "failed");
    assert.equal(
      calls.find((c) => c.callId === "deny").execution,
      "not_executed",
    );
    const sessions = await api("/instances");
    assert.equal(sessions.length, 1);
    assert.equal(sessions[0].installations.length, 2);
    assert.equal(sessions[0].connectionMode, "events");
    await hooks.dispose();
    assert.equal((await api("/instances"))[0].state, "disconnected");
    const runtimePath = join(dir, "data", "adapter", "runtime.json");
    const runtimeBytes = await readFile(runtimePath);
    await rm(runtimePath);
    hooks = await one.default({ directory: project, client });
    await assert.rejects(
      hooks["tool.execute.before"](
        { sessionID: "missing-runtime", callID: "missing", tool: "read" },
        { args },
      ),
      /AegisHook/,
    );
    await writeFile(runtimePath, runtimeBytes);
    server.kill("SIGTERM");
    await once(server, "exit");
    await assert.rejects(
      hooks["tool.execute.before"](
        { sessionID: "offline", callID: "offline", tool: "read" },
        { args },
      ),
      /AegisHook/,
    );
  } finally {
    if (hooks) await hooks.dispose();
    if (server.exitCode === null) {
      server.kill("SIGTERM");
      await once(server, "exit");
    }
    await rm(dir, { recursive: true, force: true });
  }
});
