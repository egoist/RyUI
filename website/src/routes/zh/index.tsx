import { createFileRoute } from "@tanstack/react-router"

import { Home, homeHead } from "@/components/home"
import { getHome } from "@/lib/home"

export const Route = createFileRoute("/zh/")({
  loader: () => getHome(),
  head: () => homeHead("zh"),
  component: () => <Home locale="zh" data={Route.useLoaderData()} />,
})
