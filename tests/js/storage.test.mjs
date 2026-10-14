import assert from "node:assert/strict";
import test from "node:test";

import { mergeLegacyRecords } from "../../site/assets/storage.js";

const sessionKey = "runyth-born.session.v2.";
const legacySessionKey = "template-wizard-web.session.v2.";

// Manual mock: request callbacks and the transaction completion are fired by
// the test, which is what lets a test assert between the write and the commit.
// Saves now read their deletion marker first, so each step fires the pending
// request of the transaction in flight.
const tick = () => new Promise((resolve) => setImmediate(resolve));

async function storageMock() {
  const fallback = new Map();
  globalThis.localStorage = {
    setItem: (key, value) => fallback.set(key, String(value)),
    getItem: (key) => (fallback.has(key) ? fallback.get(key) : null),
    removeItem: (key) => fallback.delete(key),
    key: (index) => [...fallback.keys()][index] ?? null,
    get length() { return fallback.size; },
  };
  const records = new Map();
  const stores = new Map([["sessions", records]]);
  let transaction;
  let lastRequest;
  globalThis.indexedDB = {
    databases: async () => [...stores.keys()].map((name) => ({ name })),
    open(name) {
      const open = {};
      queueMicrotask(() => {
        open.result = {
          objectStoreNames: { contains: (store) => (stores.get(name) ?? new Map()).has(store) },
          close() {},
          transaction() {
            transaction = {
              error: null,
              abort() {
                transaction.error = new Error("transaction aborted");
                transaction.onabort?.();
              },
              objectStore(storeName) {
                if (!stores.has(name)) {
                  stores.set(name, new Map());
                }
                const map = stores.get(name);
                return {
                  get(id) { lastRequest = { result: map.has(id) ? structuredClone(map.get(id)) : null }; return lastRequest; },
                  getAll() { lastRequest = { result: [...map.values()].map((record) => structuredClone(record)) }; return lastRequest; },
                  put(record) { lastRequest = {}; queueMicrotask(() => { map.set(record.id, structuredClone(record)); }); return lastRequest; },
                  delete(id) { lastRequest = {}; queueMicrotask(() => { map.delete(id); }); return lastRequest; },
                };
              },
            };
            return transaction;
          },
        };
        open.onsuccess();
      });
      return open;
    },
  };
  const module = await import(`../../site/assets/storage.js?case=${Math.random()}`);
  return { module, fallback, records, stores, transaction: () => transaction, request: () => lastRequest };
}

// Automatic mock: behaves like real IndexedDB (requests fire, then the
// transaction commits), so tests can simply await the storage API. Each named
// database keeps its own stores, which lets tests seed the legacy namespace.
// `noIndexedDB: true` removes IndexedDB entirely, matching privacy settings
// where only the localStorage fallback works.
async function autoStorageMock({ storedSessions = [], failTransactions = false, failDeletionTransaction = false, abortAfterPut = false, legacySessions = [], legacyFallbackSessions = [], noIndexedDB = false } = {}) {
  const fallback = new Map();
  globalThis.localStorage = {
    setItem: (key, value) => fallback.set(key, String(value)),
    getItem: (key) => (fallback.has(key) ? fallback.get(key) : null),
    removeItem: (key) => fallback.delete(key),
    key: (index) => [...fallback.keys()][index] ?? null,
    get length() { return fallback.size; },
  };
  const databases = new Map();
  const storesFor = (name) => {
    if (!databases.has(name)) {
      databases.set(name, new Map());
    }
    return databases.get(name);
  };
  const stores = storesFor("runyth-born");
  stores.set("sessions", new Map(storedSessions.map((record) => [record.id, structuredClone(record)])));
  if (legacySessions.length > 0) {
    storesFor("template-wizard-web").set("sessions", new Map(legacySessions.map((record) => [record.id, structuredClone(record)])));
  }
  for (const legacy of legacyFallbackSessions) {
    fallback.set(legacySessionKey + legacy.id, JSON.stringify(legacy));
  }
  if (noIndexedDB) {
    globalThis.indexedDB = undefined;
  } else {
    globalThis.indexedDB = {
    databases: async () => [...databases.keys()].map((name) => ({ name })),
    open(name) {
      const open = {};
      queueMicrotask(() => {
        open.result = {
          objectStoreNames: { contains: (store) => storesFor(name).has(store) },
          close() {},
          transaction(storeNames) {
            if (failTransactions) {
              throw new Error("forced IndexedDB failure");
            }
            if (failDeletionTransaction && Array.isArray(storeNames) && storeNames.includes("deleted")) {
              // Models a transient failure of the primary deletion transaction
              // while ordinary reads stay available (F11).
              throw new Error("forced primary deletion failure");
            }
            const currentTransaction = {
              error: null,
              abort() {
                currentTransaction.error = new Error("transaction aborted");
                currentTransaction.onabort?.();
              },
              objectStore(storeName) {
                const map = storesFor(name);
                if (!map.has(storeName)) {
                  map.set(storeName, new Map());
                }
                const recordsForStore = map.get(storeName);
                const issue = (request) => {
                  queueMicrotask(() => {
                    if (request.rollback) {
                      currentTransaction.onabort?.();
                      return;
                    }
                    request.apply?.();
                    request.onsuccess?.();
                  });
                  return request;
                };
                return {
                  get: (id) => issue({ result: recordsForStore.has(id) ? structuredClone(recordsForStore.get(id)) : null }),
                  getAll: () => issue({ result: [...recordsForStore.values()].map((record) => structuredClone(record)) }),
                  put: (record) => issue({ rollback: abortAfterPut, apply: () => recordsForStore.set(record.id, structuredClone(record)), result: structuredClone(record) }),
                  delete: (id) => issue({ apply: () => recordsForStore.delete(id) }),
                };
              },
            };
            setTimeout(() => currentTransaction.oncomplete?.(), 0);
            return currentTransaction;
          },
        };
        open.onsuccess();
      });
      return open;
    },
    };
  }
  const module = await import(`../../site/assets/storage.js?case=${Math.random()}`);
  return { module, fallback, stores, legacyStores: storesFor("template-wizard-web") };
}

test("IndexedDB save waits for transaction commit", async () => {
  const mock = await storageMock();
  let done = false;
  const saving = mock.module.saveSession({ id: "test", updated_at: "" }).then(() => { done = true; });
  await tick();
  // First request: the deletion-marker read of the save precheck (F11).
  mock.request().onsuccess?.();
  await tick();
  assert.equal(done, false);
  mock.transaction().oncomplete?.();
  await tick();
  assert.equal(done, false);
  // The write transaction: marker read, record read, record modification.
  mock.request().onsuccess?.();
  await tick();
  mock.request().onsuccess?.();
  await tick();
  assert.equal(done, false, "the save is not acknowledged before the commit");
  mock.transaction().oncomplete?.();
  await saving;
  assert.equal(done, true);
});

test("an abort after a successful put falls back to localStorage", async () => {
  const mock = await storageMock();
  const saving = mock.module.saveSession({ id: "test", updated_at: "" });
  await tick();
  mock.request().onsuccess?.();
  await tick();
  mock.transaction().oncomplete?.();
  await tick();
  // The write transaction: marker read, record read, then the record put.
  mock.request().onsuccess?.();
  await tick();
  mock.request().onsuccess?.();
  await tick();
  mock.transaction().error = new Error("quota exhausted");
  mock.transaction().onabort?.();
  await saving;
  assert.equal(mock.fallback.size, 1);
});

test("same-revision saves advance the stored revision and sync the caller", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 2, answer: "kept" }] });
  const session = { id: "test", revision: 2, questions: [] };
  const saved = await mock.module.saveSession(session);
  assert.equal(saved.revision, 3);
  assert.equal(session.revision, 3);
  assert.equal(mock.stores.get("sessions").get("test").revision, 3);
});

test("a stale copy cannot overwrite a newer acknowledged save", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 5, answer: "newest" }] });
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 2, answer: "stale", questions: [] }),
    (error) => error.name === "StorageConflictError",
  );
  assert.equal(mock.stores.get("sessions").get("test").answer, "newest");
  assert.equal(mock.fallback.size, 0);
});

test("a failing IndexedDB transaction falls back to localStorage", async () => {
  const mock = await autoStorageMock({ failTransactions: true });
  const session = { id: "test", questions: [] };
  await mock.module.saveSession(session);
  const stored = JSON.parse(mock.fallback.get(sessionKey + "test"));
  assert.equal(stored.revision, 1);
  assert.equal(session.revision, 1);
});

test("a stale copy cannot overwrite a newer acknowledged fallback save", async () => {
  const mock = await autoStorageMock({ failTransactions: true });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 5, answer: "newest" }));
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 2, answer: "stale", questions: [] }),
    (error) => error.name === "StorageConflictError",
  );
  assert.equal(JSON.parse(mock.fallback.get(sessionKey + "test")).answer, "newest");
});

test("loading prefers the higher revision regardless of wall-clock timestamps", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1, answer: "IndexedDB copy", updated_at: "2999-01-01T00:00:00.000Z" }] });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 2, answer: "Fallback copy", updated_at: "2020-01-01T00:00:00.000Z" }));
  const loaded = await mock.module.loadSession("test");
  assert.equal(loaded.answer, "Fallback copy");
});

test("a tied update resolves by display timestamp and prefers the primary on indistinguishable ties", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 2, answer: "IndexedDB copy", updated_at: "2999-01-01T00:00:00.000Z" }] });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 2, answer: "Fallback copy", updated_at: "2020-01-01T00:00:00.000Z" }));
  const loaded = await mock.module.loadSession("test");
  assert.equal(loaded.answer, "IndexedDB copy");
});

test("navigation positions persist separately from answers", async () => {
  const mock = await autoStorageMock();
  await mock.module.saveSessionPosition("test", 4);
  const position = await mock.module.loadSessionPosition("test");
  assert.equal(position.position, 4);
  assert.equal(mock.stores.get("positions").get("test").position, 4);
});

test("a failing IndexedDB position save falls back to localStorage", async () => {
  const mock = await autoStorageMock({ failTransactions: true });
  await mock.module.saveSessionPosition("test", 4);
  const position = await mock.module.loadSessionPosition("test");
  assert.equal(position.position, 4);
  assert.equal(mock.fallback.has(sessionKey + "test.position"), true);
});

test("preserved answer copies are stored and recoverable", async () => {
  const mock = await autoStorageMock();
  await mock.module.saveRecovery("test", { reason: "reconcile", entries: [{ label: "Name", answer: "Akito" }] });
  const loaded = await mock.module.loadRecovery("test");
  assert.equal(loaded.reason, "reconcile");
  assert.deepEqual(loaded.entries, [{ id: "", fallback_id: "", label: "Name", breadcrumb: [], answer: "Akito" }]);
});

test("a fallback recovery record stays reviewable after the primary store recovers", async () => {
  const mock = await autoStorageMock();
  mock.fallback.set(sessionKey + "test.recovery", JSON.stringify({
    id: "test", reason: "conflict", saved_at: "2026-01-02T00:00:00.000Z", entries: [{ label: "Name", answer: "Preserved fallback answer" }],
  }));
  const loaded = await mock.module.loadRecovery("test");
  assert.equal(loaded.reason, "conflict");
  assert.equal(loaded.entries[0].answer, "Preserved fallback answer");
});

test("a newer fallback recovery record wins over an older primary record", async () => {
  const mock = await autoStorageMock();
  mock.stores.set("recoveries", new Map([["test", {
    id: "test", reason: "reconcile", saved_at: "2026-01-01T00:00:00.000Z", entries: [{ label: "Name", answer: "older" }],
  }]]));
  mock.fallback.set(sessionKey + "test.recovery", JSON.stringify({
    id: "test", reason: "import", saved_at: "2026-01-02T00:00:00.000Z", entries: [{ label: "Name", answer: "newer" }],
  }));
  const loaded = await mock.module.loadRecovery("test");
  assert.equal(loaded.reason, "import");
  // Both copies stay reviewable; the fallback copy is never suppressed (F02).
  assert.ok(loaded.entries.some((entry) => entry.answer === "newer"));
  assert.ok(loaded.entries.some((entry) => entry.answer === "older"));
});

test("a recovery record from the prior schema is visible and upgraded on the next write", async () => {
  const mock = await autoStorageMock();
  mock.stores.set("recoveries", new Map([["test", {
    id: "test",
    reason: "reconcile",
    saved_at: "2026-01-01T00:00:00.000Z",
    questions: [{ id: "q1", label: "Name", answer: "Previously preserved answer" }],
  }]]));
  const loaded = await mock.module.loadRecovery("test");
  assert.equal(loaded.reason, "reconcile");
  assert.ok(loaded.entries.some((entry) => entry.answer === "Previously preserved answer"));

  // Opening a new preservation branches merges instead of replacing, so the
  // old-schema answers never vanish through an overwrite (F02).
  await mock.module.saveRecovery("test", { reason: "import", entries: [{ label: "Age", answer: "Divergent import answer" }] });
  const stored = mock.stores.get("recoveries").get("test");
  assert.equal(stored.questions, undefined, "the record upgrades to the current entries shape");
  assert.equal(stored.reason, "import");
  const merged = await mock.module.loadRecovery("test");
  assert.ok(merged.entries.some((entry) => entry.answer === "Previously preserved answer"));
  assert.ok(merged.entries.some((entry) => entry.answer === "Divergent import answer"));
});

test("a fallback old-schema recovery record upgrades through a fallback write", async () => {
  const mock = await autoStorageMock({ failTransactions: true });
  mock.fallback.set(sessionKey + "test.recovery", JSON.stringify({
    id: "test", reason: "reconcile", saved_at: "2026-01-01T00:00:00.000Z",
    questions: [{ id: "q1", label: "Name", answer: "Old fallback answer" }],
  }));
  await mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Age", answer: "Rejected edit" }] });
  // The legacy shared row stays intact; the tab writes only its own row, and
  // the reviewable union keeps both answers (F02).
  const legacy = JSON.parse(mock.fallback.get(sessionKey + "test.recovery"));
  assert.equal(legacy.questions[0].answer, "Old fallback answer");
  const ownRowKey = [...mock.fallback.keys()].find((key) => key.startsWith(sessionKey + "test.recovery."));
  assert.ok(ownRowKey, "the tab records its preservation in its own row");
  const loaded = await mock.module.loadRecovery("test");
  assert.ok(loaded.entries.some((entry) => entry.answer === "Old fallback answer"));
  assert.ok(loaded.entries.some((entry) => entry.answer === "Rejected edit"));
});

test("a clock-rolled-back fallback recovery record is never suppressed", async () => {
  const mock = await autoStorageMock();
  mock.stores.set("recoveries", new Map([["test", {
    id: "test", reason: "reconcile", saved_at: "2026-01-01T00:00:00.000Z", entries: [{ label: "Name", answer: "Older recovery copy" }],
  }]]));
  mock.fallback.set(sessionKey + "test.recovery", JSON.stringify({
    id: "test", reason: "conflict", saved_at: "2025-12-31T23:00:00.000Z", entries: [{ label: "Name", answer: "New acknowledged recovery copy" }],
  }));
  const loaded = await mock.module.loadRecovery("test");
  assert.ok(loaded.entries.some((entry) => entry.answer === "Older recovery copy"));
  assert.ok(loaded.entries.some((entry) => entry.answer === "New acknowledged recovery copy"));
});

test("a stale primary write is rejected while a newer acknowledged fallback save exists", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 3, answer: "IndexedDB copy", updated_at: "2026-01-01T00:00:00.000Z" }] });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 4, answer: "Fallback copy", updated_at: "2026-01-02T00:00:00.000Z" }));
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 3, answer: "stale", questions: [] }),
    (error) => error.name === "StorageConflictError",
  );
  assert.equal(mock.stores.get("sessions").get("test").answer, "IndexedDB copy");
  assert.equal(JSON.parse(mock.fallback.get(sessionKey + "test")).answer, "Fallback copy");
});

test("a stale fallback write is rejected while a newer acknowledged IndexedDB save exists", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 5, answer: "newest" }] });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 2, answer: "older fallback" }));
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 2, answer: "stale", questions: [] }),
    (error) => error.name === "StorageConflictError",
  );
  assert.equal(JSON.parse(mock.fallback.get(sessionKey + "test")).answer, "older fallback");
  assert.equal(mock.stores.get("sessions").get("test").answer, "newest");
});

test("a tied divergent pair loads the primary copy and preserves the other for review", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "test", revision: 4, updated_at: "2026-01-03T00:00:00.000Z", questions: [{ id: "q1", label: "Name", answer: "IndexedDB copy" }] }],
  });
  mock.fallback.set(sessionKey + "test", JSON.stringify({
    id: "test", revision: 4, updated_at: "2026-01-02T00:00:00.000Z",
    questions: [{ id: "q1", label: "Name", answer: "Fallback copy" }],
  }));
  const loaded = await mock.module.loadSession("test");
  assert.equal(loaded.questions[0].answer, "IndexedDB copy");
  const recovery = await mock.module.loadRecovery("test");
  assert.ok(recovery, "the divergent fallback copy must be preserved for review");
  assert.ok(recovery.entries.some((entry) => entry.answer === "Fallback copy"), "the unselected real question answers must be preserved (F01)");
});

test("a tied divergent pair loads the fallback copy and preserves the primary for review", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "test", revision: 4, updated_at: "2026-01-02T00:00:00.000Z", questions: [{ id: "q1", label: "Name", answer: "IndexedDB copy" }] }],
  });
  mock.fallback.set(sessionKey + "test", JSON.stringify({
    id: "test", revision: 4, updated_at: "2026-01-03T00:00:00.000Z",
    questions: [{ id: "q1", label: "Name", answer: "Fallback copy" }],
  }));
  const loaded = await mock.module.loadSession("test");
  assert.equal(loaded.questions[0].answer, "Fallback copy");
  const recovery = await mock.module.loadRecovery("test");
  assert.ok(recovery, "the divergent primary copy must be preserved for review");
  assert.ok(recovery.entries.some((entry) => entry.answer === "IndexedDB copy"), "the unselected real question answers must be preserved (F01)");
});

test("preserved tied-copy answers survive a following save", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "test", revision: 4, updated_at: "2026-01-03T00:00:00.000Z", questions: [{ id: "q1", label: "Name", answer: "IndexedDB copy" }] }],
  });
  mock.fallback.set(sessionKey + "test", JSON.stringify({
    id: "test", revision: 4, updated_at: "2026-01-02T00:00:00.000Z",
    questions: [{ id: "q1", label: "Name", answer: "Fallback copy" }],
  }));
  const loaded = await mock.module.loadSession("test");
  await mock.module.saveSession(loaded, { touch: true });
  const recovery = await mock.module.loadRecovery("test");
  assert.ok(recovery, "the preserved copy must survive the following save");
  assert.ok(recovery.entries.some((entry) => entry.answer === "Fallback copy"), "the unselected answers must remain recoverable after saving");
});

test("an aborted transaction write does not keep its record", async () => {
  const mock = await autoStorageMock({ abortAfterPut: true });
  await mock.module.saveSession({ id: "test", updated_at: "", questions: [] });
  assert.equal(mock.stores.get("sessions").has("test"), false);
  assert.equal(mock.fallback.size, 1);
});

test("deleting a session removes the session, position, and recovery copies", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1 }] });
  mock.stores.set("positions", new Map([["test", { id: "test", position: 2 }]]));
  mock.stores.set("recoveries", new Map([["test", { id: "test", questions: [] }]]));
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test" }));
  mock.fallback.set(sessionKey + "test.position", JSON.stringify({ id: "test" }));
  await mock.module.deleteSession("test");
  assert.equal(mock.stores.get("sessions").has("test"), false);
  assert.equal(mock.stores.get("positions").has("test"), false);
  assert.equal(mock.stores.get("recoveries").has("test"), false);
  assert.equal(mock.fallback.has(sessionKey + "test"), false);
  assert.equal(mock.fallback.has(sessionKey + "test.position"), false);
  assert.equal(mock.fallback.has(sessionKey + "test.deleted"), true, "the deletion marker survives for queued writers");
  assert.equal(mock.stores.get("deleted").has("test"), true, "the IndexedDB deletion marker survives for queued writers");
});

test("a deletion marker holds no answer content", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1, questions: [{ label: "Name", answer: "Private answer" }] }] });
  await mock.module.deleteSession("test");
  const marker = mock.stores.get("deleted").get("test");
  assert.deepEqual(Object.keys(marker).sort(), ["deleted_at", "id"]);
  const fallbackMarker = JSON.parse(mock.fallback.get(sessionKey + "test.deleted"));
  assert.deepEqual(Object.keys(fallbackMarker).sort(), ["deleted_at", "id"]);
});

test("a stale in-memory copy cannot recreate a deleted session", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1 }] });
  await mock.module.deleteSession("test");
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 2, questions: [{ label: "Name", answer: "Private edit entered before deletion" }] }),
    (error) => error.name === "SessionDeletedError",
  );
  assert.equal(mock.stores.get("sessions").has("test"), false);
  assert.equal(mock.fallback.has(sessionKey + "test"), false);
});

test("a stale in-memory copy cannot recreate a deleted fallback-only session", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  await mock.module.saveSession({ id: "test", revision: 1, questions: [] });
  await mock.module.deleteSession("test");
  assert.equal(mock.fallback.has(sessionKey + "test"), false);
  assert.equal(mock.fallback.has(sessionKey + "test.deleted"), true);
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 2, questions: [{ label: "Name", answer: "Private edit entered before deletion" }] }),
    (error) => error.name === "SessionDeletedError",
  );
  assert.equal(mock.fallback.has(sessionKey + "test"), false);
});

test("migration skips tombstoned legacy identifiers", async () => {
  const mock = await autoStorageMock({
    legacySessions: [{ id: "test", answer: "legacy", template_name: "Legacy", questions: [] }],
  });
  // A deletion marker for the same id from an earlier removal.
  mock.fallback.set(sessionKey + "test.deleted", JSON.stringify({ id: "test", deleted_at: new Date().toISOString() }));
  await mock.module.listRecentSessions(20);
  assert.equal(mock.stores.get("sessions").has("test"), false, "a tombstoned identifier must not migrate back");
});

test("recent sessions list only real sessions, not position or recovery keys", async () => {
  const mock = await autoStorageMock({ failTransactions: true });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 1, template_name: "Example", updated_at: "2026-01-02T00:00:00.000Z" }));
  mock.fallback.set(sessionKey + "test.position", JSON.stringify({ id: "test", position: 1 }));
  mock.fallback.set(sessionKey + "test.recovery", JSON.stringify({ id: "test", questions: [] }));
  const recent = await mock.module.listRecentSessions();
  assert.deepEqual(recent.map((entry) => entry.id), ["test"]);
});

test("legacy records merge only when the new namespace has no copy", () => {
  const records = mergeLegacyRecords({
    legacySessions: [{ id: "legacy-idb", answer: "one" }, { id: "known", answer: "superseded" }],
    legacyFallbackSessions: [{ id: "legacy-fallback", answer: "two" }, { id: "known", answer: "ignored" }],
    indexedSessions: [{ id: "known", revision: 9 }],
  });
  assert.deepEqual(records.map((record) => record.id), ["legacy-idb", "legacy-fallback"]);
  assert.ok(records.every((record) => record.revision === 1));
});

test("opening a store migrates seeded legacy sessions into the new namespace", async () => {
  const mock = await autoStorageMock({
    legacySessions: [{ id: "legacy-idb", answer: "one", template_name: "Legacy", questions: [] }],
    legacyFallbackSessions: [{ id: "legacy-fallback", answer: "two", template_name: "Legacy", questions: [] }],
  });
  await mock.module.listRecentSessions(20);
  assert.equal(mock.stores.get("sessions").get("legacy-idb").answer, "one");
  assert.equal(mock.stores.get("sessions").get("legacy-fallback").answer, "two");
  assert.equal(mock.fallback.has(legacySessionKey + "legacy-fallback"), true);
  const loaded = await mock.module.loadSession("legacy-idb");
  assert.equal(loaded.answer, "one");
});

test("legacy fallback sessions migrate without IndexedDB", async () => {
  const mock = await autoStorageMock({
    noIndexedDB: true,
    legacyFallbackSessions: [{ id: "legacy-fallback", answer: "Fallback-only migrated answer", template_name: "Legacy", questions: [] }],
  });
  await mock.module.listRecentSessions(20);
  assert.equal(JSON.parse(mock.fallback.get(sessionKey + "legacy-fallback")).answer, "Fallback-only migrated answer");
  const loaded = await mock.module.loadSession("legacy-fallback");
  assert.equal(loaded.answer, "Fallback-only migrated answer");
  assert.equal(mock.fallback.has(legacySessionKey + "legacy-fallback"), true, "the legacy original stays recoverable");
});

test("migration selects the fresher legacy copy when both legacy stores hold one id", async () => {
  const mock = await autoStorageMock({
    legacySessions: [{ id: "shared", answer: "Old legacy IndexedDB answer", template_name: "Legacy", updated_at: "2026-01-01T00:00:00.000Z", questions: [] }],
    legacyFallbackSessions: [{ id: "shared", answer: "Newest legacy fallback answer", template_name: "Legacy", updated_at: "2026-01-02T00:00:00.000Z", questions: [] }],
  });
  await mock.module.listRecentSessions(20);
  assert.equal(mock.stores.get("sessions").get("shared").answer, "Newest legacy fallback answer");
});

test("fallback-only deletion succeeds without IndexedDB", async () => {
  const mock = await autoStorageMock({
    noIndexedDB: true,
    legacyFallbackSessions: [{ id: "legacy-fallback", answer: "one", template_name: "Legacy", questions: [] }],
  });
  mock.fallback.set(sessionKey + "legacy-fallback", JSON.stringify({ id: "legacy-fallback" }));
  await mock.module.deleteSession("legacy-fallback");
  assert.equal(mock.fallback.has(sessionKey + "legacy-fallback"), false);
  assert.equal(mock.fallback.has(legacySessionKey + "legacy-fallback"), false);
});

test("deleting a migrated session also removes the legacy IndexedDB copy", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "test", revision: 1, answer: "migrated", questions: [] }],
    legacySessions: [{ id: "test", answer: "Saved before project rename", template_name: "Legacy", questions: [] }],
  });
  await mock.module.deleteSession("test");
  assert.equal(mock.stores.get("sessions").has("test"), false);
  assert.equal(mock.legacyStores.get("sessions").has("test"), false, "the deleted questionnaire must not return through migration");
});


test("migration leaves existing new-namespace records untouched", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "shared", revision: 7, answer: "newer" }],
    legacySessions: [{ id: "shared", answer: "legacy" }],
  });
  await mock.module.listRecentSessions(20);
  assert.equal(mock.stores.get("sessions").get("shared").revision, 7);
  assert.equal(mock.stores.get("sessions").get("shared").answer, "newer");
});

test("concurrent recovery writes keep every acknowledged entry", async () => {
  for (let round = 0; round < 5; round += 1) {
    const mock = await autoStorageMock();
    await Promise.all([
      mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Rejected edit from tab A" }] }),
      mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Rejected edit from tab B" }] }),
    ]);
  const loaded = await mock.module.loadRecovery("test");

    assert.ok(loaded.entries.some((entry) => entry.answer === "Rejected edit from tab A"), `round ${round}: tab A's acknowledged entry was lost`);
    assert.ok(loaded.entries.some((entry) => entry.answer === "Rejected edit from tab B"), `round ${round}: tab B's acknowledged entry was lost`);
  }
});

test("a recovery write that read stale state re-merges before conceding", async () => {
  const mock = await autoStorageMock();
  // Seed a base record so both writers stack on top of existing entries.
  await mock.module.saveRecovery("test", { reason: "reconcile", entries: [{ label: "Name", answer: "Base preserved answer" }] });
  await Promise.all([
    mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Age", answer: "Rejected edit from tab A" }] }),
    mock.module.saveRecovery("test", { reason: "import", entries: [{ label: "Age", answer: "Rejected edit from tab B" }] }),
  ]);
  const loaded = await mock.module.loadRecovery("test");
  for (const answer of ["Base preserved answer", "Rejected edit from tab A", "Rejected edit from tab B"]) {
    assert.ok(loaded.entries.some((entry) => entry.answer === answer), `${answer} must survive concurrent writers`);
  }
});

test("eight concurrent fallback writers all keep their acknowledged additions", async () => {
  await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  const writers = [];
  for (let index = 0; index < 8; index += 1) {
    const memory = new Map([["runyth-born.writer", `tab-${index}`]]);
    globalThis.sessionStorage = {
      getItem: (key) => (memory.has(key) ? memory.get(key) : null),
      setItem: (key, value) => memory.set(key, String(value)),
    };
    writers.push(await import(`../../site/assets/storage.js?writer=${index}-${Math.random()}`));
  }
  await Promise.all(writers.map((writer, index) => writer.saveRecovery("test", {
    reason: "conflict",
    entries: [{ label: "Name", answer: `Preserved answer ${index}` }],
  })));
  const loaded = await writers[0].loadRecovery("test");
  for (let index = 0; index < 8; index += 1) {
    assert.ok(loaded.entries.some((entry) => entry.answer === `Preserved answer ${index}`), `acknowledged answer ${index} must survive every other writer`);
  }
});

test("a late stale fallback writer cannot erase another tab's acknowledged addition", async () => {
  await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  const openWriter = async (token) => {
    const memory = new Map([["runyth-born.writer", token]]);
    globalThis.sessionStorage = {
      getItem: (key) => (memory.has(key) ? memory.get(key) : null),
      setItem: (key, value) => memory.set(key, String(value)),
    };
    return import(`../../site/assets/storage.js?writer=${token}-${Math.random()}`);
  };
  const first = await openWriter("tab-early");
  const second = await openWriter("tab-late");
  await first.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Early acknowledged answer" }] });
  await second.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Late acknowledged answer" }] });
  // The early tab saves again from its stale view; it may only rewrite its
  // own row (F02).
  await first.saveRecovery("test", { reason: "conflict", entries: [{ label: "Age", answer: "Early second answer" }] });
  const loaded = await first.loadRecovery("test");
  for (const answer of ["Early acknowledged answer", "Late acknowledged answer", "Early second answer"]) {
    assert.ok(loaded.entries.some((entry) => entry.answer === answer), `${answer} must remain reviewable`);
  }
});

test("cloned sessionStorage writer tokens do not share a fallback row", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  // Opener-created documents inherit one sessionStorage copy. Two live
  // documents that see that same token must still write distinct rows (F02).
  const shared = new Map([["runyth-born.writer", "cloned-from-opener"]]);
  const openWriter = async (label) => {
    globalThis.sessionStorage = {
      getItem: (key) => (shared.has(key) ? shared.get(key) : null),
      setItem: (key, value) => shared.set(key, String(value)),
    };
    return import(`../../site/assets/storage.js?cloned=${label}-${Math.random()}`);
  };
  const first = await openWriter("child-a");
  const second = await openWriter("child-b");
  await Promise.all([
    first.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Inherited writer A" }] }),
    second.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Inherited writer B" }] }),
  ]);
  const keys = [...mock.fallback.keys()].filter((key) => key.startsWith(`${sessionKey}test.recovery.`));
  assert.equal(keys.length, 2, "independently executing documents must not share a replaceable recovery row");
  const reloaded = await openWriter("reload");
  const loaded = await reloaded.loadRecovery("test");
  for (const answer of ["Inherited writer A", "Inherited writer B"]) {
    assert.ok(loaded.entries.some((entry) => entry.answer === answer), `${answer} must stay reviewable across a reload`);
  }
});

test("deleting a session removes every per-writer fallback recovery row", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  const memory = new Map([["runyth-born.writer", "tab-cleanup"]]);
  globalThis.sessionStorage = {
    getItem: (key) => (memory.has(key) ? memory.get(key) : null),
    setItem: (key, value) => memory.set(key, String(value)),
  };
  await mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Row to be removed" }] });
  mock.fallback.set(sessionKey + "test.recovery.other-tab", JSON.stringify({ id: "test", entries: [{ label: "Name", answer: "Other tab answer" }] }));
  await mock.module.deleteSession("test");
  const survivors = [...mock.fallback.keys()].filter((key) => key.startsWith(sessionKey + "test.recovery"));
  assert.deepEqual(survivors, [], "no recovery row survives the deletion");
  assert.equal(await mock.module.loadRecovery("test"), null);
});

test("a recovery write after deletion is rejected and stores nothing", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1 }] });
  const outcome = await Promise.allSettled([
    mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Private recovery queued before deletion" }] }),
    mock.module.deleteSession("test"),
  ]);
  // Either order must leave no recovery content behind (F11).
  const rejected = outcome.some((entry) => entry.status === "rejected" && entry.reason?.name === "SessionDeletedError");
  assert.equal(rejected, true, "the recovery write must not acknowledge after deletion");
  assert.equal(await mock.module.loadRecovery("test"), null, "the deleted questionnaire must not retain preserved answers");
  const recoveries = mock.stores.get("recoveries");
  assert.equal(recoveries ? recoveries.size : 0, 0, "no recovery record survives a completed deletion");
});

test("a fallback recovery write after deletion is rejected", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  mock.fallback.set(sessionKey + "test.deleted", JSON.stringify({ id: "test", deleted_at: new Date().toISOString() }));
  await assert.rejects(
    () => mock.module.saveRecovery("test", { reason: "conflict", entries: [{ label: "Name", answer: "Late preservation" }] }),
    (error) => error.name === "SessionDeletedError",
  );
  assert.equal(mock.fallback.has(sessionKey + "test.recovery"), false);
});

test("deletion retries the marker after freeing fallback space and blocks stale writers", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  // Emulate native fractional quota pressure: the marker write fails while
  // the fallback record still occupies its space, and succeeds after the
  // removal freed it, exactly like a real QuotaExceededError window (F11).
  const sessionRecord = { id: "test", revision: 1, questions: [], template_name: "Example" };
  mock.fallback.set(sessionKey + "test", JSON.stringify(sessionRecord));
  const plainSetItem = globalThis.localStorage.setItem.bind(globalThis.localStorage);
  const occupied = () => [...mock.fallback.keys()].some((key) =>
    key.startsWith(sessionKey) && !key.endsWith(".deleted") && !key.endsWith(".recovery"));
  globalThis.localStorage.setItem = (key, value) => {
    if (key === sessionKey + "test.deleted" && occupied()) {
      throw new Error("QuotaExceededError");
    }
    plainSetItem(key, value);
  };
  try {
    await mock.module.deleteSession("test");
  } finally {
    globalThis.localStorage.setItem = plainSetItem;
  }
  assert.equal(mock.fallback.has(sessionKey + "test"), false);
  assert.equal(mock.fallback.has(sessionKey + "test.deleted"), true, "the retry must record the marker after cleanup");
  await assert.rejects(
    () => mock.module.saveSession({ id: "test", revision: 9, questions: [{ label: "Name", answer: "Retained stale private answer" }] }),
    (error) => error.name === "SessionDeletedError",
  );
  assert.equal(mock.fallback.has(sessionKey + "test"), false, "a stale writer cannot resurrect the deleted questionnaire");
});

test("a failed primary deletion is reported even when the fallback marker was recorded first", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1, questions: [{ label: "Name", answer: "Primary answer surviving failed deletion" }] }], failDeletionTransaction: true });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 1 }));
  await assert.rejects(
    () => mock.module.deleteSession("test"),
    (error) => /every stored copy/u.test(error.message),
    "a surviving primary copy must not be acknowledged as deleted",
  );
  assert.equal(mock.stores.get("sessions").has("test"), true, "the primary record survives the failed cleanup");
  assert.equal(mock.fallback.has(sessionKey + "test.deleted"), true, "the fallback marker was recorded before the cleanup");
});

test("a failed primary deletion is reported when the marker retry succeeds after freeing fallback space", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 1, questions: [{ label: "Name", answer: "Primary answer surviving failed deletion" }] }], failDeletionTransaction: true });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 1 }));
  // Native quota pressure: the first marker write fails while the fallback
  // mirror occupies space and succeeds once the removal frees it (F11).
  const plainSetItem = globalThis.localStorage.setItem.bind(globalThis.localStorage);
  const occupied = () => [...mock.fallback.keys()].some((key) =>
    key.startsWith(sessionKey) && !key.endsWith(".deleted") && !key.endsWith(".recovery"));
  globalThis.localStorage.setItem = (key, value) => {
    if (key === sessionKey + "test.deleted" && occupied()) {
      throw new Error("QuotaExceededError");
    }
    plainSetItem(key, value);
  };
  try {
    await assert.rejects(
      () => mock.module.deleteSession("test"),
      (error) => /every stored copy/u.test(error.message),
      "the marker retry must not acknowledge a failed primary cleanup",
    );
  } finally {
    globalThis.localStorage.setItem = plainSetItem;
  }
  assert.equal(mock.stores.get("sessions").has("test"), true, "the primary record survives the failed cleanup");
  assert.equal(mock.fallback.has(sessionKey + "test.deleted"), true, "the retried marker is recorded");
  const marker = JSON.parse(mock.fallback.get(sessionKey + "test.deleted"));
  assert.deepEqual(Object.keys(marker).sort(), ["deleted_at", "id"], "the marker holds no answer content");
});

test("deletion without any durable marker reports incomplete removal", async () => {
  const mock = await autoStorageMock({ noIndexedDB: true, legacySessions: [] });
  await mock.module.saveSession({ id: "test", revision: 1, questions: [] });
  // Permanently full storage: even after freeing space the marker fails.
  const plainSetItem = globalThis.localStorage.setItem.bind(globalThis.localStorage);
  globalThis.localStorage.setItem = (key, value) => {
    if (key.endsWith(".deleted")) {
      throw new Error("QuotaExceededError");
    }
    plainSetItem(key, value);
  };
  try {
    await assert.rejects(
      () => mock.module.deleteSession("test"),
      (error) => /durable deletion marker/u.test(error.message),
      "the deletion must not acknowledge removal without a durable marker",
    );
  } finally {
    globalThis.localStorage.setItem = plainSetItem;
  }
});
