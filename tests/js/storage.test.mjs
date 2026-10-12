import assert from "node:assert/strict";
import test from "node:test";

import { mergeLegacyRecords } from "../../site/assets/storage.js";

const sessionKey = "runyth-born.session.v2.";
const legacySessionKey = "template-wizard-web.session.v2.";

// Manual mock: request callbacks and the transaction completion are fired by
// the test, which is what lets a test assert between the write and the commit.
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
async function autoStorageMock({ storedSessions = [], failTransactions = false, abortAfterPut = false, legacySessions = [], legacyFallbackSessions = [] } = {}) {
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
  globalThis.indexedDB = {
    databases: async () => [...databases.keys()].map((name) => ({ name })),
    open(name) {
      const open = {};
      queueMicrotask(() => {
        open.result = {
          objectStoreNames: { contains: (store) => storesFor(name).has(store) },
          close() {},
          transaction() {
            if (failTransactions) {
              throw new Error("forced IndexedDB failure");
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
  const module = await import(`../../site/assets/storage.js?case=${Math.random()}`);
  return { module, fallback, stores };
}

test("IndexedDB save waits for transaction commit", async () => {
  const mock = await storageMock();
  let done = false;
  const saving = mock.module.saveSession({ id: "test", updated_at: "" }).then(() => { done = true; });
  await new Promise((resolve) => setImmediate(resolve));
  mock.request().onsuccess?.();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(done, false);
  mock.transaction().oncomplete();
  await saving;
  assert.equal(done, true);
});

test("an abort after a successful put falls back to localStorage", async () => {
  const mock = await storageMock();
  const saving = mock.module.saveSession({ id: "test", updated_at: "" });
  await new Promise((resolve) => setImmediate(resolve));
  mock.request().onsuccess?.();
  mock.transaction().error = new Error("quota exhausted");
  mock.transaction().onabort();
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

test("equal revisions prefer the IndexedDB record", async () => {
  const mock = await autoStorageMock({ storedSessions: [{ id: "test", revision: 2, answer: "IndexedDB copy", updated_at: "2026-01-01T00:00:00.000Z" }] });
  mock.fallback.set(sessionKey + "test", JSON.stringify({ id: "test", revision: 2, answer: "Fallback copy", updated_at: "2999-01-01T00:00:00.000Z" }));
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
  await mock.module.saveRecovery("test", { reason: "reconcile", questions: [{ label: "Name", answer: "Akito" }] });
  const loaded = await mock.module.loadRecovery("test");
  assert.equal(loaded.reason, "reconcile");
  assert.deepEqual(loaded.questions, [{ label: "Name", answer: "Akito" }]);
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
  assert.equal(mock.fallback.size, 0);
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

test("migration leaves existing new-namespace records untouched", async () => {
  const mock = await autoStorageMock({
    storedSessions: [{ id: "shared", revision: 7, answer: "newer" }],
    legacySessions: [{ id: "shared", answer: "legacy" }],
  });
  await mock.module.listRecentSessions(20);
  assert.equal(mock.stores.get("sessions").get("shared").revision, 7);
  assert.equal(mock.stores.get("sessions").get("shared").answer, "newer");
});
