import * as React from "react"

import { useLocale, type Locale } from "@/lib/i18n"
import { cn } from "@/lib/utils"

type Words = { platform: string; source: string; core: string; host: string; name: string; save: string; toggle: string; dom: string; note: string }

/** A string of the table, or one that changes with the language. */
type Text = string | ((t: Words) => string)

function text(value: Text, t: Words) {
  return typeof value === "string" ? value : value(t)
}

const words: Record<Locale, Words> = {
  en: {
    platform: "Platform",
    source: "Source",
    core: "Core",
    host: "Host",
    name: "Name",
    save: "Save",
    toggle: "BUTTON, a toggle",
    dom: "The DOM",
    note: "The widgets here are sketches of each platform's, in its colors; on a device they are the platform's own.",
  },
  zh: {
    platform: "平台",
    source: "源码",
    core: "核心",
    host: "宿主",
    name: "名字",
    save: "保存",
    toggle: "BUTTON（开关样式）",
    dom: "DOM",
    note: "这里的控件只是按各平台配色画的示意图；在真机上，它们就是平台自己的控件。",
  },
  ja: {
    platform: "プラットフォーム",
    source: "ソース",
    core: "コア",
    host: "ホスト",
    name: "名前",
    save: "保存",
    toggle: "BUTTON（トグル）",
    dom: "DOM",
    note: "ここに描いたウィジェットは各プラットフォームの配色によるスケッチです。実機では、プラットフォーム純正のウィジェットそのものが表示されます。",
  },
}

// What each host makes of four of the core's components: the classes are the
// ones the hosts create (platform/ios, platform/android, host/darwin,
// host/windows, host/linux, platform/web/src).
const platforms = [
  { id: "ios", name: "iOS", toolkit: "UIKit" },
  { id: "android", name: "Android", toolkit: "Material 3" },
  { id: "macos", name: "macOS", toolkit: "AppKit" },
  { id: "windows", name: "Windows", toolkit: "Win32" },
  { id: "linux", name: "Linux", toolkit: "GTK 4 · libadwaita" },
  { id: "web", name: "Web", toolkit: "" },
] as const

type Platform = (typeof platforms)[number]["id"]

const rows: { call: string; args: Text; widget: "field" | "switch" | "slider" | "button"; native: Record<Platform, Text> }[] = [
  {
    call: "TextField",
    args: "{Value: name}",
    widget: "field",
    native: { ios: "UITextField", android: "EditText", macos: "NSTextField", windows: "EDIT", linux: "GtkEntry", web: "<input>" },
  },
  {
    call: "Toggle",
    args: "{Value: alerts}",
    widget: "switch",
    native: { ios: "UISwitch", android: "MaterialSwitch", macos: "NSSwitch", windows: (t) => t.toggle, linux: "GtkSwitch", web: '<input role="switch">' },
  },
  {
    call: "Slider",
    args: "{Value: volume}",
    widget: "slider",
    native: { ios: "UISlider", android: "Slider", macos: "NSSlider", windows: "msctls_trackbar32", linux: "GtkScale", web: '<input type="range">' },
  },
  {
    call: "Button",
    args: (t) => `{Title: ui.Val(${JSON.stringify(t.save)})}`,
    widget: "button",
    native: { ios: "UIButton", android: "MaterialButton", macos: "NSButton", windows: "BUTTON", linux: "GtkButton", web: "<button>" },
  },
]

/** A widget as the platform draws it: a sketch, in its colors and shape. */
function Mock({ widget, locale }: { widget: (typeof rows)[number]["widget"]; locale: Locale }) {
  switch (widget) {
    case "field":
      return <span className="mock-field">{words[locale].name}</span>
    case "switch":
      return (
        <span className="mock-switch">
          <i />
        </span>
      )
    case "slider":
      return (
        <span className="mock-slider">
          <b />
          <i />
        </span>
      )
    case "button":
      return <span className="mock-button">{words[locale].save}</span>
  }
}

/**
 * Go in, native out: four lines of an app, each wired to the widget a host
 * makes of it. The platforms take turns until the reader picks one (and
 * don't, for readers who prefer reduced motion); each widget changes into the
 * next platform's in place.
 */
export function Translation() {
  const locale = useLocale()
  const t = words[locale]
  const [current, setCurrent] = React.useState<Platform>("ios")
  const [picked, setPicked] = React.useState(false)
  const [hover, setHover] = React.useState(false)

  React.useEffect(() => {
    if (picked || hover || matchMedia("(prefers-reduced-motion: reduce)").matches) return
    const timer = setInterval(() => {
      setCurrent((p) => platforms[(platforms.findIndex((x) => x.id === p) + 1) % platforms.length].id)
    }, 2600)
    return () => clearInterval(timer)
  }, [picked, hover])

  const platform = platforms.find((p) => p.id === current)!

  return (
    <div onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
      <div role="tablist" aria-label={t.platform} className="flex flex-wrap gap-1.5">
        {platforms.map((p) => (
          <button
            key={p.id}
            type="button"
            role="tab"
            aria-selected={p.id === current}
            onClick={() => {
              setCurrent(p.id)
              setPicked(true)
            }}
            className={cn(
              "h-9 border px-3.5 font-mono text-[12px] tracking-[0.12em] uppercase transition-colors",
              p.id === current ? "border-unit bg-unit text-white" : "text-muted-foreground hover:border-foreground/30 hover:text-foreground"
            )}
          >
            {p.name}
          </button>
        ))}
      </div>

      <div className="hud mt-4 border bg-code [--hud:var(--signal)]">
        <div className="label hidden grid-cols-[minmax(0,1fr)_7rem_minmax(0,1.1fr)] border-b md:grid">
          <span className="px-5 py-3">{t.source} · main.go</span>
          <span className="py-3 text-center text-signal-ink">{t.core}</span>
          <span className="flex justify-between px-5 py-3">
            <span>
              {t.host} · {platform.toolkit || t.dom}
            </span>
            <span className="text-foreground">{platform.name}</span>
          </span>
        </div>

        <div className="px-5 pt-4 font-code text-[13.5px] text-muted-foreground md:pb-1">ui.Column(ui.Style{"{"}Gap: 12{"}"},</div>
        <ol>
          {rows.map((row, i) => (
            <li key={row.call} className="grid items-center gap-x-0 gap-y-3 border-b border-dashed px-5 py-4 last:border-b-0 md:grid-cols-[minmax(0,1fr)_7rem_minmax(0,1.1fr)] md:px-0 md:py-3">
              <code className="min-w-0 truncate pl-0 font-code text-[13.5px] md:pl-10">
                <span className="text-muted-foreground">ui.</span>
                <span className="text-foreground">{row.call}</span>
                <span className="text-muted-foreground">(ui.{row.call}Props</span>
                <span className="text-signal-ink">{text(row.args, t)}</span>
                <span className="text-muted-foreground">),</span>
              </code>
              <span aria-hidden className="wire relative hidden h-px bg-signal/40 md:block" style={{ ["--delay" as string]: `${i * 0.35}s` }} />
              <span className="flex min-w-0 items-center justify-between gap-4 md:px-5" data-platform={current}>
                <span className="min-w-0 truncate font-code text-[13px] text-signal-ink">{text(row.native[current], t)}</span>
                {/* As tall as the tallest platform's widget, so switching doesn't move the rows. */}
                <span aria-hidden className="flex h-12 w-44 shrink-0 items-center justify-end">
                  <Mock widget={row.widget} locale={locale} />
                </span>
              </span>
            </li>
          ))}
        </ol>
        <div className="px-5 pt-1 pb-4 font-code text-[13.5px] text-muted-foreground">)</div>
      </div>
      <p className="mt-3 text-sm text-muted-foreground">
        {t.note}
      </p>
    </div>
  )
}
