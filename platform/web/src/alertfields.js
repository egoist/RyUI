// An alert's text fields as the core sends them (ops/alert.go): u32
// count, then per field a string placeholder, a string text (its
// starting text), u8 flags (1: secure, a password), u8 Keyboard and a
// string content type (AutoFill's hint, as a TextField's ContentType).
// The user's typing is reported with FieldChanged: u32 the field's index,
// then its text.

import { Reader, Writer } from "./ops.js";

/** AlertFieldFlags (ops/alert.go). */
export const AlertFieldFlags = Object.freeze({ Secure: 1 });

/** decodeAlertFields reads an alert's fields: [{ placeholder, text, flags, keyboard, contentType }]; what's cut short is left out. */
export function decodeAlertFields(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  const out = [];
  try {
    const n = r.u32();
    for (let i = 0; i < n; i++) {
      const placeholder = r.string(), text = r.string(), flags = r.u8(), keyboard = r.u8(), contentType = r.string();
      if (r.p > b.length || flags === undefined || keyboard === undefined) break;
      out.push({ placeholder, text, flags, keyboard, contentType });
    }
  } catch {
    // cut short
  }
  return out;
}

/** fieldChanged is FieldChanged's payload: field index's text. */
export function fieldChanged(index, text) {
  return new Writer().u32(index).string(text).done();
}
