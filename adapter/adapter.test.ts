import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile, rm, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { createRequire } from "node:module";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { isLoopbackHost, validateDecision, reviewContext } from "./index";
const root = dirname(fileURLToPath(import.meta.url));
const piRequire = createRequire(
  join(root, "node_modules/@earendil-works/pi-coding-agent/package.json"),
);
const { loadExtensions } = await import(
  pathToFileURL(
    join(
      root,
      "node_modules/@earendil-works/pi-coding-agent/dist/core/extensions/loader.js",
    ),
  ).href
);

test("review context excludes denied history and assistant speculation", () => {
  const message = (value: any) => ({ type: "message", message: value });
  const call = (id: string) =>
    message({
      role: "assistant",
      content: [
        {
          type: "toolCall",
          id,
          name: "bash",
          arguments: { command: "fixture read" },
        },
      ],
    });
  const result = (id: string, text: string, isError = false) =>
    message({
      role: "toolResult",
      toolCallId: id,
      isError,
      content: [{ type: "text", text }],
    });
  const branch = [
    message({ role: "user", content: "检查测试项目" }),
    call("ok"),
    result("ok", "one fixture record"),
    message({ role: "assistant", content: "plan: download every record" }),
    call("denied"),
    result(
      "denied",
      "实际操作：读取工单；成功后的后果：批量读取；命中规则：R7",
      true,
    ),
    call("remembered"),
    result("remembered", "blocked by review"),
    call("legacy"),
    result(
      "legacy",
      "实际操作：读取工单；成功后的后果：批量读取；命中规则：R7",
    ),
    result("unpaired", "cannot tell which command ran"),
    call("failed"),
    result("failed", "request failed", true),
  ];
  const value = reviewContext(
    branch,
    new Map([["remembered", { decision: "reject" }]]),
  );
  assert.equal(value.userMessage, "检查测试项目");
  const history = JSON.parse(value.context);
  assert.equal(history.length, 1);
  assert.equal(history[0].toolCallId, "ok");
  assert.equal(history[0].result, "one fixture record");
  assert.match(history[0].argumentsPreview, /fixture read/);
  assert.equal(history[0].status, "succeeded");
  // Session reload may lose the in-memory verdict map; old rejection text is still excluded.
  assert.ok(!reviewContext(branch).context.includes("命中规则"));
});

test("custom loopback endpoints remain local", () => {
  for (const host of [
    "localhost",
    "127.0.0.1",
    "127.0.0.2",
    "[::1]",
    "[::ffff:7f00:2]",
  ]) {
    assert.equal(
      isLoopbackHost(new URL(`http://${host}:18800`).hostname),
      true,
      host,
    );
  }
  for (const host of [
    "0.0.0.0",
    "192.168.1.2",
    "[::]",
    "[::ffff:c0a8:102]",
    "localhost.evil.example",
    "127.0.0.1.evil.example",
  ]) {
    assert.equal(
      isLoopbackHost(new URL(`http://${host}:18800`).hostname),
      false,
      host,
    );
  }
});

test("invalid decisions are fail-closed", () => {
  for (const value of [
    null,
    {},
    {
      id: "r",
      decision: "allow",
      comment: "ok",
      deadline: new Date().toISOString(),
    },
    { id: "r", decision: "approve", deadline: "invalid" },
  ])
    assert.throws(() => validateDecision(value));
});

test("Pi 0.85.1 loader, real agent loop, Go review, feedback and lifecycle", async () => {
  const dir = await mkdtemp(join(tmpdir(), "aegis-adapter-test-"));
  const endpoint = "http://127.0.0.1:18792";
  const data = join(dir, "data");
  await mkdir(data);
  await writeFile(join(data, "admin.token"), "test-admin");
  await writeFile(join(data, "hook.token"), "test-hook");
  const proc = spawn(
    resolve(root, "../bin/aegishook"),
    [
      "serve",
      "--data-dir",
      data,
      "--agent-dir",
      join(dir, "pi"),
      "--addr",
      "127.0.0.1:18792",
    ],
    { stdio: "pipe" },
  );
  let ended = false;
  proc.on("exit", () => {
    ended = true;
  });
  let shutdown: (() => Promise<void>) | undefined;
  try {
    for (let n = 0; n < 100; n++) {
      try {
        if ((await fetch(endpoint)).ok) break;
      } catch {}
      if (ended) throw new Error("test service exited");
      await new Promise((r) => setTimeout(r, 30));
    }
    const login = await fetch(endpoint + "/api/v1/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: "test-admin" }),
    });
    assert.equal(login.status, 200);
    const cookie = login.headers.get("set-cookie")!.split(";")[0];
    async function admin(path: string, method = "GET", body?: unknown) {
      const r = await fetch(endpoint + "/api/v1" + path, {
        method,
        headers: { Cookie: cookie, "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      assert.ok(r.ok, await r.clone().text());
      return r.json();
    }
    const installed = await admin("/installations", "POST", {
      scope: "project",
      project: dir,
    });
    const loaded = await loadExtensions([installed.entry], dir);
    assert.deepEqual(loaded.errors, []);
    assert.equal(loaded.extensions.length, 1);
    const handlers = loaded.extensions[0].handlers;
    const visible: any[] = [];
    const statuses: string[] = [];
    const ctx: any = {
      cwd: dir,
      hasUI: true,
      ui: { setStatus: (_id: string, s: string) => statuses.push(s) },
      sessionManager: {
        getSessionId: () => "integration-session",
        getBranch: () => visible,
      },
    };
    const emit = async (name: string, event: any = {}) => {
      let result: any;
      for (const h of handlers.get(name) || [])
        result = await h({ type: name, ...event }, ctx);
      return result;
    };
    shutdown = () => emit("session_shutdown", { reason: "quit" });
    await emit("session_start", { reason: "startup" });
    assert.ok(
      statuses.some((s) => s.includes("防护已连接")),
      "adapter did not register after real Pi load",
    );
    const { Agent } = await import(
      pathToFileURL(
        join(
          dirname(
            piRequire.resolve("@earendil-works/pi-agent-core/package.json"),
          ),
          "dist/index.js",
        ),
      ).href
    );
    const ai = await import(
      pathToFileURL(
        join(
          root,
          "node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/index.js",
        ),
      ).href
    );
    const model: any = {
      id: "fixture",
      name: "fixture",
      api: "openai-completions",
      provider: "fixture",
      baseUrl: "http://127.0.0.1",
      reasoning: false,
      input: ["text"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 32000,
      maxTokens: 1000,
    };
    let turn = 0,
      deleted = 0,
      reads = 0;
    const agent = new Agent({
      initialState: {
        model,
        tools: [
          {
            name: "delete_user",
            description: "isolated test double",
            label: "delete",
            parameters: {
              type: "object",
              properties: { user: { type: "string" } },
              required: ["user"],
            },
            execute: async () => {
              deleted++;
              return { content: [{ type: "text", text: "deleted" }] };
            },
          },
          {
            name: "read",
            description: "isolated read double",
            label: "read",
            parameters: {
              type: "object",
              properties: { path: { type: "string" } },
              required: ["path"],
            },
            execute: async () => {
              reads++;
              return {
                content: [
                  { type: "text", text: "read-only verification complete" },
                ],
              };
            },
          },
        ],
      },
      streamFn: () => {
        const stream = ai.createAssistantMessageEventStream();
        const n = turn++;
        const message: any = {
          role: "assistant",
          api: model.api,
          provider: model.provider,
          model: model.id,
          timestamp: Date.now(),
          usage: {
            input: 1,
            output: 1,
            cacheRead: 0,
            cacheWrite: 0,
            totalTokens: 2,
            cost: {
              input: 0,
              output: 0,
              cacheRead: 0,
              cacheWrite: 0,
              total: 0,
            },
          },
          stopReason: n < 2 ? "toolUse" : "stop",
          content:
            n === 0
              ? [
                  {
                    type: "toolCall",
                    id: "delete-call",
                    name: "delete_user",
                    arguments: { user: "isolated-fixture" },
                  },
                ]
              : n === 1
                ? [
                    {
                      type: "toolCall",
                      id: "read-call",
                      name: "read",
                      arguments: { path: "README.md" },
                    },
                  ]
                : [
                    {
                      type: "text",
                      text: "Adjusted to read-only verification.",
                    },
                  ],
        };
        queueMicrotask(() => {
          stream.push({ type: "done", reason: message.stopReason, message });
          stream.end(message);
        });
        return stream;
      },
      beforeToolCall: async (c: any) =>
        emit("tool_call", {
          toolCallId: c.toolCall.id,
          toolName: c.toolCall.name,
          input: c.args,
        }),
    });
    agent.subscribe(async (event: any) => {
      if (event.type === "message_end")
        visible.push({ type: "message", message: event.message });
      if (event.type === "tool_execution_end")
        await emit("tool_execution_end", event);
    });
    await agent.prompt("Validate account permissions using isolated tools.");
    assert.equal(deleted, 0, "dangerous tool must never execute");
    assert.equal(
      reads,
      1,
      "agent must be able to continue with read-only operation",
    );
    const denied = agent.state.messages.find(
      (m: any) => m.role === "toolResult" && m.toolCallId === "delete-call",
    );
    assert.equal(denied?.isError, true);
    assert.match(JSON.stringify(denied), /R3/);
    const calls = await admin("/calls");
    assert.equal(
      calls.find((c: any) => c.callId === "delete-call").execution,
      "not_executed",
    );
    assert.equal(
      calls.find((c: any) => c.callId === "read-call").execution,
      "succeeded",
    );
    // Entry removal does not remove already-loaded handlers.
    await admin("/installations/" + installed.id, "DELETE");
    const still = await emit("tool_call", {
      toolCallId: "after-uninstall",
      toolName: "delete_user",
      input: { user: "isolated" },
    });
    assert.equal(still.block, true);
    const states = await admin("/installations");
    assert.equal(
      states.find((i: any) => i.id === installed.id).status,
      "waiting_reload",
    );
    // Unknown calls pause, and receive only an approval bound to their exact arguments.
    const waiting = emit("tool_call", {
      toolCallId: "manual",
      toolName: "bash",
      input: { command: "echo fixture" },
    });
    let manual: any;
    for (let n = 0; n < 100; n++) {
      manual = (await admin("/calls")).find((c: any) => c.callId === "manual");
      if (manual) break;
      await new Promise((r) => setTimeout(r, 20));
    }
    assert.equal(manual.decision, "pending");
    await admin("/approvals/" + manual.id + "/decision", "POST", {
      digest: manual.digest,
      decision: "reject",
      comment: "仅允许只读验证",
    });
    assert.equal((await waiting).block, true);
    await shutdown();
    shutdown = undefined;
    const instances = await admin("/instances");
    assert.ok(instances.every((i: any) => !i.online));
    await emit("session_start", { reason: "reload" });
    proc.kill("SIGTERM");
    await once(proc, "exit");
    const offline = await emit("tool_call", {
      toolCallId: "offline",
      toolName: "read",
      input: { path: "README.md" },
    });
    assert.equal(offline.block, true);
  } finally {
    if (shutdown) await shutdown().catch(() => {});
    if (!ended) {
      proc.kill("SIGTERM");
      await once(proc, "exit");
    }
    await rm(dir, { recursive: true, force: true });
  }
});
