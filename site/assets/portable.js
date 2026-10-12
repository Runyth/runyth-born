"use strict";

const MAX_PAYLOAD_CHARACTERS = 24 * 1024 * 1024;
const MAX_DECODED_BYTES = 18 * 1024 * 1024;

function bytesToBase64URL(bytes) {
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

function base64URLToBytes(value) {
  const normalized = value.replaceAll("-", "+").replaceAll("_", "/");
  const padding = "=".repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(normalized + padding);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

async function gzip(bytes) {
  if (typeof CompressionStream !== "function") {
    return null;
  }
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream("gzip"));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

async function gunzip(bytes) {
  if (typeof DecompressionStream !== "function") {
    throw new Error("This browser cannot decompress the resume link");
  }
  const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"));
  const reader = stream.getReader();
  const chunks = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) { break; }
      size += value.byteLength;
      if (size > MAX_DECODED_BYTES) {
        await reader.cancel();
        throw new Error("Resume payload is too large after decompression");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return result;
}

function resumableSnapshot(session) {
  return {
    version: 2,
    id: session.id,
    template_id: session.template_id,
    template_filename: session.template_filename,
    template_fingerprint: session.template_fingerprint,
    template_name: session.template_name,
    player_name: session.player_name || "",
    created_at: session.created_at,
    updated_at: session.updated_at,
    revision: Number.isSafeInteger(session.revision) && session.revision > 0 ? session.revision : 0,
    current_question: session.current_question || 0,
    completed: Boolean(session.completed),
    questions: Array.isArray(session.questions)
      ? session.questions.map((question) => ({
        id: String(question.id || ""),
        fallback_id: String(question.fallback_id || ""),
        answer: String(question.answer || ""),
      }))
      : [],
  };
}

function normalizeTimestamp(value) {
  return typeof value === "string" && !Number.isNaN(Date.parse(value)) ? value : new Date().toISOString();
}

export async function encodeResumePayload(session) {
  const raw = new TextEncoder().encode(JSON.stringify(resumableSnapshot(session)));
  if (raw.byteLength > MAX_DECODED_BYTES) {
    throw new Error("Resume payload is too large");
  }
  const compressed = await gzip(raw);
  if (compressed && compressed.length < raw.length) {
    return `g.${bytesToBase64URL(compressed)}`;
  }
  return `j.${bytesToBase64URL(raw)}`;
}

export async function decodeResumePayload(payload) {
  if (typeof payload !== "string" || payload.length < 3 || payload.length > MAX_PAYLOAD_CHARACTERS) {
    throw new Error("Resume payload is invalid or too large");
  }
  const separator = payload.indexOf(".");
  if (separator !== 1) {
    throw new Error("Resume payload format is not supported");
  }

  const kind = payload.slice(0, separator);
  let bytes = base64URLToBytes(payload.slice(separator + 1));
  if (kind === "g") {
    bytes = await gunzip(bytes);
  } else if (kind !== "j") {
    throw new Error("Resume payload format is not supported");
  }
  if (bytes.byteLength > MAX_DECODED_BYTES) {
    throw new Error("Resume payload is too large");
  }

  const decoded = JSON.parse(new TextDecoder().decode(bytes));
  if (!decoded || decoded.version !== 2 || typeof decoded.id !== "string" || typeof decoded.template_filename !== "string") {
    throw new Error("Resume payload does not contain a supported questionnaire session");
  }
  if (!/^[A-Za-z0-9_-]{20,128}$/u.test(decoded.id)) {
    throw new Error("Resume payload contains an invalid session identifier");
  }
  if (decoded.template_filename.length > 255 || decoded.template_filename.includes("/") || decoded.template_filename.includes("\\")) {
    throw new Error("Resume payload contains an invalid template filename");
  }
  decoded.player_name = String(decoded.player_name || "").trim().replace(/\s+/gu, " ").slice(0, 100);
  decoded.revision = Number.isSafeInteger(decoded.revision) && decoded.revision > 0 ? decoded.revision : 0;
  decoded.created_at = normalizeTimestamp(decoded.created_at);
  decoded.updated_at = normalizeTimestamp(decoded.updated_at);
  decoded.current_question = Number.isSafeInteger(decoded.current_question) && decoded.current_question >= 0
    ? decoded.current_question : 0;
  decoded.completed = decoded.completed === true;
  if (!Array.isArray(decoded.questions) || decoded.questions.length > 10000) {
    throw new Error("Resume payload contains an invalid question set");
  }
  decoded.questions = decoded.questions.map((question) => {
    const answer = String(question?.answer || "");
    if (answer.length > 65536) {
      throw new Error("Resume payload contains an answer larger than the supported limit");
    }
    return {
      id: String(question?.id || "").slice(0, 4096),
      fallback_id: String(question?.fallback_id || "").slice(0, 4096),
      answer,
    };
  });
  return decoded;
}

export async function buildResumeURL(session, currentURL = globalThis.location?.href || "https://example.invalid/wizard.html") {
  const payload = await encodeResumePayload(session);
  const url = new URL("./wizard.html", currentURL);
  url.search = "";
  url.hash = new URLSearchParams({ resume: payload }).toString();
  return url.toString();
}

export function resumePayloadFromLocation(locationObject = globalThis.location) {
  const parameters = new URLSearchParams(String(locationObject?.hash || "").replace(/^#/u, ""));
  return parameters.get("resume") || "";
}
