import { expect, type Page } from "@playwright/test";

// Browser-only fixtures: never write synthetic sessions into a user's service.
export async function checkMultiAgentDisplay(page: Page, screenshotDir?: string) {
  // Keep the browser-only fixture stream connected without touching the real API.
  await page.addInitScript({ content: `
    class FixtureEventSource extends EventTarget {
      constructor() { super(); setTimeout(() => this.onopen?.(new Event("open")), 0); }
      close() {}
    }
    Object.defineProperty(window, "EventSource", { value: FixtureEventSource });
  ` });
  const agents = ["pi", "claude", "codex", "opencode", "grok"];
  const names = ["Pi Agent", "Claude Code", "Codex", "OpenCode", "Grok Build"];
  const now = new Date().toISOString();
  const instances = agents.map((agent, n) => ({
    id: `qa-${agent}`, agent, sessionId: "shared-session-id", cwd: "/fixture/pi-prompts",
    connectionMode: agent === "pi" ? "" : "events", hookVersion: "0.1.0", installations: [],
    heartbeat: now, state: n === 1 || n === 2 ? "disconnected" : "idle", online: n === 0,
    disconnectReason: n === 1 ? "service_restart" : n === 2 ? "session_end" : "",
  }));
  const calls = instances.map((i, n) => ({
    id: `call-${i.agent}`, agent: i.agent, instanceId: i.id, sessionId: i.sessionId,
    cwd: i.cwd, callId: `tool-${n}`, toolName: "Bash", argumentsObj: { command: `echo ${i.agent}` },
    mode: "human", version: 1, prompt: "fixture", digest: `digest-${n}`, decision: "approve",
    ruleId: "HUMAN", comment: "界面验证样例", execution: "not_executed", createdAt: now, decidedAt: now,
  }));
  const pending = { ...calls[2], id: "pending-codex", decision: "pending", decidedAt: null };
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname.replace("/api/v1", "");
    if (path === "/events") {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }
    const bodies: Record<string, unknown> = {
      "/me": { name: "界面测试" }, "/instances": instances, "/calls": [...calls, pending],
      "/settings": { mode: "human", version: 1 },
    };
    await route.fulfill({ json: bodies[path] ?? [] });
  });
  const errors: string[] = [];
  page.on("pageerror", e => errors.push(e.message));
  page.on("console", m => { if (m.type() === "error") errors.push(m.text()); });
  await page.goto("/approvals");
  await expect(page).toHaveTitle(/AegisHook/);
  await expect(page.getByRole("heading", { name: /审批与拦截/ })).toBeVisible();
  const rows = page.locator(".records-table .source-cell");
  await expect(rows).toHaveCount(6);
  for (const name of names) await expect(rows.locator("strong").filter({ hasText: new RegExp(`^${name}$`) }).first()).toBeVisible();
  await expect(page.locator(".pending-table .source-cell strong")).toHaveText("Codex");
  await expect(rows.first()).toContainText("项目：pi-prompts");
  await expect(rows.locator("strong").filter({ hasText: /^pi-prompts$/ })).toHaveCount(0);
  if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/multi-agent-sources.png` });

  await page.goto("/sessions");
  await expect(page.getByRole("heading", { name: "执行会话", exact: true })).toBeVisible();
  const sessions = page.locator(".session-item");
  await expect(sessions).toHaveCount(5);
  await expect(sessions.filter({ hasText: "Claude Code" })).toContainText("待重新接入");
  await expect(sessions.filter({ hasText: "Codex" })).toContainText("已结束");
  await expect(sessions.filter({ hasText: "OpenCode" })).toContainText("事件接入");
  for (let n = 0; n < agents.length; n++) {
    const session = sessions.filter({ hasText: names[n] });
    await session.click();
    await expect(session).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator(".timeline-scope")).toContainText(names[n]!);
    const sources = page.locator(".timeline-source strong");
    await expect(sources).toHaveCount(n === 2 ? 2 : 1);
    await expect(sources.first()).toHaveText(names[n]!);
    await expect(page.locator(".timeline-item").filter({ hasNotText: names[n] })).toHaveCount(0);
  }
  await page.getByRole("button", { name: "全部会话", exact: true }).click();
  await expect(page.locator(".timeline-item")).toHaveCount(6);
  for (const name of names) await expect(page.locator(".timeline-source strong").filter({ hasText: new RegExp(`^${name}$`) }).first()).toBeVisible();
  if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/multi-agent-sessions.png` });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(sessions.filter({ hasText: "Claude Code" })).toBeVisible();
  await sessions.filter({ hasText: "Claude Code" }).click();
  await expect(page.locator(".timeline-source strong")).toHaveText("Claude Code");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/multi-agent-mobile.png`, fullPage: true });
  await expect(page.locator("vite-error-overlay")).toHaveCount(0);
  expect(errors).toEqual([]);
}
