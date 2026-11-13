// Debug automation (asuka dev --platform web): the commands the iOS and Android
// hosts' debug builds take, so the same GUI tests (e2e/) drive the page.
// They come from the dev server one line each:
//
//	tap <text>     the visible view showing text (a press, or a control's click;
//	               a field showing it, its text or its placeholder, is focused)
//	type <text>    typed into the focused field (or the first one showing)
//	submit         the focused field's return key
//	back           the user's back
//	tab <title>    the tab with that title
//	choose <text>  the item with that text of a picker or wheel showing (a <select>)
//	dump           what the screen shows: a text a line
//	reload         the page loads again: the app relaunches
//
// It answers with what dump read, or an error.

import { flush } from "./fs.js";

/** run runs commands (lines) on host h; it resolves to dump's lines, or "". */
export async function run(h, commands) {
  let out = "";
  for (const line of commands.split("\n")) {
    const cmd = line.trim();
    if (!cmd) continue;
    const sp = cmd.indexOf(" ");
    const verb = sp < 0 ? cmd : cmd.slice(0, sp);
    const arg = sp < 0 ? "" : cmd.slice(sp + 1);
    switch (verb) {
      case "tap": tap(h, arg); break;
      case "type": type(h, arg); break;
      case "submit": submit(); break;
      case "back": h.afterCore(() => h.nav.back()); break;
      case "tab": tab(arg); break;
      case "choose": choose(arg); break;
      case "dump": out = texts(h).join("\n") + "\n"; break;
      case "reload":
        // A relaunch: what the app keeps (its files, its language) stays,
        // its signals start again (not a dev reload, which keeps them).
        try { sessionStorage.removeItem(h.stateKey()); } catch {}
        // Once what it wrote is kept, as an app's files are on a disk.
        flush().then(() => location.reload());
        break;
      default: throw new Error("unknown automation command: " + cmd);
    }
    await new Promise((r) => setTimeout(r, 0));
  }
  return out;
}

/** shown reports whether el shows on screen: visible, not under a dialog, not a covered screen. */
function shown(el) {
  if (!el.isConnected || el.closest("[inert], .asuka-hidden, .asuka-hidden-page, .asuka-covered, .asuka-exiting, [hidden]")) return false;
  if (el.checkVisibility && !el.checkVisibility({ visibilityProperty: true, opacityProperty: true })) return false;
  const r = el.getBoundingClientRect();
  if (r.width <= 0 || r.height <= 0) return false;
  // Under a modal dialog, only what's in it shows.
  const modal = [...document.querySelectorAll("dialog[open]")].pop();
  if (modal && !modal.contains(el)) return false;
  return r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth;
}

const textSelector = ".asuka-text, .asuka-rich, .asuka-button-title, .asuka-bar-title, .asuka-tab span, .asuka-sidebar-item span, .asuka-menu-item span, .asuka-menu-header, .asuka-alert-title, .asuka-alert-message, .asuka-alert-buttons button, .asuka-segmented button, .asuka-stepper button, .asuka-bar-text, .asuka-fab span, .asuka-snackbar-message, .asuka-snackbar-action";

/** texts are what the screen shows: each text, title and field (its text, or its placeholder). */
function texts(h) {
  const out = [];
  for (const el of h.container.querySelectorAll(textSelector)) {
    if (shown(el) && el.textContent.trim()) out.push(el.textContent.trim());
  }
  // A control that shows only an icon says its label, as screen readers have it.
  for (const el of h.container.querySelectorAll("button[aria-label], [role=button][aria-label], [role=img][aria-label]")) {
    if (shown(el) && !el.textContent.trim()) out.push(el.getAttribute("aria-label"));
  }
  // A progress indicator with a value says it, as screen readers read it.
  for (const el of h.container.querySelectorAll("[role=progressbar][aria-valuenow]")) {
    if (shown(el)) out.push("progress " + el.getAttribute("aria-valuenow") + "%");
  }
  for (const el of h.container.querySelectorAll("input, textarea, select")) {
    if (!shown(el)) continue;
    if (el.type === "checkbox") continue;
    const v = el.tagName === "SELECT" ? el.selectedOptions[0]?.textContent : el.value || el.placeholder;
    if (v) out.push(v);
  }
  return out;
}

/** find is the innermost visible element showing text. */
function find(h, text) {
  let best = null;
  for (const el of h.container.querySelectorAll(textSelector + ", button, [role=button], .asuka-view, input, textarea")) {
    const field = el.tagName === "INPUT" || el.tagName === "TEXTAREA";
    if (field ? (el.value || el.placeholder) !== text : el.textContent.trim() !== text && el.getAttribute("aria-label") !== text) continue;
    if (!shown(el)) continue;
    if (!best || best.contains(el)) best = el;
  }
  return best;
}

function tap(h, text) {
  const el = find(h, text);
  if (!el) throw new Error("nothing shows " + JSON.stringify(text));
  const control = el.closest("button, a[href], input, select, textarea");
  if (control) {
    if (control.tagName === "INPUT" || control.tagName === "TEXTAREA") control.focus();
    else control.click();
    return;
  }
  const r = el.getBoundingClientRect();
  const at = { bubbles: true, cancelable: true, isPrimary: true, pointerId: 1, button: 0, pointerType: "mouse", clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
  el.dispatchEvent(new PointerEvent("pointerdown", at));
  el.dispatchEvent(new PointerEvent("pointerup", at));
  el.dispatchEvent(new MouseEvent("click", at));
}

function field() {
  const a = document.activeElement;
  if (a && (a.tagName === "INPUT" || a.tagName === "TEXTAREA")) return a;
  return [...document.querySelectorAll(".asuka input.asuka-field, .asuka textarea.asuka-field, .asuka input.asuka-search")].find(shown) || null;
}

function type(h, text) {
  const f = field();
  if (!f) throw new Error("no field to type in");
  f.focus();
  if (!document.execCommand("insertText", false, text)) {
    f.setRangeText(text, f.selectionStart, f.selectionEnd, "end");
    f.dispatchEvent(new InputEvent("input", { bubbles: true, data: text, inputType: "insertText" }));
  }
}

function submit() {
  const f = field();
  if (!f) throw new Error("no field to submit");
  f.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", code: "Enter", bubbles: true }));
}

/** choose chooses the option with text of a visible <select>, as the user does from its list. */
function choose(text) {
  for (const s of document.querySelectorAll(".asuka select")) {
    if (!shown(s) || s.disabled) continue;
    const i = [...s.options].findIndex((o) => !o.disabled && o.textContent === text);
    if (i < 0) continue;
    s.focus();
    s.selectedIndex = i;
    s.dispatchEvent(new Event("input", { bubbles: true }));
    s.dispatchEvent(new Event("change", { bubbles: true }));
    return;
  }
  throw new Error("no picker offers " + JSON.stringify(text));
}

function tab(title) {
  // Its title, not its badge.
  const b = [...document.querySelectorAll(".asuka-tab")].find((b) => b.dataset.title === title && shown(b));
  if (!b) throw new Error("no tab " + JSON.stringify(title));
  b.click();
}
