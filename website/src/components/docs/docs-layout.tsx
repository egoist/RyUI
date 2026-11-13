import { Outlet, useMatches } from "@tanstack/react-router"

import { DocsNav } from "@/components/docs/docs-nav"
import type { Doc, Navs } from "@/lib/docs"

/** The docs, in either language: the sidebar, and the page. */
export function DocsLayout({ nav }: { nav: Navs }) {
  const page = useMatches({ select: (matches) => matches.at(-1)?.loaderData as Doc | undefined })
  return (
    <div className="flex-1 lg:grid lg:grid-cols-[15rem_minmax(0,1fr)]">
      <aside className="sticky top-14 hidden h-[calc(100svh-3.5rem)] overflow-y-auto border-r px-3 py-8 lg:block">
        <DocsNav sections={nav} page={page?.toc ? page : undefined} />
      </aside>
      <Outlet />
    </div>
  )
}
