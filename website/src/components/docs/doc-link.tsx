import { Link, type LinkComponentProps } from "@tanstack/react-router"

import { useLocale } from "@/lib/i18n"

/**
 * A link to a page of the docs, in the language being read, by its slug, ""
 * for the introduction. Only the page itself is active: /docs/plugins is not
 * when /docs/plugins/fetch is.
 */
export function DocLink({ slug, ...props }: { slug: string } & Omit<LinkComponentProps<"a">, "to" | "params">) {
  const zh = useLocale() === "zh"
  if (slug) {
    return zh ? (
      <Link to="/zh/docs/$" params={{ _splat: slug }} activeOptions={{ exact: true }} {...props} />
    ) : (
      <Link to="/docs/$" params={{ _splat: slug }} activeOptions={{ exact: true }} {...props} />
    )
  }
  return <Link to={zh ? "/zh/docs" : "/docs"} activeOptions={{ exact: true }} {...props} />
}
