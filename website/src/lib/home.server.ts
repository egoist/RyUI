import fs from "node:fs/promises"
import path from "node:path"
import type { Code, Root } from "mdast"
import remarkParse from "remark-parse"
import { unified } from "unified"

import type { HomeData } from "@/lib/home"
import { highlightCode } from "@/lib/markdown.server"

/** The example at the top of the docs: an app, in Go. */
export async function loadHome(): Promise<HomeData> {
  const readme = await fs.readFile(path.resolve(process.cwd(), "../docs/README.md"), "utf8")
  const tree = unified().use(remarkParse).parse(readme) as Root
  const go = tree.children.find((n): n is Code => n.type === "code" && n.lang === "go")
  if (!go) throw new Error("docs/README.md: expected a go example")
  return { main: await highlightCode(go.value, "go") }
}
