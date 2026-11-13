import { Link } from "@tanstack/react-router"

import { buttonVariants } from "@/components/ui/button"
import { messages, useLocale } from "@/lib/i18n"

export function NotFound() {
  const locale = useLocale()
  const t = messages[locale].notFound
  const zh = locale === "zh"
  return (
    <main className="flex flex-1 flex-col items-center justify-center px-6 py-32 text-center">
      <p className="label text-unit-ink">{t.label}</p>
      <h1 className="display mt-4 text-4xl leading-tight sm:text-5xl">{t.title}</h1>
      <p className="mt-3 max-w-sm text-muted-foreground">{t.text}</p>
      <div className="mt-8 flex gap-3">
        <Link to={zh ? "/zh/docs" : "/docs"} className={buttonVariants({ size: "lg" })}>
          {t.docs}
        </Link>
        <Link to={zh ? "/zh" : "/"} className={buttonVariants({ variant: "outline", size: "lg" })}>
          {t.home}
        </Link>
      </div>
    </main>
  )
}
