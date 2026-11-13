import { Link, createFileRoute } from "@tanstack/react-router"
import { ArrowRightIcon } from "lucide-react"
import type { ReactNode } from "react"

import { InstallCommand } from "@/components/install-command"
import { Wordmark } from "@/components/logo"
import { Translation } from "@/components/translation"
import { getHome } from "@/lib/home"
import { site } from "@/lib/site"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/")({
  loader: () => getHome(),
  component: Home,
})

// The platforms an app runs on, each with the widgets it is shown with: the
// units, of which Go is the pilot.
const units = [
  { name: "iOS", toolkit: "UIKit" },
  { name: "Android", toolkit: "Android Views · Material 3" },
  { name: "macOS", toolkit: "AppKit" },
  { name: "Windows", toolkit: "Win32, drawn as Windows 11" },
  { name: "Linux", toolkit: "GTK 4 · libadwaita" },
  { name: "Web", toolkit: "The DOM" },
]

// What the README says, in short, each with the page that says more: six
// episodes, numbered as a title card numbers them.
const features = [
  {
    numeral: "壱",
    title: "The platform's own widgets",
    text: "Navigation, lists, fields, pickers, sheets and menus that look and feel as each platform's do, because they are each platform's.",
    href: "/docs/parity",
  },
  {
    numeral: "弐",
    title: "Fine-grained signals",
    text: "Components run once. A signal's change updates only the views that read it, in one batch for each frame, without diffing a tree.",
    href: "/docs/getting-started#change-it",
  },
  {
    numeral: "参",
    title: "Live as you save",
    text: "asuka dev runs the app on your computer, the simulator, an emulator, a phone or a browser, and reloads it on save with the app's state kept.",
    href: "/docs/development",
  },
  {
    numeral: "肆",
    title: "Motion",
    text: "Animations, springs, transforms, enter and exit transitions, shared elements that grow from one view into another, and gestures.",
    href: "/docs/parity#motion",
  },
  {
    numeral: "伍",
    title: "Batteries",
    text: "SQLite with live queries, secure storage, the camera and code scanning, photos, location, notifications, audio, video and web views.",
    href: "/docs/parity#data-and-services",
  },
  {
    numeral: "陸",
    title: "Ready to ship",
    text: "asuka build makes a signed .ipa for the App Store, an .aab for Google Play, a notarized Mac app, a signed Windows .exe, a Linux archive and Flatpak manifest, and a static site.",
    href: "/docs/shipping",
  },
]

const runs = [
  ["asuka new hello && cd hello", ""],
  ["asuka dev", "this computer: Mac, PC or Linux"],
  ["asuka dev --platform ios", "the iOS simulator"],
  ["asuka dev --platform android", "an emulator or a phone"],
  ["asuka dev --platform web", "your browser"],
]

/** The button that starts the story: Unit red, two corners cut. */
function Start({ className, children = "Get started" }: { className?: string; children?: ReactNode }) {
  return (
    <Link
      to="/docs/$"
      params={{ _splat: "getting-started" }}
      className={cn(
        "chamfer inline-flex h-12 items-center gap-2 bg-unit px-6 font-mono text-[13px] font-semibold tracking-[0.14em] text-white uppercase transition-[filter] hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2",
        className
      )}
    >
      {children} <ArrowRightIcon className="size-4" />
    </Link>
  )
}

/** Mincho, stretched up as a title card stretches it. */
function Tall({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn("block origin-bottom-left scale-y-[1.14]", className)}>{children}</span>
}

/** A section's heading: a numbered label over a line of mincho. */
function Heading({ label, children, aside }: { label: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="inner flex items-end justify-between gap-6 pt-24 pb-10">
      <div>
        <p className="label flex items-center gap-2 text-signal-ink">
          <span className="size-1.5 bg-signal" />
          {label}
        </p>
        <h2 className="display mt-4 text-[clamp(2.5rem,6vw,4.5rem)] leading-[0.98]">{children}</h2>
      </div>
      {aside && <div className="label hidden text-right md:block">{aside}</div>}
    </div>
  )
}

function Home() {
  const { main } = Route.useLoaderData()
  return (
    <main>
      <Hero />
      <Ticker />
      <Spec />
      <section className="border-b">
        <Heading label="Translation" aside="Pick a host">
          Go in.
          <br />
          <span className="text-unit">Native out.</span>
        </Heading>
        <div className="inner pb-24">
          <Translation />
        </div>
      </section>
      <Units />

      <section className="bleed border-t">
        <Heading label="Episode 00 / Hello" aside="One file. Every platform.">
          Hello, <span className="text-unit">native.</span>
        </Heading>
        <div className="inner pb-24">
          <div className="hud grid border bg-code lg:grid-cols-[1.4fr_1fr] lg:divide-x">
            <figure className="min-w-0">
              <figcaption className="label flex items-center justify-between border-b px-4 py-3 sm:px-6">
                <span className="flex items-center gap-2">
                  <span className="size-1.5 bg-signal" /> main.go
                </span>
                <span>Source</span>
              </figcaption>
              <div className="overflow-x-auto px-4 py-5 sm:px-6" dangerouslySetInnerHTML={{ __html: main }} />
            </figure>
            <div className="flex min-w-0 flex-col border-t lg:border-t-0">
              <p className="label flex items-center justify-between border-b px-4 py-3 sm:px-6">
                <span className="flex items-center gap-2">
                  <span className="size-1.5 bg-unit" /> Run it
                </span>
                <span>Every platform</span>
              </p>
              <pre className="overflow-x-auto px-4 py-5 font-mono text-[13px] leading-[1.7] sm:px-6">
                {runs.map(([command, comment]) => (
                  <span key={command} className="block">
                    <span className="text-signal-ink select-none">$ </span>
                    {command.padEnd(comment ? 30 : 0)}
                    {comment && <span className="text-muted-foreground"># {comment}</span>}
                  </span>
                ))}
              </pre>
              <p className="mt-auto border-t px-4 py-4 text-sm leading-6 text-muted-foreground sm:px-6">
                Save a Go file and the app reloads in a moment, its state kept. See{" "}
                <a href={`${site.repo}/tree/main/examples`} className="text-foreground underline decoration-unit/50 underline-offset-4 hover:decoration-unit">
                  the examples
                </a>
                : the gallery shows every component.
              </p>
            </div>
          </div>
        </div>
      </section>

      <Episodes />
      <Finale />
    </main>
  )
}

/**
 * The title card, in the page's column and within a laptop's screen:
 * NATIVE APPS, set to the column's full width, IN GO. flush right under it
 * at the same size, the subtitle in the space it leaves. The size comes from
 * the line's width in Shippori Mincho B1 ExtraBold: NATIVE APPS, is 7.74em.
 */
function Hero() {
  // Line height after the size: tailwind-merge drops a leading- that comes before a text- size.
  const big = "block origin-bottom-left scale-y-[1.14] whitespace-nowrap text-[calc(100cqi/7.9)] leading-[0.9]"
  const subtitle = "display leading-snug text-balance"
  return (
    <section className="relative overflow-hidden border-b">
      <div className="scanlines pointer-events-none absolute inset-0" />
      <div className="relative flex items-center justify-between border-b px-4 py-3 sm:px-10">
        <p className="label flex items-center gap-2 text-foreground/80">
          <span className="size-1.5 animate-pulse bg-unit" />
          Asuka {__ASUKA_VERSION__}
        </p>
        <p className="label hidden text-signal-ink sm:block">Core: Go / Host: native / Sync 100.0%</p>
      </div>

      <div className="relative px-4 pt-8 pb-10 sm:px-10 md:pt-10 md:pb-12">
        <div className="flex items-start justify-between">
          <p className="display text-lg text-unit-ink sm:text-xl">Framework:</p>
          <p lang="ja" className="display text-lg tracking-[0.3em] text-unit sm:text-xl">
            明日香
          </p>
        </div>

        <div className="@container mt-3">
          <h1 className="sr-only">Native apps, in Go.</h1>
          <p aria-hidden className={cn("display", big)}>
            NATIVE APPS,
          </p>
          <div className="mt-[1.6cqi] flex items-end justify-between gap-6">
            <p className={cn(subtitle, "hidden max-w-[24ch] pb-[0.8cqi] text-[clamp(1.25rem,2.9cqi,2.1rem)] sm:block")}>
              The platform&apos;s widgets, not a web view.
            </p>
            <p aria-hidden className={cn("display ml-auto origin-bottom-right", big, "origin-bottom-right")}>
              IN <span className="text-unit">GO.</span>
            </p>
          </div>
        </div>

        <div className="mt-6 grid grid-cols-1 gap-6 border-t-2 border-foreground pt-6 md:mt-8 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center lg:gap-10">
          <div>
            <p className={cn(subtitle, "mb-3 text-[1.6rem] sm:hidden")}>The platform&apos;s widgets, not a web view.</p>
            <p className="max-w-xl text-base leading-7 text-foreground/70">
              Write one app in Go alone, for phones, desktops and the web. Asuka shows it with each platform&apos;s own widgets, and updates only what changed.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Start />
            <InstallCommand className="h-12" />
          </div>
        </div>
      </div>
      <div className="hazard relative h-3" />
    </section>
  )
}

/** Warning tape that runs past: every platform and the widgets it gets. */
function Ticker() {
  const run = (
    <span className="flex shrink-0 items-center">
      {units.map((u) => (
        <span key={u.name} className="flex items-center gap-4 px-6">
          <span className="font-bold">{u.name}</span>
          <span className="opacity-70">{u.toolkit}</span>
          <span className="pl-6">Λ</span>
        </span>
      ))}
    </span>
  )
  return (
    <div className="overflow-hidden border-b bg-signal py-3 font-mono text-[13px] tracking-[0.16em] text-[#0e0d10] uppercase">
      <p className="sr-only">Runs on {units.map((u) => `${u.name} (${u.toolkit})`).join(", ")}.</p>
      <div className="ticker flex w-max" aria-hidden>
        {run}
        {run}
      </div>
    </div>
  )
}

/** The spec, as three lines of a title card. */
function Spec() {
  const lines = [
    { line: "One language.", note: "Go, and Go alone" },
    { line: "Six platforms.", note: "iOS · Android · macOS · Windows · Linux · Web" },
    { line: "Zero web views.", note: "Each platform's own widgets", red: true },
  ]
  return (
    <section className="bleed border-b">
      <div className="inner py-20 md:py-28">
        <p className="label">Specification</p>
        <dl className="mt-8">
          {lines.map(({ line, note, red }) => (
            <div key={line} className="relative flex flex-col gap-2 border-b py-5 first:border-t md:py-7">
              <dd className="label md:absolute md:top-7 md:right-0 md:max-w-56 md:text-right">{note}</dd>
              <dt className={cn("display text-[clamp(2.6rem,8.6vw,7rem)] leading-[0.95] md:whitespace-nowrap", red && "text-unit")}>
                <Tall>{line}</Tall>
              </dt>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}

/** One pilot, six units: a status panel for each platform. */
function Units() {
  return (
    <section className="bleed">
      <Heading
        label="Deployment"
        aside={
          <>
            Pilot: Go
            <br />
            Units: 6 / 6 active
          </>
        }
      >
        One pilot.
        <br />
        <span className="text-unit">Six units.</span>
      </Heading>
      <div className="inner pb-24">
        <ol className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {units.map((u, i) => (
            <li key={u.name} className="hud scanlines relative border bg-card p-5 [--hud:var(--unit)]">
              <div className="label flex items-center justify-between">
                <span>Unit {String(i + 1).padStart(2, "0")}</span>
                <span className="flex items-center gap-1.5 text-signal-ink">
                  <span className="size-1.5 animate-pulse rounded-full bg-signal" /> Active
                </span>
              </div>
              <p className="display mt-8 text-5xl leading-none">{u.name}</p>
              <p className="mt-3 font-mono text-[13px] text-foreground/70">{u.toolkit}</p>
              <div className="mt-8 flex items-center gap-3">
                <div className="h-2 flex-1 bg-foreground/10">
                  <div className="sync-fill h-full origin-left bg-signal" />
                </div>
                <span className="label text-foreground">Sync 100%</span>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}

/** The features, six episodes, each with its numeral writ large behind it. */
function Episodes() {
  return (
    <section className="bleed border-t">
      <Heading label="Six episodes" aside="Read on in the docs">
        What it does
      </Heading>
      <div className="border-t">
        <ol className="mx-auto grid max-w-6xl gap-px bg-border sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature, i) => (
            <li key={feature.title} className="bg-background">
              <a href={feature.href} className="group relative flex h-full flex-col overflow-hidden px-5 pt-7 pb-10 transition-colors hover:bg-code sm:px-8">
                <span className="absolute top-0 left-0 h-0.5 w-0 bg-unit transition-[width] duration-300 group-hover:w-full" />
                <span
                  lang="ja"
                  aria-hidden
                  className="display pointer-events-none absolute -right-3 -bottom-10 text-[11rem] leading-none text-unit/12 transition-colors group-hover:text-unit/25"
                >
                  {feature.numeral}
                </span>
                <span className="relative flex items-center justify-between">
                  <span className="display text-[15px] text-unit-ink" lang="ja">
                    第{feature.numeral}話
                  </span>
                  <span className="label">Episode {String(i + 1).padStart(2, "0")}</span>
                </span>
                <h3 className="display relative mt-8 flex items-start justify-between gap-4 text-[1.75rem] leading-tight">
                  {feature.title}
                  <ArrowRightIcon className="mt-2 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-unit-ink" />
                </h3>
                <p className="relative mt-3 max-w-sm text-sm leading-6 text-foreground/65">{feature.text}</p>
              </a>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}

/** The preview of the next episode: the motto, where to begin, and the name. */
function Finale() {
  return (
    <section className="bleed relative overflow-hidden border-t">
      <div className="hazard h-3" />
      <div className="scanlines pointer-events-none absolute inset-0" />
      <div className="inner relative pt-24 pb-16 md:pt-32">
        <p className="label text-signal-ink">Next episode</p>
        <p className="display mt-6 text-[clamp(2.6rem,8vw,6.5rem)] leading-[0.98]">
          <Tall>Go&rsquo;s in its core.</Tall>
          <Tall className="mt-[0.08em]">
            <span className="text-unit">All&rsquo;s native</span> on
          </Tall>
          <Tall className="mt-[0.08em]">every screen.</Tall>
        </p>
        <div className="mt-12 flex flex-wrap items-center gap-3">
          <Start>Begin</Start>
          <Link
            to="/docs"
            className="inline-flex h-12 items-center border px-6 font-mono text-[13px] tracking-[0.14em] uppercase transition-colors hover:border-foreground/40"
          >
            Read the docs
          </Link>
        </div>
      </div>
      <div className="relative px-[2vw] pt-10 pb-[2vw]" aria-hidden>
        <Wordmark lambda="var(--unit)" className="h-auto w-full text-foreground" />
      </div>
    </section>
  )
}
