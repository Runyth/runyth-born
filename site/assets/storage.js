"use strict";

const DATABASE_NAME = "runyth-born";
const DATABASE_VERSION = 3;
const STORE_NAME = "sessions";
const POSITION_STORE_NAME = "positions";
const RECOVERY_STORE_NAME = "recoveries";
const FALLBACK_PREFIX = "runyth-born.session.v2.";

export class StorageConflictError extends Error {
  constructor() {
    super("A newer version of this questionnaire was saved elsewhere");
    this.name = "StorageConflictError";
  }
}

// The static 2.x application stored sessions under its previous project name.
// Those records are copied into the renamed stores on first open so existing
// sessions keep working after the upgrade; the original legacy entries are
// kept untouched as recoverable copies.
const LEGACY_DATABASE_NAME = "template-wizard-web";
const LEGACY_FALLBACK_PREFIX = "template-wizard-web.session.v2.";

let databasePromise;
let legacyMigrationPromise;

function openRawDatabase() {
  if (!globalThis.indexedDB) {
    return Promise.reject(new Error("IndexedDB is unavailable"));
  }
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
    request.onupgradeneeded = () => {
      const database = request.result;
      for (const storeName of [STORE_NAME, POSITION_STORE_NAME, RECOVERY_STORE_NAME]) {
        if (!database.objectStoreNames.contains(storeName)) {
          database.createObjectStore(storeName, { keyPath: "id" });
        }
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("Could not open browser storage"));
    request.onblocked = () => reject(new Error("Browser storage upgrade is blocked by another tab"));
  });
}

function openDatabase() {
  if (!databasePromise) {
    databasePromise = openRawDatabase();
  }
  return migrateOnce().then(() => databasePromise);
}

// Reads every session from the legacy namespace, then copies the ones the new
// namespace does not have yet. Migration is idempotent: it only adds missing
// records and never deletes the legacy originals. It works on a separate raw
// database connection so it never waits on itself through openDatabase().
function migrateOnce() {
  if (!legacyMigrationPromise) {
    legacyMigrationPromise = migrateLegacyRecords().catch((error) => {
      // Retry on the next open instead of permanently disabling migration.
      legacyMigrationPromise = null;
      throw error;
    });
  }
  return legacyMigrationPromise;
}

async function migrateLegacyRecords() {
  if (!globalThis.indexedDB) {
    return;
  }
  const legacySessions = await readLegacySessions();
  const legacyFallbackSessions = readLegacyFallbackSessions();
  if (legacySessions.length === 0 && legacyFallbackSessions.length === 0) {
    return;
  }

  const database = await openRawDatabase();
  try {
    let indexedSessions = [];
    try {
      indexedSessions = await transactionOn(database, STORE_NAME, "readonly", getAllOperate());
    } catch { /* The new stores may be unavailable; the fallback check still applies. */ }
    let fallbackSessions = [];
    try {
      fallbackSessions = fallbackList();
    } catch { /* Fallback storage is unavailable. */ }

    const records = mergeLegacyRecords({ legacySessions, legacyFallbackSessions, indexedSessions, fallbackSessions });
    for (const record of records) {
      try {
        await transactionOn(database, STORE_NAME, "readwrite", putOperate(record));
      } catch {
        try {
          fallbackSave(record);
        } catch { /* Neither store could accept the migrated record. */ }
      }
    }
  } finally {
    database.close();
  }
}

function readLegacySessions() {
  if (typeof indexedDB.databases === "function") {
    return indexedDB.databases().then((databases) => {
      if (!databases.some((entry) => entry?.name === LEGACY_DATABASE_NAME)) {
        return [];
      }
      return openLegacyDatabase();
    }).catch(() => openLegacyDatabase());
  }
  return openLegacyDatabase();
}

function openLegacyDatabase() {
  return new Promise((resolve) => {
    const request = indexedDB.open(LEGACY_DATABASE_NAME);
    request.onsuccess = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(STORE_NAME)) {
        database.close();
        resolve([]);
        return;
      }
      const transaction = database.transaction(STORE_NAME, "readonly");
      const request2 = transaction.objectStore(STORE_NAME).getAll();
      request2.onsuccess = () => {
        database.close();
        resolve(request2.result || []);
      };
      request2.onerror = () => {
        database.close();
        resolve([]);
      };
    };
    request.onerror = () => resolve([]);
    request.onblocked = () => resolve([]);
  });
}

function readLegacyFallbackSessions() {
  const sessions = [];
  try {
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index);
      if (!key?.startsWith(LEGACY_FALLBACK_PREFIX)) {
        continue;
      }
      try {
        const session = JSON.parse(localStorage.getItem(key) || "null");
        if (session?.id) {
          sessions.push(session);
        }
      } catch { /* Ignore malformed legacy entries. */ }
    }
  } catch { /* Fallback storage is unavailable. */ }
  return sessions;
}

// Pure helper: returns the legacy records the new namespace is missing, with a
// starting revision so migrated sessions participate in save ordering.
export function mergeLegacyRecords({ legacySessions = [], legacyFallbackSessions = [], indexedSessions = [], fallbackSessions = [] } = {}) {
  const known = new Set([...indexedSessions, ...fallbackSessions]
    .map((entry) => entry?.id)
    .filter(Boolean));
  const queued = new Set();
  const records = [];
  for (const legacy of [...legacySessions, ...legacyFallbackSessions]) {
    if (!legacy?.id || known.has(legacy.id) || queued.has(legacy.id)) {
      continue;
    }
    queued.add(legacy.id);
    records.push({ ...structuredClone(legacy), revision: normalizeRevision(legacy.revision) || 1 });
  }
  return records;
}

// Runs one transaction and resolves with the value that `operate` records in
// `state.result` (immediately for plain writes, or from request callbacks for
// read-modify-write work). Commit failures and aborts reject; a manual abort
// from `operate` rejects with the recorded conflict instead of a storage error.
// Runs one transaction and resolves with the value that `operate` records in
// `state.result` (immediately for plain writes, or from request callbacks for
// read-modify-write work). Commit failures and aborts reject; a manual abort
// from `operate` rejects with the recorded conflict instead of a storage error.
function transactionOn(database, storeNames, mode, operate) {
  return new Promise((resolve, reject) => {
    const names = Array.isArray(storeNames) ? storeNames : [storeNames];
    const transaction = database.transaction(names, mode);
    const stores = names.map((name) => transaction.objectStore(name));
    const singleStore = stores.length === 1 ? stores[0] : stores;
    const state = {};
    transaction.oncomplete = () => resolve(state.result);
    transaction.onabort = () => {
      reject(state.conflict ? new StorageConflictError()
        : transaction.error || state.error || new Error("Browser storage transaction was aborted"));
    };
    try {
      state.result = operate(singleStore, transaction, state);
    } catch (error) {
      reject(error);
    }
  });
}

function runTransaction(storeNames, mode, operate) {
  return openDatabase().then((database) => transactionOn(database, storeNames, mode, operate));
}

function putOperate(record) {
  return (store, transaction, state) => {
    store.put(record);
    state.result = record;
    return state.result;
  };
}

function getOperate(id) {
  return (store, transaction, state) => {
    const request = store.get(id);
    request.onsuccess = () => { state.result = request.result; };
    return state.result;
  };
}

function getAllOperate() {
  return (store, transaction, state) => {
    const request = store.getAll();
    request.onsuccess = () => { state.result = request.result || []; };
    return state.result;
  };
}

function putRecord(storeName, record) {
  return runTransaction(storeName, "readwrite", putOperate(record));
}

function getRecord(storeName, id) {
  return runTransaction(storeName, "readonly", getOperate(id));
}

function getAllRecords(storeName) {
  return runTransaction(storeName, "readonly", getAllOperate());
}

export function normalizeRevision(value) {
  const revision = Number(value);
  return Number.isSafeInteger(revision) && revision > 0 ? revision : 0;
}

function pickNewer(left, right) {
  if (!left) { return right; }
  if (!right) { return left; }
  return normalizeRevision(right.revision) > normalizeRevision(left.revision) ? right : left;
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
    revision: 0,
    questions: [],
  };
}

// Saves are acknowledged only after a transaction commit, and every saved copy
// carries a monotonic revision. A stored record with a higher revision than the
// incoming copy means another tab or device acknowledged a newer save; the
// incoming copy must never silently replace it. Wall-clock timestamps are
// display metadata only and never decide which copy is authoritative (F03).
export async function saveSession(session, { touch = true } = {}) {
  if (!session?.id) {
    throw new Error("Cannot save a session without an identifier");
  }
  const copy = structuredClone(session);
  copy.revision = normalizeRevision(copy.revision);
  if (touch) {
    copy.updated_at = new Date().toISOString();
    session.updated_at = copy.updated_at;
  }

  try {
    const saved = await saveSessionRecord(copy);
    session.revision = saved.revision;
    return saved;
  } catch (error) {
    if (error instanceof StorageConflictError) {
      throw error;
    }
    try {
      const saved = fallbackSaveRecord(copy);
      session.revision = saved.revision;
      return saved;
    } catch (fallbackError) {
      if (fallbackError instanceof StorageConflictError) {
        throw fallbackError;
      }
      throw new Error(`Could not save locally: ${fallbackError.message || error.message}`);
    }
  }
}

// Reads and updates the stored record inside one transaction so a concurrent
// save cannot slip between the comparison and the write (F01).
function saveSessionRecord(copy) {
  return runTransaction(STORE_NAME, "readwrite", (store, transaction, state) => {
    const request = store.get(copy.id);
    request.onsuccess = () => {
      const storedRevision = normalizeRevision(request.result?.revision);
      if (storedRevision > copy.revision) {
        state.conflict = true;
        try { transaction.abort(); } catch { /* aborting after another failure is harmless */ }
        return;
      }
      copy.revision = Math.max(storedRevision, copy.revision) + 1;
      state.result = copy;
      store.put(copy);
    };
    return state.result;
  });
}

function fallbackSaveRecord(copy) {
  const storedRevision = normalizeRevision(fallbackLoad(copy.id)?.revision);
  if (storedRevision > copy.revision) {
    throw new StorageConflictError();
  }
  copy.revision = Math.max(storedRevision, copy.revision) + 1;
  fallbackSave(copy);
  return copy;
}

export async function loadSession(id) {
  if (!id) {
    return null;
  }

  let indexedSession = null;
  let fallbackSession = null;
  try {
    indexedSession = await getRecord(STORE_NAME, id);
  } catch {
    indexedSession = null;
  }
  try {
    fallbackSession = fallbackLoad(id);
  } catch {
    fallbackSession = null;
  }

  const selected = pickNewer(indexedSession, fallbackSession);
  if (!selected) {
    return null;
  }

  // The most recently navigated position is persisted separately from the
  // answers so a stale tab navigating cannot rewrite saved answers (F01).
  try {
    const position = await loadSessionPosition(id);
    if (position && Number.isSafeInteger(position.position) && position.position >= 0) {
      selected.current_question = position.position;
    }
  } catch {
    // Keep the position stored inside the session record.
  }
  return selected;
}

// Navigation positions are cheap UI state and are stored independently, with
// their own timestamps for choosing between the IndexedDB record and the
// fallback mirror.
export async function saveSessionPosition(id, position) {
  const record = {
    id,
    position: Math.max(0, Math.floor(Number(position) || 0)),
    updated_at: new Date().toISOString(),
  };
  try {
    await putRecord(POSITION_STORE_NAME, record);
  } catch (indexedDBError) {
    try {
      localStorage.setItem(`${FALLBACK_PREFIX}${id}.position`, JSON.stringify(record));
    } catch (fallbackError) {
      throw new Error(`Could not save the position locally: ${fallbackError.message || indexedDBError.message}`);
    }
  }
  return record;
}

export async function loadSessionPosition(id) {
  let indexedPosition = null;
  let fallbackPosition = null;
  try {
    indexedPosition = await getRecord(POSITION_STORE_NAME, id);
  } catch {
    indexedPosition = null;
  }
  try {
    const raw = localStorage.getItem(`${FALLBACK_PREFIX}${id}.position`);
    fallbackPosition = raw ? JSON.parse(raw) : null;
  } catch {
    fallbackPosition = null;
  }
  if (!indexedPosition) { return fallbackPosition; }
  if (!fallbackPosition) { return indexedPosition; }
  return String(fallbackPosition.updated_at || "").localeCompare(String(indexedPosition.updated_at || "")) > 0
    ? fallbackPosition : indexedPosition;
}

// Preserved answer copies (unmatched after reconciliation, divergent portable
// imports, rejected conflicting edits) are recoverable instead of lost.
export async function saveRecovery(id, recovery) {
  const record = { ...recovery, id, saved_at: new Date().toISOString() };
  try {
    await putRecord(RECOVERY_STORE_NAME, record);
  } catch (indexedDBError) {
    try {
      localStorage.setItem(`${FALLBACK_PREFIX}${id}.recovery`, JSON.stringify(record));
    } catch (fallbackError) {
      throw new Error(`Could not preserve the answers locally: ${fallbackError.message || indexedDBError.message}`);
    }
  }
  return record;
}

export async function loadRecovery(id) {
  try {
    return await getRecord(RECOVERY_STORE_NAME, id);
  } catch {
    try {
      const raw = localStorage.getItem(`${FALLBACK_PREFIX}${id}.recovery`);
      return raw ? JSON.parse(raw) : null;
    } catch {
      return null;
    }
  }
}

export async function deleteRecovery(id) {
  try {
    await runTransaction(RECOVERY_STORE_NAME, "readwrite", (store, transaction, state) => {
      store.delete(id);
      state.result = null;
      return state.result;
    });
  } catch { /* The preserved copy may only exist in the fallback store. */ }
  try {
    localStorage.removeItem(`${FALLBACK_PREFIX}${id}.recovery`);
  } catch { /* Fallback storage is unavailable; nothing to remove. */ }
}

// Removes every stored copy of a questionnaire: the session, its navigation
// position, and any preserved recovery record (including fallback mirrors).
export async function deleteSession(id) {
  if (!id) {
    throw new Error("A session identifier is required");
  }
  const fallbackKeys = ["", ".position", ".recovery"];
  try {
    await runTransaction([STORE_NAME, POSITION_STORE_NAME, RECOVERY_STORE_NAME], "readwrite", (stores, transaction, state) => {
      const [sessions, positions, recoveries] = stores;
      sessions.delete(id);
      positions.delete(id);
      recoveries.delete(id);
      state.result = null;
      return state.result;
    });
  } catch {
    try {
      for (const suffix of fallbackKeys) {
        localStorage.removeItem(`${FALLBACK_PREFIX}${id}${suffix}`);
      }
    } catch { /* Fallback storage is unavailable. */ }
    throw new Error("Could not delete the stored questionnaire");
  }
  for (const suffix of fallbackKeys) {
    try {
      localStorage.removeItem(`${FALLBACK_PREFIX}${id}${suffix}`);
    } catch { /* Fallback storage is unavailable. */ }
  }
}

export async function listRecentSessions(limit = 8) {
  let indexedSessions = [];
  let fallbackSessions = [];
  try {
    indexedSessions = await getAllRecords(STORE_NAME);
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
    if (!existing || normalizeRevision(candidate.revision) > normalizeRevision(existing.revision)) {
      byID.set(candidate.id, candidate);
    }
  }

  return [...byID.values()]
    .sort((left, right) => String(right.updated_at).localeCompare(String(left.updated_at)))
    .slice(0, limit);
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
    if (!key?.startsWith(FALLBACK_PREFIX) || key.endsWith(".position") || key.endsWith(".recovery")) {
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
