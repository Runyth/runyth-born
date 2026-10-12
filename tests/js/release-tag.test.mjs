import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

const run = promisify(execFile);
const scriptsDir = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "..", "scripts");

async function fixtureCopy(version) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "runyth-born-tag-"));
  await fs.mkdir(path.join(root, "scripts"), { recursive: true });
  await fs.copyFile(path.join(scriptsDir, "check-release-tag.sh"), path.join(root, "scripts", "check-release-tag.sh"));
  await fs.writeFile(path.join(root, "VERSION"), `${version}\n`);
  return root;
}

async function runGuard(root, ref, version) {
  return run("bash", ["scripts/check-release-tag.sh", ref, version], { cwd: root })
    .then(() => true)
    .catch(() => false);
}

test("a matching version tag is accepted", async () => {
  const root = await fixtureCopy("2.0.0");
  try {
    assert.equal(await runGuard(root, "v2.0.0", "2.0.0"), true);
  } finally {
    await fs.rm(root, { recursive: true, force: true });
  }
});

test("tags that disagree with the packaged version are rejected", async () => {
  const root = await fixtureCopy("2.0.0");
  try {
    assert.equal(await runGuard(root, "v9.9.9", "2.0.0"), false, "a v9.9.9 tag must not publish v2.0.0 assets");
    assert.equal(await runGuard(root, "master", "2.0.0"), false, "a non-version ref must be rejected");
  } finally {
    await fs.rm(root, { recursive: true, force: true });
  }
});
