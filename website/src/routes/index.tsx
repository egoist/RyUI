import { createFileRoute } from "@tanstack/react-router"

import { Home, homeHead } from "@/components/home"
import { getHome } from "@/lib/home"

export const Route = createFileRoute("/")({
  loader: () => getHome(),
  head: () => homeHead("en"),
  component: () => <Home locale="en" data={Route.useLoaderData()} />,
})
