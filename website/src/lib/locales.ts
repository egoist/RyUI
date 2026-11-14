// The site's languages: English at the root, Chinese under /zh and Japanese
// under /ja. The docs of each come from the repository: docs/, docs/zh/ and
// docs/ja/. (No React here: the Vite config reads this too.)

export const locales = ["en", "zh", "ja"] as const
export type Locale = (typeof locales)[number]

export const localeInfo: Record<Locale, { name: string; short: string; lang: string; prefix: string; docsDir: string }> = {
  en: { name: "English", short: "EN", lang: "en", prefix: "", docsDir: "docs" },
  zh: { name: "简体中文", short: "中文", lang: "zh-CN", prefix: "/zh", docsDir: "docs/zh" },
  ja: { name: "日本語", short: "日本語", lang: "ja", prefix: "/ja", docsDir: "docs/ja" },
}

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (locales as readonly string[]).includes(value)
}

/** The language of a path: /zh and what's under it are Chinese, /ja Japanese. */
export function localeOf(pathname: string): Locale {
  for (const l of locales) {
    const prefix = localeInfo[l].prefix
    if (prefix && (pathname === prefix || pathname.startsWith(prefix + "/"))) return l
  }
  return "en"
}

/** A path without its language: /zh/docs/layout → /docs/layout. */
export function unlocalized(pathname: string) {
  const prefix = localeInfo[localeOf(pathname)].prefix
  return prefix ? pathname.slice(prefix.length) || "/" : pathname
}

/** A path of the site in a language: /docs/layout → /zh/docs/layout. */
export function localized(locale: Locale, pathname: string) {
  const prefix = localeInfo[locale].prefix
  return prefix && pathname === "/" ? prefix : prefix + pathname
}

/** The prefixes of the site's paths, a regular expression's source: (?:/zh|/ja)?. */
export const prefixPattern = `(?:${locales
  .map((l) => localeInfo[l].prefix)
  .filter(Boolean)
  .map((p) => p.replace(/\//g, "\\/"))
  .join("|")})?`

/** The routes of a language's home page, docs and pages of the docs, for links. */
export const routes = {
  en: { home: "/", docs: "/docs", page: "/docs/$" },
  zh: { home: "/zh", docs: "/zh/docs", page: "/zh/docs/$" },
  ja: { home: "/ja", docs: "/ja/docs", page: "/ja/docs/$" },
} as const satisfies Record<Locale, { home: string; docs: string; page: string }>
