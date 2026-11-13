import { Link, createFileRoute } from "@tanstack/react-router"
import { ArrowRightIcon } from "lucide-react"

import { InstallCommand } from "@/components/install-command"
import { buttonVariants } from "@/components/ui/button"
import { getHome } from "@/lib/home"
import { site } from "@/lib/site"

export const Route = createFileRoute("/")({
  loader: () => getHome(),
  component: Home,
})

// What the README says, in short, each with the page that says more.
const features = [
  {
    title: "The platform's own widgets",
    text: "UIKit on iOS, Material 3 on Android, AppKit on the Mac, Win32 drawn as Windows 11's, GTK 4 and libadwaita on Linux, the DOM in a browser: navigation, lists, fields, pickers, sheets and menus that look and feel as each platform's do.",
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
]

function Home() {
  const { main } = Route.useLoaderData()
  return (
    <main>
      <section className="px-4 py-20 sm:px-10 md:py-28">
        <h1 className="text-4xl leading-[1.05] font-semibold tracking-[-0.04em] sm:text-5xl md:text-6xl">
          Native apps in Go.
          <span className="block text-muted-foreground">The platform&apos;s widgets, not a web view.</span>
        </h1>
        <p className="mt-6 max-w-xl text-lg leading-8 text-muted-foreground">
          Write one app in Go alone, for phones, desktops and the web. Asuka shows it with each platform&apos;s own widgets, and updates only what changed.
        </p>
        <div className="mt-10 flex flex-wrap items-center gap-3">
          <InstallCommand />
          <Link to="/docs/$" params={{ _splat: "getting-started" }} className={buttonVariants({ size: "lg", className: "h-11 px-5" })}>
            Get started <ArrowRightIcon />
          </Link>
        </div>
      </section>

      <div className="grid border-t bg-code lg:grid-cols-[1.4fr_1fr] lg:divide-x">
        <figure className="min-w-0">
          <figcaption className="label border-b px-4 py-3 sm:px-6">main.go</figcaption>
          <div className="overflow-x-auto px-4 py-5 sm:px-6" dangerouslySetInnerHTML={{ __html: main }} />
        </figure>
        <div className="flex min-w-0 flex-col border-t lg:border-t-0">
          <p className="label border-b px-4 py-3 sm:px-6">Run it</p>
          <pre className="overflow-x-auto px-4 py-5 font-mono text-[13px] leading-[1.7] sm:px-6">
            <span className="text-gopher-ink select-none">$ </span>
            {"asuka new hello && cd hello\n"}
            <span className="text-gopher-ink select-none">$ </span>
            {"asuka dev                    # this computer: Mac, PC or Linux\n"}
            <span className="text-gopher-ink select-none">$ </span>
            {"asuka dev -platform ios      # the iOS simulator\n"}
            <span className="text-gopher-ink select-none">$ </span>
            {"asuka dev -platform android  # an emulator or a phone\n"}
            <span className="text-gopher-ink select-none">$ </span>
            {"asuka dev -platform web      # your browser\n"}
          </pre>
          <p className="mt-auto border-t px-4 py-4 text-sm leading-6 text-muted-foreground sm:px-6">
            Save a Go file and the app reloads in a moment, its state kept. See{" "}
            <a href={`${site.repo}/tree/main/examples`} className="text-foreground underline underline-offset-4">
              the examples
            </a>
            : the gallery shows every component.
          </p>
        </div>
      </div>

      <ul className="grid gap-px border-t bg-border sm:grid-cols-2">
        {features.map((feature) => (
          <li key={feature.title} className="bg-background">
            <a href={feature.href} className="group flex h-full flex-col px-4 py-8 transition-colors hover:bg-code sm:px-10">
              <h2 className="flex items-center justify-between font-semibold tracking-tight">
                {feature.title}
                <ArrowRightIcon className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
              </h2>
              <p className="mt-2 max-w-md text-sm leading-6 text-muted-foreground">{feature.text}</p>
            </a>
          </li>
        ))}
      </ul>
    </main>
  )
}
