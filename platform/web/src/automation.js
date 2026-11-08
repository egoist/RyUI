// Debug automation (ryui dev -web): the commands the iOS and Android
// hosts' debug builds take, so the same GUI tests (e2e/) drive the page.
// They come from the dev server one line each:
//
//	tap <text>     the visible view showing text (a press, or a control's click)
//	type <text>    typed into the focused field (or the first one showing)
//	submit         the focused field's return key
//	back           the user's back
//	tab <title>    the tab with that title
//	dump           what the screen shows: a text a line
//	reload         the page loads again: the app relaunches
//
// It answers with what dump read, or an error.

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
      case "dump": out = texts(h).join("\n") + "\n"; break;
      case "reload":
        // A relaunch: what the app keeps (its files, its language) stays,
        // its signals start again (not a dev reload, which keeps them).
        try { sessionStorage.removeItem(h.stateKey()); } catch {}
        setTimeout(() => location.reload(), 50);
        break;
      default: throw new Error("unknown automation command: " + cmd);
    }
    await new Promise((r) => setTimeout(r, 0));
  }
  return out;
}

/** shown reports whether el shows on screen: visible, not under a dialog, not a covered screen. */
function shown(el) {
  if (!el.isConnected || el.closest("[inert], .ryui-hidden, .ryui-hidden-page, .ryui-covered, .ryui-exiting, [hidden]")) return false;
  if (el.checkVisibility && !el.checkVisibility({ visibilityProperty: true, opacityProperty: true })) return false;
  const r = el.getBoundingClientRect();
  if (r.width <= 0 || r.height <= 0) return false;
  // Under a modal dialog, only what's in it shows.
  const modal = [...document.querySelectorAll("dialog[open]")].pop();
  if (modal && !modal.contains(el)) return false;
  return r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth;
}

const textSelector = ".ryui-text, .ryui-rich, .ryui-button-title, .ryui-bar-title, .ryui-tab span, .ryui-sidebar-item span, .ryui-menu-item span, .ryui-alert-title, .ryui-alert-message, .ryui-alert-buttons button, .ryui-segmented button, .ryui-stepper button, .ryui-bar-text";

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
  for (const el of h.container.querySelectorAll(textSelector + ", button, [role=button], .ryui-view")) {
    if (el.textContent.trim() !== text && el.getAttribute("aria-label") !== text) continue;
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
    control.click();
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
  return [...document.querySelectorAll(".ryui input.ryui-field, .ryui textarea.ryui-field, .ryui input.ryui-search")].find(shown) || null;
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

function tab(title) {
  const b = [...document.querySelectorAll(".ryui-tab")].find((b) => b.textContent.trim() === title && shown(b));
  if (!b) throw new Error("no tab " + JSON.stringify(title));
  b.click();
}
