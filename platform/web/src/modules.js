// Native modules (ui.Call): the ones a browser has. A method answers once,
// with Result on the root: u32 call id, u8 ok, then the result's bytes (an
// error's message when not ok). A module the web host doesn't have, or a
// method a browser can't do, answers with an error that says so.
//
// The built-in modules are in modules/: each file exports modules(h), the
// modules it has by name, each a map of methods. A method takes the call's
// bytes and the call ({ root, h }) and returns (or resolves to) the
// result's bytes (a Uint8Array, or a string, sent as UTF-8), or throws.

import { Events, Reader, Writer } from "./ops.js";
import { modules as system } from "./modules/system.js";
import { modules as hardware } from "./modules/hardware.js";
import { modules as web } from "./modules/web.js";
import { modules as desktop } from "./modules/desktop.js";
import { modules as dialog } from "./modules/dialog.js";
import { modules as purchase } from "./modules/purchase.js";

const enc = new TextEncoder();

export class Modules {
  constructor(h) {
    this.h = h;
    this.registered = new Map(); // asuka.registerModule's
    this.builtins = null; // made on the first call
  }

  call(root, args) {
    const r = new Reader(args);
    const id = r.u32();
    const module = r.string(), method = r.string();
    const body = args.slice(r.p);
    const reply = (result, err) => {
      const w = new Writer().u32(id).u8(err ? 0 : 1);
      if (err) w.raw(enc.encode(String(err?.message ?? err)));
      else if (result != null) w.raw(result instanceof Uint8Array ? result : enc.encode(String(result)));
      this.h.dispatch(Events.result(root.id, w.done()));
    };
    const m = this.registered.get(module)?.[method] || this.builtin(module)?.[method];
    if (!m) {
      reply(null, new Error(`asuka: ${module}.${method} isn't on the web`));
      return;
    }
    // Answered once the core has returned: never inside the batch.
    this.h.afterCore(() => {
      Promise.resolve()
        .then(() => m(body, { root, h: this.h }))
        .then((v) => reply(v, null), (e) => reply(null, e));
    });
  }

  builtin(name) {
    this.builtins ??= { ...system(this.h), ...hardware(this.h), ...web(this.h), ...desktop(this.h), ...dialog(this.h), ...purchase(this.h) };
    return Object.hasOwn(this.builtins, name) ? this.builtins[name] : null;
  }
}
