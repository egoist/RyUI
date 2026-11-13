// The docs number their pages as a title card numbers episodes: 第参話,
// Episode 03. The numerals are the formal ones (壱, 弐, 参), which the site's
// cut of Shippori Mincho has.

const digits = ["零", "壱", "弐", "参", "肆", "伍", "陸", "漆", "捌", "玖"]

/** n in kanji: 0 to 99. */
export function kanji(n: number): string {
  if (n < 10) return digits[n]
  const tens = Math.floor(n / 10)
  const ones = n % 10
  return (tens > 1 ? digits[tens] : "") + "拾" + (ones ? digits[ones] : "")
}

/** "第参話". */
export function episodeKanji(n: number) {
  return `第${kanji(n)}話`
}

/** "03". */
export function episodeNumber(n: number) {
  return String(n).padStart(2, "0")
}
