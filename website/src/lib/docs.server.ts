// The docs of the repository (../docs), read and rendered on the server. The
// site is prerendered, so this runs at build time only.

import fs from "node:fs/promises"
import path from "node:path"
import type { List, Root } from "mdast"
import { toString } from "mdast-util-to-string"
import remarkParse from "remark-parse"
import { unified } from "unified"

import type { Doc, Navs, NavItem, NavSection } from "@/lib/docs"
import { renderMarkdown, slugOf, type RenderedMarkdown } from "@/lib/markdown.server"
import { unpublished } from "@/lib/markdown-source"
import { site } from "@/lib/site"

// Vite runs in website/, next to the repository's docs.
const repoDir = path.resolve(process.cwd(), "..")
const docsDir = path.join(repoDir, "docs")

/** The files a page may be: x.md, else x/README.md. */
function filesOf(slug: string) {
  if (slug === "") return ["README.md"]
  if (!/^[a-z0-9-]+(\/[a-z0-9-]+)*$/.test(slug)) return []
  return [`${slug}.md`, `${slug}/README.md`]
}

/** The file of a page, relative to the docs, or undefined for none. */
async function fileOf(slug: string) {
  for (const file of filesOf(slug)) {
    if (unpublished.has(file)) continue
    if (await fs.stat(path.join(docsDir, file)).then((s) => s.isFile(), () => false)) return file
  }
  return undefined
}

// Pages are rendered once per build; in development, on every request.
const cache = new Map<string, Promise<RenderedMarkdown | undefined>>()

function render(slug: string) {
  let page = cache.get(slug)
  if (!page) {
    page = fileOf(slug).then((file) => {
      if (!file) return undefined
      const abs = path.join(docsDir, file)
      return fs.readFile(abs, "utf8").then(
        (source) => renderMarkdown(source, abs, { docsDir, repoDir, repoUrl: site.repo }),
        () => undefined
      )
    })
    if (!import.meta.env.DEV) cache.set(slug, page)
  }
  return page
}

/**
 * The sidebar: the lists of the docs' README, in their order, under their
 * headings, then the pages it leaves out.
 */
export async function loadNav(): Promise<Navs> {
  const readme = await fs.readFile(path.join(docsDir, "README.md"), "utf8")
  const tree = unified().use(remarkParse).parse(readme) as Root
  const sections: NavSection[] = [{ title: "Overview", items: [{ slug: "", title: "Introduction" }] }]
  let heading = ""
  for (const node of tree.children) {
    if (node.type === "heading" && node.depth === 2) heading = toString(node)
    if (node.type === "list" && heading) sections.push({ title: heading, items: navItems(node, ".") })
  }

  const listed = new Set(sections.flatMap((s) => s.items.map((i) => i.slug)))
  const files = (await fs.readdir(docsDir, { recursive: true })).filter((f) => f.endsWith(".md") && !unpublished.has(f)).sort()
  const rest: NavItem[] = []
  for (const file of files) {
    const slug = slugOf(file)
    if (listed.has(slug)) continue
    const page = await render(slug)
    rest.push({ slug, title: page?.title || slug })
  }
  if (rest.length) sections.push({ title: "More", items: rest })
  return sections
}

function navItems(list: List, base: string): NavItem[] {
  return list.children.flatMap((item): NavItem[] => {
    const paragraph = item.children[0]
    if (paragraph?.type !== "paragraph") return []
    const [first, ...rest] = paragraph.children
    const description = sentence(rest.map((n) => toString(n)).join(""))
    if (first?.type === "link" && first.url.endsWith(".md") && !/^[a-z]+:/.test(first.url)) {
      return [{ slug: slugOf(path.join(base, first.url)), title: toString(first), description }]
    }
    // "The Go API: `go doc -all <package>`, …": the package on pkg.go.dev.
    const pkg = rest[0]?.type === "inlineCode" ? /^go doc (?:-all )?(\S+)$/.exec(rest[0].value)?.[1] : undefined
    if (first?.type === "text" && pkg) {
      return [{ href: `https://pkg.go.dev/${pkg}`, title: first.value.split(":")[0]!.replace(/^The /, "") }]
    }
    return []
  })
}

/** "`: install the tools, then …`" → "Install the tools, then …". */
function sentence(text: string) {
  const s = text.replace(/^[\s:]+/, "").replace(/\s+/g, " ").trim()
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export async function loadDoc(slug: string): Promise<Doc | undefined> {
  const [page, nav, file] = await Promise.all([render(slug), loadNav(), fileOf(slug)])
  if (!page || !file) return undefined
  const sections = nav
  const order = sections.flatMap((s) => s.items).filter((i) => i.slug !== undefined)
  const index = order.findIndex((i) => i.slug === slug)
  const item = order[index]
  const link = (i: NavItem | undefined) => (i?.slug === undefined ? undefined : { slug: i.slug, title: i.title })
  return {
    slug,
    title: page.title,
    description: item?.description || page.lead,
    summary: item?.description,
    section: sections.find((s) => s.items.includes(item!))?.title,
    html: page.html,
    toc: page.toc,
    prev: index > 0 ? link(order[index - 1]) : undefined,
    next: index >= 0 ? link(order[index + 1]) : undefined,
    editUrl: `${site.repo}/edit/main/docs/${file}`,
  }
}
