import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Both installation scopes import this same module. Review IDs remain stable even
// if OpenCode invokes the plugin twice; changed arguments are rejected by the server.
const states = new Map();
const root = dirname(fileURLToPath(import.meta.url));
export default async function AegisHook({ directory, client }) {
  const key = directory;
  if (states.has(key)) return states.get(key);
  const sessions = new Set(),
    messages = new Map(),
    calls = new Map(),
    children = new Set();
  let disposed = false;
  async function run(event, passive = false) {
    if (disposed)
      return Promise.reject(new Error("AegisHook: adapter disposed"));
    let runtime;
    try {
      runtime = JSON.parse(await readFile(join(root, "runtime.json"), "utf8"));
      if (typeof runtime.binary !== "string")
        throw new Error("invalid runtime");
    } catch {
      throw new Error("AegisHook: cannot read hook runtime; tool blocked");
    }
    return new Promise((resolve, reject) => {
      const child = spawn(
        runtime.binary,
        [
          "hook",
          "--agent",
          "opencode",
          "--connection",
          join(root, "connection.json"),
        ],
        { stdio: ["pipe", "pipe", "pipe"], windowsHide: true },
      );
      children.add(child);
      let stdout = "",
        stderr = "",
        overflow = false;
      const timeout = setTimeout(
        () => child.kill(),
        passive ? 10000 : 86520000,
      );
      child.stdout.on("data", (b) => {
        stdout += b;
        if (stdout.length > 2 * 1024 * 1024) {
          overflow = true;
          child.kill();
        }
      });
      child.stderr.on("data", (b) => {
        stderr = (stderr + b).slice(-16000);
      });
      child.stdin.on("error", () => {});
      child.once("error", reject);
      child.once("close", (code) => {
        clearTimeout(timeout);
        children.delete(child);
        if (overflow || code !== 0)
          return reject(
            new Error(
              stderr.trim() || "AegisHook: review failed; tool blocked",
            ),
          );
        if (passive) return resolve();
        try {
          const result = JSON.parse(stdout);
          if (result.decision !== "approve")
            throw new Error(result.comment || "invalid review decision");
          resolve();
        } catch (e) {
          reject(new Error(`AegisHook: ${e.message}`));
        }
      });
      child.stdin.end(JSON.stringify({ cwd: directory, ...event }));
    });
  }
  async function passive(event) {
    try {
      await run(event, true);
    } catch (error) {
      try {
        await client?.app?.log({
          body: {
            service: "aegishook",
            level: "error",
            message: error.message,
          },
        });
      } catch {}
    }
  }
  const payload = (input, event) => ({
    hook_event_name: event,
    session_id: input.sessionID,
    tool_use_id: input.callID,
    tool_name: input.tool,
  });
  async function report(input, event, result, state) {
    const key = `${input.sessionID}:${input.callID}`;
    const call = calls.get(key);
    if (!call?.approved || call.reported) return;
    call.reported = true;
    await passive({
      ...payload(input, event),
      tool_response: result,
      executionState: state,
      error: typeof result === "string" ? result : "",
    });
  }
  const hooks = {
    "chat.message": async (input, output) => {
      sessions.add(input.sessionID);
      messages.set(
        input.sessionID,
        output.parts
          .filter((p) => p.type === "text")
          .map((p) => p.text)
          .join("\n")
          .slice(-24000),
      );
    },
    "tool.execute.before": async (input, output) => {
      sessions.add(input.sessionID);
      const key = `${input.sessionID}:${input.callID}`,
        argumentsJSON = JSON.stringify(output.args);
      const previous = calls.get(key);
      if (previous) {
        if (previous.argumentsJSON !== argumentsJSON)
          throw new Error("AegisHook: parameters changed after review");
        return previous.promise;
      }
      const call = { argumentsJSON, approved: false, reported: false };
      call.promise = run({
        ...payload(input, "PreToolUse"),
        tool_input: output.args,
        userMessage: messages.get(input.sessionID) || "",
      }).then(() => {
        call.approved = true;
      });
      calls.set(key, call);
      return call.promise;
    },
    "tool.execute.after": async (input, output) => {
      const exit = output.metadata?.exit ?? output.metadata?.exitCode;
      await report(
        input,
        "PostToolUse",
        output,
        typeof exit === "number" && exit !== 0 ? "failed" : "succeeded",
      );
    },
    event: async ({ event }) => {
      const info = event.properties?.info;
      if (event.type === "session.created" && info?.id) {
        sessions.add(info.id);
        await passive({ hook_event_name: "SessionStart", session_id: info.id });
      }
      if (event.type === "session.deleted" && info?.id) {
        await passive({ hook_event_name: "SessionEnd", session_id: info.id });
        sessions.delete(info.id);
        messages.delete(info.id);
        for (const key of calls.keys())
          if (key.startsWith(`${info.id}:`)) calls.delete(key);
      }
      const part = event.properties?.part;
      if (
        event.type === "message.part.updated" &&
        part?.type === "tool" &&
        part.state?.status === "error"
      ) {
        await report(
          { sessionID: part.sessionID, callID: part.callID, tool: part.tool },
          "PostToolUseFailure",
          part.state.error,
          "failed",
        );
      }
    },
    dispose: async () => {
      if (disposed) return;
      await Promise.all(
        [...sessions].map((id) =>
          passive({ hook_event_name: "SessionEnd", session_id: id }),
        ),
      );
      disposed = true;
      for (const child of children) child.kill();
      states.delete(key);
      calls.clear();
      messages.clear();
      sessions.clear();
    },
  };
  states.set(key, hooks);
  return hooks;
}
