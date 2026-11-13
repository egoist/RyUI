// The rest of the device's modules in the browser: what a page can do, and
// a clear error for what it can't.
//
//   - biometric: WebAuthn's platform authenticator (Touch ID, Face ID,
//     Windows Hello, Android's screen lock) with user verification
//     required: the first Authenticate makes a passkey for the site, named
//     after the app, and each after asks the user to verify with it. Check's
//     kind is a guess from the device (a browser doesn't say which).
//   - tracking: no prompt; Denied when the user asks sites not to track them
//     (Global Privacy Control, Do Not Track), else Allowed (as Android).
//   - webAuth: a popup window; done when it comes back to a page of the
//     app's own origin (the provider redirects there), whose URL done gets.
//   - background: a page runs while it's open (throttled while hidden):
//     tasks start and end, with no notification.
//   - screen: lock with the Screen Orientation API (a browser locks only a
//     full-screen page, or an installed app); the bars hidden is full
//     screen.
//   - library.save: a download. contacts.pick: the Contact Picker API
//     (Chrome on Android). contacts.add and calendar.add: a vCard and an
//     iCalendar file, which the system offers to add.
//   - splash: a page has no launch screen: nothing to hold.
//   - transcribe.check: not on the device.
//   - The rest fail, saying why: review, integrity, ageRange, appleAuth,
//     periodic, widget, the photo library, contacts' and calendars'
//     reading, screenshots. (push is system-push.js's: Web Push.)

import { Reader } from "../ops.js";
import * as fs from "../fs.js";

const dec = new TextDecoder();
const notOnWeb = (what, why) => new Error(`asuka: ${what} isn't on the web: ${why}`);
const fail = (what, why) => () => { throw notOnWeb(what, why); };

// --- biometric ---

const b64url = {
  encode: (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""),
  decode: (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0)),
};

/** userVerified reads WebAuthn authenticator data's UV flag. */
export function userVerified(authData) {
  const b = new Uint8Array(authData);
  return b.length > 32 && (b[32] & 0x04) !== 0;
}

/** biometricKind guesses the device's biometrics from its user agent: Face ID on iPhones and iPads, else a fingerprint. */
export function biometricKind(ua) {
  return /iPhone|iPad/.test(ua) ? 2 : 1;
}

function biometric(h) {
  const key = h.prefix() + "biometric.credential";
  const available = async () => {
    try {
      return !!globalThis.PublicKeyCredential && await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable();
    } catch {
      return false;
    }
  };
  const random = (n) => crypto.getRandomValues(new Uint8Array(n));
  const errorOf = (e) => {
    switch (e?.name) {
      case "NotAllowedError":
      case "AbortError":
        return new Error("canceled");
      case "SecurityError":
      case "NotSupportedError":
        return new Error("not available");
    }
    return new Error("failed");
  };
  return {
    check: async () => {
      const ok = await available();
      return new Uint8Array(ok ? [biometricKind(navigator.userAgent), 1, 1] : [0, 0, 0]);
    },
    authenticate: async (b) => {
      // The reason and cancel title (b's strings): WebAuthn's prompt is the
      // browser's own, which shows the site instead.
      if (!await available()) throw new Error("not available");
      const name = h.options.title || document.title || h.options.name || "App";
      let id = null;
      try { id = localStorage.getItem(key); } catch {}
      try {
        if (!id) {
          const c = await navigator.credentials.create({
            publicKey: {
              challenge: random(32),
              rp: { name },
              user: { id: random(16), name, displayName: name },
              pubKeyCredParams: [{ type: "public-key", alg: -7 }, { type: "public-key", alg: -257 }],
              authenticatorSelection: { authenticatorAttachment: "platform", userVerification: "required", residentKey: "discouraged" },
              attestation: "none",
              timeout: 120000,
            },
          });
          try { localStorage.setItem(key, b64url.encode(c.rawId)); } catch {}
          return null; // making it verified the user
        }
        const a = await navigator.credentials.get({
          publicKey: {
            challenge: random(32),
            allowCredentials: [{ type: "public-key", id: b64url.decode(id), transports: ["internal"] }],
            userVerification: "required",
            timeout: 120000,
          },
        });
        if (!userVerified(a.response.authenticatorData)) throw new Error("failed");
        return null;
      } catch (e) {
        if (e?.message === "failed") throw e;
        throw errorOf(e);
      }
    },
  };
}

// --- webAuth ---

function webAuth() {
  const sessions = new Map(); // id -> end(url, err)
  return {
    start: (b) => {
      const r = new Reader(b);
      const id = r.u32();
      r.u8(); // flags: ephemeral, which a page can't ask of the browser
      const url = r.string(), scheme = r.string();
      return new Promise((resolve, reject) => {
        const w = window.open(url, "asuka-webauth-" + id, "popup,width=500,height=700");
        if (!w) return reject(new Error("canceled"));
        const end = (u, err) => {
          clearInterval(t);
          sessions.delete(id);
          try { w.close(); } catch {}
          err ? reject(err) : resolve(u);
        };
        sessions.set(id, end);
        // The provider sends the user back to a page of the app's origin
        // (a callback URL of the app's scheme can't open a page).
        const t = setInterval(() => {
          if (w.closed) return end("", new Error("canceled"));
          let href = "";
          try { href = w.location.href; } catch { return; } // another site's
          if (!href || href === "about:blank") return;
          if (scheme && !href.startsWith(scheme + ":") && new URL(href).origin !== location.origin) return;
          end(href, null);
        }, 200);
      });
    },
    cancel: (b) => {
      const id = new Reader(b).u32();
      sessions.get(id)?.("", new Error("canceled"));
    },
  };
}

// --- background ---

function background() {
  const tasks = new Set();
  const waiting = [];
  return {
    start: (b) => { tasks.add(JSON.parse(dec.decode(b)).id); },
    update: () => {},
    end: (b) => {
      tasks.delete(JSON.parse(dec.decode(b)).id);
      if (!tasks.size) for (const w of waiting.splice(0)) w(new Error("ended"));
    },
    // A page has no notification to tap: no action comes.
    next: () => new Promise((_, reject) => { if (!tasks.size) reject(new Error("ended")); else waiting.push(reject); }),
  };
}

// --- screen ---

/** orientation is the Screen Orientation API's lock for asuka/screen's Orientation (null: unlock). */
export function orientation(o) {
  return [null, "portrait", "portrait-primary", "landscape", "landscape-primary", "landscape-secondary"][o] ?? null;
}

function screenModule() {
  return {
    lock: async (b) => {
      const o = orientation(b[0]);
      const so = globalThis.screen?.orientation;
      if (!so?.lock) throw notOnWeb("screen.lock", "this browser can't lock the orientation");
      if (o == null) so.unlock?.();
      else await so.lock(o); // a browser locks only a full-screen page, or an installed app
    },
    bars: async (b) => {
      const hide = (b[1] & 3) !== 0;
      const d = document;
      if (hide && !d.fullscreenElement) await d.documentElement.requestFullscreen?.({ navigationUI: "hide" });
      else if (!hide && d.fullscreenElement) await d.exitFullscreen?.();
    },
    preventCapture: fail("screen.preventCapture", "a page can't keep itself out of screenshots"),
    screenshot: fail("screen.screenshot", "a page isn't told of screenshots"),
    stopScreenshots: () => {},
  };
}

// --- files the system adds: vCard, iCalendar ---

function download(bytes, name, type) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([bytes], { type }));
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 10000);
}

const vEscape = (s) => String(s ?? "").replace(/\\/g, "\\\\").replace(/\n/g, "\\n").replace(/([,;])/g, "\\$1");

/** vCard is a contact (asuka/contacts' JSON) as a vCard 3.0. */
export function vCard(c) {
  const lines = ["BEGIN:VCARD", "VERSION:3.0"];
  const fn = c.name || [c.givenName, c.familyName].filter(Boolean).join(" ") || c.organization || "";
  lines.push(`N:${vEscape(c.familyName)};${vEscape(c.givenName)};;;`, `FN:${vEscape(fn)}`);
  if (c.organization) lines.push(`ORG:${vEscape(c.organization)}`);
  if (c.jobTitle) lines.push(`TITLE:${vEscape(c.jobTitle)}`);
  for (const p of c.phones || []) lines.push(`TEL${p.label ? `;TYPE=${vEscape(p.label).toUpperCase()}` : ""}:${vEscape(p.value)}`);
  for (const e of c.emails || []) lines.push(`EMAIL${e.label ? `;TYPE=${vEscape(e.label).toUpperCase()}` : ""}:${vEscape(e.value)}`);
  if (c.birthday) lines.push(`BDAY:${c.birthday}`);
  if (c.note) lines.push(`NOTE:${vEscape(c.note)}`);
  lines.push("END:VCARD");
  return lines.join("\r\n") + "\r\n";
}

const icsDate = (ms, allDay) => {
  const d = new Date(ms);
  const p = (n) => String(n).padStart(2, "0");
  if (allDay) return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`;
  return `${d.getUTCFullYear()}${p(d.getUTCMonth() + 1)}${p(d.getUTCDate())}T${p(d.getUTCHours())}${p(d.getUTCMinutes())}${p(d.getUTCSeconds())}Z`;
};

/** iCalendar is an event (asuka/calendar's add JSON) as an iCalendar file. */
export function iCalendar(e, uid, now = Date.now()) {
  const all = !!e.allDay;
  const end = all && e.endMs <= e.startMs ? e.startMs + 86400000 : e.endMs;
  const lines = [
    "BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Asuka//web//EN", "BEGIN:VEVENT",
    `UID:${uid}`, `DTSTAMP:${icsDate(now, false)}`,
    all ? `DTSTART;VALUE=DATE:${icsDate(e.startMs, true)}` : `DTSTART:${icsDate(e.startMs, false)}`,
    all ? `DTEND;VALUE=DATE:${icsDate(end, true)}` : `DTEND:${icsDate(end, false)}`,
    `SUMMARY:${vEscape(e.title)}`,
  ];
  if (e.location) lines.push(`LOCATION:${vEscape(e.location)}`);
  if (e.notes) lines.push(`DESCRIPTION:${vEscape(e.notes)}`);
  if (e.url) lines.push(`URL:${e.url}`);
  lines.push("END:VEVENT", "END:VCALENDAR");
  return lines.join("\r\n") + "\r\n";
}

const fileName = (s, ext) => (String(s || "").replace(/[\\/:*?"<>|]+/g, " ").trim() || "untitled") + ext;

function contacts() {
  return {
    request: () => new Uint8Array([1]), // Denied: a page can't read the address book
    list: () => { throw new Error("not allowed"); },
    pick: async () => {
      if (!navigator.contacts?.select) throw notOnWeb("contacts.pick", "this browser has no contact picker (Chrome on Android has)");
      const props = await navigator.contacts.getProperties?.() ?? ["name", "email", "tel"];
      const [c] = await navigator.contacts.select(props.filter((p) => ["name", "email", "tel"].includes(p)), { multiple: false });
      if (!c) return null;
      const name = c.name?.[0] || "";
      const parts = name.split(/\s+/);
      return JSON.stringify({
        id: "", name, givenName: parts.length > 1 ? parts.slice(0, -1).join(" ") : name, familyName: parts.length > 1 ? parts.at(-1) : "",
        organization: "", jobTitle: "", birthday: "", note: "",
        phones: (c.tel || []).map((value) => ({ label: "", value })),
        emails: (c.email || []).map((value) => ({ label: "", value })),
      });
    },
    // The system offers to add the vCard; the page isn't told if it did.
    add: (b) => {
      const c = JSON.parse(dec.decode(b));
      download(vCard(c), fileName(c.name || [c.givenName, c.familyName].filter(Boolean).join(" "), ".vcf"), "text/vcard");
      return new Uint8Array([0]);
    },
  };
}

function calendar() {
  return {
    request: () => new Uint8Array([1]), // Denied: a page can't read calendars
    calendars: () => { throw new Error("not allowed"); },
    events: () => { throw new Error("not allowed"); },
    add: (b) => {
      const e = JSON.parse(dec.decode(b));
      const uid = (globalThis.crypto?.randomUUID?.() ?? String(Date.now())) + "@asuka";
      download(iCalendar(e, uid), fileName(e.title, ".ics"), "text/calendar");
      return new Uint8Array([0]);
    },
  };
}

function library() {
  return {
    save: (b) => {
      const o = JSON.parse(dec.decode(b));
      const bytes = fs.readFile(o.path);
      if (!bytes) throw new Error(`no file ${o.path}`);
      download(bytes, o.name, o.mime || "application/octet-stream");
      if (o.move) fs.memfs.unlink(o.path, () => {});
      return o.name; // in the browser's downloads
    },
  };
}

export function modules(h) {
  const noLibrary = "a page can't read the photo library; device.PickPhotos picks photos";
  return {
    biometric: biometric(h),
    review: { request: fail("review.request", "a browser has no store's rating prompt (open review.AppStoreURL or PlayStoreURL)") },
    tracking: {
      status: () => new Uint8Array([navigator.globalPrivacyControl || navigator.doNotTrack === "1" ? 2 : 3]),
      request: () => new Uint8Array([navigator.globalPrivacyControl || navigator.doNotTrack === "1" ? 2 : 3]),
    },
    integrity: {
      supported: () => new Uint8Array([0]),
      generateKey: fail("integrity.generateKey", "a browser has no app attestation"),
      attestKey: fail("integrity.attestKey", "a browser has no app attestation"),
      assert: fail("integrity.assert", "a browser has no app attestation"),
      prepare: fail("integrity.prepare", "a browser has no app attestation"),
      token: fail("integrity.token", "a browser has no app attestation"),
    },
    ageRange: { request: () => { throw new Error("not available"); } },
    appleAuth: {
      available: () => new Uint8Array([0]),
      signIn: () => { throw new Error("not available"); },
      state: () => { throw new Error("not available"); },
    },
    webAuth: webAuth(),
    periodic: {
      schedule: fail("periodic.schedule", "a browser runs no page's tasks while it's closed"),
      cancel: () => {},
      next: fail("periodic.next", "a browser runs no page's tasks while it's closed"),
      done: () => {},
    },
    background: background(),
    widget: { set: fail("widget.set", "a page has no home screen widgets") },
    splash: { hold: () => {}, hide: () => {} },
    screen: screenModule(),
    transcribe: {
      check: (b) => JSON.stringify({ onDevice: false, language: dec.decode(b) || navigator.language || "" }),
      file: () => { throw new Error("not available"); },
    },
    photoLibrary: {
      current: () => "denied",
      request: () => "denied",
      manage: fail("photoLibrary.manage", noLibrary),
      list: fail("photoLibrary.list", noLibrary),
      thumbnail: fail("photoLibrary.thumbnail", noLibrary),
      export: fail("photoLibrary.export", noLibrary),
    },
    library: library(),
    contacts: contacts(),
    calendar: calendar(),
  };
}
