// asuka/speech, asuka/audio and asuka/audio/record in the browser.
//
//   - speech: speechSynthesis. "speak" (JSON: id, text, language, voice,
//     pitch, rate, volume) answers once spoken, or fails "stopped"; "stop",
//     "pause", "resume", "speaking" (u8), "voices" (JSON: id, name,
//     language, quality, network: the browser's voices, the network ones
//     its online voices).
//   - audio: an HTMLAudioElement a player. Each call's args begin with the
//     player's ID (u32-length string): load (the source: a URL, a file of
//     the app's, an asset), play, playOnce, pause, seek (f64 seconds),
//     volume (f64), rate (f64, pitch kept), loop (u8), release; status
//     answers f64 position, f64 duration, u8 playing, u8 ended.
//   - audioRecord: the microphone (getUserMedia) through a MediaRecorder:
//     AAC in MPEG-4 (.m4a) where the browser records it (Safari, Chrome),
//     else Opus in WebM (.webm, Firefox). "start" (u8 quality, u32 max ms,
//     u8 flags, u32-length title, u32-length path) answers with the file's
//     path once stopped; "stop", "pause", "resume", "level" (u8),
//     "status" (u8 level, u8 recording, u32 ms). The level is an analyser's
//     RMS, on iOS's scale (silence below -60 dB).

import * as fs from "../fs.js";

const dec = new TextDecoder();

/** splitID reads a call's player ID (a u32-length string) and the rest. */
export function splitID(b) {
  if (b.length < 4) return ["", b.subarray(0, 0)];
  const n = new DataView(b.buffer, b.byteOffset, b.byteLength).getUint32(0, true);
  if (n > b.length - 4) return ["", b.subarray(0, 0)];
  return [dec.decode(b.subarray(4, 4 + n)), b.subarray(4 + n)];
}

export const f64 = (b) => b.length >= 8 ? new DataView(b.buffer, b.byteOffset, b.byteLength).getFloat64(0, true) : 0;

/** sourceURL is where a player loads a source from: a URL, a file of the app's, or one of its assets. */
export function sourceURL(h, src) {
  if (!src) return "";
  if (/^(https?|blob|data):/.test(src)) return src;
  if (src.startsWith("file://")) src = decodeURIComponent(src.slice(7));
  if (src.startsWith("/")) return fs.blobURL(src);
  return h.assets + "/" + src.replace(/^\.?\//, "");
}

/** status is a player's status as asuka/audio's reply: f64 position, f64 duration, u8 playing, u8 ended. */
export function encodeStatus(position, duration, playing, ended) {
  const b = new Uint8Array(18);
  const v = new DataView(b.buffer);
  v.setFloat64(0, Number.isFinite(position) ? position : 0, true);
  v.setFloat64(8, Number.isFinite(duration) ? duration : 0, true);
  b[16] = playing ? 1 : 0;
  b[17] = ended ? 1 : 0;
  return b;
}

// --- speech ---

function speech() {
  const synth = () => {
    if (!globalThis.speechSynthesis) throw new Error("asuka: speech isn't in this browser");
    return speechSynthesis;
  };
  const pending = new Map(); // utterance -> { resolve, reject } (kept: Chrome drops an utterance's events if it's collected)
  const voices = () => new Promise((resolve) => {
    const vs = synth().getVoices();
    if (vs.length) return resolve(vs);
    // Chrome loads them after the first ask.
    const t = setTimeout(() => resolve(synth().getVoices()), 1000);
    synth().addEventListener("voiceschanged", () => { clearTimeout(t); resolve(synth().getVoices()); }, { once: true });
  });
  return {
    speak: async (b) => {
      const o = JSON.parse(dec.decode(b));
      const s = synth();
      const u = new SpeechSynthesisUtterance(o.text || "");
      const vs = o.voice || o.language ? await voices() : [];
      let voice = o.voice ? vs.find((v) => v.voiceURI === o.voice || v.name === o.voice) : null;
      if (!voice && o.language) {
        const lang = o.language.toLowerCase();
        voice = vs.find((v) => v.lang.toLowerCase() === lang) || vs.find((v) => v.lang.toLowerCase().startsWith(lang.split("-")[0]));
      }
      if (voice) u.voice = voice;
      u.lang = voice?.lang || o.language || "";
      u.pitch = Math.min(Math.max(o.pitch || 1, 0.5), 2);
      u.rate = Math.min(Math.max(o.rate || 1, 0.5), 2);
      u.volume = Math.min(Math.max(o.volume ?? 1, 0), 1);
      return new Promise((resolve, reject) => {
        const end = (err) => {
          if (!pending.has(u)) return;
          pending.delete(u);
          err ? reject(err) : resolve(null);
        };
        pending.set(u, end);
        u.onend = () => end(null);
        u.onerror = (e) => end(new Error(e.error === "canceled" || e.error === "interrupted" ? "stopped" : `speech: ${e.error}`));
        s.speak(u);
      });
    },
    stop: () => {
      const ends = [...pending.values()];
      pending.clear();
      globalThis.speechSynthesis?.cancel();
      for (const end of ends) end(new Error("stopped"));
    },
    pause: () => { synth().pause(); },
    resume: () => { synth().resume(); },
    speaking: () => new Uint8Array([globalThis.speechSynthesis && (speechSynthesis.speaking || speechSynthesis.paused || speechSynthesis.pending) ? 1 : 0]),
    voices: async () => JSON.stringify((await voices()).map((v) => ({ id: v.voiceURI, name: v.name, language: v.lang, quality: 0, network: !v.localService }))),
  };
}

// --- audio ---

function audio(h) {
  const players = new Map(); // id -> { el, once, ended, rate }
  const release = (id) => {
    const p = players.get(id);
    if (!p) return;
    players.delete(id);
    p.el.pause();
    p.el.removeAttribute("src");
    p.el.load();
  };
  const play = (p) => {
    if (!p) return;
    if (p.ended) { p.ended = false; p.el.currentTime = 0; }
    p.el.playbackRate = p.rate;
    p.el.play().catch((e) => console.warn("asuka: audio:", e.message));
  };
  const m = (f) => (b) => {
    const [id, rest] = splitID(b);
    f(id, players.get(id), rest);
  };
  return {
    load: m((id, _, rest) => {
      release(id);
      const el = new Audio();
      el.preload = "auto";
      el.preservesPitch = true; // a voice keeps its pitch at any rate
      const p = { el, once: false, ended: false, rate: 1 };
      el.addEventListener("ended", () => {
        if (el.loop) return;
        if (p.once) release(id);
        else p.ended = true;
      });
      el.src = sourceURL(h, dec.decode(rest));
      players.set(id, p);
    }),
    play: m((id, p) => play(p)),
    playOnce: m((id, p) => { if (p) { p.once = true; play(p); } }),
    pause: m((id, p) => p?.el.pause()),
    seek: m((id, p, rest) => { if (p) { p.ended = false; p.el.currentTime = f64(rest); } }),
    volume: m((id, p, rest) => { if (p) p.el.volume = Math.min(Math.max(f64(rest), 0), 1); }),
    loop: m((id, p, rest) => { if (p) p.el.loop = rest[0] === 1; }),
    rate: m((id, p, rest) => { if (p) { p.rate = f64(rest) || 1; p.el.playbackRate = p.rate; } }),
    release: m((id) => release(id)),
    status: (b) => {
      const p = players.get(splitID(b)[0]);
      if (!p) return encodeStatus(0, 0, false, false);
      return encodeStatus(p.el.currentTime, p.el.duration, !p.el.paused && !p.el.ended, p.ended);
    },
  };
}

// --- audioRecord ---

/** parseRecordArgs reads audioRecord.start's args. */
export function parseRecordArgs(b) {
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  let p = 6;
  const str = () => {
    if (p + 4 > b.length) return "";
    const n = v.getUint32(p, true);
    const s = dec.decode(b.subarray(p + 4, p + 4 + n));
    p += 4 + n;
    return s;
  };
  const quality = b[0] || 0, maxMs = b.length >= 5 ? v.getUint32(1, true) : 0, flags = b[5] || 0;
  const title = str(), path = str();
  return { quality, maxMs, background: !!(flags & 1), stream: !!(flags & 2), title, path };
}

/** recordingType is the container and codec the browser records in, and the file's extension. */
export function recordingType(supported, video = false) {
  const kinds = video
    ? [["video/mp4;codecs=avc1,mp4a.40.2", "mp4"], ["video/mp4", "mp4"], ["video/webm;codecs=vp9,opus", "webm"], ["video/webm", "webm"]]
    : [["audio/mp4;codecs=mp4a.40.2", "m4a"], ["audio/mp4", "m4a"], ["audio/webm;codecs=opus", "webm"], ["audio/ogg;codecs=opus", "ogg"], ["audio/webm", "webm"]];
  for (const [type, ext] of kinds) if (supported(type)) return { type, ext };
  return { type: "", ext: "webm" }; // the browser's default
}

/** levelOf is an RMS amplitude (0 to 1) as a byte, on iOS's meter's scale: silence below -60 dB. */
export function levelOf(rms) {
  if (!(rms > 0.001)) return 0;
  return Math.round(Math.min(rms, 1) * 255);
}

export const uuid = () => globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2) + Date.now().toString(36);

/** Recording is a MediaRecorder writing a file of the app's (shared with camera.View's video). */
export class Recording {
  constructor(stream, { type, path, bitrate, video, maxMs, stream: progressive }, done) {
    this.stream = stream;
    this.path = path;
    this.chunks = [];
    this.done = done;
    this.elapsed = 0;
    this.since = performance.now();
    const opts = {};
    if (type) opts.mimeType = type;
    if (bitrate) opts[video ? "bitsPerSecond" : "audioBitsPerSecond"] = bitrate;
    this.rec = new MediaRecorder(stream, opts);
    this.rec.ondataavailable = (e) => {
      if (!e.data.size) return;
      this.chunks.push(e.data);
      if (progressive) this.write();
    };
    // onstop comes after the last data.
    this.rec.onstop = () => this.finish(null);
    this.rec.onerror = (e) => this.finish(e.error || new Error("couldn't record"));
    this.rec.start(1000);
    if (maxMs > 0) this.timer = setTimeout(() => this.stop(), maxMs);
  }
  /** write writes what's recorded so far to the file (in order: each after the last). */
  write() {
    const blob = new Blob(this.chunks, { type: this.rec.mimeType });
    this.writing = (this.writing || Promise.resolve()).then(async () => {
      fs.writeFile(this.path, new Uint8Array(await blob.arrayBuffer()));
    });
    return this.writing;
  }
  get recording() { return this.rec.state === "recording"; }
  ms() { return Math.round(this.elapsed + (this.recording ? performance.now() - this.since : 0)); }
  pause() {
    if (!this.recording) return;
    this.elapsed += performance.now() - this.since;
    this.rec.pause();
  }
  resume() {
    if (this.rec.state !== "paused") return;
    this.since = performance.now();
    this.rec.resume();
  }
  stop() {
    clearTimeout(this.timer);
    if (this.rec.state !== "inactive") {
      if (this.recording) this.elapsed += performance.now() - this.since;
      this.rec.stop();
    }
  }
  finish(err) {
    if (!this.done) return;
    const done = this.done;
    this.done = null;
    clearTimeout(this.timer);
    if (err) done(err, "");
    else this.write().then(() => done(null, this.path), (e) => done(e, ""));
  }
}

export function mediaError(e) {
  switch (e?.name) {
    case "NotAllowedError":
    case "SecurityError":
      return new Error("not allowed");
    case "NotFoundError":
    case "OverconstrainedError":
      return null; // no such device: the caller says which
  }
  return e instanceof Error ? e : new Error(String(e));
}

function audioRecord() {
  let rec = null; // a Recording
  let meter = null; // { ctx, analyser, data }
  const level = () => {
    if (!rec?.recording || !meter) return 0;
    meter.analyser.getFloatTimeDomainData(meter.data);
    let sum = 0;
    for (const x of meter.data) sum += x * x;
    return levelOf(Math.sqrt(sum / meter.data.length));
  };
  return {
    start: async (b) => {
      if (rec) throw new Error("already recording");
      const o = parseRecordArgs(b);
      const music = o.quality === 1;
      if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") throw new Error("asuka: audioRecord isn't in this browser");
      let stream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          audio: music
            ? { channelCount: { ideal: 2 }, echoCancellation: false, noiseSuppression: false, autoGainControl: false }
            : { channelCount: { ideal: 1 }, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        });
      } catch (e) {
        throw mediaError(e) || new Error("no microphone");
      }
      const { type, ext } = recordingType((t) => MediaRecorder.isTypeSupported(t));
      const path = o.path || `/tmp/asuka-audio/${uuid()}.${ext}`;
      try {
        const ctx = new AudioContext();
        ctx.resume().catch(() => {});
        const analyser = ctx.createAnalyser();
        analyser.fftSize = 1024;
        ctx.createMediaStreamSource(stream).connect(analyser);
        meter = { ctx, analyser, data: new Float32Array(analyser.fftSize) };
      } catch {
        meter = null;
      }
      return new Promise((resolve, reject) => {
        rec = new Recording(stream, { type, path, bitrate: music ? 128000 : 64000, maxMs: o.maxMs, stream: o.stream }, (err, p) => {
          stream.getTracks().forEach((t) => t.stop());
          meter?.ctx.close().catch(() => {});
          meter = null;
          rec = null;
          err ? reject(err) : resolve(p);
        });
      });
    },
    stop: () => { rec?.stop(); },
    pause: () => { rec?.pause(); },
    resume: () => { rec?.resume(); },
    level: () => new Uint8Array([level()]),
    status: () => {
      const b = new Uint8Array(6);
      b[0] = level();
      b[1] = rec?.recording ? 1 : 0;
      new DataView(b.buffer).setUint32(2, rec ? rec.ms() : 0, true);
      return b;
    },
  };
}

export function modules(h) {
  return { speech: speech(), audio: audio(h), audioRecord: audioRecord() };
}
