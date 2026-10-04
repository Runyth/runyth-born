import assert from "node:assert/strict";
import test from "node:test";

async function storageMock() {
  const fallback = new Map();
  globalThis.localStorage = { setItem: (key, value) => fallback.set(key, value) };
  let transaction;
  let request;
  globalThis.indexedDB = {
    open() {
      const open = {};
      queueMicrotask(() => {
        open.result = { transaction() {
          transaction = { objectStore() { return { put() { request = {}; return request; } }; } };
          return transaction;
        } };
        open.onsuccess();
      });
      return open;
    },
  };
  const module = await import(`../../site/assets/storage.js?case=${Math.random()}`);
  return { module, fallback, transaction: () => transaction, request: () => request };
}

test("IndexedDB save waits for transaction commit", async () => {
  const mock = await storageMock();
  let done = false;
  const saving = mock.module.saveSession({ id: "test", updated_at: "" }).then(() => { done = true; });
  await new Promise((resolve) => setImmediate(resolve));
  mock.request().result = "test";
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
  mock.transaction().onabort();
  await saving;
  assert.equal(mock.fallback.size, 1);
});
