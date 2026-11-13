import { useMatch } from "@tanstack/react-router"
import { ArrowLeftIcon, ArrowRightIcon, ChevronRightIcon, MenuIcon } from "lucide-react"
import * as React from "react"

import { DocContent } from "@/components/docs/doc-content"
import { DocLink } from "@/components/docs/doc-link"
import { DocsNav } from "@/components/docs/docs-nav"
import { NotFound } from "@/components/not-found"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { getDoc, type Doc, type Navs } from "@/lib/docs"
import { episodeKanji, episodeNumber } from "@/lib/episode"
import { alternates, localized, messages, useMessages, type Locale } from "@/lib/i18n"

/**
 * The loader of a page: null, before anything is fetched, for a slug without
 * a page, which the docs show as not found. (Rather than throwing notFound():
 * the router renders routes as the prerendered 404.html does, not errors.)
 */
export async function loadDocPage(locale: Locale, slug: string, parent: Promise<{ loaderData?: Navs }>) {
  const nav = (await parent).loaderData
  if (!nav?.some((s) => s.items.some((i) => i.slug === slug))) return null
  return getDoc({ data: { locale, slug } })
}

export function docHead(locale: Locale, doc: Doc | null | undefined) {
  if (!doc) return { meta: [{ title: messages[locale].notFound.pageTitle }] }
  const title = doc.slug ? `${doc.title} · Asuka` : doc.title
  const path = doc.slug ? `/docs/${doc.slug}` : "/docs"
  return {
    meta: [
      { title },
      { name: "description", content: doc.description },
      { property: "og:title", content: title },
      { property: "og:description", content: doc.description },
    ],
    links: [
      { rel: "alternate", type: "text/markdown", href: localized(locale, path) + ".md" },
      { rel: "describedby", href: localized(locale, "/llms.txt") },
      ...alternates(path),
    ],
  }
}

export function DocRoute({ doc }: { doc: Doc | null }) {
  return doc ? <DocPage doc={doc} /> : <NotFound />
}

/** The sidebar of the docs being read, English or Chinese. */
function useNav(): Navs {
  const en = useMatch({ from: "/docs", shouldThrow: false })
  const zh = useMatch({ from: "/zh/docs", shouldThrow: false })
  return (zh ?? en)?.loaderData ?? []
}

export function DocPage({ doc }: { doc: Doc }) {
  const nav = useNav()
  const t = useMessages().docs
  const [menuOpen, setMenuOpen] = React.useState(false)
  const article = React.useRef<HTMLElement>(null)
  const bar = React.useRef<HTMLDivElement>(null)
  useReadingProgress(article, bar)

  return (
    <div className="min-w-0">
      <div ref={bar} className="reading sticky top-14 z-30 flex h-11 items-center gap-1 border-b bg-background/85 px-2 backdrop-blur-xl lg:hidden">
        <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
          <SheetTrigger render={<Button variant="ghost" size="sm" />}>
            <MenuIcon /> {t.menu}
          </SheetTrigger>
          <SheetContent side="left" className="w-72 overflow-y-auto">
            <SheetHeader>
              <SheetTitle>{t.documentation}</SheetTitle>
            </SheetHeader>
            <div className="px-2 pb-8">
              <DocsNav sections={nav} page={doc} onNavigate={() => setMenuOpen(false)} />
            </div>
          </SheetContent>
        </Sheet>
        <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-sm">{doc.title}</span>
        <span aria-hidden className="label shrink-0 pr-2 pl-3 text-signal-ink">
          {t.sync} <span className="reading-rate" />
        </span>
        <span aria-hidden className="reading-bar absolute inset-x-0 -bottom-px h-0.5 origin-left bg-signal" />
      </div>

      <article ref={article} className="max-w-[52rem] px-4 pt-10 pb-20 sm:px-10 lg:pt-14">
        {/* The page's title card: its episode, its title, a band of hazard stripes. */}
        <header>
          <div className="label flex items-center justify-between gap-4">
            <span className="flex items-center gap-2">
              <span className="size-1.5 bg-unit" />
              {doc.section}
            </span>
            {doc.episode !== undefined && <span>Episode {episodeNumber(doc.episode)}</span>}
          </div>
          {doc.episode !== undefined && (
            <p lang="ja" className="display mt-8 text-lg text-unit-ink sm:text-xl">
              {episodeKanji(doc.episode)}
            </p>
          )}
          <h1 className="display mt-2 text-[clamp(2.6rem,6.5vw,4.5rem)] leading-[1.02] text-balance">
            <span className="block origin-bottom-left scale-y-[1.12]">{doc.title}</span>
          </h1>
          {doc.summary && <p className="mt-5 max-w-2xl text-lg leading-8 text-foreground/70">{doc.summary}</p>}
          <div className="hazard mt-8 h-2" />
        </header>

        <div className="mt-10">
          <DocContent html={doc.html} />
        </div>

        <footer className="mt-20">
          <nav className="grid gap-px border bg-border sm:grid-cols-2">
            {doc.prev ? (
              <DocLink slug={doc.prev.slug} className="group flex flex-col gap-2 bg-background p-5 transition-colors hover:bg-code">
                <span className="label flex items-center gap-1.5">
                  <ArrowLeftIcon className="size-3" /> {t.previous}
                </span>
                <span lang="ja" className="display text-sm text-unit-ink">
                  {episodeKanji(doc.prev.episode)}
                </span>
                <span className="display text-xl leading-tight transition-colors group-hover:text-unit-ink">{doc.prev.title}</span>
              </DocLink>
            ) : (
              <span className="hidden bg-background sm:block" />
            )}
            {doc.next && (
              <DocLink slug={doc.next.slug} className="group flex flex-col items-end gap-2 bg-background p-5 text-right transition-colors hover:bg-code">
                <span className="label flex items-center gap-1.5 text-signal-ink">
                  {t.next} <ArrowRightIcon className="size-3" />
                </span>
                <span lang="ja" className="display text-sm text-unit-ink">
                  {episodeKanji(doc.next.episode)}
                </span>
                <span className="display text-xl leading-tight transition-colors group-hover:text-unit-ink">{doc.next.title}</span>
              </DocLink>
            )}
          </nav>
          <a href={doc.editUrl} className="mt-6 inline-block text-sm text-muted-foreground hover:text-foreground">
            {t.edit}
          </a>
        </footer>
      </article>
    </div>
  )
}

/**
 * How much of the page has been read, as the sync rate of the docs' bar on a
 * phone: 0% with the page's top under the bar, 100% with its end at the
 * bottom of the window. It sets --read (0 to 1) and --read-percent on the
 * bar, which CSS shows; the page's height is watched too, for a frame that
 * loads late or a window turned on its side.
 */
function useReadingProgress(article: React.RefObject<HTMLElement | null>, bar: React.RefObject<HTMLElement | null>) {
  React.useEffect(() => {
    const page = article.current
    const out = bar.current
    if (!page || !out) return
    let frame = 0
    function update() {
      frame = 0
      const rect = page!.getBoundingClientRect()
      const top = out!.getBoundingClientRect().bottom
      const span = rect.height - (window.innerHeight - top)
      const read = span <= 0 ? 1 : Math.min(1, Math.max(0, (top - rect.top) / span))
      out!.style.setProperty("--read", String(read))
      out!.style.setProperty("--read-percent", String(Math.round(read * 100)))
    }
    function schedule() {
      frame ||= requestAnimationFrame(update)
    }
    update()
    const resize = new ResizeObserver(schedule)
    resize.observe(page)
    window.addEventListener("scroll", schedule, { passive: true })
    window.addEventListener("resize", schedule)
    return () => {
      resize.disconnect()
      window.removeEventListener("scroll", schedule)
      window.removeEventListener("resize", schedule)
      cancelAnimationFrame(frame)
    }
  }, [article, bar])
}
