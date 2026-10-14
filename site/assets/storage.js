"use strict";

const DATABASE_NAME = "runyth-born";
const DATABASE_VERSION = 4;
const STORE_NAME = "sessions";
const POSITION_STORE_NAME = "positions";
const RECOVERY_STORE_NAME = "recoveries";
const DELETION_STORE_NAME = "deleted";
const FALLBACK_PREFIX = "runyth-born.session.v2.";

export class StorageConflictError extends Error {
  constructor() {
    super("A newer version of this questionnaire was saved elsewhere");
    this.name = "StorageConflictError";
  }
}

export class SessionDeletedError extends Error {
  constructor() {
    super("This questionnaire was deleted from this browser");
    this.name = "SessionDeletedError";
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
      for (const storeName of [STORE_NAME, POSITION_STORE_NAME, RECOVERY_STORE_NAME, DELETION_STORE_NAME]) {
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
  // The legacy fallback keys are migrated even when IndexedDB is unavailable:
  // that is exactly the compatibility situation where the fallback store is
  // the only usable copy (F06).
  const legacySessions = [];
  if (globalThis.indexedDB) {
    try {
      legacySessions.push(...await readLegacySessions());
    } catch { /* Legacy IndexedDB is unreadable; the fallback path still applies. */ }
  }
  const legacyFallbackSessions = readLegacyFallbackSessions();
  if (legacySessions.length === 0 && legacyFallbackSessions.length === 0) {
    return;
  }

  let database = null;
  let indexedSessions = [];
  if (globalThis.indexedDB) {
    try {
      database = await openRawDatabase();
      indexedSessions = await transactionOn(database, STORE_NAME, "readonly", getAllOperate());
    } catch {
      database = null;
      indexedSessions = [];
    }
  }
  let fallbackSessions = [];
  try {
    fallbackSessions = fallbackList();
  } catch { /* Fallback storage is unavailable. */ }

  const records = mergeLegacyRecords({ legacySessions, legacyFallbackSessions, indexedSessions, fallbackSessions });
  for (const record of records) {
    // The deletion check runs against the already-open migration connection
    // so it never re-enters the migration itself (F11).
    const indexedDeletion = database
      ? await transactionOn(database, DELETION_STORE_NAME, "readonly", getOperate(record.id)).catch(() => false)
      : false;
    if (readFallbackTombstone(record.id) || indexedDeletion) {
      continue;
    }
    let stored = false;
    if (database) {
      try {
        await transactionOn(database, STORE_NAME, "readwrite", putOperate(record));
        stored = true;
      } catch { /* Fall through to the fallback store. */ }
    }
    if (!stored) {
      try {
        fallbackSave(record);
      } catch { /* Neither store could accept the migrated record. */ }
    }
  }
  database?.close();
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
// starting revision so migrated sessions participate in save ordering. When
// both legacy stores hold one questionnaire, the fresher wall-clock copy is
// migrated — the compatibility rule the legacy application used — and the
// unselected original remains recoverable in its legacy store (F06).
export function mergeLegacyRecords({ legacySessions = [], legacyFallbackSessions = [], indexedSessions = [], fallbackSessions = [] } = {}) {
  const known = new Set([...indexedSessions, ...fallbackSessions]
    .map((entry) => entry?.id)
    .filter(Boolean));
  const byID = new Map();
  for (const legacy of [...legacySessions, ...legacyFallbackSessions]) {
    if (!legacy?.id || known.has(legacy.id)) {
      continue;
    }
    const existing = byID.get(legacy.id);
    if (!existing || String(legacy.updated_at || "").localeCompare(String(existing.updated_at || "")) > 0) {
      byID.set(legacy.id, legacy);
    }
  }
  return [...byID.values()].map((legacy) => ({ ...structuredClone(legacy), revision: normalizeRevision(legacy.revision) || 1 }));
}

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
        : state.deleted ? new SessionDeletedError()
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
  const leftRevision = normalizeRevision(left.revision);
  const rightRevision = normalizeRevision(right.revision);
  if (leftRevision !== rightRevision) {
    return rightRevision > leftRevision ? right : left;
  }
  // Equal revisions: the legacy application ordered copies by wall-clock
  // timestamp; keep that compatibility rule and prefer the primary store on
  // indistinguishable ties.
  const fallbackNewer = String(right.updated_at || "").localeCompare(String(left.updated_at || "")) > 0;
  return fallbackNewer ? right : left;
}

// Answers decide divergence between two copies of one session; timestamps and
// positions never do (F01/F03).
export function recordsDiverge(left, right) {
  if (!left || !right) { return false; }
  if (String(left.player_name || "") !== String(right.player_name || "")) { return true; }
  if (Boolean(left.completed) !== Boolean(right.completed)) { return true; }
  const leftAnswers = new Map((left.questions || []).map((question) => [String(question.id || ""), String(question.answer ?? "")]));
  for (const question of right.questions || []) {
    if ((leftAnswers.get(String(question.id || "")) ?? "") !== String(question.answer ?? "")) { return true; }
  }
  const answeredCount = (questions) => (questions || []).filter((item) => String(item.answer || "").trim() !== "").length;
  return answeredCount(left.questions) !== answeredCount(right.questions);
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
// carries a monotonic revision. Authority is established across BOTH storage
// copies before accepting a write (F01): a stale tab must not write over a
// newer acknowledged save that lives in the other store, and a recovered
// primary store must not overwrite newer fallback state. Wall-clock timestamps
// are display metadata only and never decide answer authority (F03). A
// deletion marker invalidates every earlier writer generation, so a queued
// save can never recreate a removed session (F11).
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

  if (await isSessionDeleted(copy.id)) {
    throw new SessionDeletedError();
  }

  const fallbackRecord = readFallbackRecord(copy.id);
  const fallbackRevision = normalizeRevision(fallbackRecord?.revision);
  if (fallbackRevision > copy.revision) {
    throw new StorageConflictError();
  }

  try {
    const saved = await saveSessionRecord(copy, fallbackRevision);
    session.revision = saved.revision;
    return saved;
  } catch (error) {
    if (error instanceof StorageConflictError || error instanceof SessionDeletedError) {
      throw error;
    }
    try {
      const saved = fallbackSaveRecord(copy);
      session.revision = saved.revision;
      return saved;
    } catch (fallbackError) {
      if (fallbackError instanceof StorageConflictError || fallbackError instanceof SessionDeletedError) {
        throw fallbackError;
      }
      throw new Error(`Could not save locally: ${fallbackError.message || error.message}`);
    }
  }
}

// Reads and updates the stored record inside one transaction so a concurrent
// save cannot slip between the comparison and the write (F01). The fallback
// copy's revision participates in the bump so the primary never ties or lands
// below a newer acknowledged fallback save, and the deletion marker inside the
// same transaction invalidates a writer that started before the deletion (F11).
function saveSessionRecord(copy, fallbackRevision) {
  return runTransaction([STORE_NAME, DELETION_STORE_NAME], "readwrite", (stores, transaction, state) => {
    const [sessions, deletions] = stores;
    const deletionRequest = deletions.get(copy.id);
    deletionRequest.onsuccess = () => {
      if (deletionRequest.result) {
        state.deleted = true;
        try { transaction.abort(); } catch { /* aborting after another failure is harmless */ }
        return;
      }
      const request = sessions.get(copy.id);
      request.onsuccess = () => {
        const storedRevision = normalizeRevision(request.result?.revision);
        if (storedRevision > copy.revision) {
          state.conflict = true;
          try { transaction.abort(); } catch { /* aborting after another failure is harmless */ }
          return;
        }
        copy.revision = Math.max(storedRevision, fallbackRevision, copy.revision) + 1;
        state.result = copy;
        sessions.put(copy);
      };
      return state.result;
    };
    return state.result;
  });
}

function fallbackSaveRecord(copy) {
  // The deletion marker is re-checked at execution time so a queued fallback
  // writer that started before the deletion still cannot recreate it (F11).
  if (readFallbackTombstone(copy.id)) {
    throw new SessionDeletedError();
  }
  const storedRevision = normalizeRevision(readFallbackRecord(copy.id)?.revision);
  if (storedRevision > copy.revision) {
    throw new StorageConflictError();
  }
  // The incoming revision already chains this tab's acknowledged history, and
  // the up-front cross-store check rejected anything newer elsewhere (F01), so
  // the fallback copy's own revision is the remaining floor here.
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

  // Equal-revision divergent copies across the two stores are handled
  // explicitly: the selected record loads, and the other copy is preserved for
  // review instead of being silently buried (F01). Preservation is awaited so
  // the caller's own reconciliation writes cannot race it away.
  if (indexedSession && fallbackSession
    && normalizeRevision(indexedSession.revision) === normalizeRevision(fallbackSession.revision)
    && recordsDiverge(indexedSession, fallbackSession)) {
    try {
      await stashDivergentCopy(id, selected === indexedSession ? fallbackSession : indexedSession);
    } catch {
      // A failed preservation must not block loading the selected copy.
    }
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
// imports, rejected conflicting edits) are recoverable instead of lost. The
// write reads, merges, and commits inside one IndexedDB transaction so two
// concurrent writers serially merge instead of replacing each other's
// acknowledged additions (F02); the fallback copy re-checks after writing and
// merges an interfering writer back. Deletion markers gate the write in both
// paths so a pending preservation cannot repopulate a removed questionnaire
// (F11).
export async function saveRecovery(id, recovery) {
  if (await isSessionDeleted(id)) {
    throw new SessionDeletedError();
  }
  const incoming = {
    id,
    reason: recovery?.reason ?? null,
    fingerprint: recovery?.fingerprint ?? null,
    entries: answeredEntries(recovery?.entries),
    saved_at: new Date().toISOString(),
  };
  try {
    return await runTransaction([RECOVERY_STORE_NAME, DELETION_STORE_NAME], "readwrite", (stores, transaction, state) => {
      const [recoveries, deletions] = stores;
      const deletionRequest = deletions.get(id);
      deletionRequest.onsuccess = () => {
        if (deletionRequest.result) {
          state.deleted = true;
          try { transaction.abort(); } catch { /* aborting after another failure is harmless */ }
          return;
        }
        const request = recoveries.get(id);
        request.onsuccess = () => {
          const merged = mergeRecoveryRecords(request.result, incoming, id);
          state.result = merged;
          recoveries.put(merged);
        };
      };
      return state.result;
    });
  } catch (error) {
    if (error instanceof StorageConflictError || error instanceof SessionDeletedError) {
      throw error;
    }
    try {
      return await fallbackSaveRecovery(id, incoming);
    } catch (fallbackError) {
      if (fallbackError instanceof SessionDeletedError) {
        throw fallbackError;
      }
      throw new Error(`Could not preserve the answers locally: ${fallbackError.message || error.message}`);
    }
  }
}

// The fallback store has no transactions, so independently executing documents
// must never share one replaceable recovery row. A token stored in
// sessionStorage is not a unique writer: opener-created documents inherit a
// copy of that storage, and a response-policy mismatch can then separate
// their opener groups while leaving the copied token in place (F02). Each
// live document therefore generates its own identity in module memory.
// Reloads create a new identity; readers still union every existing row.
let fallbackWriterToken = null;

function writerToken() {
  if (!fallbackWriterToken) {
    fallbackWriterToken = globalThis.crypto?.randomUUID?.()
      ?? `w${Math.random().toString(36).slice(2)}${Date.now().toString(36)}`;
  }
  return fallbackWriterToken;
}

function recoveryRowPrefix(id) {
  return `${FALLBACK_PREFIX}${id}.recovery`;
}

// Reads every fallback recovery row for an identifier: the legacy shared row
// and each per-writer row, skipping malformed records (F02).
function readFallbackRecoveryRows(id) {
  const rows = [];
  const prefix = recoveryRowPrefix(id);
  try {
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index);
      if (!key || (key !== prefix && !key.startsWith(`${prefix}.`))) {
        continue;
      }
      try {
        const raw = localStorage.getItem(key);
        if (raw) {
          rows.push(JSON.parse(raw));
        }
      } catch { /* A malformed row cannot hide the other writers' rows. */ }
    }
  } catch { /* Fallback storage is unavailable, so no fallback rows exist. */ }
  return rows;
}

function removeFallbackRecoveryRows(id) {
  for (const prefix of [FALLBACK_PREFIX, LEGACY_FALLBACK_PREFIX]) {
    const rowPrefix = `${prefix}${id}.recovery`;
    const keys = [];
    try {
      for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (key === rowPrefix || key?.startsWith(`${rowPrefix}.`)) {
          keys.push(key);
        }
      }
      for (const key of keys) {
        localStorage.removeItem(key);
      }
    } catch { /* Fallback storage is unavailable, so it held no removable copies. */ }
  }
}

async function fallbackSaveRecovery(id, incoming) {
  const key = `${recoveryRowPrefix(id)}.${writerToken()}`;
  if (readFallbackTombstone(id)) {
    throw new SessionDeletedError();
  }
  let ownRow = null;
  try {
    const raw = localStorage.getItem(key);
    ownRow = raw ? JSON.parse(raw) : null;
  } catch {
    ownRow = null;
  }
  const merged = mergeRecoveryRecords(ownRow, incoming, id);
  try {
    localStorage.setItem(key, JSON.stringify(merged));
  } catch (fallbackError) {
    throw new Error(`Could not preserve the answers locally: ${fallbackError.message}`);
  }
  if (readFallbackTombstone(id)) {
    // A deletion landed while this row was written; drop it so a deleted
    // questionnaire cannot regain preserved answers (F11).
    try { localStorage.removeItem(key); } catch { /* already gone */ }
    throw new SessionDeletedError();
  }
  return merged;
}

export async function loadRecovery(id) {
  // Every copy is consulted: the primary record, the legacy shared fallback
  // row, and each tab's own fallback row, so an addition acknowledged by any
  // writer stays reviewable. The fold normalizes every stored shape (F02).
  const records = [];
  try {
    const indexed = await getRecord(RECOVERY_STORE_NAME, id);
    if (indexed) {
      records.push(indexed);
    }
  } catch { /* The primary store is unavailable; fallback rows still count. */ }
  records.push(...readFallbackRecoveryRows(id));
  return records.reduce((merged, record) => mergeRecoveryRecords(merged, record, id), null);
}

// Normalizes any stored recovery shape to the current `entries` contract.
// Records from the intermediate implementation stored `questions` instead
// (F02); every recognized answer from every recognized shape survives, and a
// record without recognized answers yields nothing.
function normalizeRecoveryRecord(record, id) {
  if (!record || typeof record !== "object") {
    return null;
  }
  const collected = [];
  if (Array.isArray(record.entries)) {
    collected.push(...record.entries);
  }
  if (Array.isArray(record.questions)) {
    collected.push(...record.questions);
  }
  const entries = answeredEntries(collected);
  if (entries.length === 0) {
    return null;
  }
  return {
    id,
    reason: record.reason ?? null,
    fingerprint: record.fingerprint ?? null,
    saved_at: record.saved_at ?? null,
    entries,
  };
}

// Merges both stored copies into one reviewable record. Copies are never
// ordered by timestamp in a way that can lose an acknowledged copy (F02):
// entries are unioned, identical answers collapse, divergent answers stay
// side by side for review, and the display metadata comes from the copy with
// the newer recorded save time.
export function mergeRecoveryRecords(primaryRecord, fallbackRecord, id) {
  const primary = normalizeRecoveryRecord(primaryRecord, id);
  const fallback = normalizeRecoveryRecord(fallbackRecord, id);
  if (!primary) { return fallback; }
  if (!fallback) { return primary; }
  const fallbackNewer = String(fallbackRecord.saved_at || "").localeCompare(String(primaryRecord.saved_at || "")) > 0;
  const head = fallbackNewer ? fallback : primary;
  return {
    id,
    reason: head.reason,
    fingerprint: head.fingerprint,
    saved_at: head.saved_at,
    entries: dedupeEntries([...primary.entries, ...fallback.entries]),
  };
}

export async function deleteRecovery(id) {
  try {
    await runTransaction(RECOVERY_STORE_NAME, "readwrite", (store, transaction, state) => {
      store.delete(id);
      state.result = null;
      return state.result;
    });
  } catch { /* The preserved copy may only exist in the fallback store. */ }
  removeFallbackRecoveryRows(id);
}

// Removes every stored copy of a questionnaire: the session, its navigation
// position, any preserved recovery record (including fallback mirrors), and
// the copies left by the legacy migration, so a deleted questionnaire cannot
// return on the next open (F11). A deletion marker is written before the
// records are removed, in both stores, so an already-queued writer from any
// tab is invalidated instead of recreating the session; the marker holds only
// the identifier and deletion time, never answer content (F11). Cleanup is
// reported as incomplete only when an IndexedDB copy may survive; a missing
// IndexedDB with a successful fallback cleanup is a complete deletion.
export async function deleteSession(id) {
  if (!id) {
    throw new Error("A session identifier is required");
  }
  // The marker is the durable invalidation every later writer consults, so
  // deletion is only acknowledged once one store holds it. Fallback storage
  // may refuse under quota pressure; the marker is retried after the removal
  // frees space, and an unrecorded deletion reports incompleteness (F11).
  let durableIndexedDBMarker = false;
  try {
    localStorage.setItem(fallbackTombstoneKey(id), JSON.stringify({ id, deleted_at: new Date().toISOString() }));
  } catch { /* The fallback tombstone may be unwritable; the IndexedDB marker still applies. */ }

  let removedIndexedDB = true;
  if (globalThis.indexedDB) {
    try {
      await runTransaction([STORE_NAME, POSITION_STORE_NAME, RECOVERY_STORE_NAME, DELETION_STORE_NAME], "readwrite", (stores, transaction, state) => {
        const [sessions, positions, recoveries, deletions] = stores;
        // The tombstone is committed together with the record removal so no
        // concurrent writer can observe the session without the marker.
        deletions.put({ id, deleted_at: new Date().toISOString() });
        sessions.delete(id);
        positions.delete(id);
        recoveries.delete(id);
        state.result = null;
        return state.result;
      });
      durableIndexedDBMarker = true;
    } catch {
      removedIndexedDB = false;
    }
    try {
      await deleteLegacySessionRecord(id);
    } catch {
      removedIndexedDB = false;
    }
  }
  try {
    for (const prefix of [FALLBACK_PREFIX, LEGACY_FALLBACK_PREFIX]) {
      for (const suffix of ["", ".position"]) {
        localStorage.removeItem(`${prefix}${id}${suffix}`);
      }
    }
    // Recovery rows include the legacy shared row and every per-writer row.
    removeFallbackRecoveryRows(id);
  } catch { /* Fallback storage is unavailable, so it held no removable copies. */ }
  if (durableIndexedDBMarker || readFallbackTombstone(id)) {
    if (!removedIndexedDB) {
      throw new Error("Could not delete every stored copy of this questionnaire");
    }
    return;
  }
  try {
    localStorage.setItem(fallbackTombstoneKey(id), JSON.stringify({ id, deleted_at: new Date().toISOString() }));
  } catch { /* The fallback marker could not be recorded under storage pressure. */ }
  if (!readFallbackTombstone(id)) {
    throw new Error("Could not record a durable deletion marker; the questionnaire may be recreated by an already-queued writer");
  }
  if (!removedIndexedDB) {
    // The durable invalidation marker alone does not erase surviving primary
    // content: a still-readable session copy is an incomplete deletion (F11).
    throw new Error("Could not delete every stored copy of this questionnaire");
  }
}

async function deleteLegacySessionRecord(id) {
  const legacyDatabases = typeof indexedDB.databases === "function"
    ? await indexedDB.databases().catch(() => null)
    : null;
  if (legacyDatabases && !legacyDatabases.some((entry) => entry?.name === LEGACY_DATABASE_NAME)) {
    return;
  }
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(LEGACY_DATABASE_NAME);
    request.onsuccess = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(STORE_NAME)) {
        database.close();
        resolve();
        return;
      }
      const transaction = database.transaction(STORE_NAME, "readwrite");
      transaction.objectStore(STORE_NAME).delete(id);
      transaction.oncomplete = () => {
        database.close();
        resolve();
      };
      transaction.onabort = () => {
        database.close();
        reject(transaction.error || new Error("Could not delete the legacy questionnaire record"));
      };
      transaction.onerror = () => {
        database.close();
        reject(transaction.error || new Error("Could not delete the legacy questionnaire record"));
      };
    };
    request.onerror = () => resolve();
    request.onblocked = () => reject(new Error("The legacy questionnaire record is blocked by another tab"));
  });
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

function readFallbackRecord(id) {
  try {
    return fallbackLoad(id);
  } catch {
    return null;
  }
}

function fallbackTombstoneKey(id) {
  return `${FALLBACK_PREFIX}${id}.deleted`;
}

function readFallbackTombstone(id) {
  try {
    const raw = localStorage.getItem(fallbackTombstoneKey(id));
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

// Whether a deletion marker exists in either store. A marked identifier is
// unreloadable and unwritable from any tab, which is what stops an
// already-queued save from recreating a removed questionnaire (F11).
export async function isSessionDeleted(id) {
  if (!id) {
    return false;
  }
  if (readFallbackTombstone(id)) {
    return true;
  }
  try {
    const marker = await getRecord(DELETION_STORE_NAME, id);
    return Boolean(marker);
  } catch {
    // An unavailable IndexedDB cannot undo the fallback marker.
    return false;
  }
}

// Preserved-answer entries are the shared shape between the storage layer and
// the wizard review dialog: one record per at-risk answered question.
export function answeredEntries(questions) {
  return (questions || [])
    .filter((item) => String(item.answer || "").trim() !== "")
    .map((item) => ({
      id: String(item.id || ""),
      fallback_id: String(item.fallback_id || ""),
      label: String(item.label || String(item.fallback_id || "").replace(/#\d+$/u, "") || "Unmatched answer"),
      breadcrumb: Array.isArray(item.breadcrumb) ? item.breadcrumb.map(String) : [],
      answer: String(item.answer || ""),
    }));
}

export function dedupeEntries(entries) {
  const seen = new Set();
  const result = [];
  for (const entry of entries) {
    const key = [...entry.breadcrumb, entry.label, entry.answer].join("\u0000");
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(entry);
  }
  return result;
}

async function stashDivergentCopy(id, record) {
  const existing = await loadRecovery(id).catch(() => null);
  // The unselected copy is a whole session record: its preserved answers are
  // the real `questions` entries, which is where session answers live (F01).
  const entries = dedupeEntries([...(existing?.entries || []), ...answeredEntries(record?.questions)]);
  if (entries.length === 0) {
    return null;
  }
  return saveRecovery(id, {
    reason: existing?.reason || "divergent",
    fingerprint: existing?.fingerprint ?? null,
    entries,
  });
}

function fallbackList() {
  const sessions = [];
  for (let index = 0; index < localStorage.length; index += 1) {
    const key = localStorage.key(index);
    if (!key?.startsWith(FALLBACK_PREFIX) || key.endsWith(".position") || key.includes(".recovery") || key.endsWith(".deleted")) {
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
