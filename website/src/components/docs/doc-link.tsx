import { Link, type LinkComponentProps } from "@tanstack/react-router"

import { routes, useLocale } from "@/lib/i18n"

/**
 * A link to a page of the docs, in the language being read, by its slug, ""
 * for the introduction. Only the page itself is active: /docs/plugins is not
 * when /docs/plugins/fetch is.
 */
export function DocLink({ slug, ...props }: { slug: string } & Omit<LinkComponentProps<"a">, "to" | "params">) {
  const to = routes[useLocale()]
  if (slug) return <Link to={to.page} params={{ _splat: slug }} activeOptions={{ exact: true }} {...props} />
  return <Link to={to.docs} activeOptions={{ exact: true }} {...props} />
}
