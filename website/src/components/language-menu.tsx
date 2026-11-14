import { useLocation, useNavigate } from "@tanstack/react-router"
import { LanguagesIcon } from "lucide-react"
import * as React from "react"

import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import {
  detectLocale,
  localeInfo,
  localeOf,
  locales,
  localized,
  messages,
  readPreference,
  unlocalized,
  writePreference,
  type Preference,
} from "@/lib/i18n"

/** Whether a page of the site has a page in each language: the home page and the docs. */
function translated(path: string) {
  return path === "/" || path === "/docs" || path.startsWith("/docs/")
}

/**
 * The language menu: Auto (the browser's language, the default), or a
 * language. The choice is kept in the browser, and the script in <head>
 * (localeScript) follows it on the next visit.
 */
export function LanguageMenu() {
  const pathname = useLocation({ select: (l) => l.pathname })
  const navigate = useNavigate()
  const locale = localeOf(pathname)
  const t = messages[locale].language
  // "auto" until the page is in the browser: prerendered pages know no reader.
  const [preference, setPreference] = React.useState<Preference>("auto")
  React.useEffect(() => setPreference(readPreference()), [])

  function choose(value: Preference) {
    setPreference(value)
    writePreference(value)
    const target = value === "auto" ? detectLocale() : value
    const path = unlocalized(pathname)
    if (target !== locale && translated(path)) void navigate({ href: localized(target, path) })
  }

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={<Button variant="ghost" className="h-8 gap-1.5 px-2 font-mono text-[12px] tracking-[0.08em] text-muted-foreground hover:text-foreground" aria-label={t.label} />}
      >
        <LanguagesIcon />
        <span className="hidden sm:inline">{localeInfo[locale].short}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" positionMethod="fixed" className="w-44">
        <DropdownMenuRadioGroup value={preference} onValueChange={(v) => choose(v as Preference)}>
          <DropdownMenuRadioItem value="auto" className="min-h-9">
            {t.auto}
          </DropdownMenuRadioItem>
          {locales.map((l) => (
            <DropdownMenuRadioItem key={l} value={l} lang={localeInfo[l].lang} className="min-h-9">
              {localeInfo[l].name}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
