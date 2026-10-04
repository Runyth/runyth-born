import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { once } from "node:events";
import { execFile } from "node:child_process";
import { tmpdir } from "node:os";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const dist = path.resolve(process.argv[2] || "dist");
const projectRoot = fileURLToPath(new URL("../../", import.meta.url));
const configuredBase = (await fs.readFile(path.join(dist, "404.html"), "utf8")).match(/<base href="([^"]*)">/u)?.[1];
assert.ok(configuredBase?.startsWith("/") && configuredBase.endsWith("/"), "Build must configure the 404 base path");
const liveURL = process.env.LIVE_SITE_URL ? new URL(process.env.LIVE_SITE_URL) : null;
if (liveURL) {
  assert.ok(["http:", "https:"].includes(liveURL.protocol));
  assert.equal(liveURL.pathname, configuredBase, "Live URL must match the built artifact's base path");
  assert.equal(liveURL.search + liveURL.hash, "");
}
const temporaryDist = liveURL ? null : await fs.mkdtemp(path.join(tmpdir(), "template-wizard-browser-"));
const extraBase = configuredBase === "/" ? "/template-wizard/" : "/";
const fixtures = (liveURL ? [[configuredBase, dist]] : [[configuredBase, dist], [extraBase, temporaryDist]])
  .sort((a, b) => b[0].length - a[0].length);
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json" };
const server = http.createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
    const [base, directory] = fixtures.find(([mount]) => pathname.startsWith(mount));
    const relative = "/" + pathname.slice(base.length);
    const filename = path.resolve(directory, "." + relative + (relative.endsWith("/") ? "index.html" : ""));
    if (!filename.startsWith(directory + path.sep)) { throw new Error("Invalid path"); }
    try {
      const data = await fs.readFile(filename);
      response.writeHead(200, { "Content-Type": mime[path.extname(filename)] || "text/plain" });
      response.end(data);
    } catch {
      const html = await fs.readFile(path.join(directory, "404.html"));
      response.writeHead(404, { "Content-Type": "text/html" });
      response.end(html);
    }
  } catch {
    response.writeHead(400);
    response.end();
  }
});
let origin = liveURL?.origin;
let browser;
let activePage;
// Bound action waits so a regression fails instead of leaving CI hanging.

async function loaded(page) {
  await page.waitForFunction(() => document.getElementById("question-card")?.hidden === false
    || document.getElementById("fatal-error")?.hidden === false);
  assert.equal(await page.locator("#fatal-error").isVisible(), false, await page.locator("#fatal-error").textContent());
}

try {
  // Build each deployment path independently. Serve all generated files,
  // including 404.html, unchanged so the harness cannot repair a broken build.
  if (!liveURL) {
    await promisify(execFile)("bash", [path.join(projectRoot, "scripts/build-static.sh"), temporaryDist], {
      cwd: projectRoot, env: { ...process.env, SITE_BASE_PATH: extraBase }, timeout: 120000,
    });
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    origin = `http://127.0.0.1:${server.address().port}`;
  }
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  for (const [base, directory] of fixtures) {
    const errors = [];
    const requests = [];
    const context = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
    context.on("page", (page) => {
      page.on("pageerror", (error) => errors.push(error.message));
      page.on("console", (message) => { if (message.type() === "error") { errors.push(message.text()); } });
    });
    context.on("request", (request) => requests.push({ url: request.url(), method: request.method() }));
    const page = await context.newPage();
    activePage = page;
    page.setDefaultTimeout(15000);
    console.log(`Checking ${base}`);
    await page.goto(origin + base);
    await page.locator(".start-form button").first().waitFor();
    await page.locator(".start-form input").first().fill("Akito");
    await page.locator(".start-form button").first().click();
    await loaded(page);
    assert.ok(page.url().startsWith(origin + base + "wizard.html?session="));
    const answer = "Name _literal_ `code`\n# plain text <script>";
    await page.locator("#answer").fill(answer);
    await page.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
    await page.reload();
    await loaded(page);
    assert.equal(await page.locator("#answer").inputValue(), answer);
    await page.locator("#next-question").tap();
    await page.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 2 "));
    await page.locator("#open-toc").tap();
    await page.locator(".section-button").last().tap();
    await page.waitForFunction(() => !document.getElementById("toc").classList.contains("open"));
    // Return to the first section via touch, then clear an answer and reload.
    await page.locator("#open-toc").tap();
    await page.locator(".section-button").first().tap();
    await page.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 1 "));
    await page.locator("#answer").fill("");
    await page.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
    await page.reload();
    await loaded(page);
    assert.equal(await page.locator("#answer").inputValue(), "");
    await page.locator("#answer").fill(answer);
    await page.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
    const downloadEvent = page.waitForEvent("download");
    await page.locator("#finish-download").click();
    const download = await downloadEvent;
    const markdown = await fs.readFile(await download.path(), "utf8");
    assert.ok(markdown.includes("Name \\_literal\\_ \\`code\\`"));
    assert.ok(markdown.includes("\\# plain text \\<script\\>"));
    await page.waitForFunction(() => document.getElementById("finish-resume-link").value.startsWith("http"));
    const portable = await page.locator("#finish-resume-link").inputValue();
    assert.equal(new URL(portable).search, "");
    assert.ok(new URL(portable).hash.startsWith("#resume="));
    const fresh = await browser.newContext();
    const resumed = await fresh.newPage();
    await resumed.goto(portable);
    await loaded(resumed);
    assert.equal(await resumed.locator("#answer").inputValue(), answer);
    assert.equal(new URL(resumed.url()).hash, "");
    await resumed.reload();
    await loaded(resumed);
    assert.equal(await resumed.locator("#answer").inputValue(), answer);
    await fresh.close();
    assert.equal(errors.length, 0, errors.join("\n"));
    assert.ok(requests.every((request) => request.method === "GET" && request.url.startsWith(origin + base)));
    assert.ok(requests.every((request) => !/\.wasm|\/api\//u.test(request.url)));
    await page.goto(origin + base + "wizard.html?session=missing");
    await page.locator("#fatal-error").waitFor();
    assert.equal(await page.locator("#finish-download").isDisabled(), true);
    assert.equal(await page.locator("#copy-link").isDisabled(), true);
    const missing = await page.goto(origin + base + "nested/missing/page");
    assert.equal(missing.status(), 404);
    assert.equal(await missing.text(), await fs.readFile(path.join(directory, "404.html"), "utf8"));
    assert.equal(await page.locator("a").getAttribute("href"), "./index.html");
    assert.equal(await page.locator("a").evaluate((link) => link.href), origin + base + "index.html");
    await page.locator("a").click();
    await page.locator(".start-form button").first().waitFor();
    await context.close();
    console.log(`Browser acceptance passed: ${base} (autosave, touch navigation, clearing, download, portable resume, errors, 404)`);
  }
  // Storage compatibility fallback uses the same site, without IndexedDB.
  const fallback = await browser.newContext();
  await fallback.addInitScript(() => Object.defineProperty(globalThis, "indexedDB", { value: undefined }));
  const page = await fallback.newPage();
  await page.goto(origin + fixtures[0][0]);
  await page.locator(".start-form button").first().click();
  await loaded(page);
  await page.locator("#answer").fill("Fallback saved");
  await page.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
  await page.reload();
  await loaded(page);
  assert.equal(await page.locator("#answer").inputValue(), "Fallback saved");
  await fallback.close();
  console.log("Browser acceptance passed: localStorage fallback");
} catch (error) {
  if (activePage && !activePage.isClosed()) {
    console.error(await activePage.evaluate(() => ({ question: document.getElementById("question-number")?.textContent,
      status: document.getElementById("save-status")?.textContent, fatal: document.getElementById("fatal-error")?.textContent,
      contents: document.getElementById("toc")?.className })));
  }
  throw error;
} finally {
  await browser?.close();
  if (server.listening) { server.close(); }
  if (temporaryDist) { await fs.rm(temporaryDist, { recursive: true, force: true }); }
}
