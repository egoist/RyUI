// The site's languages in the browser: the reader's choice, and the words of
// the site's own pages in each.

import { useLocation } from "@tanstack/react-router"

import { localeInfo, localeOf, localized, locales, type Locale } from "@/lib/locales"
import { messages } from "@/lib/messages"
import { site } from "@/lib/site"

export * from "@/lib/locales"
export { messages, type Messages } from "@/lib/messages"

/** For search engines: a page's address in each language, from its path without one. */
export function alternates(path: string) {
  return [
    ...locales.map((l) => ({ rel: "alternate", hrefLang: localeInfo[l].lang, href: site.url + localized(l, path) })),
    { rel: "alternate", hrefLang: "x-default", href: site.url + path },
  ]
}

/** The language of the page being shown. */
export function useLocale(): Locale {
  return localeOf(useLocation({ select: (l) => l.pathname }))
}

export function useMessages() {
  return messages[useLocale()]
}

// The reader's choice in the language menu, kept in their browser: a
// language, or "auto" (the default) for the browser's.
export const preferenceKey = "asuka-locale"
export type Preference = Locale | "auto"

// detectLocale, as the source of a function for the inline script, which
// runs before any of the site's modules load.
const detectSource = `function(){var a=navigator.languages&&navigator.languages.length?navigator.languages:[navigator.language];for(var i=0;i<a.length;i++){var x=String(a[i]).toLowerCase();if(x.indexOf("zh")===0)return"zh";if(x.indexOf("en")===0)return"en"}return"en"}`

/**
 * Run in <head> before the page shows, so a reader who prefers Chinese never
 * sees the English page first. A choice of language takes the reader to its
 * page; "auto" takes a reader whose browser prefers Chinese from an English
 * page to the Chinese one, and leaves a Chinese page that someone linked to
 * as it is (as it does for search engines' crawlers). Only the home page and
 * the docs have a page in each language.
 */
export const localeScript = `(function(){try{
var p=localStorage.getItem(${JSON.stringify(preferenceKey)})||"auto",l=location,path=l.pathname;
var zh=path==="/zh"||path.indexOf("/zh/")===0,bare=zh?path.slice(3)||"/":path;
if(bare!=="/"&&bare!=="/docs"&&bare.indexOf("/docs/")!==0)return;
var want=p==="auto"?(zh?"zh":(${detectSource})()):p;
if(want==="zh"&&!zh)l.replace((bare==="/"?"/zh":"/zh"+bare)+l.search+l.hash);
else if(want==="en"&&zh)l.replace(bare+l.search+l.hash);
}catch(e){}})()`

/** The browser's language, of the site's: the first of the reader's languages that the site has. */
export function detectLocale(): Locale {
  for (const lang of navigator.languages?.length ? navigator.languages : [navigator.language]) {
    const l = lang.toLowerCase()
    if (l.startsWith("zh")) return "zh"
    if (l.startsWith("en")) return "en"
  }
  return "en"
}

export function readPreference(): Preference {
  try {
    const p = localStorage.getItem(preferenceKey)
    return p === "en" || p === "zh" ? p : "auto"
  } catch {
    return "auto"
  }
}

export function writePreference(p: Preference) {
  try {
    if (p === "auto") localStorage.removeItem(preferenceKey)
    else localStorage.setItem(preferenceKey, p)
  } catch {
    // Private windows may refuse: the choice then lasts as long as the page.
  }
}
