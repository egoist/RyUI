// Rules shared by the HTML pages and the Markdown downloads.
import path from "node:path"
import type { Root } from "mdast"

import { localeInfo, locales, type Locale } from "./locales.ts"

export interface LinkOptions {
  docsDir: string
  repoDir: string
  repoUrl: string
  /** Where the docs are on the site: /docs, /zh/docs, /ja/docs. */
  base: string
}

/** Pages of the repository's docs that aren't the site's: notes for maintainers. */
const unpublished: Record<Locale, Set<string>> = {
  en: new Set(["device-checklist.md"]),
  zh: new Set(["TRANSLATING.md"]),
  ja: new Set(["TRANSLATING.md"]),
}

/** The directories of the docs in other languages, inside docs/: zh, ja. */
const translations = locales.flatMap((l) => (l === "en" ? [] : [localeInfo[l].docsDir.replace(/^docs\//, "")]))

/** Whether a file of a language's docs, relative to them, is a page of the site. */
export function isPublished(locale: Locale, file: string) {
  const name = file.split(path.sep).join("/")
  if (!name.endsWith(".md") || unpublished[locale].has(name)) return false
  return locale !== "en" || !translations.some((dir) => name.startsWith(dir + "/"))
}

/** Where a language's docs are on the site. */
export function docsBase(locale: Locale) {
  return localeInfo[locale].prefix + "/docs"
}

/** README.md is the directory's page; other files keep their path. */
export function slugOf(file: string) {
  const name = file.replace(/\.md$/, "").split(path.sep).join("/")
  return name === "README" ? "" : name.replace(/\/README$/, "")
}

/** Repository-only blocks stay on GitHub, outside all published formats. */
export function omitRepositoryOnly(tree: Root) {
  let hidden = false
  tree.children = tree.children.filter((node) => {
    if (node.type === "html") {
      const marker = node.value.trim()
      if (marker === "<!-- repository-only:start -->" || marker === "<!-- repository-only:end -->") {
        hidden = marker === "<!-- repository-only:start -->"
        return false
      }
    }
    return !hidden
  })
}

/** Links between docs stay on the site; links to repository files go to GitHub. */
export function rewriteHref(href: string, file: string, links: LinkOptions, markdown = false) {
  if (/^[a-z][a-z0-9+.-]*:|^\/\/|^#|^\//i.test(href)) return href
  const end = href.search(/[?#]/)
  const target = end < 0 ? href : href.slice(0, end)
  const suffix = end < 0 ? "" : href.slice(end)
  const abs = path.resolve(path.dirname(file), decodeURIComponent(target))
  const rel = path.relative(links.docsDir, abs)
  if (abs.endsWith(".md") && !rel.startsWith("..") && !path.isAbsolute(rel)) {
    const slug = slugOf(rel)
    return (slug ? `${links.base}/${slug}` : links.base) + (markdown ? ".md" : "") + suffix
  }
  const repoPath = path.relative(links.repoDir, abs).split(path.sep).join("/")
  return `${links.repoUrl}/${path.extname(abs) ? "blob" : "tree"}/main/${repoPath}${suffix}`
}
