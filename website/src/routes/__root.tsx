import { HeadContent, Outlet, Scripts, createRootRoute } from "@tanstack/react-router"
import * as React from "react"

import { NotFound } from "@/components/not-found"
import { SiteFooter } from "@/components/site-footer"
import { SiteHeader } from "@/components/site-header"
import { localeInfo, localeScript, useLocale } from "@/lib/i18n"
import { site } from "@/lib/site"
import appCss from "@/styles.css?url"

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: site.title },
      { name: "description", content: site.description },
      { property: "og:type", content: "website" },
      { property: "og:site_name", content: site.name },
      { property: "og:title", content: site.title },
      { property: "og:description", content: site.description },
      { name: "twitter:card", content: "summary" },
    ],
    links: [
      { rel: "stylesheet", href: appCss },
      { rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
    ],
    // Before anything shows: the page in the reader's language.
    scripts: [{ children: localeScript }],
    // An analytics script, if the site has one, goes here (in production only).
  }),
  shellComponent: RootDocument,
  component: RootLayout,
  notFoundComponent: NotFound,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  const locale = useLocale()
  return (
    <html lang={localeInfo[locale].lang} className="dark">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  return (
    <div className="flex min-h-svh flex-col">
      <SiteHeader />
      <div className="frame flex flex-1 flex-col">
        <Outlet />
      </div>
      <SiteFooter />
    </div>
  )
}
