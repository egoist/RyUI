// A snackbar's time and place (src/snackbar.js): the durations, and the
// bar centered at the bottom of its window, above what covers it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { timeout, place, margin, maxWidth } from "../src/snackbar.js";

test("durations", () => {
  assert.equal(timeout(0), 4000); // Short: Material's
  assert.equal(timeout(1), 10000); // Long
  assert.equal(timeout(2), 0); // Indefinite: until it's dismissed
});

test("placed at the bottom, centered, above a tab bar or the keyboard", () => {
  const area = { x: 0, y: 0, w: 400, h: 800 };
  assert.deepEqual(place(area, 0, 300, 48), { x: 50, y: 800 - margin - 48, w: 300 });
  // A bottom tab bar (or the keyboard) 56 high: above it.
  assert.equal(place(area, 56, 300, 48).y, 800 - 56 - margin - 48);
  // As wide as the window lets it be, less its margins.
  assert.deepEqual(place({ x: 0, y: 0, w: 320, h: 600 }, 0, 500, 48), { x: margin, y: 600 - margin - 48, w: 320 - 2 * margin });
  // At most maxWidth on a wide window; a Window's panel's area is offset.
  const wide = place({ x: 100, y: 50, w: 1600, h: 900 }, 0, 2000, 48);
  assert.equal(wide.w, maxWidth);
  assert.equal(wide.x, 100 + (1600 - maxWidth) / 2);
  assert.equal(wide.y, 50 + 900 - margin - 48);
});
