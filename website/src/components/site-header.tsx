import { Link } from "@tanstack/react-router"
import { MenuIcon } from "lucide-react"

import { GitHubIcon } from "@/components/icons"
import { Mark, Wordmark } from "@/components/logo"
import { Search } from "@/components/search"
import { Button, buttonVariants } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { site } from "@/lib/site"

const navLink = "font-mono text-[12px] tracking-[0.14em] uppercase text-muted-foreground transition-colors hover:text-foreground data-[status=active]:text-unit-ink"
const mobileNavLink = "min-h-11 px-3 aria-[current=page]:bg-accent"

export function SiteHeader() {
  return (
    <header className="frame sticky top-0 z-40 flex h-14 items-center gap-6 border-b bg-background/80 px-4 backdrop-blur-xl sm:px-6">
      <Link to="/" className="flex items-center gap-2.5 text-[19px]">
        <Mark className="size-7" />
        <Wordmark />
      </Link>
      <nav className="hidden items-center gap-6 sm:flex">
        <Link to="/docs" className={navLink}>
          Docs
        </Link>
        <a href={`${site.repo}/tree/main/examples`} className={navLink}>
          Examples
        </a>
      </nav>
      <div className="ml-auto flex items-center gap-1">
        <Search />
        <a href={site.repo} aria-label="GitHub" className={buttonVariants({ variant: "ghost", size: "icon" })}>
          <GitHubIcon />
        </a>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="sm:hidden" aria-label="Navigation menu" />}>
            <MenuIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44 sm:hidden">
            <DropdownMenuItem className={mobileNavLink} render={<Link to="/docs" />}>
              Docs
            </DropdownMenuItem>
            <DropdownMenuItem className={mobileNavLink} render={<a href={`${site.repo}/tree/main/examples`} />}>
              Examples
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}
