import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { once } from "node:events";
import { chromium } from "playwright";

// Native regression for copied writer identities (F02). A parent without COOP
// opens same-origin children whose responses carry COOP: same-origin. Those
// children inherit sessionStorage and then execute independently. Each live
// document must keep its own fallback row, so every acknowledged answer survives.
const dist = path.resolve(process.argv[2] || "dist");
const configuredBase = (await fs.readFile(path.join(dist, "404.html"), "utf8")).match(/<base href="([^"]*)">/u)?.[1];
assert.ok(configuredBase?.startsWith("/") && configuredBase.endsWith("/"), "Build must configure the 404 base path");
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json" };

const server = http.createServer(async (request, response) => {
  const requestURL = new URL(request.url, "http://127.0.0.1");
  const pathname = decodeURIComponent(requestURL.pathname);
  if (!pathname.startsWith(configuredBase)) {
    response.writeHead(404);
    response.end();
    return;
  }
  const relative = pathname.slice(configuredBase.length);
  if (relative === "writer-probe.html") {
    const storageURL = new URL("assets/storage.js", `http://127.0.0.1${configuredBase}`).pathname;
    const headers = { "Content-Type": "text/html; charset=utf-8" };
    if (requestURL.searchParams.get("coop") === "child") {
      headers["Cross-Origin-Opener-Policy"] = "same-origin";
    }
    response.writeHead(200, headers);
    response.end(`<!doctype html><meta charset="utf-8"><script type="module">
      const storage = await import(${JSON.stringify(storageURL)});
      globalThis.runythProbe = storage;
      document.documentElement.dataset.ready = "yes";
    </script>`);
    return;
  }
  const filename = path.resolve(dist, relative);
  if (!filename.startsWith(dist + path.sep) && filename !== dist) {
    response.writeHead(400);
    response.end();
    return;
  }
  try {
    const data = await fs.readFile(filename);
    response.writeHead(200, { "Content-Type": mime[path.extname(filename)] || "text/plain" });
    response.end(data);
  } catch {
    response.writeHead(404);
    response.end();
  }
});

server.listen(0, "127.0.0.1");
await once(server, "listening");
const origin = `http://127.0.0.1:${server.address().port}`;
const parentURL = `${origin}${configuredBase}writer-probe.html`;
const childURL = `${parentURL}?coop=child`;
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_PATH || undefined,
  args: ["--disable-popup-blocking"],
});

try {
  const context = await browser.newContext();
  await context.addInitScript(() => {
    Object.defineProperty(globalThis, "indexedDB", { configurable: true, get() { return undefined; } });
  });
  const childHeaders = [];
  context.on("response", (response) => {
    if (response.url().includes("writer-probe.html?coop=child") && response.request().resourceType() === "document") {
      childHeaders.push(response.headers()["cross-origin-opener-policy"]);
    }
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const parentResponse = await page.goto(parentURL);
  assert.equal(parentResponse.headers()["cross-origin-opener-policy"], undefined);
  await page.waitForFunction(() => document.documentElement.dataset.ready === "yes");
  assert.equal(await page.evaluate(() => globalThis.indexedDB), undefined, "the probe must exercise the fallback store");
  await page.evaluate(() => sessionStorage.setItem("runyth-born.writer", "parent-copied-token"));
  await page.evaluate(async () => {
    await globalThis.runythProbe.saveRecovery("probe", {
      reason: "conflict",
      entries: [{ label: "Name", answer: "Parent preserved answer" }],
    });
  });

  const popups = [];
  for (let index = 0; index < 8; index += 1) {
    const wanted = `n=${index}`;
    await page.evaluate((url) => { window.open(url); }, `${childURL}&${wanted}`);
    const deadline = Date.now() + 15000;
    let popup = null;
    while (Date.now() < deadline) {
      popup = context.pages().find((candidate) => candidate.url().includes(wanted)) ?? null;
      if (popup) {
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    assert.ok(popup, `child ${index} did not open`);
    popup.setDefaultTimeout(15000);
    await popup.waitForFunction(() => document.documentElement.dataset.ready === "yes");
    assert.equal(await popup.evaluate(() => window.opener), null, "the policy mismatch must separate the opener group");
    assert.equal(await popup.evaluate(() => sessionStorage.getItem("runyth-born.writer")), "parent-copied-token");
    popups.push(popup);
  }
  assert.equal(new Set(popups).size, popups.length, "each child must be a distinct document");

  await Promise.all(popups.map((popup, index) => popup.evaluate(async (answer) => {
    await globalThis.runythProbe.saveRecovery("probe", {
      reason: "conflict",
      entries: [{ label: "Name", answer }],
    });
  }, `Child preserved answer ${index}`)));

  assert.equal(childHeaders.length, popups.length);
  assert.ok(childHeaders.every((header) => header === "same-origin"), "child documents must be served with COOP: same-origin");
  const expected = ["Parent preserved answer", ...popups.map((_, index) => `Child preserved answer ${index}`)];
  const readRecovery = () => page.evaluate(async () => {
    const record = await globalThis.runythProbe.loadRecovery("probe");
    return {
      answers: record?.entries?.map((entry) => entry.answer) ?? [],
      keys: [...Array(localStorage.length)].map((_, index) => localStorage.key(index)).filter((key) => key?.includes(".recovery.")),
    };
  });
  // COOP can place children in another process; localStorage visibility to the
  // parent can lag the child's fulfilled write. Poll until the union settles.
  let recovered = await readRecovery();
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline && (recovered.keys.length < expected.length || expected.some((answer) => !recovered.answers.includes(answer)))) {
    await new Promise((resolve) => setTimeout(resolve, 100));
    recovered = await readRecovery();
  }
  for (const answer of expected) {
    assert.ok(recovered.answers.includes(answer), `${answer} must survive the inherited-token policy transition; saw ${JSON.stringify(recovered)}`);
  }
  assert.equal(recovered.keys.length, expected.length, `each live document must keep its own fallback row; saw ${JSON.stringify(recovered.keys)}`);
  console.log(`writer isolation passed: ${expected.length} acknowledged answers, ${recovered.keys.length} rows`);
} finally {
  await browser.close();
  server.close();
}
