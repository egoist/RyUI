import { createFileRoute } from "@tanstack/react-router"

import { DocRoute, docHead, loadDocPage } from "@/components/docs/doc-page"

export const Route = createFileRoute("/docs/")({
  loader: ({ parentMatchPromise }) => loadDocPage("en", "", parentMatchPromise),
  head: ({ loaderData }) => docHead("en", loaderData),
  component: () => <DocRoute doc={Route.useLoaderData()} />,
})
