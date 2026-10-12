import assert from "node:assert/strict";
import test from "node:test";

import { callCore, escapeAnswer } from "../../site/assets/runtime.js";

// The renderer is a development-only dependency; environments without an
// install (for example a fresh Source ZIP extraction) skip the render checks.
let MarkdownIt = null;
try {
  ({ default: MarkdownIt } = await import("markdown-it"));
} catch {
  // Renderer-based validation stays available wherever the dev deps exist.
}

const renderer = MarkdownIt ? new MarkdownIt({ html: false, linkify: false }) : null;

function compiledTemplate(answer) {
  return JSON.stringify({
    schema_version: 1,
    fingerprint: "entities",
    template_name: "Entities",
    questions: [{ id: "entities\u001fname#1", fallback_id: "name#1", label: "Name", breadcrumb: ["Entities", "Core"], answer: "" }],
    parts: [{ text: "- **Name:**" }, { question: 0, continuation: "  \n  " }, { text: "\n" }],
  });
}

test("entity-like answer text survives the exported Markdown round trip", { skip: !renderer }, async () => {
  const answer = "Alice &copy; &lt;test&gt; &#169; &#x41; & Tom & Jerry";
  const result = await callCore("reconcile", {
    source: compiledTemplate(),
    saved_questions: [{ id: "entities\u001fname#1", fallback_id: "name#1", answer }],
  });

  assert.equal(result.markdown, `- **Name:** ${answer.split("\n").map(escapeAnswer).join("  \n  ")}\n`);

  const rendered = renderer.render(result.markdown);
  assert.ok(rendered.includes("&amp;copy;"), rendered);
  assert.ok(rendered.includes("&amp;lt;"), rendered);
  assert.ok(rendered.includes("&amp;#169;"), rendered);
  assert.ok(rendered.includes("&amp;#x41;"), rendered);
  assert.ok(rendered.includes("Tom &amp; Jerry"), rendered);
  assert.ok(!rendered.includes("©"), rendered);
  assert.ok(!rendered.includes("<test>"), rendered);
});
