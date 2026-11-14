import { createFileRoute } from "@tanstack/react-router"

import { DocsLayout } from "@/components/docs/docs-layout"
import { getDocsNav } from "@/lib/docs"

export const Route = createFileRoute("/ja/docs")({
  loader: () => getDocsNav({ data: "ja" }),
  staleTime: Infinity,
  component: () => <DocsLayout nav={Route.useLoaderData()} />,
})
