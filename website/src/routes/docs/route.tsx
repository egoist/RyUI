import { createFileRoute } from "@tanstack/react-router"

import { DocsLayout } from "@/components/docs/docs-layout"
import { getDocsNav } from "@/lib/docs"

export const Route = createFileRoute("/docs")({
  loader: () => getDocsNav({ data: "en" }),
  staleTime: Infinity,
  component: () => <DocsLayout nav={Route.useLoaderData()} />,
})
