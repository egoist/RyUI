// The site's languages: English at the root, Chinese under /zh. The docs of
// each come from the repository: docs/ and docs/zh/. (No React here: the
// Vite config reads this too.)

export const locales = ["en", "zh"] as const
export type Locale = (typeof locales)[number]

export const localeInfo: Record<Locale, { name: string; lang: string; prefix: string; docsDir: string }> = {
  en: { name: "English", lang: "en", prefix: "", docsDir: "docs" },
  zh: { name: "简体中文", lang: "zh-CN", prefix: "/zh", docsDir: "docs/zh" },
}

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (locales as readonly string[]).includes(value)
}

/** The language of a path: /zh and what's under it are Chinese. */
export function localeOf(pathname: string): Locale {
  return pathname === "/zh" || pathname.startsWith("/zh/") ? "zh" : "en"
}

/** A path without its language: /zh/docs/layout → /docs/layout. */
export function unlocalized(pathname: string) {
  return localeOf(pathname) === "zh" ? pathname.slice(3) || "/" : pathname
}

/** A path of the site in a language: /docs/layout → /zh/docs/layout. */
export function localized(locale: Locale, pathname: string) {
  const prefix = localeInfo[locale].prefix
  return prefix && pathname === "/" ? prefix : prefix + pathname
}
