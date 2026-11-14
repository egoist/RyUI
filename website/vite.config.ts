import { execFileSync } from "node:child_process"
import fs from "node:fs"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { tanstackStart } from "@tanstack/react-start/plugin/vite"
import viteReact from "@vitejs/plugin-react"
import { defineConfig } from "vite"
import { llmsDocs } from "./llms.ts"
import { localeInfo, locales, localized } from "./src/lib/locales.ts"
import { isPublished } from "./src/lib/markdown-source.ts"

const repo = (file: string) => new URL(`../${file}`, import.meta.url)

// The pages of the repository's docs, in each language: shipping.md at
// /docs/shipping (docs/zh/shipping.md at /zh/docs/shipping, docs/ja/shipping.md at /ja/docs/shipping), and a
// directory's README at the directory. /docs itself, like every route
// without parameters, is found without being listed.
const docs = locales.flatMap((locale) =>
  fs
    .readdirSync(repo(localeInfo[locale].docsDir), { recursive: true, encoding: "utf8" })
    .filter((file) => file !== "README.md" && isPublished(locale, file))
    .map((file) => localized(locale, `/docs/${file.slice(0, -3).split(path.sep).join("/").replace(/\/README$/, "")}`))
)

// Asuka's version: the highest release tag. Not `git describe`: it needs the
// tag's commit in HEAD's history, which a shallow clone (Cloudflare's builds)
// cuts off. A shallow clone may also come without tags, so fetch them once.
const asukaVersion = (() => {
  const latest = () => execFileSync("git", ["tag", "--list", "v*", "--sort=-v:refname"], { encoding: "utf8" }).split("\n")[0].trim()
  let version = latest()
  if (!version) {
    execFileSync("git", ["fetch", "--tags", "--quiet"], { stdio: "ignore" })
    version = latest()
  }
  if (!version) throw new Error("no v* release tag found, even after git fetch --tags")
  return version
})()

export default defineConfig({
  define: {
    __ASUKA_VERSION__: JSON.stringify(asukaVersion),
  },
  resolve: { tsconfigPaths: true },
  plugins: [
    llmsDocs(path.resolve(import.meta.dirname, "..")),
    tailwindcss(),
    tanstackStart({
      // A static site: every page is rendered to HTML at build time.
      prerender: { enabled: true, crawlLinks: true, filter: ({ path }) => !path.includes("#") },
      pages: [
        ...docs.map((path) => ({ path })),
        ...locales.map((locale) => ({ path: localized(locale, "/docs/search.json") })),
        // Served by Cloudflare for any path that has no page.
        { path: "/404", prerender: { enabled: true, outputPath: "/404.html" } },
      ],
    }),
    viteReact(),
  ],
})
