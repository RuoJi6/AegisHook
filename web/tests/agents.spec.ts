import { test, expect } from "@playwright/test";
import { mkdtemp, mkdir, writeFile, rm, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
let dir: string, proc: ChildProcess;
const base = "http://127.0.0.1:18797";
test.beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), "aegis-agents-ui-"));
  await mkdir(join(dir, "data"));
  await mkdir(join(dir, "project"));
  await writeFile(join(dir, "data", "admin.token"), "fixture-admin");
  await writeFile(join(dir, "data", "hook.token"), "fixture-hook");
  proc = spawn(
    fileURLToPath(new URL("../../bin/aegishook", import.meta.url)),
    ["serve", "--data-dir", join(dir, "data"), "--addr", "127.0.0.1:18797"],
    { stdio: "pipe" },
  );
  for (let n = 0; n < 100; n++) {
    try {
      if ((await fetch(base)).ok) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 30));
  }
});
test.afterAll(async () => {
  if (proc.exitCode === null) {
    proc.kill("SIGTERM");
    await once(proc, "exit");
  }
  await rm(dir, { recursive: true, force: true });
});
test("multi-agent project installation, actual event states and responsive themes", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && !m.text().includes("401 (Unauthorized)"))
      errors.push(m.text());
  });
  await page.goto(base + "/agents");
  await expect(page).toHaveTitle(/AegisHook/);
  await page.getByLabel("管理令牌", { exact: true }).fill("fixture-admin");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await expect(
    page.getByRole("heading", { name: "Agent 接入", exact: true }),
  ).toBeVisible();
  const output =
    process.env.AEGIS_SCREENSHOTS || join(tmpdir(), "aegis-agents-ui");
  await mkdir(output, { recursive: true });
  for (const [agent, name] of [
    ["claude", "Claude Code"],
    ["codex", "Codex"],
    ["opencode", "OpenCode"],
    ["grok", "Grok Build"],
  ]) {
    const select = page.getByRole("combobox", {
      name: "选择 Agent",
      exact: true,
    });
    await select.click();
    await page.getByRole("option", { name, exact: true }).click();
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "安装 Hook", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("heading", { name: `安装 ${name} Hook`, exact: true }),
    ).toBeVisible();
    await page.getByRole("combobox", { name: "安装范围", exact: true }).click();
    await page.getByRole("option", { name: "指定项目", exact: true }).click();
    await page.getByLabel("项目绝对路径").fill(join(dir, "project"));
    await page
      .locator(".modal")
      .getByRole("button", { name: "安装 Hook", exact: true })
      .click();
    await expect(page.locator(".modal")).toHaveCount(0);
    const row = page
      .locator("table")
      .first()
      .locator("tbody>tr")
      .filter({ hasText: name });
    await expect(row).toContainText("已配置");
    await expect(row).toContainText("尚未收到事件");
    const registration = await request.post(base + "/api/v1/instances", {
      headers: { Authorization: "Bearer fixture-hook" },
      data: {
        id: agent,
        sessionId: "session-" + agent,
        agent,
        connectionMode: "events",
        cwd: join(dir, "project"),
        hookVersion: "0.1.0",
      },
    });
    expect(registration.ok()).toBeTruthy();
    await expect(row).toContainText("已配置");
    await expect(row).toContainText("已收到过事件");
    await expect(
      page
        .locator("table")
        .nth(1)
        .locator("tbody>tr")
        .filter({ hasText: name }),
    ).toContainText("事件接入");
  }
  expect(
    (await page.locator(".connection-card").boundingBox())!.height,
  ).toBeLessThan(230);
  await page.screenshot({
    path: join(output, "agents-light.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换深色主题" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.screenshot({
    path: join(output, "agents-dark.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("combobox", { name: "选择 Agent", exact: true }).click();
  const menu = page.getByRole("listbox", { name: "选择 Agent", exact: true });
  const box = await menu.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(391);
  await page.screenshot({ path: join(output, "agents-mobile-dropdown.png") });
  await page.keyboard.press("Escape");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    ),
  ).toBeTruthy();
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.getByRole("button", { name: "切换浅色主题" }).click();
  const row = page
    .locator("table")
    .first()
    .locator("tbody>tr")
    .filter({ hasText: "Claude Code" });
  const shutdown = await request.post(
    base + "/api/v1/instances/claude/shutdown",
    {
      headers: { Authorization: "Bearer fixture-hook" },
    },
  );
  expect(shutdown.ok()).toBeTruthy();
  await expect(
    page
      .locator("table")
      .nth(1)
      .locator("tbody>tr")
      .filter({ hasText: "Claude Code" }),
  ).toContainText("已结束");
  await expect(row).toContainText("已配置");
  await expect(row).toContainText("已收到过事件");
  await row.getByRole("button", { name: "卸载", exact: true }).click();
  await page.getByRole("button", { name: "移除入口" }).click();
  await expect(row).toContainText("已卸载");
  await expect(row).toContainText("已收到过事件");
  expect(
    JSON.parse(
      await readFile(join(dir, "project", ".claude", "settings.json"), "utf8"),
    ).hooks,
  ).toBeUndefined();
  await expect(
    page
      .locator("table")
      .first()
      .locator("tbody>tr")
      .filter({ hasText: "Codex" }),
  ).toContainText("已收到过事件");
  await page.goto(base + "/sessions");
  await expect(page.locator(".session-item")).toHaveCount(4);
  await expect(page.locator(".session-item").first()).toContainText("事件接入");
  expect(await page.locator(".session-item .dot.green").count()).toBe(0);
  expect(errors).toEqual([]);
});
