import { createFileRoute } from "@tanstack/react-router"

import { Home, homeHead } from "@/components/home"
import { getHome } from "@/lib/home"

export const Route = createFileRoute("/ja/")({
  loader: () => getHome(),
  head: () => homeHead("ja"),
  component: () => <Home locale="ja" data={Route.useLoaderData()} />,
})
