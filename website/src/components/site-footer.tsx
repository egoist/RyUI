import { Link } from "@tanstack/react-router"

import { Mark } from "@/components/logo"
import { messages, useLocale } from "@/lib/i18n"
import { site } from "@/lib/site"

export function SiteFooter() {
  const locale = useLocale()
  const t = messages[locale].nav
  return (
    <footer className="frame label flex flex-wrap items-center justify-between gap-4 border-t px-4 py-6 sm:px-6">
      <div className="flex items-center gap-3">
        <Mark className="size-5" />
        <a href={`${site.repo}/blob/main/LICENSE`} className="hover:text-foreground">
          {t.license}
        </a>
      </div>
      <nav className="flex flex-wrap gap-x-6 gap-y-2">
        <Link to={locale === "zh" ? "/zh/docs" : "/docs"} className="hover:text-foreground">
          {t.docs}
        </Link>
        <a href={`${site.repo}/releases`} className="hover:text-foreground">
          {t.releases}
        </a>
        <a href={site.repo} className="hover:text-foreground">
          GitHub
        </a>
      </nav>
    </footer>
  )
}
