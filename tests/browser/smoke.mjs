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
const temporaryDist = liveURL ? null : await fs.mkdtemp(path.join(tmpdir(), "runyth-born-browser-"));
const extraBase = configuredBase === "/" ? "/runyth-born/" : "/";
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

  // Stale-tab protection: a second tab navigating without edits must never
  // rewrite saved answers, and divergent edits are rejected, never stored.
  const tabs = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const tabA = await tabs.newPage();
  await tabA.goto(origin + fixtures[0][0]);
  await tabA.locator(".start-form input").first().fill("Alex");
  await tabA.locator(".start-form button").first().click();
  await loaded(tabA);
  const sharedSessionURL = tabA.url();
  await tabA.locator("#answer").fill("Original q1");
  await tabA.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");

  const tabB = await tabs.newPage();
  await tabB.goto(sharedSessionURL);
  await loaded(tabB);
  assert.equal(await tabB.locator("#answer").inputValue(), "Original q1");

  await tabA.locator("#answer").fill("New q1 entered in tab A");
  await tabA.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");

  await tabB.locator("#next-question").tap();
  await tabB.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 2 "));

  await tabA.reload();
  await loaded(tabA);
  await tabA.locator("#previous-question").tap();
  await tabA.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 1 "));
  assert.equal(await tabA.locator("#answer").inputValue(), "New q1 entered in tab A");

  await tabB.locator("#answer").fill("Stale edit from tab B");
  await tabB.waitForFunction(() => /newer version/u.test(document.getElementById("save-status").textContent));
  await tabs.close();
  console.log("Browser acceptance passed: stale tabs");

  // Clock independence: revision, not wall-clock order, resolves divergent
  // portable copies in both directions.
  const clocks = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const clockPage = await clocks.newPage();
  await clockPage.goto(origin + fixtures[0][0]);
  await clockPage.locator(".start-form input").first().fill("Alex");
  await clockPage.locator(".start-form button").first().click();
  await loaded(clockPage);
  const clockSessionURL = clockPage.url();
  const clockSessionID = new URL(clockSessionURL).searchParams.get("session");
  await clockPage.locator("#answer").fill("Device edit");
  await clockPage.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");

  const craftLink = async (revision, answer) => clockPage.evaluate(async ({ sessionID, revision, answer }) => {
    const portable = await import("./assets/portable.js");
    const storage = await import("./assets/storage.js");
    const session = await storage.loadSession(sessionID);
    const snapshot = { ...session, revision, questions: session.questions.map((question) => ({ ...question, answer })) };
    return portable.buildResumeURL(snapshot, window.location.href);
  }, { sessionID: clockSessionID, revision, answer });

  const newerLink = await craftLink(9999, "Newer imported answer");
  const newer = await clocks.newPage();
  await newer.goto(newerLink);
  await loaded(newer);
  assert.equal(await newer.locator("#answer").inputValue(), "Newer imported answer");

  const olderLink = await craftLink(1, "Stale imported answer");
  const older = await clocks.newPage();
  await older.goto(olderLink);
  await loaded(older);
  assert.equal(await older.locator("#answer").inputValue(), "Newer imported answer");

  // A lower-revision divergent branch is preserved and reviewable, never
  // silently dropped because its per-device counter is smaller (F10).
  const lowerLink = await craftLink(1, "Divergent branch answer from device B");
  const lower = await clocks.newPage();
  await lower.goto(lowerLink);
  await loaded(lower);
  assert.equal(await lower.locator("#answer").inputValue(), "Newer imported answer");
  assert.equal(await lower.locator("#reconcile-notice").isVisible(), true);
  await lower.locator("#review-preserved").click();
  assert.ok((await lower.locator(".orphan-entry").count()) >= 1, "the preserved branch is listed for review");
  assert.equal(await lower.locator("#orphan-import").isVisible(), true, "imported copies offer the import choice");

  const divergentLink = await craftLink(10000, "Stale imported answer");
  const divergent = await clocks.newPage();
  await divergent.goto(divergentLink);
  await loaded(divergent);
  assert.equal(await divergent.locator("#answer").inputValue(), "Newer imported answer");
  assert.equal(await divergent.locator("#reconcile-notice").isVisible(), true);
  await clocks.close();
  console.log("Browser acceptance passed: clock-skew imports");

  // Legacy migration: sessions stored under the previous project namespace
  // become reachable again, and the legacy originals are kept.
  const legacy = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const seeder = await legacy.newPage();
  await seeder.goto(origin + fixtures[0][0]);
  await seeder.evaluate(async () => {
    const now = new Date().toISOString();
    const session = {
      version: 2, id: "legacy-idb-session-0001", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "legacy",
      template_name: "Character Backstory", player_name: "Alex", created_at: now, updated_at: now,
      current_question: 0, completed: false,
      questions: [{ id: "legacy\u001fname#1", fallback_id: "name#1", answer: "Legacy saved answer",
        label: "Name", breadcrumb: ["Character Backstory", "Core"], context: [], label_ordinal: 1 }],
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("template-wizard-web", 2);
      request.onupgradeneeded = () => {
        const database = request.result;
        if (!database.objectStoreNames.contains("sessions")) {
          database.createObjectStore("sessions", { keyPath: "id" });
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    localStorage.setItem("template-wizard-web.session.v2.legacy-fallback-session-01",
      JSON.stringify({ ...session, id: "legacy-fallback-session-01" }));
  });
  const legacyIDB = await legacy.newPage();
  await legacyIDB.goto(origin + fixtures[0][0] + "wizard.html?session=legacy-idb-session-0001");
  await loaded(legacyIDB);
  assert.equal(await legacyIDB.locator("#answer").inputValue(), "Legacy saved answer");
  const legacyFallback = await legacy.newPage();
  await legacyFallback.goto(origin + fixtures[0][0] + "wizard.html?session=legacy-fallback-session-01");
  await loaded(legacyFallback);
  assert.equal(await legacyFallback.locator("#answer").inputValue(), "Legacy saved answer");
  assert.equal(await seeder.evaluate(() => localStorage.getItem("template-wizard-web.session.v2.legacy-fallback-session-01") !== null), true);
  await seeder.reload();
  await seeder.locator(".start-form button").first().waitFor();
  assert.equal(await seeder.locator("#recent-list a").count() >= 2, true);
  await legacy.close();
  console.log("Browser acceptance passed: legacy migration");

  // Storage cleanup: the finish dialog can delete the local questionnaire.
  const cleanup = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const cleanupPage = await cleanup.newPage();
  await cleanupPage.goto(origin + fixtures[0][0]);
  await cleanupPage.locator(".start-form input").first().fill("Alex");
  await cleanupPage.locator(".start-form button").first().click();
  await loaded(cleanupPage);
  await cleanupPage.locator("#answer").fill("Cleanup answer");
  await cleanupPage.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
  await cleanupPage.locator("#finish-download").click();
  await cleanupPage.waitForFunction(() => document.getElementById("finish-dialog").open);
  await cleanupPage.locator("#finish-delete").click();
  assert.equal(await cleanupPage.locator("#finish-delete").textContent(), "Confirm delete");
  await cleanupPage.locator("#finish-delete").click();
  await cleanupPage.waitForURL(/index\.html$/u);
  await cleanupPage.locator("#template-grid").waitFor();
  assert.equal(await cleanupPage.locator("#recent-section").isVisible(), false);
  await cleanup.close();
  console.log("Browser acceptance passed: delete local copy");

  // Serialized single-tab saves never conflict with their own predecessor
  // (F09), even when navigation or blur queues saves ahead of the debounce.
  const serial = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const serialPage = await serial.newPage();
  await serialPage.goto(origin + fixtures[0][0]);
  await serialPage.locator(".start-form input").first().fill("Alex");
  await serialPage.locator(".start-form button").first().click();
  await loaded(serialPage);
  await serialPage.locator("#answer").fill("Immediate next answer");
  await serialPage.locator("#next-question").tap();
  await serialPage.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 2 "));
  await serialPage.locator("#previous-question").tap();
  await serialPage.waitForFunction(() => document.getElementById("question-number").textContent.startsWith("Question 1 "));
  await serialPage.locator("#answer").fill("First single-tab edit");
  await serialPage.locator("#answer").blur();
  await serialPage.locator("#answer").fill("Latest single-tab edit");
  await serialPage.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
  await serialPage.reload();
  await loaded(serialPage);
  assert.equal(await serialPage.locator("#answer").inputValue(), "Latest single-tab edit");
  await serial.close();
  console.log("Browser acceptance passed: serialized saves");

  // Deleting a migrated questionnaire also removes its legacy copies, so it
  // cannot return through a re-migration (F11).
  const migrated = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const seedPage = await migrated.newPage();
  await seedPage.goto(origin + fixtures[0][0]);
  await seedPage.evaluate(async () => {
    const now = new Date().toISOString();
    const session = {
      version: 2, id: "legacy-delete-session-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "legacy",
      template_name: "Character Backstory", player_name: "Alex", created_at: now, updated_at: now,
      current_question: 0, completed: false,
      questions: [{ id: "legacy\u001fname#1", fallback_id: "name#1", answer: "Saved before project rename",
        label: "Name", breadcrumb: ["Character Backstory", "Core"], context: [], label_ordinal: 1 }],
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("template-wizard-web", 2);
      request.onupgradeneeded = () => {
        const database = request.result;
        if (!database.objectStoreNames.contains("sessions")) {
          database.createObjectStore("sessions", { keyPath: "id" });
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
  });
  const wizardPage = await migrated.newPage();
  await wizardPage.goto(origin + fixtures[0][0] + "wizard.html?session=legacy-delete-session-01");
  await loaded(wizardPage);
  assert.equal(await wizardPage.locator("#answer").inputValue(), "Saved before project rename");
  await wizardPage.locator("#answer").fill("Migrated answer");
  await wizardPage.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
  await wizardPage.locator("#finish-download").click();
  await wizardPage.waitForFunction(() => document.getElementById("finish-dialog").open);
  await wizardPage.locator("#finish-delete").click();
  assert.equal(await wizardPage.locator("#finish-delete").textContent(), "Confirm delete");
  await wizardPage.locator("#finish-delete").click();
  await wizardPage.waitForURL(/index\.html$/u);
  await wizardPage.goto(origin + fixtures[0][0]);
  await wizardPage.locator("#template-grid").waitFor();
  assert.equal(await wizardPage.locator("#recent-section").isVisible(), false, "the migrated questionnaire must not return");
  const cleared = await wizardPage.evaluate(async () => {
    const legacyKeys = [];
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index);
      if (key?.startsWith("template-wizard-web.session.v2.")) {
        legacyKeys.push(key);
      }
    }
    let legacyRecords = 0;
    try {
      const databases = await indexedDB.databases();
      if (databases.some((entry) => entry?.name === "template-wizard-web")) {
        const database = await new Promise((resolve, reject) => {
          const open = indexedDB.open("template-wizard-web");
          open.onsuccess = () => resolve(open.result);
          open.onerror = () => reject(open.error);
        });
        const records = await new Promise((resolve, reject) => {
          const transaction = database.transaction("sessions", "readonly");
          const request = transaction.objectStore("sessions").getAll();
          request.onsuccess = () => resolve(request.result || []);
          request.onerror = () => reject(request.error);
        });
        legacyRecords = records.length;
        database.close();
      }
    } catch { /* The legacy database may be absent. */ }
    return { legacyKeys, legacyRecords };
  });
  assert.equal(cleared.legacyKeys.length, 0, "legacy fallback copies of the deleted questionnaire must be gone");
  assert.equal(cleared.legacyRecords, 0, "legacy database copies of the deleted questionnaire must be gone");
  await migrated.close();
  console.log("Browser acceptance passed: migrated deletion");

  // A save queued before the deletion cannot recreate a removed questionnaire
  // (F11): the second tab's pending autosave runs after tab A deleted the
  // session and must not acknowledge success or resurrect it.
  const pendingDelete = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const deleter = await pendingDelete.newPage();
  await deleter.goto(origin + fixtures[0][0]);
  await deleter.locator(".start-form input").first().fill("Alex");
  await deleter.locator(".start-form button").first().click();
  await loaded(deleter);
  const pendingSessionURL = deleter.url();
  const pendingSessionID = new URL(pendingSessionURL).searchParams.get("session");

  const queued = await pendingDelete.newPage();
  await queued.clock.install();
  await queued.goto(pendingSessionURL);
  await loaded(queued);
  await queued.locator("#answer").fill("Private edit entered before deletion");
  // No save delay is granted: the 400 ms autosave stays scheduled.

  await deleter.locator("#finish-download").click();
  await deleter.waitForFunction(() => document.getElementById("finish-dialog").open);
  await deleter.locator("#finish-delete").click();
  assert.equal(await deleter.locator("#finish-delete").textContent(), "Confirm delete");
  await deleter.locator("#finish-delete").click();
  await deleter.waitForURL(/index\.html$/u);
  assert.equal(await deleter.locator("#recent-section").isVisible(), false, "the deleted questionnaire must not be listed");

  // Only now does the queued save execute — with no new editing afterwards.
  await queued.clock.runFor(500);
  await queued.waitForFunction(() => document.getElementById("save-status").textContent !== "Unsaved changes", { timeout: 15000 });
  assert.equal(await queued.locator("#save-status").textContent(), "Removed locally in another tab", "the queued save must not report success");
  const resurrected = await queued.evaluate(async (sessionID) => {
    const storage = await import("./assets/storage.js");
    return { loaded: await storage.loadSession(sessionID), recent: (await storage.listRecentSessions(10)).map((entry) => entry.id) };
  }, pendingSessionID);
  assert.equal(resurrected.loaded, null, "the deleted session must not return");
  assert.ok(!resurrected.recent.includes(pendingSessionID), "the deleted session must not be listed");
  await pendingDelete.close();
  console.log("Browser acceptance passed: pending-save deletion");

  // Equal-revision divergent copies preserve the real question answers of the
  // unselected session record (F01), and the reviews survive a later save.
  const tied = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const tiedSeed = await tied.newPage();
  await tiedSeed.goto(origin + fixtures[0][0]);
  await tiedSeed.evaluate(async () => {
    const now = new Date().toISOString();
    const questions = (answer) => [{ id: "tied\u001fname#1", fallback_id: "name#1", answer, label: "Name",
      breadcrumb: ["Character Backstory", "Core"], context: [], label_ordinal: 1 }];
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onupgradeneeded = () => {
        const database = request.result;
        for (const store of ["sessions", "positions", "recoveries", "deleted"]) {
          if (!database.objectStoreNames.contains(store)) {
            database.createObjectStore(store, { keyPath: "id" });
          }
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put({
          version: 2, id: "tied-copy-session-01", template_id: "legacy-template",
          template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
          template_name: "Character Backstory", player_name: "Alex", created_at: now, updated_at: now,
          current_question: 0, completed: false, revision: 10,
          questions: questions("IndexedDB tied copy"),
        });
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    localStorage.setItem("runyth-born.session.v2.tied-copy-session-01", JSON.stringify({
      version: 2, id: "tied-copy-session-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
      template_name: "Character Backstory", player_name: "Alex", created_at: now, updated_at: now,
      current_question: 0, completed: false, revision: 10,
      questions: questions("Fallback tied copy"),
    }));
  });
  const tiedPage = await tied.newPage();
  await tiedPage.goto(origin + fixtures[0][0] + "wizard.html?session=tied-copy-session-01");
  await loaded(tiedPage);
  assert.ok(["IndexedDB tied copy", "Fallback tied copy"].includes(await tiedPage.locator("#answer").inputValue()));
  await tiedPage.locator("#review-preserved").click();
  const tiedEntries = await tiedPage.locator(".orphan-entry").allTextContents();
  assert.equal(tiedEntries.length, 1, "exactly the unselected copy is preserved");
  assert.match(tiedEntries[0], /IndexedDB tied copy|Fallback tied copy/u, "the unselected real question answer is preserved");
  await tiedPage.keyboard.press("Escape");
  // A following save and a reload must both keep the unselected copy
  // available for review (F01).
  await tiedPage.locator("#answer").fill("Post-tie edit");
  await tiedPage.waitForFunction(() => document.getElementById("save-status").textContent === "Saved locally");
  await tiedPage.reload();
  await loaded(tiedPage);
  assert.equal(await tiedPage.locator("#reconcile-notice").isVisible(), true, "the unselected copy remains preserved after saving");
  await tiedPage.locator("#review-preserved").click();
  const tiedAfter = await tiedPage.locator(".orphan-entry").allTextContents();
  assert.match(tiedAfter[0], /IndexedDB tied copy|Fallback tied copy/u, "the unselected answer survives the preservations of later saves (F01)");
  await tiedPage.keyboard.press("Escape");
  await tied.close();
  console.log("Browser acceptance passed: tied divergent copies");

  // Recovery records from both storage copies merge, so a clock-rolled-back
  // acknowledged record is never suppressed (F02).
  const recovery = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const recoverySeed = await recovery.newPage();
  await recoverySeed.goto(origin + fixtures[0][0]);
  await recoverySeed.evaluate(async () => {
    const questions = (answer) => [{ id: "merge\u001fname#1", fallback_id: "name#1", answer, label: "Name",
      breadcrumb: [], context: [], label_ordinal: 1 }];
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onupgradeneeded = (event) => {
        const database = request.result;
        for (const store of ["sessions", "positions", "recoveries", "deleted"]) {
          if (!database.objectStoreNames.contains(store)) {
            database.createObjectStore(store, { keyPath: "id" });
          }
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("recoveries", "readwrite");
        transaction.objectStore("recoveries").put({
          id: "merged-recovery-session-01", reason: "reconcile",
          saved_at: "2026-01-01T00:00:00.000Z",
          entries: questions("Older recovery copy"),
        });
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    const session = {
      version: 2, id: "merged-recovery-session-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
      template_name: "Character Backstory", player_name: "Alex",
      created_at: "2025-12-31T00:00:00.000Z", updated_at: "2025-12-31T00:00:00.000Z",
      current_question: 0, completed: false, revision: 3,
      questions: questions("Base question copy"),
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    localStorage.setItem("runyth-born.session.v2.merged-recovery-session-01.recovery",
      JSON.stringify({
        id: "merged-recovery-session-01", reason: "conflict",
        saved_at: "2025-12-31T23:00:00.000Z",
        entries: questions("New acknowledged recovery copy"),
      }));
  });
  const recoveryPage = await recovery.newPage();
  await recoveryPage.goto(origin + fixtures[0][0] + "wizard.html?session=merged-recovery-session-01");
  await loaded(recoveryPage);
  await recoveryPage.locator("#review-preserved").click();
  const recoveryEntries = await recoveryPage.locator(".orphan-entry").allTextContents();
  assert.ok(recoveryEntries.some((entry) => entry.includes("Older recovery copy")));
  assert.ok(recoveryEntries.some((entry) => entry.includes("New acknowledged recovery copy")), "the fallback recovery copy stays visible under clock rollback (F02)");
  // The earlier `questions` recovery schema upgrades into the entries shape.
  const upgrade = await recoverySeed.evaluate(async (legacyAnswer) => {
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("recoveries", "readwrite");
        transaction.objectStore("recoveries").put({
          id: "old-schema-recovery-01", reason: "reconcile",
          saved_at: "2026-01-01T00:00:00.000Z",
          questions: [{ id: "old\u001fname#1", fallback_id: "name#1", answer: legacyAnswer,
            label: "Name", breadcrumb: [], context: [], label_ordinal: 1 }],
        });
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    const session = {
      version: 2, id: "old-schema-recovery-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
      template_name: "Character Backstory", player_name: "Alex",
      created_at: "2025-12-31T00:00:00.000Z", updated_at: "2025-12-31T00:00:00.000Z",
      current_question: 0, completed: false, revision: 1,
      questions: [],
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
  }, "Previously preserved answer");
  void upgrade;
  const oldSchemaPage = await recovery.newPage();
  await oldSchemaPage.goto(origin + fixtures[0][0] + "wizard.html?session=old-schema-recovery-01");
  await loaded(oldSchemaPage);
  assert.equal(await oldSchemaPage.locator("#reconcile-notice").isVisible(), true, "the prior schema recovery record surfaces for review");
  await oldSchemaPage.locator("#review-preserved").click();
  const oldSchemaEntries = await oldSchemaPage.locator(".orphan-entry").allTextContents();
  assert.ok(oldSchemaEntries.some((entry) => entry.includes("Previously preserved answer")), "old-schema answers survive the upgrade (F02)");
  await recovery.close();
  console.log("Browser acceptance passed: recovery records merge");

  // Concurrent recovery writers merge instead of replacing each other's
  // acknowledged additions (F02); native scheduling decides the ordering.
  const concurrent = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const concurrentSeed = await concurrent.newPage();
  await concurrentSeed.goto(origin + fixtures[0][0]);
  await concurrentSeed.evaluate(async () => {
    const questions = (answer) => [{ id: "race\u001fname#1", fallback_id: "name#1", answer, label: "Name", breadcrumb: [], context: [], label_ordinal: 1 }];
    const session = {
      version: 2, id: "concurrent-recovery-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
      template_name: "Character Backstory", player_name: "Alex",
      created_at: "2025-12-31T00:00:00.000Z", updated_at: "2025-12-31T00:00:00.000Z",
      current_question: 0, completed: false, revision: 4,
      questions: questions("Local tab copy"),
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onupgradeneeded = () => {
        const database = request.result;
        for (const store of ["sessions", "positions", "recoveries", "deleted"]) {
          if (!database.objectStoreNames.contains(store)) {
            database.createObjectStore(store, { keyPath: "id" });
          }
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
    await Promise.all([
      (async () => {
        const storage = await import("./assets/storage.js");
        await storage.saveRecovery("concurrent-recovery-01",
          { reason: "conflict", entries: questions("Rejected edit from tab A") });
      })(),
      (async () => {
        const storage = await import("./assets/storage.js");
        await storage.saveRecovery("concurrent-recovery-01",
          { reason: "conflict", entries: questions("Rejected edit from tab B") });
      })(),
    ]);
  });
  const concurrentPage = await concurrent.newPage();
  await concurrentPage.goto(origin + fixtures[0][0] + "wizard.html?session=concurrent-recovery-01");
  await loaded(concurrentPage);
  await concurrentPage.locator("#review-preserved").click();
  const simultaneousEntries = await concurrentPage.locator(".orphan-entry").allTextContents();
  assert.ok(simultaneousEntries.some((entry) => entry.includes("Rejected edit from tab A")), "tab A's acknowledged entry must survive concurrent writers");
  assert.ok(simultaneousEntries.some((entry) => entry.includes("Rejected edit from tab B")), "tab B's acknowledged entry must survive concurrent writers");
  await concurrent.close();
  console.log("Browser acceptance passed: concurrent recovery writes");

  // A recovery write begun before the deletion cannot repopulate a removed
  // questionnaire (F11): the write and the deletion race natively inside the
  // browser, and either order leaves no preserved answers.
  const race = await browser.newContext({ hasTouch: true, viewport: { width: 390, height: 844 } });
  const raceSeed = await race.newPage();
  await raceSeed.goto(origin + fixtures[0][0]);
  await raceSeed.evaluate(async () => {
    const questions = (answer) => [{ id: "race\u001fname#1", fallback_id: "name#1", answer, label: "Name", breadcrumb: [], context: [], label_ordinal: 1 }];
    const session = {
      version: 2, id: "recovery-delete-race-01", template_id: "legacy-template",
      template_filename: "minimal-template.mdast.json", template_fingerprint: "seed",
      template_name: "Character Backstory", player_name: "Alex",
      created_at: "2025-12-31T00:00:00.000Z", updated_at: "2025-12-31T00:00:00.000Z",
      current_question: 0, completed: false, revision: 2,
      questions: questions("Race session answer"),
    };
    await new Promise((resolve, reject) => {
      const request = indexedDB.open("runyth-born", 4);
      request.onupgradeneeded = () => {
        const database = request.result;
        for (const store of ["sessions", "positions", "recoveries", "deleted"]) {
          if (!database.objectStoreNames.contains(store)) {
            database.createObjectStore(store, { keyPath: "id" });
          }
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        const transaction = database.transaction("sessions", "readwrite");
        transaction.objectStore("sessions").put(session);
        transaction.oncomplete = () => { database.close(); resolve(); };
        transaction.onerror = () => reject(transaction.error);
      };
      request.onerror = () => reject(request.error);
    });
  });
  const raceOutcome = await raceSeed.evaluate(async () => {
    const storage = await import("./assets/storage.js");
    const entries = [{ id: "", fallback_id: "name#1", label: "Name", breadcrumb: [], answer: "Private recovery queued before deletion" }];
    const [write, deletion] = await Promise.allSettled([
      storage.saveRecovery("recovery-delete-race-01", { reason: "conflict", entries }),
      storage.deleteSession("recovery-delete-race-01"),
    ]);
    return {
      writeRejected: write.status === "rejected" && write.reason?.name === "SessionDeletedError",
      deletionSettled: deletion.status === "fulfilled" || deletion.status === "rejected",
      recovery: await storage.loadRecovery("recovery-delete-race-01"),
      loaded: await storage.loadSession("recovery-delete-race-01"),
      recent: (await storage.listRecentSessions(10)).map((entry) => entry.id),
    };
  });
  assert.equal(raceOutcome.recovery, null, "no preserved answers may survive a completed deletion");
  assert.equal(raceOutcome.loaded, null, "the deleted session must not return");
  assert.ok(!raceOutcome.recent.includes("recovery-delete-race-01"), "the deleted questionnaire must not be listed");
  void raceOutcome.writeRejected;
  void raceOutcome.deletionSettled;
  await race.close();
  console.log("Browser acceptance passed: recovery-delete race");
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
