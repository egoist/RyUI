import { createFileRoute } from "@tanstack/react-router"

import { DocRoute, docHead, loadDocPage } from "@/components/docs/doc-page"

export const Route = createFileRoute("/zh/docs/")({
  loader: ({ parentMatchPromise }) => loadDocPage("zh", "", parentMatchPromise),
  head: ({ loaderData }) => docHead("zh", loaderData),
  component: () => <DocRoute doc={Route.useLoaderData()} />,
})
