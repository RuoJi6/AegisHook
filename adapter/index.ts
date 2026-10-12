import type {
  ExtensionAPI,
  ExtensionContext,
} from "@earendil-works/pi-coding-agent";
import { readFileSync, realpathSync, statSync } from "node:fs";
import { basename, dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash, randomUUID } from "node:crypto";
import { isIPv4, isIPv6 } from "node:net";
import { platform } from "node:os";

type Config = { endpoint: string; token: string; nodeId?: string };
type Review = {
  id: string;
  decision: "pending" | "approve" | "reject";
  comment: string;
  deadline: string;
};
export const HOOK_VERSION = "0.1.0";

const MAX_SCRIPT_BYTES = 1 << 20;
const MAX_SCRIPT_FILES = 8;
const SCRIPT_INTERPRETER = /^(python[0-9.]*|bash|sh|zsh|fish|node|nodejs|ruby|perl|php|pwsh|powershell)$/;
const SCRIPT_EXTENSIONS = new Set([
  ".sh", ".bash", ".zsh", ".fish", ".py", ".py3", ".js", ".mjs",
  ".cjs", ".ts", ".tsx", ".rb", ".pl", ".php", ".ps1", ".lua",
]);

export type ReviewScript = {
  path: string;
  sha256?: string;
  bytes?: number;
  content?: string;
  complete: boolean;
  truncated?: boolean;
  readError?: string;
};

function literalToken(token: string): string | undefined {
  if (!token || /[$`*?\[\]~]/.test(token)) return undefined;
  if ((token.startsWith("\"") && token.endsWith("\"")) || (token.startsWith("'") && token.endsWith("'"))) return token.slice(1, -1);
  return token;
}

function commandTokens(command: string): string[] {
  const out: string[] = [];
  const re = /"(?:\\.|[^"\\])*"|'[^']*'|[^\s]+/g;
  for (const match of command.matchAll(re)) out.push(match[0]);
  return out;
}

function captureReferences(command: string, workingDir: string): Array<{ path?: string; problem?: string; direct?: boolean }> {
  if (/[$`]|\$\(|\$\{|[*?\[\]]/.test(command)) return [{ problem: "执行脚本路径包含动态展开，无法确定实际文件" }];
  const tokens = commandTokens(command);
  const refs: Array<{ path?: string; problem?: string; direct?: boolean }> = [];
  let dir = workingDir;
  const add = (raw: string | undefined, direct = false) => {
    const value = literalToken(raw || "");
    if (!value) {
      refs.push({ problem: "执行脚本路径包含动态展开，无法确定实际文件" });
      return;
    }
    refs.push({ path: resolve(dir, value), direct });
  };
  for (let i = 0; i < tokens.length; i++) {
    const raw = literalToken(tokens[i]);
    if (!raw) {
      refs.push({ problem: "动态命令入口，无法确定是否执行脚本" });
      continue;
    }
    const base = basename(raw);
    if (base === "cd") {
      const next = literalToken(tokens[++i] || "");
      dir = next && next !== "-" ? resolve(dir, next) : "";
      continue;
    }
    if (base === "source" || base === ".") {
      add(tokens[++i]);
      continue;
    }
    if (["env", "sudo", "exec", "nohup", "timeout"].includes(base)) continue;
    if (SCRIPT_INTERPRETER.test(base)) {
      for (i++; i < tokens.length; i++) {
        const value = literalToken(tokens[i]);
        if (!value) {
          refs.push({ problem: "解释器参数包含动态展开，无法确定实际脚本" });
          break;
        }
        if (["-c", "-e", "--eval", "--print"].includes(value)) {
          if (value === "-c" && tokens[i + 1]) {
            const inline = literalToken(tokens[++i]);
            if (inline) refs.push(...captureReferences(inline, dir));
            else refs.push({ problem: "Shell 内联执行内容不确定" });
          }
          break;
        }
        if (value === "-m" || value === "-") {
          refs.push({ problem: "解释器未提供可确定的本地脚本文件" });
          break;
        }
        if (value.startsWith("-")) continue;
        add(tokens[i]);
        break;
      }
      continue;
    }
    if (raw.includes("/") || SCRIPT_EXTENSIONS.has(extname(raw).toLowerCase())) add(tokens[i], true);
  }
  return refs;
}

export function captureScripts(workingDir: string, tool: string, args: Record<string, unknown>) {
  if (!["Bash", "bash", "shell", "shell_open", "shell_send", "command", "script", "powershell", "PowerShell", "execute_command", "run_command", "terminal", "cmd"].includes(tool)) return { scriptFiles: [] as ReviewScript[], scriptCaptureStatus: "not_applicable" };
  const key = tool === "shell_send" ? "text" : "command";
  let command = typeof args[key] === "string" ? String(args[key]) : "";
  if (!command.trim()) {
    for (const candidate of ["script", "code", "cmd"]) {
      if (typeof args[candidate] === "string") { command = String(args[candidate]); break; }
    }
  }
  if (!command.trim()) return { scriptFiles: [] as ReviewScript[], scriptCaptureStatus: "not_applicable" };
  const refs = captureReferences(command, workingDir);
  const files: ReviewScript[] = [];
  const seen = new Set<string>();
  let status = "complete";
  let total = 0;
  for (const ref of refs) {
    const key = `${ref.path || ""}:${ref.problem || ""}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const file: ReviewScript = { path: ref.path || "<无法确定的脚本>", complete: false };
    if (ref.problem) {
      file.readError = ref.problem;
      status = "incomplete";
      files.push(file);
      continue;
    }
    if (files.length >= MAX_SCRIPT_FILES) {
      file.readError = "本次调用超过 8 个脚本文件";
      status = "incomplete";
      files.push(file);
      break;
    }
    try {
      const info = statSync(ref.path!);
      if (!info.isFile()) throw new Error("不是普通文件");
      if (info.size > MAX_SCRIPT_BYTES) {
        file.bytes = info.size;
        file.truncated = true;
        file.readError = "脚本超过完整快照限制（1 MiB）";
        status = "incomplete";
        files.push(file);
        continue;
      }
      const content = readFileSync(ref.path!);
      file.bytes = content.length;
      if (ref.direct && (content.subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46])) || content.subarray(0, 2).equals(Buffer.from([0x4d, 0x5a])))) continue;
      if (tool === "shell_send") throw new Error("交互会话可能已切换目录或连接远端，无法确认本机文件就是实际执行脚本");
      const text = content.toString("utf8");
      if (!Buffer.from(text, "utf8").equals(content) || content.includes(0)) throw new Error("脚本不是有效 UTF-8 文本");
      if (content.length > MAX_SCRIPT_BYTES || total + content.length > MAX_SCRIPT_BYTES) {
        file.truncated = true;
        file.readError = "脚本超过完整快照总量限制（1 MiB），未附加部分内容";
        status = "incomplete";
        files.push(file);
        continue;
      }
      file.sha256 = createHash("sha256").update(content).digest("hex");
      file.content = text;
      file.complete = true;
      total += content.length;
      files.push(file);
    } catch (error) {
      file.readError = error instanceof Error ? error.message : "脚本读取失败";
      status = "incomplete";
      files.push(file);
    }
  }
  return files.length ? { scriptFiles: files, scriptCaptureStatus: status } : { scriptFiles: [], scriptCaptureStatus: "not_applicable" };
}

export function isLoopbackHost(host: string): boolean {
  const value = host.replace(/^\[|\]$/g, "").toLowerCase();
  return (
    value === "localhost" ||
    (isIPv4(value) && value.startsWith("127.")) ||
    (isIPv6(value) &&
      (value === "::1" || /^::ffff:7f[0-9a-f]{2}:[0-9a-f]{1,4}$/.test(value)))
  );
}

export function validateDecision(value: unknown): Review {
  const r = value as Review;
  if (
    !r ||
    typeof r.id !== "string" ||
    !r.id ||
    !["pending", "approve", "reject"].includes(r.decision) ||
    typeof r.comment !== "string" ||
    !Number.isFinite(Date.parse(r.deadline))
  )
    throw new Error("审查服务返回非法裁决");
  return r;
}

// Keep only the latest user message for local audit / human review. The server
// excludes it from model input; previous tool calls and results are not collected.
export function reviewContext(branch: readonly any[]) {
  for (let i = branch.length - 1; i >= 0; i--) {
    const entry = branch[i];
    if (entry.type !== "message" || entry.message.role !== "user") continue;
    const m = entry.message;
    const content =
      typeof m.content === "string"
        ? m.content
        : (Array.isArray(m.content) ? m.content : [])
            .filter((b: any) => b.type === "text")
            .map((b: any) => b.text)
            .join("\n");
    return { userMessage: content.slice(0, 6000) };
  }
  return { userMessage: "" };
}

export default function aegisHook(pi: ExtensionAPI) {
  // Factory can run during Pi trust discovery. No network/timers until session_start.
  let config: Config | undefined;
  let instanceId = "";
  let controller = new AbortController();
  let timer: ReturnType<typeof setInterval> | undefined;
  let status = "idle";
  let registered = false;
  let epoch = 0;
  const calls = new Map<string, { id: string; decision: string }>();
  let pending = 0;

  async function request(
    path: string,
    body?: unknown,
    signal = controller.signal,
  ): Promise<any> {
    if (!config) throw new Error("Hook 未配置");
    const response = await fetch(`${config.endpoint}/api/v1${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers: {
        Authorization: `Bearer ${config.token}`,
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.any([signal, AbortSignal.timeout(5000)]),
    });
    if (!response.ok)
      throw new Error(`审查服务不可用（HTTP ${response.status}）`);
    return response.json();
  }
  function setStatus(ctx: ExtensionContext, text: string) {
    if (ctx.hasUI) ctx.ui.setStatus("aegishook", text);
  }
  async function register(ctx: ExtensionContext) {
    const ownEpoch = epoch;
    await request("/instances", {
      id: instanceId,
      sessionId: ctx.sessionManager.getSessionId(),
      cwd: ctx.cwd,
      piVersion: "0.85.1",
      hookVersion: HOOK_VERSION,
      nodeId: config?.nodeId,
      platform: platform(),
    });
    if (ownEpoch === epoch) {
      registered = true;
      setStatus(ctx, "AegisHook · 防护已连接");
    }
  }
  pi.on("session_start", async (_event, ctx) => {
    epoch++;
    if (timer) clearInterval(timer);
    controller.abort();
    controller = new AbortController();
    instanceId = randomUUID();
    calls.clear();
    registered = false;
    pending = 0;
    try {
      config = undefined;
      const candidate: Config = JSON.parse(
        readFileSync(
          join(
            dirname(realpathSync(fileURLToPath(import.meta.url))),
            "connection.json",
          ),
          "utf8",
        ),
      );
      const u = new URL(candidate.endpoint);
      if (
        !(u.protocol === "https:" || (u.protocol === "http:" && isLoopbackHost(u.hostname))) ||
        u.username ||
        u.password ||
        u.search ||
        u.hash ||
        u.pathname !== "/" ||
        typeof candidate.token !== "string" ||
        !candidate.token
      )
        throw new Error("Hook 连接配置无效");
      config = candidate;
      await register(ctx);
    } catch {
      setStatus(ctx, "AegisHook · 审查不可用，工具将被拒绝");
    }
    let beating = false;
    timer = setInterval(async () => {
      if (beating || controller.signal.aborted) return;
      beating = true;
      try {
        if (!registered) await register(ctx);
        else
          await request(`/instances/${instanceId}/heartbeat`, {
            state: pending ? "awaiting_review" : status,
          });
      } catch {
        registered = false;
        setStatus(ctx, "AegisHook · 连接失败，工具将被拒绝");
      } finally {
        beating = false;
      }
    }, 10000);
    timer.unref?.();
  });
  pi.on("session_shutdown", async () => {
    epoch++;
    if (timer) clearInterval(timer);
    timer = undefined;
    controller.abort();
    registered = false;
    calls.clear();
    try {
      await request(
        `/instances/${instanceId}/shutdown`,
        {},
        AbortSignal.timeout(1000),
      );
    } catch {
      /* lease expires server-side */
    }
  });
  pi.on("agent_start", async () => {
    status = "running";
  });
  pi.on("agent_settled", async () => {
    status = "idle";
  });
  pi.on("tool_call", async (event, ctx) => {
    const signal = controller.signal;
    pending++;
    try {
      if (!registered) await register(ctx);
      let review = validateDecision(
        await request(
          "/reviews",
          {
            instanceId,
            callId: event.toolCallId,
            toolName: event.toolName,
            argumentsObj: event.input,
            ...captureScripts(ctx.cwd, event.toolName, event.input as Record<string, unknown>),
            ...reviewContext(ctx.sessionManager.getBranch()),
          },
          signal,
        ),
      );
      calls.set(event.toolCallId, { id: review.id, decision: review.decision });
      while (review.decision === "pending") {
        setStatus(ctx, "AegisHook · 等待审查，工具尚未执行");
        if (Date.now() > Date.parse(review.deadline) + 1000)
          throw new Error("审查等待超时");
        await new Promise<void>((resolve, reject) => {
          if (signal.aborted) {
            reject(new Error("会话已结束"));
            return;
          }
          const abort = () => {
            clearTimeout(t);
            reject(new Error("会话已结束"));
          };
          const t = setTimeout(() => {
            signal.removeEventListener("abort", abort);
            resolve();
          }, 750);
          signal.addEventListener("abort", abort, { once: true });
        });
        review = validateDecision(
          await request(
            `/reviews/${review.id}?instanceId=${encodeURIComponent(instanceId)}`,
            undefined,
            signal,
          ),
        );
      }
      calls.set(event.toolCallId, { id: review.id, decision: review.decision });
      setStatus(
        ctx,
        review.decision === "approve"
          ? "AegisHook · 已允许本次调用"
          : "AegisHook · 已拦截",
      );
      if (review.decision === "reject")
        return { block: true, reason: review.comment };
      return undefined;
    } catch (error) {
      const call = calls.get(event.toolCallId);
      if (call?.decision === "approve") {
        try {
          await request(`/reviews/${call.id}/result`, {
            instanceId,
            state: "not_executed",
            result: "适配器校验未通过",
          });
        } catch {}
      }
      setStatus(ctx, "AegisHook · 本次调用已拒绝");
      return {
        block: true,
        reason: `AegisHook 拒绝执行：${error instanceof Error ? error.message : "审查异常"}。工具未执行，请检查控制台后重新提交。`,
      };
    } finally {
      pending--;
    }
  });
  pi.on("tool_execution_end", async (event, ctx) => {
    const call = calls.get(event.toolCallId);
    if (!call) return;
    const result =
      typeof event.result === "string"
        ? event.result
        : JSON.stringify(event.result);
    let delivered = false;
    for (let n = 0; n < 3 && !controller.signal.aborted; n++) {
      try {
        await request(`/reviews/${call.id}/result`, {
          instanceId,
          state:
            call.decision === "reject"
              ? "not_executed"
              : event.isError
                ? "failed"
                : "succeeded",
          result: result?.slice(0, 12000),
        });
        delivered = true;
        break;
      } catch {}
    }
    if (!delivered) setStatus(ctx, "AegisHook · 执行结果回传失败");
    calls.delete(event.toolCallId);
  });
}
