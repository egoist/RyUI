import { createFileRoute } from "@tanstack/react-router"

import { DocRoute, docHead, loadDocPage } from "@/components/docs/doc-page"

// Every page but the introduction, nested ones too: /docs/plugins/fetch.
export const Route = createFileRoute("/docs/$")({
  loader: ({ params, parentMatchPromise }) => loadDocPage("en", params._splat ?? "", parentMatchPromise),
  head: ({ loaderData }) => docHead("en", loaderData),
  component: () => <DocRoute doc={Route.useLoaderData()} />,
})
