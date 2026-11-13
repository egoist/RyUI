import { Link } from "@tanstack/react-router"
import { ArrowRightIcon } from "lucide-react"
import type { ReactNode } from "react"

import { InstallCommand } from "@/components/install-command"
import { LiveCounter } from "@/components/live-counter"
import { Wordmark } from "@/components/logo"
import { Translation } from "@/components/translation"
import type { HomeData } from "@/lib/home"
import { alternates, messages, type Locale } from "@/lib/i18n"
import { site } from "@/lib/site"
import { cn } from "@/lib/utils"

// The platforms an app runs on, each with the widgets it is shown with: the
// units, of which Go is the pilot.
const platforms = ["iOS", "Android", "macOS", "Windows", "Linux", "Web"]

// The six features' numerals, as a title card numbers episodes.
const numerals = ["壱", "弐", "参", "肆", "伍", "陸"]

/** The words of the home page, in a language. */
interface Copy {
  toolkits: string[]
  hero: {
    status: string
    framework: string
    title: string
    subtitle: string
    lead: string
  }
  start: string
  spec: { label: string; lines: { line: string; note: string; red?: boolean }[] }
  translation: { label: string; aside: string; heading: ReactNode }
  units: { label: string; aside: ReactNode; heading: ReactNode; unit: (n: string) => string; active: string; sync: string }
  hello: {
    label: string
    aside: string
    heading: ReactNode
    source: string
    run: string
    every: string
    runs: [string, string][]
    note: (examples: ReactNode) => ReactNode
    examples: string
  }
  episodes: { label: string; aside: string; heading: string; features: { title: string; text: string; href: string }[] }
  finale: { label: string; motto: ReactNode; begin: string; docs: string }
  runsOn: string
}

const en: Copy = {
  toolkits: ["UIKit", "Android Views · Material 3", "AppKit", "Win32, drawn as Windows 11", "GTK 4 · libadwaita", "The DOM"],
  hero: {
    status: "Core: Go / Host: native / Sync",
    framework: "Framework:",
    title: "Native widgets, in Go.",
    subtitle: "UIKit, Material, AppKit, Win32, GTK and the DOM. Not a canvas, not a web view.",
    lead: "Write one app in Go alone, for phones, desktops and the web. Each platform shows it with its own controls, its text input and its accessibility, and Asuka updates only what changed.",
  },
  start: "Get started",
  spec: {
    label: "Specification",
    lines: [
      { line: "One language.", note: "Go, and Go alone" },
      { line: "Six platforms.", note: "iOS · Android · macOS · Windows · Linux · Web" },
      { line: "Zero web views.", note: "Each platform's own widgets", red: true },
    ],
  },
  translation: {
    label: "Translation",
    aside: "Pick a host",
    heading: (
      <>
        Go in.
        <br />
        <span className="text-unit">Native out.</span>
      </>
    ),
  },
  units: {
    label: "Deployment",
    aside: (
      <>
        Pilot: Go
        <br />
        Units: 6 / 6 active
      </>
    ),
    heading: (
      <>
        One pilot.
        <br />
        <span className="text-unit">Six units.</span>
      </>
    ),
    unit: (n) => `Unit ${n}`,
    active: "Active",
    sync: "Sync 100%",
  },
  hello: {
    label: "Episode 00 / Hello",
    aside: "One file. Every platform.",
    heading: (
      <>
        Hello, <span className="text-unit">native.</span>
      </>
    ),
    source: "Source",
    run: "Run it",
    every: "Every platform",
    runs: [
      ["asuka new hello && cd hello", ""],
      ["asuka dev", "this computer: Mac, PC or Linux"],
      ["asuka dev --platform ios", "the iOS simulator"],
      ["asuka dev --platform android", "an emulator or a phone"],
      ["asuka dev --platform web", "your browser"],
    ],
    note: (examples) => <>Save a Go file and the app reloads in a moment, its state kept. See {examples}: the gallery shows every component.</>,
    examples: "the examples",
  },
  episodes: {
    label: "Six episodes",
    aside: "Read on in the docs",
    heading: "What it does",
    // What the README says, in short, each with the page that says more.
    features: [
      {
        title: "The platform's own widgets",
        text: "Navigation, lists, fields, pickers, sheets and menus that look and feel as each platform's do, because they are each platform's.",
        href: "/docs/parity",
      },
      {
        title: "Fine-grained signals",
        text: "Components run once. A signal's change updates only the views that read it, in one batch for each frame, without diffing a tree.",
        href: "/docs/getting-started#change-it",
      },
      {
        title: "Live as you save",
        text: "asuka dev runs the app on your computer, the simulator, an emulator, a phone or a browser, and reloads it on save with the app's state kept.",
        href: "/docs/development",
      },
      {
        title: "Motion",
        text: "Animations, springs, transforms, enter and exit transitions, shared elements that grow from one view into another, and gestures.",
        href: "/docs/parity#motion",
      },
      {
        title: "Batteries",
        text: "SQLite with live queries, secure storage, the camera and code scanning, photos, location, notifications, audio, video and web views.",
        href: "/docs/parity#data-and-services",
      },
      {
        title: "Ready to ship",
        text: "asuka build makes a signed .ipa for the App Store, an .aab for Google Play, a notarized Mac app, a signed Windows .exe, a Linux archive and Flatpak manifest, and a static site.",
        href: "/docs/shipping",
      },
    ],
  },
  finale: {
    label: "Next episode",
    // After NERV's motto, from Browning: God's in his heaven, all's right with the world.
    motto: (
      <>
        <Tall>Go&rsquo;s in its core.</Tall>
        <Tall className="mt-[0.08em]">
          <span className="text-unit">All&rsquo;s native</span> on
        </Tall>
        <Tall className="mt-[0.08em]">every screen.</Tall>
      </>
    ),
    begin: "Begin",
    docs: "Read the docs",
  },
  runsOn: "Runs on",
}

const zh: Copy = {
  toolkits: ["UIKit", "Android Views · Material 3", "AppKit", "Win32，Windows 11 外观", "GTK 4 · libadwaita", "DOM"],
  hero: {
    status: "核心：Go / 宿主：原生 / 同步率",
    framework: "框架：",
    title: "跨平台原生控件，用 Go 写。",
    // No breaks inside a phrase: Chinese breaks between any two characters.
    subtitle: "UIKit、Material、AppKit、Win32、GTK\u00a0和\u00a0DOM。不是\u00a0GPU\u00a0自绘，也不是\u00a0Web\u00a0View。",
    lead: "只用 Go 写一个应用，就能跑在手机、桌面和 Web 上。每个平台都用自己的控件、输入法和无障碍功能来呈现它，Asuka 只更新真正变化的部分。",
  },
  start: "快速上手",
  spec: {
    label: "规格",
    lines: [
      { line: "一种语言。", note: "只用 Go" },
      { line: "六个平台。", note: "iOS · Android · macOS · Windows · Linux · Web" },
      { line: "零 Web View。", note: "全是各平台自己的控件", red: true },
    ],
  },
  translation: {
    label: "转译",
    aside: "选一个宿主看看",
    heading: (
      <>
        写的是 Go，
        <br />
        <span className="text-unit">跑的是原生。</span>
      </>
    ),
  },
  units: {
    label: "出击",
    aside: (
      <>
        驾驶员：Go
        <br />
        机体：6 / 6 启动
      </>
    ),
    heading: (
      <>
        一名驾驶员，
        <br />
        <span className="text-unit">六台机体。</span>
      </>
    ),
    unit: (n) => `${n} 号机`,
    active: "运行中",
    sync: "同步率 100%",
  },
  hello: {
    label: "第 00 话 / Hello",
    aside: "一个文件，所有平台。",
    heading: (
      <>
        你好，<span className="text-unit">原生。</span>
      </>
    ),
    source: "源码",
    run: "运行",
    every: "全平台",
    runs: [
      ["asuka new hello && cd hello", ""],
      ["asuka dev", "本机：Mac、PC、Linux"],
      ["asuka dev --platform ios", "iOS 模拟器"],
      ["asuka dev --platform android", "Android 模拟器或手机"],
      ["asuka dev --platform web", "浏览器"],
    ],
    note: (examples) => <>保存 Go 文件，应用片刻间就会重新加载，状态原样保留。更多用法请看{examples}，其中的 gallery 展示了全部组件。</>,
    examples: "示例",
  },
  episodes: {
    label: "全六话",
    aside: "详见文档",
    heading: "它能做什么",
    features: [
      {
        title: "平台原生控件",
        text: "导航、列表、输入框、选择器、弹出面板和菜单，外观和手感都与各平台一致——因为它们本来就是平台自己的控件。",
        href: "/zh/docs/parity",
      },
      {
        title: "细粒度更新",
        text: "组件只运行一次。signal 变化时，只有读取它的视图才会更新，每帧合成一批提交，无需比对整棵树。",
        href: "/zh/docs/getting-started#修改应用",
      },
      {
        title: "保存即生效",
        text: "asuka dev 能在你的电脑、iOS 模拟器、Android 模拟器、手机或浏览器上运行应用，保存后自动重载，应用状态不丢失。",
        href: "/zh/docs/development",
      },
      {
        title: "动效",
        text: "动画、弹簧、变换、进场与退场过渡、从一个视图“长”成另一个视图的共享元素，还有手势。",
        href: "/zh/docs/parity#动画",
      },
      {
        title: "开箱即用",
        text: "支持实时查询的 SQLite、安全存储、相机与扫码、相册、定位、通知、音频、视频和网页视图，一应俱全。",
        href: "/zh/docs/parity#数据与服务",
      },
      {
        title: "随时上架",
        text: "asuka build 能打出提交 App Store 的签名 .ipa、提交 Google Play 的 .aab、经过公证的 Mac 应用、签名的 Windows .exe、Linux 压缩包与 Flatpak 清单，还有静态网站。",
        href: "/zh/docs/shipping",
      },
    ],
  },
  finale: {
    label: "下回预告",
    // 化用 NERV 的箴言（出自勃朗宁）：神在天堂，世间一切安好。
    motto: (
      <>
        <Tall>Go 在核心，</Tall>
        {/* Chinese characters fill the em, so the lines need more room than Latin's. */}
        <Tall className="mt-[0.22em]">
          万屏<span className="text-unit">皆原生。</span>
        </Tall>
      </>
    ),
    begin: "开始",
    docs: "阅读文档",
  },
  runsOn: "可运行于",
}

const copies: Record<Locale, Copy> = { en, zh }

export function homeHead(locale: Locale) {
  const t = messages[locale]
  return {
    meta: [
      { title: t.siteTitle },
      { name: "description", content: t.siteDescription },
      { property: "og:title", content: t.siteTitle },
      { property: "og:description", content: t.siteDescription },
    ],
    links: alternates("/"),
  }
}

/** The button that starts the story: Unit red, two corners cut. */
function Start({ locale, className, children }: { locale: Locale; className?: string; children: ReactNode }) {
  return (
    <Link
      to={locale === "zh" ? "/zh/docs/$" : "/docs/$"}
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

export function Home({ locale, data }: { locale: Locale; data: HomeData }) {
  const t = copies[locale]
  const units = platforms.map((name, i) => ({ name, toolkit: t.toolkits[i]! }))
  return (
    <main>
      <Hero locale={locale} t={t} />
      <Ticker units={units} t={t} />
      <Spec t={t} />
      <section className="border-b">
        <Heading label={t.translation.label} aside={t.translation.aside}>
          {t.translation.heading}
        </Heading>
        <div className="inner pb-24">
          <Translation />
        </div>
      </section>
      <Units units={units} t={t} />

      <section className="bleed border-t">
        <Heading label={t.hello.label} aside={t.hello.aside}>
          {t.hello.heading}
        </Heading>
        <div className="inner pb-24">
          <LiveCounter code={data.main} />
          <div className="mt-4 grid border lg:grid-cols-[1.35fr_1fr] lg:divide-x">
            <div className="min-w-0">
              <p className="label flex items-center justify-between border-b px-4 py-3 sm:px-6">
                <span className="flex items-center gap-2">
                  <span className="size-1.5 bg-unit" /> {t.hello.run}
                </span>
                <span>{t.hello.every}</span>
              </p>
              <pre className="overflow-x-auto px-4 py-5 font-code text-[13.5px] leading-[1.7] sm:px-6">
                {t.hello.runs.map(([command, comment]) => (
                  <span key={command} className="block">
                    <span className="text-signal-ink select-none">$ </span>
                    {command.padEnd(comment ? 30 : 0)}
                    {comment && <span className="text-muted-foreground"># {comment}</span>}
                  </span>
                ))}
              </pre>
            </div>
            <p className="flex items-end border-t px-4 py-5 text-sm leading-6 text-muted-foreground sm:px-6 lg:border-t-0">
              <span>
                {t.hello.note(
                  <a href={`${site.repo}/tree/main/examples`} className="text-foreground underline decoration-unit/50 underline-offset-4 hover:decoration-unit">
                    {t.hello.examples}
                  </a>
                )}
              </span>
            </p>
          </div>
        </div>
      </section>

      <Episodes t={t} />
      <Finale locale={locale} t={t} />
    </main>
  )
}

/**
 * The title card, in the page's column and within a laptop's screen.
 *
 * In English, NATIVE WIDGETS, set to the column's full width, IN GO. flush
 * right under it and larger, the subtitle in the space it leaves. The sizes
 * come from the lines' widths in Shippori Mincho B1 ExtraBold: NATIVE
 * WIDGETS, is 10.23em.
 *
 * In Chinese, whose characters are each an em wide, likewise: 跨平台原生控件，
 * (8em) across the column, 用 Go 写。 flush right under it and larger, the
 * subtitle beside it.
 */
function Hero({ locale, t }: { locale: Locale; t: Copy }) {
  // Line height after the size: tailwind-merge drops a leading- that comes before a text- size.
  const big = (size: string) => cn("block origin-bottom-left scale-y-[1.14] whitespace-nowrap", size, "leading-[0.9]")
  const subtitle = (size: string) => cn("display text-balance", size, "leading-snug")
  const zh = locale === "zh"
  return (
    <section className="relative overflow-hidden border-b">
      <div className="scanlines pointer-events-none absolute inset-0" />
      <div className="relative flex items-center justify-between border-b px-4 py-3 sm:px-10">
        <p className="label flex items-center gap-2 text-foreground/80">
          <span className="size-1.5 animate-pulse bg-unit" />
          {__ASUKA_VERSION__}
        </p>
        <p className="label hidden text-signal-ink sm:block">
          {t.hero.status} <SyncRate />
        </p>
      </div>

      <div className="relative px-4 pt-8 pb-10 sm:px-10 md:pt-10 md:pb-12">
        <div className="flex items-start justify-between">
          <p className="display text-lg text-unit-ink sm:text-xl">{t.hero.framework}</p>
          <p lang="ja" className="display text-lg tracking-[0.3em] text-unit sm:text-xl">
            明日香
          </p>
        </div>

        <div className="@container mt-3">
          <h1 className="sr-only">{t.hero.title}</h1>
          {zh ? (
            <>
              <p aria-hidden className={cn("display", big("text-[calc(100cqi/8.1)]"))}>
                跨平台原生控件，
              </p>
              <div className="mt-[1.6cqi] flex items-end justify-between gap-6">
                <p className={cn(subtitle("text-[clamp(1.2rem,2.4cqi,1.8rem)]"), "hidden max-w-[17em] pb-[0.8cqi] break-keep sm:block")}>{t.hero.subtitle}</p>
                <p aria-hidden className={cn("display ml-auto", big("text-[calc(100cqi/6.6)]"), "origin-bottom-right")}>
                  用 <span className="text-unit">Go</span> 写。
                </p>
              </div>
            </>
          ) : (
            <>
              <p aria-hidden className={cn("display", big("text-[calc(100cqi/10.4)]"))}>
                NATIVE WIDGETS,
              </p>
              <div className="mt-[1.6cqi] flex items-end justify-between gap-6">
                <p className={cn(subtitle("text-[clamp(1.2rem,2.5cqi,1.9rem)]"), "hidden max-w-[26ch] pb-[0.8cqi] sm:block")}>{t.hero.subtitle}</p>
                <p aria-hidden className={cn("display ml-auto", big("text-[calc(100cqi/7.6)]"), "origin-bottom-right")}>
                  IN <span className="text-unit">GO.</span>
                </p>
              </div>
            </>
          )}
        </div>

        <div className="mt-6 grid grid-cols-1 gap-6 border-t-2 border-foreground pt-6 md:mt-8 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center lg:gap-10">
          <div>
            <p className={cn(subtitle("text-[1.5rem]"), "mb-3 break-keep sm:hidden")}>{t.hero.subtitle}</p>
            <p className="max-w-xl text-base leading-7 text-foreground/70">{t.hero.lead}</p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Start locale={locale}>{t.start}</Start>
            <InstallCommand className="h-12" />
          </div>
        </div>
      </div>
      <div className="hazard relative h-3" />
    </section>
  )
}

/**
 * The sync rate, 100.0%, counting up from 0.0% as a gauge does when the page
 * opens, the tenths flickering past. In CSS alone (an integer property, its
 * thousandths shown as counters), so the page paints it already counting;
 * still, at 100.0%, for readers who prefer reduced motion.
 */
function SyncRate() {
  return (
    <>
      <span aria-hidden className="sync-rate" />
      <span className="sr-only">100.0%</span>
    </>
  )
}

type Unit = { name: string; toolkit: string }

/** Warning tape that runs past, fading out at its ends and held while pointed at: every platform and the widgets it gets. */
function Ticker({ units, t }: { units: Unit[]; t: Copy }) {
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
      <p className="sr-only">
        {t.runsOn} {units.map((u) => `${u.name} (${u.toolkit})`).join(", ")}.
      </p>
      <div className="ticker-track" aria-hidden>
        <div className="ticker flex w-max">
          {run}
          {run}
        </div>
      </div>
    </div>
  )
}

/** The spec, as three lines of a title card. */
function Spec({ t }: { t: Copy }) {
  return (
    <section className="bleed border-b">
      <div className="inner py-20 md:py-28">
        <p className="label">{t.spec.label}</p>
        <dl className="mt-8">
          {t.spec.lines.map(({ line, note, red }) => (
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
function Units({ units, t }: { units: Unit[]; t: Copy }) {
  return (
    <section className="bleed">
      <Heading label={t.units.label} aside={t.units.aside}>
        {t.units.heading}
      </Heading>
      <div className="inner pb-24">
        <ol className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {units.map((u, i) => (
            <li key={u.name} className="hud scanlines relative border bg-card p-5 [--hud:var(--unit)]">
              <div className="label flex items-center justify-between">
                <span>{t.units.unit(String(i + 1).padStart(2, "0"))}</span>
                <span className="flex items-center gap-1.5 text-signal-ink">
                  <span className="size-1.5 animate-pulse rounded-full bg-signal" /> {t.units.active}
                </span>
              </div>
              <p className="display mt-8 text-5xl leading-none">{u.name}</p>
              <p className="mt-3 font-mono text-[13px] text-foreground/70">{u.toolkit}</p>
              <div className="mt-8 flex items-center gap-3">
                <div className="h-2 flex-1 bg-foreground/10">
                  <div className="sync-fill h-full origin-left bg-signal" />
                </div>
                <span className="label text-foreground">{t.units.sync}</span>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}

/** The features, six episodes, each with its numeral writ large behind it. */
function Episodes({ t }: { t: Copy }) {
  return (
    <section className="bleed border-t">
      <Heading label={t.episodes.label} aside={t.episodes.aside}>
        {t.episodes.heading}
      </Heading>
      <div className="border-t">
        <ol className="mx-auto grid max-w-6xl gap-px bg-border sm:grid-cols-2 lg:grid-cols-3">
          {t.episodes.features.map((feature, i) => (
            <li key={feature.title} className="bg-background">
              <a href={feature.href} className="group relative flex h-full flex-col overflow-hidden px-5 pt-7 pb-10 transition-colors hover:bg-code sm:px-8">
                <span className="absolute top-0 left-0 h-0.5 w-0 bg-unit transition-[width] duration-300 group-hover:w-full" />
                <span
                  lang="ja"
                  aria-hidden
                  className="display pointer-events-none absolute -right-3 -bottom-10 text-[11rem] leading-none text-unit/12 transition-colors group-hover:text-unit/25"
                >
                  {numerals[i]}
                </span>
                <span className="relative flex items-center justify-between">
                  <span className="display text-[15px] text-unit-ink" lang="ja">
                    第{numerals[i]}話
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

/**
 * The preview of the next episode: the motto, where to begin, and the name.
 * Its overflow is clipped, not hidden: a section that hides its overflow is a
 * scroll container, which the wordmark's view() timeline would follow.
 */
function Finale({ locale, t }: { locale: Locale; t: Copy }) {
  return (
    <section className="bleed relative overflow-clip border-t">
      <div className="hazard h-3" />
      <div className="scanlines pointer-events-none absolute inset-0" />
      <div className="inner relative pt-24 pb-16 md:pt-32">
        <p className="label text-signal-ink">{t.finale.label}</p>
        <p className="display mt-6 text-[clamp(2.6rem,8vw,6.5rem)] leading-[0.98]">{t.finale.motto}</p>
        <div className="mt-12 flex flex-wrap items-center gap-3">
          <Start locale={locale}>{t.finale.begin}</Start>
          <Link
            to={locale === "zh" ? "/zh/docs" : "/docs"}
            className="inline-flex h-12 items-center border px-6 font-mono text-[13px] tracking-[0.14em] uppercase transition-colors hover:border-foreground/40"
          >
            {t.finale.docs}
          </Link>
        </div>
      </div>
      <div className="relative px-[2vw] pt-10 pb-[2vw]" aria-hidden>
        <Wordmark lambda="var(--unit)" className="wordmark-slam h-auto w-full text-foreground" />
      </div>
    </section>
  )
}
