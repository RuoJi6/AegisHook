import { test, expect, type Page } from "@playwright/test";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { createServer, type Server } from "node:http";
async function choose(page: Page, name: string, option: string) {
  const trigger = page.getByRole("combobox", { name, exact: true });
  await trigger.click();
  const menu = page.getByRole("listbox", { name, exact: true });
  await expect(menu).toBeVisible();
  const box = await menu.boundingBox();
  const view = page.viewportSize()!;
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(view.width + 1);
  expect(box!.y + box!.height).toBeLessThanOrEqual(view.height + 1);
  await menu.getByRole("option", { name: option, exact: true }).click();
  await expect(menu).toHaveCount(0);
  await expect(trigger).toContainText(option);
}
let dir: string, proc: ChildProcess, modelServer: Server;
const base = "http://127.0.0.1:18794";
test.use({ deviceScaleFactor: 2 });
// Each test owns its service and data; the offline test stops its own process.
test.beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "aegis-browser-"));
  await mkdir(join(dir, "data"));
  await mkdir(join(dir, "project"));
  await writeFile(join(dir, "data", "admin.token"), "browser-test-admin");
  await writeFile(join(dir, "data", "hook.token"), "browser-test-hook");
  proc = spawn(
    fileURLToPath(new URL("../../bin/aegishook", import.meta.url)),
    [
      "serve",
      "--data-dir",
      join(dir, "data"),
      "--agent-dir",
      join(dir, "pi"),
      "--addr",
      "127.0.0.1:18794",
    ],
    { stdio: "pipe" },
  );
  for (let n = 0; n < 100; n++) {
    try {
      if ((await fetch(base)).ok) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 30));
  }
  modelServer = createServer((req, res) => {
    let body = "";
    req.on("data", (chunk) => {
      body += chunk;
    });
    req.on("end", () => {
      const ask = body.includes("uncertain-fixture-file");
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          usage: {
            prompt_tokens: 1000,
            completion_tokens: 50,
            prompt_tokens_details: { cached_tokens: 200 },
          },
          choices: [
            {
              message: {
                content: JSON.stringify({
                  decision: ask ? "ask" : "approve",
                  comment: ask
                    ? "实际操作：删除归属不明的文件；成功后的后果：数据可能丢失，需确认是否为测试产物；命中规则：H1"
                    : "实际操作：读取文档；成功后的后果：获得文档内容；命中规则：A7",
                }),
              },
            },
          ],
        }),
      );
    });
  });
  modelServer.listen(18795, "127.0.0.1");
  await once(modelServer, "listening");
});
test.afterEach(async () => {
  if (proc?.exitCode === null) {
    proc.kill("SIGTERM");
    await once(proc, "exit");
  }
  if (modelServer?.listening) {
    modelServer.closeAllConnections();
    await new Promise<void>((resolve, reject) => {
      modelServer.close((error) => (error ? reject(error) : resolve()));
    });
  }
  await rm(dir, { recursive: true, force: true });
});
test("full local console workflow and visual states", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/approvals");
  await expect(page).toHaveTitle(/AegisHook/);
  await page.getByLabel("管理令牌", { exact: true }).fill("browser-test-admin");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await expect(
    page.getByRole("heading", { name: "审批与拦截", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Agent 接入", exact: true }).click();
  await page
    .getByRole("button", { name: "安装 Hook", exact: true })
    .first()
    .click();
  await choose(page, "安装范围", "指定项目");
  await page.getByLabel("项目绝对路径").fill(join(dir, "project"));
  await page
    .locator(".modal")
    .getByRole("button", { name: "安装 Hook", exact: true })
    .click();
  await expect(page.locator("table").first()).toContainText("等待加载");
  const hook = { Authorization: "Bearer browser-test-hook" };
  async function call(tool: string, args: any, id: string) {
    const r = await request.post(base + "/api/v1/reviews", {
      headers: hook,
      data: {
        instanceId: "browser-fixture",
        callId: id,
        toolName: tool,
        argumentsObj: args,
        userMessage: "隔离的界面测试，仅验证审查，不执行系统操作。",
      },
    });
    expect(r.ok()).toBeTruthy();
    return r.json();
  }
  const registration = await request.post(base + "/api/v1/instances", {
    headers: hook,
    data: {
      id: "browser-fixture",
      sessionId: "browser-session",
      cwd: join(dir, "project"),
      piVersion: "0.85.1",
      hookVersion: "0.1.0",
    },
  });
  expect(registration.ok()).toBeTruthy();
  const beat = setInterval(() => {
    request
      .post(base + "/api/v1/instances/browser-fixture/heartbeat", {
        headers: hook,
        data: { state: "idle" },
      })
      .catch(() => {});
  }, 10000);
  try {
    const denied = await call(
      "delete_user",
      { user: "isolated-test-user" },
      "denied",
    );
    expect(denied.decision).toBe("reject");
    await call(
      "bash",
      { command: "passwd isolated-fixture" },
      "password-denied",
    );
    const allowed = await call("read", { path: "README.md" }, "read");
    expect(allowed.decision).toBe("approve");
    await request.post(base + `/api/v1/reviews/${allowed.id}/result`, {
      headers: hook,
      data: {
        instanceId: "browser-fixture",
        state: "succeeded",
        result: "fixture output",
      },
    });
    await call(
      "bash",
      { command: "unknown-fixture-script --dry-run" },
      "manual",
    );
    await page.getByRole("link", { name: /^审批与拦截/ }).click();
    await expect(page.getByRole("button", { name: "允许本次" })).toBeVisible();
    const output =
      process.env.AEGIS_SCREENSHOTS || join(tmpdir(), "aegishook-restyle");
    await mkdir(output, { recursive: true });
    await expect(page.locator(".toast")).toHaveCount(0);
    await page.setViewportSize({ width: 1664, height: 785 });
    await page.screenshot({
      path: join(output, "approvals-reference-native.png"),
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page
      .locator("tr")
      .filter({ hasText: "修改账号或权限" })
      .getByRole("button", { name: "查看详情" })
      .click();
    await expect(page.locator(".call-detail")).toContainText("未执行");
    await expect(page.locator(".toast")).toHaveCount(0);
    await page.screenshot({
      path: join(output, "light-1440.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.screenshot({
      path: join(output, "light-1920.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1672, height: 941 });
    await page.screenshot({
      path: join(output, "light-reference-size.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "切换深色主题" }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator(".brand")).toHaveCSS(
      "color",
      "rgb(237, 237, 241)",
    );
    await page.screenshot({
      path: join(output, "dark-reference-size.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.getByRole("button", { name: "切换浅色主题" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole("button", { name: "允许本次" })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: join(output, "mobile-390.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("button", { name: "允许本次" }).click();
    await expect(page.getByRole("button", { name: "允许本次" })).toHaveCount(0);
    await call("bash", { command: "second-unknown-script" }, "manual-reject");
    await expect(
      page.getByRole("button", { name: "拒绝", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "拒绝", exact: true }).click();
    await page.getByLabel("拒绝原因").fill("测试仅允许只读检查，请调整方案。");
    await page.getByRole("button", { name: "确认拒绝" }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await choose(page, "筛选裁决", "已拒绝");
    await choose(page, "筛选裁决", "全部记录");
    await page.getByLabel("搜索操作或规则").fill("manual-reject");
    await expect(page.locator(".records tbody>tr")).toHaveCount(1);
    await page.getByLabel("搜索操作或规则").fill("");
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "导出记录" }).click();
    expect((await download).suggestedFilename()).toBe("aegishook-calls.json");
    await page.getByRole("link", { name: "策略规则", exact: true }).click();
    await page.getByRole("button", { name: "新建规则" }).click();
    await page.getByLabel("名称", { exact: true }).fill("测试只读探测");
    await page.getByLabel("工具名", { exact: true }).fill("bash");
    await page.getByLabel("参数路径", { exact: true }).fill("command");
    await page.getByLabel("匹配表达式（Go 正则）").fill("^whoami$");
    await choose(page, "裁决", "允许");
    await page.getByRole("button", { name: "保存规则" }).click();
    await expect(page.getByText("测试只读探测", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "运行试判" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("approve");
    // Built-in edits must affect the actual reviewer, not only the table.
    const builtinRow = () =>
      page
        .locator("tr")
        .filter({ has: page.locator(".mono", { hasText: /^R1$/ }) });
    await builtinRow()
      .getByRole("button", { name: "查看语义", exact: true })
      .click();
    await expect(page.locator(".rule-semantics")).toContainText(
      "passwd、chpasswd",
    );
    await expect(page.locator(".rule-semantics")).toContainText(
      "不会判断账号是否属于测试账号",
    );
    await page.screenshot({
      path: join(output, "rule-semantics-expanded.png"),
      fullPage: true,
    });
    await builtinRow()
      .getByRole("button", { name: "收起语义", exact: true })
      .click();
    await builtinRow()
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "编辑内置规则" }),
    ).toBeVisible();
    await expect(page.getByLabel("规则编号", { exact: true })).toHaveAttribute(
      "readonly",
      "",
    );
    await expect(page.locator(".modal .rule-semantics")).toContainText(
      "passwd、chpasswd",
    );
    await page.getByRole("combobox", { name: "匹配方式", exact: true }).click();
    await expect(page.getByRole("listbox")).toBeVisible();
    await page.screenshot({
      path: join(output, "dropdown-dialog-light.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page
      .getByRole("combobox", { name: "匹配方式", exact: true })
      .press("Escape");
    await expect(page.getByRole("listbox")).toHaveCount(0);
    await page.getByLabel("名称", { exact: true }).fill("密码操作测试规则");
    await choose(page, "裁决", "允许");
    await page.getByLabel("优先级", { exact: true }).fill("25");
    await page.getByLabel("裁决补充说明").fill("隔离环境测试许可");
    await page.screenshot({
      path: join(output, "builtin-edit-desktop.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page
      .getByRole("button", { name: "保存规则", exact: true })
      .scrollIntoViewIfNeeded();
    await expect(
      page.getByRole("button", { name: "保存规则", exact: true }),
    ).toBeInViewport();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: join(output, "builtin-edit-mobile.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("button", { name: "保存规则", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.reload();
    await expect(builtinRow()).toContainText("密码操作测试规则");
    await expect(builtinRow()).toContainText("直接允许");
    await page
      .getByLabel("参数 JSON")
      .fill(JSON.stringify({ command: "passwd isolated-fixture" }));
    await page.getByRole("button", { name: "运行试判" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("approve");
    await expect(page.locator(".form-panel .notice")).toContainText(
      "隔离环境测试许可",
    );
    await builtinRow()
      .getByRole("button", { name: "已启用", exact: true })
      .click();
    await expect(
      builtinRow().getByRole("button", { name: "未启用" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "运行试判" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("pending");
    await builtinRow()
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    await page.getByRole("button", { name: "恢复默认", exact: true }).click();
    await expect(page.getByLabel("名称", { exact: true })).toHaveValue(
      "禁止修改密码及登录状态",
    );
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await expect(builtinRow()).toContainText("密码操作测试规则");
    await builtinRow()
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    await choose(page, "匹配方式", "包含文本");
    await choose(page, "匹配对象", "工具名称");
    await page.getByLabel("匹配文本（区分大小写）").fill("fixture");
    await choose(page, "裁决", "拒绝");
    await page.getByLabel("启用规则", { exact: true }).check();
    await page.getByRole("button", { name: "保存规则", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.getByLabel("工具名称", { exact: true }).fill("fixture_tool");
    await page.getByRole("button", { name: "运行试判" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("reject");
    await builtinRow()
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    await page.getByRole("button", { name: "恢复默认", exact: true }).click();
    await expect(
      page.getByRole("combobox", { name: "匹配方式", exact: true }),
    ).toContainText("内置语义识别");
    await page.getByRole("button", { name: "保存规则", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.getByLabel("工具名称", { exact: true }).fill("bash");
    await page.getByRole("button", { name: "运行试判" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("reject");
    await expect(page.locator(".form-panel .notice")).toContainText("R1");
    await page.screenshot({
      path: join(output, "builtin-rules-restored.png"),
      fullPage: true,
    });
    await page.getByRole("link", { name: "授权范围", exact: true }).click();
    await page
      .getByRole("button", { name: "添加范围", exact: true })
      .first()
      .click();
    await page.getByLabel("名称", { exact: true }).fill("隔离测试项目");
    await page.getByLabel("项目绝对路径").fill(join(dir, "project"));
    await page
      .getByLabel("允许目标（每行一个精确主机名或 IP）")
      .fill("staging.example.test");
    await page.getByRole("button", { name: "保存范围" }).click();
    await expect(page.getByText("隔离测试项目", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "系统设置", exact: true }).click();
    await choose(page, "API 协议", "Anthropic（Messages）");
    await choose(page, "API 协议", "OpenAI 兼容（Chat Completions）");
    const protocol = page.getByRole("combobox", {
      name: "API 协议",
      exact: true,
    });
    await protocol.press("ArrowDown");
    await protocol.press("End");
    await protocol.press("Enter");
    await expect(protocol).toContainText("Anthropic");
    await protocol.click();
    await page.getByRole("heading", { name: "审查模型", exact: true }).click();
    await expect(page.getByRole("listbox")).toHaveCount(0);
    await choose(page, "API 协议", "OpenAI 兼容（Chat Completions）");
    await page.getByRole("button", { name: "切换深色主题" }).click();
    await protocol.click();
    await expect(page.getByRole("listbox")).toHaveCSS(
      "background-color",
      "rgb(17, 17, 18)",
    );
    await page.screenshot({
      path: join(output, "dropdown-settings-dark.png"),
      fullPage: false,
      animations: "disabled",
    });
    await protocol.press("Escape");
    await page.getByRole("button", { name: "切换浅色主题" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await protocol.scrollIntoViewIfNeeded();
    await choose(page, "API 协议", "Anthropic（Messages）");
    await protocol.click();
    await page.screenshot({
      path: join(output, "dropdown-mobile.png"),
      fullPage: false,
      animations: "disabled",
    });
    await protocol.press("Escape");
    await choose(page, "API 协议", "OpenAI 兼容（Chat Completions）");
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByLabel("Base URL").fill("http://127.0.0.1:18795/v1");
    await page.getByLabel("模型名称").fill("fixture-reviewer");
    await page
      .getByRole("switch", { name: "启用费用估算", exact: true })
      .check();
    await page.getByLabel("输入单价", { exact: true }).fill("2");
    await page.getByLabel("输出单价", { exact: true }).fill("4");
    await page.getByLabel("缓存读取单价", { exact: true }).fill("0.5");
    await page.getByRole("button", { name: "保存设置" }).click();
    await expect(
      page.getByRole("button", { name: "测试已保存的配置" }),
    ).toBeEnabled();
    await page.getByRole("button", { name: "测试已保存的配置" }).click();
    await expect(
      page
        .locator(".form-panel")
        .filter({ hasText: "审查模型" })
        .getByText("测试通过", { exact: true }),
    ).toBeVisible();
    await page.locator('input[value="model"]').check();
    await page.getByRole("button", { name: "保存设置" }).click();
    await expect(page.locator('input[value="model"]')).toBeChecked();
    await call(
      "fixture_cleanup",
      { path: "uncertain-fixture-file" },
      "model-ask",
    );
    await page.getByRole("link", { name: "审批与拦截", exact: true }).click();
    await expect(page.locator(".pending-table")).toContainText(
      "暂无待审批操作",
    );
    await expect(
      page.getByRole("button", { name: "允许本次", exact: true }),
    ).toHaveCount(0);
    await expect(page.locator(".records")).toContainText(
      "模型裁决缺少有效结论或说明",
    );
    await expect(page.locator(".records")).not.toContainText("模型转人工");
    await page.screenshot({
      path: join(output, "invalid-model-ask-rejected.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("link", { name: "系统设置", exact: true }).click();
    await page.locator('input[value="human"]').check();
    await page.getByRole("button", { name: "保存设置" }).click();
    // Paginate real, isolated review records; none of these tools are executed.
    for (let n = 0; n < 11; n++) {
      await call(
        "fixture_pending",
        { note: `page-fixture-${n}` },
        `page-pending-${n}`,
      );
      await call("read", { path: `page-fixture-${n}.md` }, `page-read-${n}`);
    }
    await page.getByRole("link", { name: "审批与拦截", exact: true }).click();
    const pendingPager = page.getByRole("navigation", {
      name: "待处理分页",
      exact: true,
    });
    await expect(pendingPager).toContainText("共 11 条");
    await expect(
      page.getByRole("button", { name: "允许本次", exact: true }),
    ).toHaveCount(10);
    await pendingPager.getByRole("button", { name: "下一页" }).click();
    await expect(
      page.getByRole("button", { name: "允许本次", exact: true }),
    ).toHaveCount(1);
    await page.getByRole("button", { name: "允许本次", exact: true }).click();
    await expect(pendingPager).toContainText("1 / 1");
    await expect(
      page.getByRole("button", { name: "允许本次", exact: true }),
    ).toHaveCount(10);
    await page.getByRole("link", { name: "工具调用", exact: true }).click();
    const callPager = page.getByRole("navigation", {
      name: "工具调用分页",
      exact: true,
    });
    await expect(page.locator(".records tbody>tr")).toHaveCount(10);
    const firstID = await page
      .locator(".records .record-id")
      .first()
      .getAttribute("title");
    await callPager.getByRole("button", { name: "下一页" }).click();
    await expect(
      page.locator(".records .record-id").first(),
    ).not.toHaveAttribute("title", firstID!);
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await expect(callPager.locator(".pagination-position")).toContainText(
      "2 /",
    );
    await page.getByLabel("搜索操作或规则").fill("page-fixture-");
    await expect(callPager).toContainText("共 22 条");
    await expect(callPager).toContainText("1 / 3");
    await choose(page, "工具调用每页条数", "20 条 / 页");
    await expect(page.locator(".records tbody>tr")).toHaveCount(20);
    await callPager.getByRole("button", { name: "下一页" }).click();
    await expect(page.locator(".records tbody>tr")).toHaveCount(2);
    await page.getByLabel("搜索操作或规则").fill("no-such-page-record");
    await expect(callPager).toContainText("共 0 条");
    await expect(
      callPager.getByRole("button", { name: "下一页" }),
    ).toBeDisabled();
    await page.getByLabel("搜索操作或规则").fill("");
    const allCalls = await (
      await page.request.get(base + "/api/v1/calls")
    ).json();
    const exported = await (
      await page.request.get(base + "/api/v1/calls/export")
    ).json();
    expect(exported.length).toBe(allCalls.length);
    await page.screenshot({
      path: join(output, "calls-pagination.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("link", { name: "Agent 接入", exact: true }).click();
    await expect(
      page.getByRole("navigation", { name: "安装范围分页" }),
    ).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "会话状态分页" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "卸载", exact: true }).click();
    await page.getByRole("button", { name: "移除入口" }).click();
    await expect(page.locator("table").first()).toContainText("等待重载");
    await request.post(base + "/api/v1/instances/browser-fixture/shutdown", {
      headers: hook,
      data: {},
    });
    await page.getByRole("link", { name: "审计日志", exact: true }).click();
    await expect(page.locator("table")).toContainText("hook.uninstall");
    const auditPager = page.getByRole("navigation", { name: "审计日志分页" });
    await expect(page.locator("tbody>tr")).toHaveCount(10);
    const auditFirst = await page.locator("tbody>tr").first().innerText();
    await auditPager.getByRole("button", { name: "下一页" }).click();
    await expect(page.locator("tbody>tr").first()).not.toHaveText(auditFirst);
    await choose(page, "审计日志每页条数", "20 条 / 页");
    await expect(page.locator("tbody>tr")).toHaveCount(20);
    await expect(auditPager.locator(".pagination-position")).toContainText(
      "1 /",
    );
    await page.getByPlaceholder("搜索操作、对象或详情").fill("hook.uninstall");
    await expect(auditPager).toContainText("共 1 条");
    await expect(
      auditPager.getByRole("button", { name: "下一页" }),
    ).toBeDisabled();
    await page.getByPlaceholder("搜索操作、对象或详情").fill("");
    await choose(page, "审计日志每页条数", "10 条 / 页");
    await page.screenshot({
      path: join(output, "audit-pagination.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "切换深色主题" }).click();
    await page.getByRole("combobox", { name: "审计日志每页条数" }).click();
    await page.screenshot({
      path: join(output, "audit-pagination-dark.png"),
      animations: "disabled",
    });
    await page
      .getByRole("combobox", { name: "审计日志每页条数" })
      .press("Escape");
    await page.getByRole("button", { name: "切换浅色主题" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await choose(page, "审计日志每页条数", "20 条 / 页");
    await page.screenshot({
      path: join(output, "audit-pagination-mobile.png"),
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("link", { name: "执行会话", exact: true }).click();
    await expect(page.locator(".session-item")).not.toContainText(
      join(dir, "project"),
    );
    const timelinePager = page.getByRole("navigation", {
      name: "执行时间线分页",
    });
    await expect(page.locator(".timeline-item")).toHaveCount(10);
    await timelinePager.getByRole("button", { name: "下一页" }).click();
    await expect(timelinePager.locator(".pagination-position")).toContainText(
      "2 /",
    );
    await page.locator(".session-item").first().click();
    await expect(timelinePager.locator(".pagination-position")).toContainText(
      "1 /",
    );
    await page.screenshot({
      path: join(output, "sessions-no-path.png"),
      fullPage: true,
      animations: "disabled",
    });
    await expect(page.getByText("执行时间线", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "总览", exact: true }).click();
    await expect(page.getByRole("heading", { name: "执行总览" })).toBeVisible();
    await expect(page.locator(".dashboard")).toContainText(
      "审查模型 Token 消耗",
    );
    const approvalChart = page.locator(".review-charts .trend-chart").first();
    const approvalBox = await page.locator(".review-charts").boundingBox(),
      usageBox = await page.locator(".usage-panel").boundingBox();
    expect(approvalBox!.y).toBeLessThan(usageBox!.y);
    const svg = approvalChart.locator("svg");
    await svg.focus();
    await svg.press("End");
    await expect(approvalChart.locator(".chart-tooltip")).toContainText(
      "14 次",
    );
    await svg.press("Home");
    await expect(approvalChart.locator(".chart-tooltip")).toContainText("0 次");
    const graphBox = await svg.boundingBox();
    await svg.hover({ position: { x: graphBox!.width - 18, y: 60 } });
    await expect(approvalChart.locator(".chart-tooltip")).toContainText(
      "14 次",
    );
    await page
      .locator(".donut-legend button")
      .filter({ hasText: "已拦截" })
      .click();
    await expect(page.locator(".donut-center")).toContainText("14");
    await expect(page.locator(".donut-center")).toContainText("50.0%");
    await expect(page.locator(".cost-number")).toHaveText("¥0.0038");
    await expect(page.locator(".usage-summary")).toContainText("2,100");
    await page.getByRole("button", { name: "30 天", exact: true }).click();
    await expect(page).toHaveURL(/days=30/);
    await page.locator(".usage-chart summary").click();
    await expect(page.locator(".usage-chart tbody tr")).toHaveCount(30);
    await choose(page, "统计模型", "openai:fixture-reviewer");
    await expect(page.locator(".cost-number")).toHaveText("¥0.0038");
    await choose(page, "统计币种", "USD 美元");
    await expect(page.locator(".cost-number")).toHaveText("—");
    await choose(page, "统计币种", "CNY 人民币");
    await page.locator(".usage-chart summary").click();
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-1920.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-light.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "切换深色主题" }).click();
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-dark.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "切换浅色主题" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-mobile.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    expect(errors).toEqual([]);
    proc.kill("SIGTERM");
    await once(proc, "exit");
    await expect(
      page.getByText("审查服务连接失败", { exact: true }),
    ).toBeVisible();
  } finally {
    clearInterval(beat);
  }
});

test("post-verification data guard defaults off and preserves prompt edits", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/settings");
  await page.getByLabel("管理令牌", { exact: true }).fill("browser-test-admin");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page).toHaveTitle(/AegisHook/);
  await expect(
    page.getByRole("heading", { name: "模型审查提示词", exact: true }),
  ).toBeVisible();
  // The unauthenticated startup probe returns 401 before login by design.
  // Check console errors for the authenticated settings flow below.
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  const toggle = page.getByRole("switch", {
    name: /限制漏洞确认后的批量取数/,
  });
  const editor = page.getByLabel("提示词内容", { exact: true });
  const template = await (
    await page.request.get("/api/v1/settings/default-prompt")
  ).json();
  const start = template.dataGuardStart as string;
  await expect(toggle).toBeEnabled();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  expect(template.prompt).not.toContain(start);
  const help = page.getByRole("button", {
    name: "限制漏洞确认后的批量取数说明",
    exact: true,
  });
  const tooltip = page.getByRole("tooltip");
  await expect(tooltip).toHaveCount(0);
  await help.hover();
  await expect(tooltip).toContainText("结合上下文中的漏洞验证证据");
  await expect(tooltip).toContainText("保存设置后生效");
  await page.mouse.move(0, 0);
  await expect(tooltip).toHaveCount(0);
  await help.focus();
  await expect(tooltip).toBeVisible();
  await help.press("Escape");
  await expect(tooltip).toHaveCount(0);
  await toggle.check();
  const original = await editor.inputValue();
  expect(original).toContain(template.dataGuardPrompt);
  expect(original.indexOf(start)).toBeLessThan(
    original.indexOf(template.dataGuardAnchor),
  );

  const customSuffix = "\n自定义要求：裁决说明保持简洁。";
  await editor.fill(original + customSuffix);
  await toggle.uncheck();
  await expect(toggle).not.toBeChecked();
  const disabled = await editor.inputValue();
  expect(disabled).not.toContain(start);
  expect(disabled).toContain(customSuffix);
  async function saveAndReload() {
    const response = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/settings") &&
        response.request().method() === "PUT",
    );
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    expect((await response).ok()).toBeTruthy();
    await page.reload();
    await expect(toggle).toBeEnabled();
  }
  const pricingToggle = page.getByRole("switch", {
    name: "启用费用估算",
    exact: true,
  });
  const originalPricing = await pricingToggle.getAttribute("aria-checked");
  await pricingToggle.click();
  await saveAndReload();
  await expect(pricingToggle).toHaveAttribute(
    "aria-checked",
    originalPricing === "true" ? "false" : "true",
  );
  await page
    .getByRole("button", { name: "启用费用估算说明", exact: true })
    .hover();
  await expect(tooltip).toContainText("实际账单以服务商为准");
  await page.mouse.move(0, 0);
  await pricingToggle.click();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(disabled);
  await toggle.check();
  await expect(editor).toHaveValue(original + customSuffix);
  await toggle.uncheck();
  await toggle.check();
  expect((await editor.inputValue()).split(start)).toHaveLength(2);
  await saveAndReload();
  await expect(toggle).toBeChecked();
  await expect(editor).toHaveValue(original + customSuffix);

  // A custom prompt without the standard section gets a prepended block.
  const custom =
    "自定义审查提示词。保留这段文字、换行和输出格式。\n只输出 JSON。";
  await editor.fill(custom);
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  expect(await editor.inputValue()).toBe(
    template.dataGuardPrompt + "\n\n" + custom,
  );
  await toggle.uncheck();
  await expect(editor).toHaveValue(custom);
  await page.getByRole("button", { name: "恢复默认", exact: true }).click();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  await saveAndReload();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  await expect(page.locator("vite-error-overlay")).toHaveCount(0);
  await help.hover();
  await expect(tooltip).toBeVisible();
  await page.screenshot({
    path: join(tmpdir(), "aegishook-data-guard-desktop.png"),
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await help.click();
  await expect(tooltip).toBeVisible();
  await expect(toggle).toBeVisible();
  const tooltipBox = await tooltip.boundingBox();
  expect(tooltipBox!.x).toBeGreaterThanOrEqual(0);
  expect(tooltipBox!.x + tooltipBox!.width).toBeLessThanOrEqual(390);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: join(tmpdir(), "aegishook-data-guard-mobile.png"),
  });
  expect(errors).toEqual([]);
});

test("multiple agents retain distinct sources, lifecycle labels and timelines", async ({ page }) => {
  const { checkMultiAgentDisplay } = await import("./multi-agent-checks");
  await checkMultiAgentDisplay(page);
});
