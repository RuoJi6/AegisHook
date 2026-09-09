import type {
  ExtensionAPI,
  ExtensionContext,
} from "@earendil-works/pi-coding-agent";
import { readFileSync, realpathSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { randomUUID } from "node:crypto";
import { isIPv4, isIPv6 } from "node:net";

type Config = { endpoint: string; token: string };
type Review = {
  id: string;
  decision: "pending" | "approve" | "reject";
  comment: string;
  deadline: string;
};
export const HOOK_VERSION = "0.1.0";

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
  function contextOf(ctx: ExtensionContext) {
    let userMessage = "";
    const recent: string[] = [];
    for (const entry of ctx.sessionManager.getBranch()) {
      if (entry.type !== "message") continue;
      const m = entry.message as any;
      const content =
        typeof m.content === "string"
          ? m.content
          : Array.isArray(m.content)
            ? m.content
                .filter((b: any) => b.type === "text")
                .map((b: any) => b.text)
                .join("\n")
            : "";
      if (m.role === "user") userMessage = content;
      if (content) recent.push(`${m.role}: ${content.slice(0, 2000)}`);
    }
    return {
      userMessage: userMessage.slice(0, 6000),
      context: recent.slice(-6).join("\n").slice(0, 12000),
    };
  }
  async function register(ctx: ExtensionContext) {
    const ownEpoch = epoch;
    await request("/instances", {
      id: instanceId,
      sessionId: ctx.sessionManager.getSessionId(),
      cwd: ctx.cwd,
      piVersion: "0.85.1",
      hookVersion: HOOK_VERSION,
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
        !isLoopbackHost(u.hostname) ||
        u.protocol !== "http:" ||
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
            ...contextOf(ctx),
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
