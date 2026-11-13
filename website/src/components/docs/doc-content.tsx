import { useLocation, useNavigate } from "@tanstack/react-router"
import * as React from "react"

/**
 * A page rendered from markdown: its links navigate within the site, its code
 * blocks copy, and the heading a navigation went to flashes.
 */
export function DocContent({ html }: { html: string }) {
  const ref = React.useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    function onClick(e: MouseEvent) {
      const target = e.target as Element
      const copy = target.closest<HTMLButtonElement>(".copy-button")
      if (copy) {
        const code = copy.closest(".code-block")?.querySelector("pre")?.textContent ?? ""
        void navigator.clipboard.writeText(code).then(() => {
          copy.dataset.copied = ""
          setTimeout(() => delete copy.dataset.copied, 1500)
        })
        return
      }
      const a = target.closest("a")
      if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
      const url = new URL(a.href)
      if (url.origin !== location.origin || a.target) return
      e.preventDefault()
      void navigate({ href: url.pathname + url.search + url.hash })
    }
    el.addEventListener("click", onClick)
    return () => el.removeEventListener("click", onClick)
  }, [navigate])

  useFlashHeading(ref, html)

  return <div ref={ref} className="prose-docs" dangerouslySetInnerHTML={{ __html: html }} />
}

/**
 * Flashes the heading of the page that each navigation goes to (by the
 * sidebar, search, a § link or a link from another page, and on the first
 * load), and again for a link to the heading already gone to, which the
 * router doesn't navigate. :target can't: navigating within the site changes
 * the URL with history.pushState, which leaves it as it was.
 */
function useFlashHeading(ref: React.RefObject<HTMLDivElement | null>, html: string) {
  const hash = useLocation({ select: (l) => l.hash })
  const key = useLocation({ select: (l) => l.state.__TSR_key })

  React.useEffect(() => {
    const heading = ref.current && hash ? headingOf(ref.current, hash) : null
    if (heading) flash(heading)
  }, [ref, html, hash, key])

  React.useEffect(() => {
    function onClick(e: MouseEvent) {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
      const a = (e.target as Element).closest("a")
      if (!a?.hash || !ref.current) return
      const url = new URL(a.href)
      if (url.origin !== location.origin || url.pathname !== location.pathname || url.hash !== location.hash) return
      const heading = headingOf(ref.current, url.hash.slice(1))
      if (heading) flash(heading)
    }
    // Capturing, so it sees the URL before a link's own handler navigates.
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [ref])
}

/** The heading of the page with an id, as a URL's hash spells it. */
function headingOf(page: HTMLElement, hash: string) {
  let id = hash
  try {
    id = decodeURIComponent(hash)
  } catch {}
  const heading = document.getElementById(id)
  return heading && page.contains(heading) && /^H[2-4]$/.test(heading.tagName) ? heading : null
}

/** Starts a heading's flash (styles.css), over again if it is flashing. */
function flash(heading: HTMLElement) {
  delete heading.dataset.flash
  void heading.offsetWidth // a style recalculation without it, so the animation restarts
  heading.dataset.flash = ""
  heading.addEventListener("animationend", function done(e) {
    if (e.target !== heading || e.animationName !== "heading-ink") return
    delete heading.dataset.flash
    heading.removeEventListener("animationend", done)
  })
}
