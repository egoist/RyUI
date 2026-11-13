// Shader: a Kage program drawn with WebGL 2. The Go side translates the
// compiled shader to GLSL ES 3.00 (internal/shadercode's GLSL); its
// uniforms are U0, U1... in the order of the code's, their values the
// core's lanes, and the size, scale and Time lanes the host's own.

import { Kind, KindProp as P, Reader } from "./ops.js";
import { own } from "./views.js";

const vertex = `#version 300 es
void main() {
  vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
  gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}`;

function decodeCode(b) {
  const r = new Reader(b);
  r.bytes(); r.bytes(); r.bytes(); // Kage, Metal, AGSL
  const lanes = r.u32(), size = r.u32(), scale = r.u32(), time = r.u32() - 1;
  r.u32(); // Metal's buffer size
  for (let i = 0; i < lanes; i++) r.u32();
  const n = r.u32();
  const uniforms = [];
  for (let i = 0; i < n; i++) uniforms.push({ name: r.string(), lane: r.u32(), lanes: r.u32(), kind: r.atEnd ? 0 : r.u8() });
  return { lanes, size, scale, time, uniforms };
}

const shader = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-shader";
    const c = document.createElement("canvas");
    el.appendChild(c);
    n.canvas = c;
    n.lanes = new Uint32Array(0);
    n.start = performance.now();
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Shader.Code:
        n.code = decodeCode(v);
        n.glsl = h.core.shaderGLSL(v.slice());
        n.program = null;
        if (!n.glsl) console.warn("asuka: the web host can't draw this shader");
        break;
      case P.Shader.Uniforms: {
        const k = Math.floor(v.length / 4);
        n.lanes = new Uint32Array(k);
        new Uint8Array(n.lanes.buffer).set(v.subarray(0, k * 4));
        break;
      }
      case P.Shader.Animated:
        n.animated = !!v;
        if (v) tick(h, n);
        break;
    }
    draw(h, n);
  },
  resized(h, n) { draw(h, n); },
  destroy(h, n) { n.animated = false; n.dead = true; return false; },
  measure() { return [0, 0]; },
};

function tick(h, n) {
  if (n.ticking) return;
  n.ticking = true;
  const f = () => {
    if (!n.animated || n.dead) { n.ticking = false; return; }
    draw(h, n);
    requestAnimationFrame(f);
  };
  requestAnimationFrame(f);
}

function compile(n) {
  const gl = n.gl;
  const sh = (type, src) => {
    const s = gl.createShader(type);
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s));
    return s;
  };
  const prog = gl.createProgram();
  gl.attachShader(prog, sh(gl.VERTEX_SHADER, vertex));
  gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, n.glsl));
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(prog));
  // Where each active uniform's values come from: its uniform's lanes.
  const setters = [];
  const count = gl.getProgramParameter(prog, gl.ACTIVE_UNIFORMS);
  for (let i = 0; i < count; i++) {
    const info = gl.getActiveUniform(prog, i);
    const m = /^U(\d+)(\[0\])?$/.exec(info.name);
    if (!m) continue;
    const u = n.code.uniforms[Number(m[1])];
    if (!u) continue;
    setters.push({ loc: gl.getUniformLocation(prog, info.name), type: info.type, size: info.size, lane: u.lane, lanes: u.lanes });
  }
  n.program = prog;
  n.setters = setters;
}

function draw(h, n) {
  if (!n.glsl || !n.placed) return;
  const c = n.canvas;
  const k = window.devicePixelRatio || 1;
  const w = n.frame[2], hh = n.frame[3];
  const pw = Math.max(1, Math.round(w * k)), ph = Math.max(1, Math.round(hh * k));
  if (c.width !== pw) c.width = pw;
  if (c.height !== ph) c.height = ph;
  c.style.width = w + "px";
  c.style.height = hh + "px";
  if (!n.gl) n.gl = c.getContext("webgl2", { premultipliedAlpha: true, alpha: true, antialias: false });
  const gl = n.gl;
  if (!gl) return;
  if (!n.program) {
    try { compile(n); } catch (e) { console.warn("asuka: shader:", e.message); n.glsl = null; return; }
  }
  const code = n.code;
  const lanes = new Uint32Array(Math.max(code.lanes, n.lanes.length));
  lanes.set(n.lanes.subarray(0, lanes.length));
  const f = new Float32Array(lanes.buffer);
  f[code.size] = w;
  f[code.size + 1] = hh;
  f[code.scale] = k;
  if (code.time >= 0) f[code.time] = (performance.now() - n.start) / 1000;
  const i = new Int32Array(lanes.buffer);
  gl.viewport(0, 0, pw, ph);
  gl.useProgram(n.program);
  for (const s of n.setters) {
    const fl = f.subarray(s.lane, s.lane + s.lanes), it = i.subarray(s.lane, s.lane + s.lanes), ui = lanes.subarray(s.lane, s.lane + s.lanes);
    switch (s.type) {
      case gl.FLOAT: gl.uniform1fv(s.loc, fl); break;
      case gl.FLOAT_VEC2: gl.uniform2fv(s.loc, fl); break;
      case gl.FLOAT_VEC3: gl.uniform3fv(s.loc, fl); break;
      case gl.FLOAT_VEC4: gl.uniform4fv(s.loc, fl); break;
      case gl.FLOAT_MAT2: gl.uniformMatrix2fv(s.loc, false, fl); break;
      case gl.FLOAT_MAT3: gl.uniformMatrix3fv(s.loc, false, fl); break;
      case gl.FLOAT_MAT4: gl.uniformMatrix4fv(s.loc, false, fl); break;
      case gl.INT: case gl.BOOL: gl.uniform1iv(s.loc, it); break;
      case gl.INT_VEC2: case gl.BOOL_VEC2: gl.uniform2iv(s.loc, it); break;
      case gl.INT_VEC3: case gl.BOOL_VEC3: gl.uniform3iv(s.loc, it); break;
      case gl.INT_VEC4: case gl.BOOL_VEC4: gl.uniform4iv(s.loc, it); break;
      case gl.UNSIGNED_INT: gl.uniform1uiv(s.loc, ui); break;
      case gl.UNSIGNED_INT_VEC2: gl.uniform2uiv(s.loc, ui); break;
      case gl.UNSIGNED_INT_VEC3: gl.uniform3uiv(s.loc, ui); break;
      case gl.UNSIGNED_INT_VEC4: gl.uniform4uiv(s.loc, ui); break;
    }
  }
  gl.clearColor(0, 0, 0, 0);
  gl.clear(gl.COLOR_BUFFER_BIT);
  gl.drawArrays(gl.TRIANGLES, 0, 3);
}

export const shaderKind = shader;
