import { createFileRoute } from "@tanstack/react-router"

import { DocRoute, docHead, loadDocPage } from "@/components/docs/doc-page"

export const Route = createFileRoute("/ja/docs/")({
  loader: ({ parentMatchPromise }) => loadDocPage("ja", "", parentMatchPromise),
  head: ({ loaderData }) => docHead("ja", loaderData),
  component: () => <DocRoute doc={Route.useLoaderData()} />,
})
