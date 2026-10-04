import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const dist = path.resolve(process.argv[2] || "dist");
const { callCore } = await import(pathToFileURL(path.join(dist, "assets", "runtime.js")));
const catalog = JSON.parse(await fs.readFile(path.join(dist, "templates", "catalog.json"), "utf8"));
assert.ok(catalog.templates.length, "Template catalog is empty");
for (const entry of catalog.templates) {
  const source = await fs.readFile(path.resolve(dist, decodeURIComponent(entry.url)), "utf8");
  const initial = await callCore("analyze", { source });
  assert.equal(initial.questions.length, entry.question_count);
  assert.equal(initial.fingerprint, entry.fingerprint);
  assert.equal(initial.sections.length, entry.section_count);
  const saved = structuredClone(initial.questions);
  saved[0].answer = "Akito *literal*\n# still an answer";
  const resumed = await callCore("reconcile", { source, saved_questions: saved });
  assert.equal(resumed.questions[0].answer, saved[0].answer);
  assert.ok(resumed.markdown.includes("Akito \\*literal\\*"));
  assert.ok(resumed.markdown.includes("\\# still an answer"));
  assert.equal(resumed.sections[0].answered, initial.sections[0].answered + (initial.questions[0].answer ? 0 : 1));
  saved[0].answer = "";
  const cleared = await callCore("reconcile", { source, saved_questions: saved });
  assert.equal(cleared.questions[0].answer, "");
}
console.log(`Static JavaScript smoke test passed: ${catalog.templates.length} templates`);
