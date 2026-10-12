import assert from "node:assert/strict";
import test from "node:test";

import { callCore, escapeAnswer, preservedAnswersMarkdown } from "../../site/assets/runtime.js";

// The renderer is a required development dependency for this fidelity gate:
// it must fail loudly when unavailable instead of silently reducing
// coverage (F13). Every workflow installs the locked dependencies before
// running the quality gates.
const { default: MarkdownIt } = await import("markdown-it");
const renderer = new MarkdownIt({ html: false, linkify: false });

function compiledTemplate(answer) {
  return JSON.stringify({
    schema_version: 1,
    fingerprint: "entities",
    template_name: "Entities",
    questions: [{ id: "entities\u001fname#1", fallback_id: "name#1", label: "Name", breadcrumb: ["Entities", "Core"], answer: "" }],
    parts: [{ text: "- **Name:**" }, { question: 0, continuation: "  \n  " }, { text: "\n" }],
  });
}

test("entity-like answer text survives the exported Markdown round trip", async () => {
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

test("preserved-answer serialization keeps entity strings, headings, and raw HTML literal", async () => {
  const output = preservedAnswersMarkdown([
    { label: "Age & origin:", breadcrumb: ["Story", "Core"], answer: "Alice &copy; &lt;test&gt;\n# heading\n<script>" },
    { label: "Name", breadcrumb: [], answer: "Ordinary & answer" },
  ]);

  assert.ok(output.includes("## Story › Core › Age \\& origin:"), output);
  assert.ok(output.includes("Alice \\&copy; \\&lt;test\\&gt;  \n\\# heading  \n\\<script\\>"), output);
  assert.ok(output.includes("## Name"), output);
  assert.ok(output.includes("Ordinary \\& answer"), output);

  const rendered = renderer.render(output);
  assert.ok(!rendered.includes("©"), rendered);
  assert.ok(!rendered.includes("<test>"), rendered);
  assert.ok(!rendered.includes('<script>'), rendered);
  assert.ok(!rendered.includes("<h1>heading</h1>"), rendered);
  assert.ok(rendered.includes("&amp;copy;"), rendered);
});
