// asuka/secure on the web. A browser has no keychain: the values are kept
// in the app's IndexedDB database, each encrypted (AES-GCM, 256 bits)
// under a key WebCrypto made non-extractable, kept in the same database
// (IndexedDB keeps a CryptoKey as it is, so no script, the app's own
// included, can read the key's bytes: only use it). That keeps the values
// out of the page's other storage and off the disk in the clear, but
// anything running in the app's origin can still ask the key to decrypt:
// it's a web app's best, not a keychain.
//
// Each value is bound to its key's name (the cipher's additional data),
// so a value can't be moved to another name.

const enc = new TextEncoder();

/** SecureStore keeps values, encrypted, in a backend: { get(store, k), put(store, k, v), delete(store, k), keys(store) }, all async. */
export class SecureStore {
  constructor(backend, subtle = globalThis.crypto?.subtle) {
    this.backend = backend;
    this.subtle = subtle;
    this.keyP = null;
  }

  key() {
    this.keyP ??= (async () => {
      if (!this.subtle) throw new Error("asuka: secureStore isn't on the web here: the page has no WebCrypto (it needs https or localhost)");
      let k = await this.backend.get("keys", "key");
      if (!k) {
        k = await this.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
        await this.backend.put("keys", "key", k);
      }
      return k;
    })().catch((e) => { this.keyP = null; throw e; });
    return this.keyP;
  }

  /** get is a key's value (Uint8Array), or null. */
  async get(name) {
    const rec = await this.backend.get("values", name);
    if (!rec) return null;
    const k = await this.key();
    try {
      return new Uint8Array(await this.subtle.decrypt({ name: "AES-GCM", iv: rec.iv, additionalData: enc.encode(name) }, k, rec.data));
    } catch {
      // Made under a key that's gone (the site's data partly cleared).
      await this.backend.delete("values", name);
      return null;
    }
  }

  async set(name, value) {
    const k = await this.key();
    const iv = globalThis.crypto.getRandomValues(new Uint8Array(12));
    const data = new Uint8Array(await this.subtle.encrypt({ name: "AES-GCM", iv, additionalData: enc.encode(name) }, k, value));
    await this.backend.put("values", name, { iv, data });
  }

  async delete(name) { await this.backend.delete("values", name); }
}

/** idbBackend is a SecureStore's backend in the IndexedDB database name. */
export function idbBackend(name) {
  let dbP = null;
  const db = () => {
    dbP ??= new Promise((resolve, reject) => {
      if (!globalThis.indexedDB) return reject(new Error("asuka: secureStore isn't on the web here: the browser keeps no IndexedDB for this page"));
      const r = indexedDB.open(name, 1);
      r.onupgradeneeded = () => {
        r.result.createObjectStore("keys");
        r.result.createObjectStore("values");
      };
      r.onsuccess = () => resolve(r.result);
      r.onerror = () => reject(r.error);
    }).catch((e) => { dbP = null; throw e; });
    return dbP;
  };
  const run = async (store, mode, f) => {
    const d = await db();
    return new Promise((resolve, reject) => {
      const tx = d.transaction(store, mode);
      const req = f(tx.objectStore(store));
      tx.oncomplete = () => resolve(req?.result);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error || new Error("aborted"));
    });
  };
  return {
    get: (store, k) => run(store, "readonly", (s) => s.get(k)),
    put: (store, k, v) => run(store, "readwrite", (s) => s.put(v, k)),
    delete: (store, k) => run(store, "readwrite", (s) => s.delete(k)),
  };
}

/** migrateLocalStorage moves values the web host kept in localStorage (before it encrypted them) into store. */
export async function migrateLocalStorage(store, prefix, storage = globalThis.localStorage) {
  if (!storage) return;
  const p = prefix + "secure.";
  const keys = [];
  for (let i = 0; i < storage.length; i++) {
    const k = storage.key(i);
    if (k?.startsWith(p)) keys.push(k);
  }
  for (const k of keys) {
    const v = storage.getItem(k);
    if (v != null) {
      const bytes = Uint8Array.from(atob(v), (c) => c.charCodeAt(0));
      await store.set(k.slice(p.length), bytes);
    }
    storage.removeItem(k);
  }
}
