import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

const run = promisify(execFile);
const projectRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

async function disposableCopy() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "runyth-born-build-guard-"));
  await run("git", ["clone", "--quiet", "--no-hardlinks", projectRoot, root]);
  return root;
}

test("build destinations that overlap tracked source are rejected", async () => {
  const copy = await disposableCopy();
  try {
    const before = await run("git", ["-C", copy, "ls-files", "tests"]);
    const tracked = before.stdout.trim().split("\n").filter(Boolean);
    assert.ok(tracked.length > 0);

    const result = await run("bash", ["scripts/build-static.sh", "tests"], { cwd: copy })
      .then(() => ({ failed: false }))
      .catch(() => ({ failed: true }));
    assert.equal(result.failed, true, "an output override over tracked tests must fail");

    const after = await run("git", ["-C", copy, "ls-files", "tests"]);
    assert.deepEqual(after.stdout.trim().split("\n").filter(Boolean), tracked, "tracked tests must survive");
    assert.equal((await run("git", ["-C", copy, "status", "--porcelain"])).stdout, "", "the workspace must stay clean");

    const readme = await run("bash", ["scripts/build-static.sh", "README.md"], { cwd: copy })
      .then(() => ({ failed: false }))
      .catch(() => ({ failed: true }));
    assert.equal(readme.failed, true, "an output override over a tracked root file must fail");
    assert.equal(await fs.readFile(path.join(copy, "README.md"), "utf8").then(() => true).catch(() => false), true);
  } finally {
    await fs.rm(copy, { recursive: true, force: true });
  }
});

test("a valid external empty output directory still builds", async () => {
  const copy = await disposableCopy();
  const external = await fs.mkdtemp(path.join(os.tmpdir(), "runyth-born-build-out-"));
  try {
    await fs.rmdir(external);
    await run("bash", ["scripts/build-static.sh", external], { cwd: copy, timeout: 120000 });
    const built = await fs.readdir(external);
    assert.ok(built.includes("index.html"), built.join(", "));
  } finally {
    await fs.rm(copy, { recursive: true, force: true });
    await fs.rm(external, { recursive: true, force: true });
  }
});

test("invoking the build through a symlinked checkout still rejects tracked outputs", async () => {
  const copy = await disposableCopy();
  const alias = path.join(os.tmpdir(), `runyth-born-alias-${process.pid}-${Date.now()}`);
  await fs.symlink(copy, alias, "dir");
  try {
    const result = await run("bash", [path.join(alias, "scripts", "build-static.sh"), path.join(alias, "tests")], { cwd: os.tmpdir(), timeout: 120000 })
      .then(() => ({ failed: false }))
      .catch(() => ({ failed: true }));
    assert.equal(result.failed, true, "a symlinked invocation over tracked tests must fail");

    const tracked = (await run("git", ["-C", copy, "ls-files", "tests"])).stdout.trim().split("\n").filter(Boolean);
    const after = (await run("git", ["-C", copy, "ls-files", "tests"])).stdout.trim().split("\n").filter(Boolean);
    assert.deepEqual(after, tracked, "tracked tests must survive the symlinked invocation");

    const rootResult = await run("bash", [path.join(alias, "scripts", "build-static.sh"), path.join(alias, "README.md")], { cwd: os.tmpdir(), timeout: 120000 })
      .then(() => ({ failed: false }))
      .catch(() => ({ failed: true }));
    assert.equal(rootResult.failed, true, "a symlinked invocation over a tracked root file must fail");
  } finally {
    await fs.rm(copy, { recursive: true, force: true });
    await fs.rm(alias, { recursive: true, force: true });
  }
});
