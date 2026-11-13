import { Link } from "@tanstack/react-router"
import { MenuIcon } from "lucide-react"

import { GitHubIcon } from "@/components/icons"
import { LanguageMenu } from "@/components/language-menu"
import { Mark, Wordmark } from "@/components/logo"
import { Search } from "@/components/search"
import { Button, buttonVariants } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { messages, useLocale } from "@/lib/i18n"
import { site } from "@/lib/site"

const navLink = "font-mono text-[12px] tracking-[0.14em] uppercase text-muted-foreground transition-colors hover:text-foreground data-[status=active]:text-unit-ink"
const mobileNavLink = "min-h-11 px-3 aria-[current=page]:bg-accent"

export function SiteHeader() {
  const locale = useLocale()
  const t = messages[locale].nav
  const zh = locale === "zh"
  return (
    <header className="frame sticky top-0 z-40 flex h-14 items-center gap-6 border-b bg-background/80 px-4 backdrop-blur-xl sm:px-6">
      <Link to={zh ? "/zh" : "/"} className="flex items-center gap-2.5 text-[19px]">
        <Mark className="size-7" />
        <Wordmark />
      </Link>
      <nav className="hidden items-center gap-6 sm:flex">
        <Link to={zh ? "/zh/docs" : "/docs"} className={navLink}>
          {t.docs}
        </Link>
        <a href={`${site.repo}/tree/main/examples`} className={navLink}>
          {t.examples}
        </a>
      </nav>
      <div className="ml-auto flex items-center gap-1">
        <Search />
        <LanguageMenu />
        <a href={site.repo} aria-label="GitHub" className={buttonVariants({ variant: "ghost", size: "icon" })}>
          <GitHubIcon />
        </a>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="sm:hidden" aria-label={t.menu} />}>
            <MenuIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" positionMethod="fixed" className="w-44 sm:hidden">
            <DropdownMenuItem className={mobileNavLink} render={<Link to={zh ? "/zh/docs" : "/docs"} />}>
              {t.docs}
            </DropdownMenuItem>
            <DropdownMenuItem className={mobileNavLink} render={<a href={`${site.repo}/tree/main/examples`} />}>
              {t.examples}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}
