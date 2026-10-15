import assert from "node:assert/strict";
import test from "node:test";
import { gzipSync } from "node:zlib";

import { buildResumeURL, decodeResumePayload, encodeResumePayload } from "../../site/assets/portable.js";
import { createSession, randomToken } from "../../site/assets/storage.js";

const validSessionID = "abcdefghijklmnopqrstuvwxyzABCDEFGH123456789";

function exampleSession() {
  return {
    version: 2,
    id: validSessionID,
    template_id: "template-example",
    template_filename: "example.mdast.json",
    template_fingerprint: "abc123",
    template_name: "Example",
    player_name: "Akito",
    created_at: "2026-09-11T12:00:00.000Z",
    updated_at: "2026-09-11T12:01:00.000Z",
    current_question: 1,
    completed: false,
    questions: [{
      id: "core\u001fname#1",
      fallback_id: "name#1",
      label: "Name",
      breadcrumb: ["Example", "Core"],
      context: ["Context"],
      answer: "Akito",
      label_ordinal: 1,
    }],
  };
}

test("portable resume payload round-trips only the state needed to reconcile answers", async () => {
  const session = exampleSession();
  const payload = await encodeResumePayload(session);
  const restored = await decodeResumePayload(payload);

  assert.equal(restored.id, session.id);
  assert.equal(restored.template_filename, session.template_filename);
  assert.equal(restored.player_name, session.player_name);
  assert.deepEqual(restored.questions, [{
    id: "core\u001fname#1",
    fallback_id: "name#1",
    answer: "Akito",
  }]);
});

test("a blank marked answered survives a portable resume", async () => {
  const session = exampleSession();
  session.questions[0].answer = "";
  session.questions[0].marked_answered = true;
  const restored = await decodeResumePayload(await encodeResumePayload(session));
  assert.deepEqual(restored.questions, [{
    id: "core\u001fname#1",
    fallback_id: "name#1",
    answer: "",
    marked_answered: true,
  }]);
});

test("portable resume URL keeps state in the fragment", async () => {
  const value = await buildResumeURL(exampleSession(), "https://example.invalid/runyth-born/wizard.html?session=old");
  const url = new URL(value);
  assert.equal(url.pathname, "/runyth-born/wizard.html");
  assert.equal(url.search, "");
  assert.match(url.hash, /^#resume=/u);
});

test("session helpers create cryptographically-sized local identifiers", () => {
  const token = randomToken();
  assert.ok(token.length >= 40);

  const session = createSession({
    id: "template-id",
    filename: "example.mdast.json",
    fingerprint: "abc",
    name: "Example",
  }, "  Akito   Example  ");
  assert.equal(session.player_name, "Akito Example");
  assert.equal(session.template_id, "template-id");
  assert.equal(session.questions.length, 0);
});

test("resume payload rejects oversized answers", async () => {
  const session = exampleSession();
  session.questions[0].answer = "x".repeat(65537);
  const payload = await encodeResumePayload(session);
  await assert.rejects(() => decodeResumePayload(payload), /larger than the supported limit/u);
});

test("compressed payloads have a decoded size limit", async () => {
  const compressed = gzipSync(Buffer.alloc(18 * 1024 * 1024 + 1, 120));
  await assert.rejects(() => decodeResumePayload(`g.${compressed.toString("base64url")}`), /too large/u);
});

test("plain JSON encoding works without compression support", async () => {
  const original = globalThis.CompressionStream;
  try {
    globalThis.CompressionStream = undefined;
    const payload = await encodeResumePayload(exampleSession());
    assert.ok(payload.startsWith("j."));
    assert.equal((await decodeResumePayload(payload)).player_name, "Akito");
  } finally {
    globalThis.CompressionStream = original;
  }
});
