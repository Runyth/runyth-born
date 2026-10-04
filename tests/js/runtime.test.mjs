import assert from "node:assert/strict";
import test from "node:test";
import { callCore } from "../../site/assets/runtime.js";

function source(questions) {
  return JSON.stringify({ schema_version: 1, fingerprint: "example", template_name: "Example", questions,
    parts: [{ text: "- **Name:**" }, { question: 0, continuation: "  \n  " }, { text: "\n" }] });
}
const question = { id: "section\u001fname#1", fallback_id: "name#1", answer: "Default", breadcrumb: ["Example", "Core"] };

test("cleared answers override template defaults", async () => {
  const result = await callCore("reconcile", { source: source([question]), saved_questions: [{ ...question, answer: "" }] });
  assert.equal(result.questions[0].answer, "");
  assert.equal(result.markdown, "- **Name:**\n");
});

test("duplicate labels cannot transfer answers into a different moved section", async () => {
  const result = await callCore("reconcile", { source: source([question, { ...question, id: "other#1", fallback_id: "name#2" }]),
    saved_questions: [{ ...question, id: "old#1", answer: "Private answer" }] });
  assert.equal(result.questions[0].answer, "Default");
  assert.equal(result.questions[1].answer, "Default");
});

test("globally unique labels reconcile after a section move", async () => {
  const result = await callCore("reconcile", { source: source([question]),
    saved_questions: [{ ...question, id: "old#1", answer: "Akito" }] });
  assert.equal(result.questions[0].answer, "Akito");
});

test("multiline Markdown punctuation remains literal and inside its list item", async () => {
  const result = await callCore("reconcile", { source: source([question]),
    saved_questions: [{ ...question, answer: "_name_ `code`\n# heading\n- list\n<script>" }] });
  assert.equal(result.markdown, "- **Name:** \\_name\\_ \\`code\\`  \n  \\# heading  \n  \\- list  \n  \\<script\\>\n");
});
