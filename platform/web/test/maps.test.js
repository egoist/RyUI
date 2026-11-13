// maps.View's map in the browser (modules/maps.js): its projection, the
// zoom a region needs, its tiles' URLs, its pins' colors.
import { test } from "node:test";
import assert from "node:assert/strict";
import { mercY, mercLat, zoomFor, tileURL, cssColor } from "../src/modules/maps.js";
import { natives } from "../src/modules/hardware.js";

const near = (a, b, eps = 1e-9) => assert.ok(Math.abs(a - b) < eps, `${a} ≉ ${b}`);

test("Web Mercator: the equator is the middle, a latitude comes back as it went", () => {
  near(mercY(0), 0.5);
  near(mercY(85.05112878), 0, 1e-8);
  near(mercY(-85.05112878), 1, 1e-8);
  near(mercY(90), mercY(85.05112878)); // past the edge: clamped
  for (const lat of [-80, -45.5, -1, 0, 12.34, 51.5, 80]) near(mercLat(mercY(lat)), lat, 1e-9);
});

test("a region's zoom: its span fits the view, its narrower side deciding", () => {
  // The world (360°) in 256 points is zoom 0; in 512, zoom 1.
  near(zoomFor({ center: { lat: 0, lng: 0 }, latDelta: 0, lngDelta: 360 }, 256, 256), 0);
  near(zoomFor({ center: { lat: 0, lng: 0 }, latDelta: 0, lngDelta: 360 }, 512, 512), 1);
  // A city (0.1° of longitude) in 400 points.
  near(zoomFor({ center: { lat: 0, lng: 0 }, latDelta: 0, lngDelta: 0.1 }, 400, 1000), Math.log2((400 * 360) / (256 * 0.1)));
  // Tall and narrow: the latitude's span decides.
  const r = { center: { lat: 48.85, lng: 2.35 }, latDelta: 0.2, lngDelta: 0.01 };
  const span = Math.abs(mercY(48.95) - mercY(48.75));
  near(zoomFor(r, 400, 300), Math.log2(300 / (256 * span)));
  // Never past the tiles' last level, never below the world's.
  assert.equal(zoomFor({ center: { lat: 0, lng: 0 }, latDelta: 1e-9, lngDelta: 1e-9 }, 400, 400), 19);
  assert.equal(zoomFor({ center: { lat: 0, lng: 0 }, latDelta: 0, lngDelta: 360 }, 10, 10), 0);
});

test("tiles' URLs follow the template", () => {
  assert.equal(tileURL("https://tile.openstreetmap.org/{z}/{x}/{y}.png", 3, 4, 5), "https://tile.openstreetmap.org/3/4/5.png");
  assert.equal(tileURL("http://localhost:9/t?z={z}&x={x}&y={y}", 0, 0, 0), "http://localhost:9/t?z=0&x=0&y=0");
});

test("a pin's color is the marker's #rrggbbaa, or none", () => {
  assert.equal(cssColor("#ff000080"), "#ff000080");
  assert.equal(cssColor(""), "");
  assert.equal(cssColor(undefined), "");
  assert.equal(cssColor("red"), "");
});

test("the host registers asuka.map", () => {
  assert.equal(typeof natives["asuka.map"], "function");
  assert.equal(typeof natives["asuka.camera"], "function");
});
