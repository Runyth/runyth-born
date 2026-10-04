"use strict";

const DATABASE_NAME = "template-wizard-web";
const DATABASE_VERSION = 2;
const STORE_NAME = "sessions";
const FALLBACK_PREFIX = "template-wizard-web.session.v2.";

let databasePromise;

function openDatabase() {
  if (!globalThis.indexedDB) {
    return Promise.reject(new Error("IndexedDB is unavailable"));
  }
  if (!databasePromise) {
    databasePromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
      request.onupgradeneeded = () => {
        const database = request.result;
        if (!database.objectStoreNames.contains(STORE_NAME)) {
          database.createObjectStore(STORE_NAME, { keyPath: "id" });
        }
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error || new Error("Could not open browser storage"));
      request.onblocked = () => reject(new Error("Browser storage upgrade is blocked by another tab"));
    });
  }
  return databasePromise;
}

async function withStore(mode, callback) {
  const database = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(STORE_NAME, mode);
    const store = transaction.objectStore(STORE_NAME);
    let request;
    try {
      request = callback(store);
    } catch (error) {
      reject(error);
      return;
    }
    // A successful put request can still be followed by a transaction abort
    // (for example quota exhaustion). Confirm the commit before claiming save.
    transaction.oncomplete = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("Browser storage operation failed"));
    transaction.onabort = () => reject(transaction.error || new Error("Browser storage transaction was aborted"));
  });
}

function fallbackSave(session) {
  localStorage.setItem(`${FALLBACK_PREFIX}${session.id}`, JSON.stringify(session));
}

function fallbackLoad(id) {
  const raw = localStorage.getItem(`${FALLBACK_PREFIX}${id}`);
  return raw ? JSON.parse(raw) : null;
}

function fallbackList() {
  const sessions = [];
  for (let index = 0; index < localStorage.length; index += 1) {
    const key = localStorage.key(index);
    if (!key?.startsWith(FALLBACK_PREFIX)) {
      continue;
    }
    try {
      const session = JSON.parse(localStorage.getItem(key) || "null");
      if (session?.id) {
        sessions.push(session);
      }
    } catch {
      // Ignore malformed fallback entries rather than hiding healthy sessions.
    }
  }
  return sessions;
}

export function randomToken(bytes = 32) {
  const values = new Uint8Array(bytes);
  crypto.getRandomValues(values);
  let binary = "";
  for (const value of values) {
    binary += String.fromCharCode(value);
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

export function createSession(template, playerName = "") {
  const now = new Date().toISOString();
  return {
    version: 2,
    id: randomToken(),
    template_id: template.id,
    template_filename: template.filename,
    template_fingerprint: template.fingerprint,
    template_name: template.name,
    player_name: String(playerName).trim().replace(/\s+/gu, " ").slice(0, 100),
    created_at: now,
    updated_at: now,
    current_question: 0,
    completed: false,
    questions: [],
  };
}

export async function saveSession(session, { touch = true } = {}) {
  const copy = structuredClone(session);
  if (touch) {
    copy.updated_at = new Date().toISOString();
    session.updated_at = copy.updated_at;
  }

  try {
    await withStore("readwrite", (store) => store.put(copy));
  } catch (indexedDBError) {
    try {
      fallbackSave(copy);
    } catch (fallbackError) {
      throw new Error(`Could not save locally: ${fallbackError.message || indexedDBError.message}`);
    }
  }
  return copy;
}

export async function loadSession(id) {
  if (!id) {
    return null;
  }

  let indexedSession = null;
  let fallbackSession = null;
  try {
    indexedSession = await withStore("readonly", (store) => store.get(id));
  } catch {
    indexedSession = null;
  }
  try {
    fallbackSession = fallbackLoad(id);
  } catch {
    fallbackSession = null;
  }

  if (!indexedSession) {
    return fallbackSession;
  }
  if (!fallbackSession) {
    return indexedSession;
  }

  const indexedTime = Date.parse(indexedSession.updated_at || "") || 0;
  const fallbackTime = Date.parse(fallbackSession.updated_at || "") || 0;
  return fallbackTime > indexedTime ? fallbackSession : indexedSession;
}

export async function listRecentSessions(limit = 8) {
  let indexedSessions = [];
  let fallbackSessions = [];
  try {
    indexedSessions = await withStore("readonly", (store) => store.getAll());
  } catch {
    indexedSessions = [];
  }
  try {
    fallbackSessions = fallbackList();
  } catch {
    fallbackSessions = [];
  }

  const byID = new Map();
  for (const candidate of [...indexedSessions, ...fallbackSessions]) {
    if (!candidate?.id || !candidate?.template_name) {
      continue;
    }
    const existing = byID.get(candidate.id);
    if (!existing || String(candidate.updated_at).localeCompare(String(existing.updated_at)) > 0) {
      byID.set(candidate.id, candidate);
    }
  }

  return [...byID.values()]
    .sort((left, right) => String(right.updated_at).localeCompare(String(left.updated_at)))
    .slice(0, limit);
}
