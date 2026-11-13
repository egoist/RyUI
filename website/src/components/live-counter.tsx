import * as React from "react"

import { useLocale, type Locale } from "@/lib/i18n"
import { cn } from "@/lib/utils"

type Words = {
  live: string
  source: string
  ops: string
  recorded: string
  replay: string
  mount: string
  flush: (n: number, ops: number, batches: number) => string
  ran: string
  times: (n: number) => string
  last: string
  opCount: (n: number) => string
  wider: string
}

const words: Record<Locale, Words> = {
  en: {
    live: "Live",
    source: "Source",
    ops: "Ops to the host",
    recorded: "As host/fake records them for this code, on an iPhone's 390 × 844.",
    replay: "Replay",
    mount: "mount",
    flush: (n, ops, batches) => `flush ${String(n).padStart(2, "0")} · ${ops} op${ops === 1 ? "" : "s"}${batches > 1 ? ` · ${batches} batches` : ""}`,
    ran: "ran",
    times: (n) => `${n}×`,
    last: "last flush",
    opCount: (n) => `${n} op${n === 1 ? "" : "s"}`,
    wider: "wider text, a new frame",
  },
  zh: {
    live: "运行中",
    source: "源码",
    ops: "发给宿主的 op",
    recorded: "即 host/fake 为这段代码记录下的 op，屏幕为 iPhone 的 390 × 844。",
    replay: "重放",
    mount: "挂载",
    flush: (n, ops, batches) => `第 ${String(n).padStart(2, "0")} 次提交 · ${ops} 个 op${batches > 1 ? ` · ${batches} 批` : ""}`,
    ran: "运行",
    times: (n) => `${n} 次`,
    last: "上次提交",
    opCount: (n) => `${n} 个 op`,
    wider: "文字变宽，重新排版",
  },
}

/**
 * What mounting the docs' first example sends the host, in two batches
 * (props, then frames), as host/fake logs it on its default iPhone. Its
 * text is measured as 8 points a character, 20 high.
 */
const mountOps = [
  "Create 1 View",
  "Command 1 Locale 00656e",
  "Create 2 Text",
  'SetProp 2 Text="Count: 0"',
  "Create 3 Button",
  "SetProp 3 Variant=1",
  'SetProp 3 Title="Add"',
  "Listen 3 Press",
  "Insert 1 2 @0",
  "Insert 1 3 @1",
  "SetRoot 1",
  "SetFrame 2 163,396.66666 64x20",
  "SetFrame 3 171,416.66666 48x44",
  "SetFrame 1 0,0 390x844",
]

const label = (n: number) => `Count: ${n}`

/** The frame host/fake gives the label: centered, as wide as its text. */
function frame(n: number) {
  const w = label(n).length * 8
  return `SetFrame 2 ${(390 - w) / 2},396.66666 ${w}x20`
}

type Row = { key: number; kind: "op" | "event" | "flush" | "note"; text: string }

/** An op of the log: its code bright, the rest dim, the strings green. */
function Op({ text }: { text: string }) {
  const [code, ...rest] = text.split(" ")
  const tail = rest.join(" ")
  const eq = tail.indexOf("=")
  return (
    <>
      <span className="text-signal-ink">{code}</span>{" "}
      {eq < 0 ? (
        <span className="text-foreground/80">{tail}</span>
      ) : (
        <>
          <span className="text-foreground/80">{tail.slice(0, eq + 1)}</span>
          <span className="text-[#a5d6a7]">{tail.slice(eq + 1)}</span>
        </>
      )}
    </>
  )
}

/**
 * Hello, native, running: the docs' first example beside the app it makes
 * and the ops it sends the host. Pressing Add lights the lines that run,
 * a press's path through the code: the button's OnPress, the label that
 * reads the count, the Text that shows it; boxes the one view that
 * changed; and logs the flush: one op. Counter itself ran once, at mount.
 * When it scrolls into view it mounts, its ops streaming in, and presses
 * Add by itself to 10, where the text grows wider and gets a new frame,
 * until the reader presses it themselves (and never by itself, for readers
 * who prefer reduced motion).
 */
export function LiveCounter({ code }: { code: string }) {
  const locale = useLocale()
  const t = words[locale]

  const root = React.useRef<HTMLDivElement>(null)
  const source = React.useRef<HTMLDivElement>(null)
  const box = React.useRef<HTMLSpanElement>(null)
  const button = React.useRef<HTMLButtonElement>(null)
  const touch = React.useRef<HTMLSpanElement>(null)

  const [count, setCount] = React.useState(0)
  const [labelRuns, setLabelRuns] = React.useState(1)
  const [lastOps, setLastOps] = React.useState(mountOps.length)
  const [rows, setRows] = React.useState<Row[]>(() => [
    { key: -2, kind: "flush", text: t.flush(1, mountOps.length, 2) },
    ...mountOps.map((text, i) => ({ key: -100 - i, kind: "op" as const, text })),
  ])
  const [mounted, setMounted] = React.useState(true)

  // Everything a press or the mount schedules, cleared when it starts over or the page goes.
  const timers = React.useRef(new Set<ReturnType<typeof setTimeout>>())
  const after = React.useCallback((ms: number, f: () => void) => {
    const id = setTimeout(() => {
      timers.current.delete(id)
      f()
    }, ms)
    timers.current.add(id)
  }, [])
  const clear = React.useCallback(() => {
    for (const id of timers.current) clearTimeout(id)
    timers.current.clear()
  }, [])
  React.useEffect(() => clear, [clear])

  const state = React.useRef({ count: 0, flush: 1, key: 0, auto: true, visible: false, reduce: false })

  const push = React.useCallback((kind: Row["kind"], text: string) => {
    const key = ++state.current.key
    setRows((rs) => [...rs.slice(-40), { key, kind, text }])
  }, [])

  /** Lights a line of the source: the first whose text includes needle. */
  const light = React.useCallback((needle: string, color = "var(--signal)") => {
    const el = source.current
    if (!el || state.current.reduce) return
    const line = [...el.querySelectorAll<HTMLElement>(".line")].find((l) => l.textContent?.includes(needle))
    if (!line) return
    const outer = el.getBoundingClientRect()
    const r = line.getBoundingClientRect()
    const lh = parseFloat(getComputedStyle(line).lineHeight) || r.height
    const bar = document.createElement("span")
    bar.setAttribute("aria-hidden", "true")
    Object.assign(bar.style, {
      position: "absolute",
      left: "0",
      right: "0",
      top: `${r.top - outer.top + el.scrollTop - (lh - r.height) / 2}px`,
      height: `${lh}px`,
      pointerEvents: "none",
      background: `linear-gradient(to right, color-mix(in oklch, ${color} 26%, transparent), color-mix(in oklch, ${color} 6%, transparent) 70%, transparent)`,
      boxShadow: `inset 2px 0 0 ${color}`,
    })
    el.append(bar)
    bar.animate([{ opacity: 1 }, { opacity: 1, offset: 0.25 }, { opacity: 0 }], { duration: 1100, easing: "ease-out" }).onfinish = () => bar.remove()
  }, [])

  /** Boxes the label, the one view the flush changed. */
  const mark = React.useCallback((color: string) => {
    const el = box.current
    if (!el || state.current.reduce) return
    el.style.setProperty("--hud", color)
    el.animate(
      [
        { opacity: 0, inset: "-14px -18px" },
        { opacity: 1, inset: "-6px -10px", offset: 0.2 },
        { opacity: 1, inset: "-6px -10px", offset: 0.7 },
        { opacity: 0, inset: "-6px -10px" },
      ],
      { duration: 1000, easing: "cubic-bezier(0.16, 1, 0.3, 1)" }
    )
  }, [])

  /** Add, pressed: the host's event in, a flush out. */
  const press = React.useCallback(() => {
    const s = state.current
    const n = ++s.count
    const f = ++s.flush
    light("OnPress")
    button.current?.animate([{ scale: 1 }, { scale: 0.94, opacity: 0.7 }, { scale: 1 }], { duration: 260, easing: "ease-out" })
    after(s.reduce ? 0 : 140, () => {
      light("count.Get()")
      setLabelRuns((r) => r + 1)
    })
    after(s.reduce ? 0 : 280, () => {
      light("ui.Text(label)")
      setCount(n)
      const grew = label(n).length !== label(n - 1).length
      // The press's rows together, however quickly presses come.
      push("event", "→ Press 3")
      push("op", `SetProp 2 Text="${label(n)}"`)
      if (grew) push("op", frame(n))
      push("flush", t.flush(f, grew ? 2 : 1, grew ? 2 : 1))
      setLastOps(grew ? 2 : 1)
      if (grew) push("note", t.wider)
      mark(grew ? "var(--unit)" : "var(--signal)")
    })
  }, [after, light, mark, push, t])

  /** The tap the page makes by itself: a touch on Add, then the press. */
  const autoTap = React.useCallback(() => {
    touch.current?.animate(
      [
        { opacity: 0, scale: 1.6 },
        { opacity: 0.55, scale: 1, offset: 0.4 },
        { opacity: 0, scale: 0.9 },
      ],
      { duration: 420, easing: "ease-out" }
    )
    after(120, press)
  }, [after, press])

  /** Mounts the app afresh: Counter runs, and its ops stream in. */
  const mount = React.useCallback(
    (stream: boolean) => {
      clear()
      const s = state.current
      s.count = 0
      s.flush = 1
      setCount(0)
      setLabelRuns(1)
      setLastOps(mountOps.length)
      setMounted(true)
      if (!stream || s.reduce) {
        setRows([{ key: ++s.key, kind: "flush", text: t.flush(1, mountOps.length, 2) }, ...mountOps.map((text) => ({ key: ++s.key, kind: "op" as const, text }))])
        return
      }
      setRows([])
      setMounted(false)
      // Counter's body runs once, top to bottom.
      for (const [i, needle] of ["func Counter", "signal.New", "signal.Func", "count.Get()", "ui.SafeArea", "ui.Text(label)", "ui.Button", "OnPress"].entries()) {
        after(i * 55, () => light(needle, "var(--unit)"))
      }
      mountOps.forEach((op, i) => after(200 + i * 45, () => push("op", op)))
      const done = 200 + mountOps.length * 45
      after(done, () => {
        push("flush", t.flush(1, mountOps.length, 2))
        setMounted(true)
      })
      // Then Add, pressed to 10, while the reader watches.
      const tick = () => {
        if (!s.auto || s.count >= 10) return
        if (s.visible) autoTap()
        after(s.count < 8 ? 950 : 1300, tick)
      }
      after(done + 900, tick)
    },
    [after, autoTap, clear, light, push, t]
  )

  // Mount when the panel first comes into view; press by itself only while it's in view.
  React.useEffect(() => {
    const el = root.current
    if (!el) return
    const s = state.current
    s.reduce = matchMedia("(prefers-reduced-motion: reduce)").matches
    if (s.reduce) return
    let started = false
    const io = new IntersectionObserver(
      ([e]) => {
        s.visible = e.isIntersecting
        if (e.isIntersecting && !started) {
          started = true
          mount(true)
        }
      },
      { threshold: 0.4 }
    )
    io.observe(el)
    return () => io.disconnect()
  }, [mount])

  // The newest op in view.
  const log = React.useRef<HTMLOListElement>(null)
  React.useLayoutEffect(() => {
    const el = log.current
    if (el) el.scrollTop = el.scrollHeight
  }, [rows])

  const stop = () => {
    state.current.auto = false
  }

  return (
    <div ref={root} className="hud grid border bg-code lg:grid-cols-[1.35fr_1fr] lg:divide-x" onPointerDown={stop}>
      <figure className="flex min-w-0 flex-col">
        <figcaption className="label flex items-center justify-between border-b px-4 py-3 sm:px-6">
          <span className="flex items-center gap-2">
            <span className="size-1.5 bg-signal" /> main.go
          </span>
          <span>{t.source}</span>
        </figcaption>
        <div ref={source} className="relative flex-1 overflow-x-auto px-4 py-5 sm:px-6" dangerouslySetInnerHTML={{ __html: code }} />
        <dl className="label grid grid-cols-3 border-t">
          {[
            ["Counter()", t.times(1)],
            ["label", t.times(labelRuns)],
            [t.last, t.opCount(lastOps)],
          ].map(([name, value], i) => (
            <div key={i} className="flex flex-col gap-1 border-r px-4 py-3 last:border-r-0 sm:px-6">
              <dt className="normal-case">
                {name}
                {i < 2 && <span className="uppercase"> · {t.ran}</span>}
              </dt>
              <dd key={value} className={cn("font-code text-[15px] tracking-normal text-foreground", i > 0 && "live-row")}>
                {value}
              </dd>
            </div>
          ))}
        </dl>
      </figure>

      <div className="flex min-w-0 flex-col border-t lg:border-t-0">
        <p className="label flex items-center justify-between border-b px-4 py-3 sm:px-6">
          <span className="flex items-center gap-2">
            <span className="size-1.5 animate-pulse rounded-full bg-unit" /> {t.live}
          </span>
          <button type="button" onClick={() => {
              state.current.auto = true
              mount(true)
            }} className="label transition-colors hover:text-foreground">
            ↻ {t.replay}
          </button>
        </p>

        {/* The app, as an iPhone shows it. */}
        <div className="flex justify-center px-4 py-6" data-platform="ios">
          <div className="relative flex h-[260px] w-[200px] flex-col items-center justify-center gap-2 rounded-[34px] border-[5px] border-[#2a282e] bg-black shadow-[0_0_0_1px_rgb(255_255_255/0.06)]">
            <span aria-hidden className="absolute top-2.5 h-[18px] w-[64px] rounded-full bg-[#1c1b1f]" />
            <span className={cn("relative transition-opacity duration-200", !mounted && "opacity-0")}>
              <span ref={box} aria-hidden className="live-box hud absolute opacity-0 [inset:-6px_-10px]" />
              <span aria-live="polite" className="text-[22px] font-bold text-white tabular-nums [font-family:-apple-system,'SF_Pro_Display',system-ui,sans-serif]">
                {label(count)}
              </span>
            </span>
            <span className={cn("relative transition-opacity duration-200", !mounted && "opacity-0")}>
              <button ref={button} type="button" onClick={press} className="mock-button cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2">
                Add
              </button>
              <span ref={touch} aria-hidden className="pointer-events-none absolute top-1/2 left-1/2 -mt-5 -ml-5 size-10 rounded-full bg-white opacity-0" />
            </span>
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col border-t">
          <p className="label flex items-center gap-2 px-4 pt-3 sm:px-6">
            <span className="size-1.5 bg-signal" /> {t.ops}
          </p>
          <ol ref={log} aria-live="off" className="live-log h-56 overflow-hidden px-4 pt-2 pb-3 font-code text-[12px] leading-[1.75] sm:px-6">
            {rows.map((row) => (
              <li
                key={row.key}
                className={cn(
                  "live-row truncate",
                  row.kind === "event" && "mt-1.5 text-unit-ink",
                  row.kind === "flush" && "text-muted-foreground",
                  row.kind === "note" && "text-unit-ink before:content-['↳_']"
                )}
              >
                {row.kind === "op" ? <Op text={row.text} /> : row.text}
              </li>
            ))}
          </ol>
          <p className="border-t px-4 py-3 text-xs leading-5 text-muted-foreground sm:px-6">
            {t.recorded}
          </p>
        </div>
      </div>
    </div>
  )
}
