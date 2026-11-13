// WebView's modules on the web:
//
//   - web: "eval" (args: u32 a WebView's node id, then the script)
//     answers with the script's value as JSON (EvalJSResult): in a page
//     of the app's own site, or another site's that cooperates (see
//     webview.js); "cookies" (args: a URL) with the app's own site's
//     cookies (document.cookie) as a Cookie header: a browser keeps
//     another site's from it.
//   - js (asuka/js): "eval" (args: u32 timeout ms, then the script) runs
//     the script in a hidden, sandboxed frame made for it, and answers
//     with its value as JSON, or what it threw (or "timeout").

import { evalIn, cookies, runScript } from "../webview.js";

const dec = new TextDecoder();

const u32 = (b) => (b.length < 4 ? -1 : new DataView(b.buffer, b.byteOffset, 4).getUint32(0, true));

export function modules(h) {
  return {
    web: {
      eval: (b) => {
        const id = u32(b);
        if (id < 0) throw new Error("bad arguments");
        const n = h.nodes.get(id);
        if (!n?.web) throw new Error("not a WebView");
        return evalIn(n, dec.decode(b.subarray(4)));
      },
      cookies: (b) => cookies(dec.decode(b)),
    },
    js: {
      eval: (b) => {
        const ms = u32(b);
        if (ms < 0) throw new Error("bad arguments");
        return runScript(ms, dec.decode(b.subarray(4)));
      },
    },
  };
}
