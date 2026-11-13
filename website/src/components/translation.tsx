import * as React from "react"

import { cn } from "@/lib/utils"

// What each host makes of four of the core's components: the classes are the
// ones the hosts create (platform/ios, platform/android, host/darwin,
// host/windows, host/linux, platform/web/src).
const platforms = [
  { id: "ios", name: "iOS", toolkit: "UIKit" },
  { id: "android", name: "Android", toolkit: "Material 3" },
  { id: "macos", name: "macOS", toolkit: "AppKit" },
  { id: "windows", name: "Windows", toolkit: "Win32" },
  { id: "linux", name: "Linux", toolkit: "GTK 4 · libadwaita" },
  { id: "web", name: "Web", toolkit: "The DOM" },
] as const

type Platform = (typeof platforms)[number]["id"]

const rows: { call: string; args: string; widget: "field" | "switch" | "slider" | "button"; native: Record<Platform, string> }[] = [
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
    native: { ios: "UISwitch", android: "MaterialSwitch", macos: "NSSwitch", windows: "BUTTON, a toggle", linux: "GtkSwitch", web: '<input role="switch">' },
  },
  {
    call: "Slider",
    args: "{Value: volume}",
    widget: "slider",
    native: { ios: "UISlider", android: "Slider", macos: "NSSlider", windows: "msctls_trackbar32", linux: "GtkScale", web: '<input type="range">' },
  },
  {
    call: "Button",
    args: '{Title: ui.Val("Save")}',
    widget: "button",
    native: { ios: "UIButton", android: "MaterialButton", macos: "NSButton", windows: "BUTTON", linux: "GtkButton", web: "<button>" },
  },
]

/** A widget as the platform draws it: a sketch, in its colors and shape. */
function Mock({ widget }: { widget: (typeof rows)[number]["widget"] }) {
  switch (widget) {
    case "field":
      return <span className="mock-field">Name</span>
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
      return <span className="mock-button">Save</span>
  }
}

/**
 * Go in, native out: four lines of an app, each wired to the widget a host
 * makes of it. The platforms take turns until the reader picks one (and
 * don't, for readers who prefer reduced motion); each widget changes into the
 * next platform's in place.
 */
export function Translation() {
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
      <div role="tablist" aria-label="Platform" className="flex flex-wrap gap-1.5">
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
          <span className="px-5 py-3">Source · main.go</span>
          <span className="py-3 text-center text-signal-ink">Core</span>
          <span className="flex justify-between px-5 py-3">
            <span>Host · {platform.toolkit}</span>
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
                <span className="text-signal-ink">{row.args}</span>
                <span className="text-muted-foreground">),</span>
              </code>
              <span aria-hidden className="wire relative hidden h-px bg-signal/40 md:block" style={{ ["--delay" as string]: `${i * 0.35}s` }} />
              <span className="flex min-w-0 items-center justify-between gap-4 md:px-5" data-platform={current}>
                <span className="min-w-0 truncate font-code text-[13px] text-signal-ink">{row.native[current]}</span>
                {/* As tall as the tallest platform's widget, so switching doesn't move the rows. */}
                <span aria-hidden className="flex h-12 w-44 shrink-0 items-center justify-end">
                  <Mock widget={row.widget} />
                </span>
              </span>
            </li>
          ))}
        </ol>
        <div className="px-5 pt-1 pb-4 font-code text-[13.5px] text-muted-foreground">)</div>
      </div>
      <p className="mt-3 text-sm text-muted-foreground">
        The widgets here are sketches of each platform&apos;s, in its colors; on a device they are the platform&apos;s own.
      </p>
    </div>
  )
}
